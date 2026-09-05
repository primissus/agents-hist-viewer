package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"claude-code-hist-viewer/internal/domain"
)

const (
	minEmbedTextRunes     = 15
	assistantTextMaxRunes = 2000
	defaultEmbedBatch     = 32
	unembeddedFetchFactor = 4
)

type EmbedStats struct {
	Embedded int
	Skipped  int
}

type EmbedOptions struct {
	Vendor      domain.Vendor
	ProjectPath string
	Force       bool
	Limit       int
	BatchSize   int
}

type EmbedService struct {
	embedder domain.Embedder
	repo     domain.EmbeddingRepository
}

func NewEmbedService(e domain.Embedder, r domain.EmbeddingRepository) *EmbedService {
	return &EmbedService{embedder: e, repo: r}
}

// Run embeds all not-yet-embedded eligible message blocks (see
// sqlite.embeddableWhere / cleanEmbedUnit) and is resumable: interrupting it
// only loses the in-flight batch, since PutEmbeddings commits per batch.
func (s *EmbedService) Run(ctx context.Context, opts EmbedOptions, progress func(processed int)) (EmbedStats, error) {
	if err := s.repo.InitEmbeddings(ctx); err != nil {
		return EmbedStats{}, fmt.Errorf("init embeddings: %w", err)
	}
	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = defaultEmbedBatch
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 1 << 30
	}

	model := s.embedder.Model()
	filter := domain.EmbedFilter{Vendor: opts.Vendor, ProjectPath: opts.ProjectPath, Force: opts.Force}

	var stats EmbedStats
	for {
		remaining := limit - stats.Embedded - stats.Skipped
		if remaining <= 0 {
			break
		}
		fetch := min(remaining, batchSize*unembeddedFetchFactor)

		units, err := s.repo.UnembeddedUnits(ctx, model, filter, fetch)
		if err != nil {
			return stats, fmt.Errorf("unembedded units: %w", err)
		}
		if len(units) == 0 {
			break
		}
		// Advance the cursor unconditionally so units skipped below (too
		// short, non-Bash tool_use, ...) don't get re-fetched forever —
		// they never reach PutEmbeddings, so the LEFT JOIN NULL check alone
		// would keep returning them on every iteration.
		filter.AfterRowID = units[len(units)-1].MessageRowID

		var texts []string
		var kept []domain.EmbedUnit
		for _, u := range units {
			text, ok := cleanEmbedUnit(u)
			if !ok {
				stats.Skipped++
				continue
			}
			texts = append(texts, text)
			kept = append(kept, u)
		}

		for i := 0; i < len(texts); i += batchSize {
			end := min(i+batchSize, len(texts))
			vectors, err := s.embedder.Embed(ctx, texts[i:end])
			if err != nil {
				return stats, fmt.Errorf("embed: %w", err)
			}
			batch := make([]domain.Embedding, len(vectors))
			for j, v := range vectors {
				batch[j] = domain.Embedding{
					MessageRowID: kept[i+j].MessageRowID,
					SessionID:    kept[i+j].SessionID,
					Vector:       v,
					Model:        model,
				}
			}
			if err := s.repo.PutEmbeddings(ctx, batch); err != nil {
				return stats, fmt.Errorf("put embeddings: %w", err)
			}
			stats.Embedded += len(batch)
			if progress != nil {
				progress(stats.Embedded + stats.Skipped)
			}
		}
	}
	return stats, nil
}

// cleanEmbedUnit applies the per-kind text-preparation rules from the plan's
// "Embed unit selection" spec and reports whether the unit should be embedded.
func cleanEmbedUnit(u domain.EmbedUnit) (string, bool) {
	switch u.Kind {
	case domain.KindText:
		text := domain.CleanText(u.Text)
		if utf8.RuneCountInString(text) < minEmbedTextRunes {
			return "", false
		}
		if u.Role == domain.RoleAssistant {
			text = truncateRunesApp(text, assistantTextMaxRunes)
		}
		return text, true
	case domain.KindToolUse:
		if u.ToolName != "Bash" {
			return "", false
		}
		cmd := strings.TrimSpace(bashCommandFromText(u.Text))
		if cmd == "" {
			return "", false
		}
		return cmd, true
	default:
		return "", false
	}
}

// bashCommandFromText extracts the "command" field from a Bash tool_use block
// stored as `"<name> <json-input>"` (see adapter/transcript/parse.go:blockToRaw).
func bashCommandFromText(text string) string {
	_, jsonPart, ok := strings.Cut(text, " ")
	if !ok {
		return ""
	}
	var input struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(jsonPart), &input); err != nil {
		return ""
	}
	return input.Command
}

func truncateRunesApp(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max])
}
