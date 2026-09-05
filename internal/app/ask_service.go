package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"claude-code-hist-viewer/internal/domain"
)

const (
	neighborRadius        = 2
	maxHitsPerSession     = 3
	defaultAskK           = 12
	askSnippetMaxRunes    = 200
	askExcerptMaxRunes    = 500
	nearestPoolMultiplier = 4
)

type AskHit struct {
	Num          int           `json:"num"`
	SessionID    string        `json:"session_id"`
	SessionTitle string        `json:"session_title"`
	Vendor       domain.Vendor `json:"vendor"`
	Timestamp    time.Time     `json:"timestamp"`
	Snippet      string        `json:"snippet"`
	Score        float64       `json:"score"`
}

type AskResult struct {
	Answer string   `json:"answer"`
	Hits   []AskHit `json:"hits"`
}

type AskOptions struct {
	K           int
	Vendor      domain.Vendor
	ProjectPath string
	Since       time.Time
	NoLLM       bool
}

type AskService struct {
	embedder domain.Embedder
	chat     domain.ChatModel
	repo     domain.EmbeddingRepository
	search   domain.SearchRepository
}

func NewAskService(e domain.Embedder, c domain.ChatModel, r domain.EmbeddingRepository, search domain.SearchRepository) *AskService {
	return &AskService{embedder: e, chat: c, repo: r, search: search}
}

// Ask embeds the question, retrieves the nearest message blocks (deduped to
// at most maxHitsPerSession per session), and — unless NoLLM or no ChatModel
// is configured — asks the chat model to answer from the retrieved excerpts.
func (s *AskService) Ask(ctx context.Context, question string, opts AskOptions) (AskResult, error) {
	k := opts.K
	if k <= 0 {
		k = defaultAskK
	}
	if err := s.repo.InitEmbeddings(ctx); err != nil {
		return AskResult{}, fmt.Errorf("init embeddings: %w", err)
	}

	vecs, err := s.embedder.Embed(ctx, []string{question})
	if err != nil {
		return AskResult{}, fmt.Errorf("embed query: %w", err)
	}
	if len(vecs) == 0 {
		return AskResult{}, fmt.Errorf("embed query: empty response")
	}

	filter := domain.EmbedFilter{Vendor: opts.Vendor, ProjectPath: opts.ProjectPath, Since: opts.Since}
	raw, err := s.repo.Nearest(ctx, s.embedder.Model(), vecs[0], k*nearestPoolMultiplier, filter)
	if err != nil {
		return AskResult{}, fmt.Errorf("nearest: %w", err)
	}
	raw = dedupeBySession(raw, maxHitsPerSession)
	if len(raw) > k {
		raw = raw[:k]
	}

	var hits []AskHit
	var excerpts []string
	for i, h := range raw {
		msg, err := s.repo.MessageByRowID(ctx, h.MessageRowID)
		if err != nil {
			continue
		}
		var title string
		var vendor domain.Vendor
		var started time.Time
		if detail, err := s.search.SessionByID(ctx, h.SessionID); err == nil {
			title, vendor, started = detail.Session.Title, detail.Session.Vendor, detail.Session.StartedAt
		}
		hits = append(hits, AskHit{
			Num: i + 1, SessionID: h.SessionID, SessionTitle: title, Vendor: vendor,
			Timestamp: started, Snippet: snippet(msg.Text, askSnippetMaxRunes), Score: h.Score,
		})

		if !opts.NoLLM {
			neighbors, err := s.repo.Neighbors(ctx, h.SessionID, msg.Sequence, neighborRadius)
			if err != nil || len(neighbors) == 0 {
				neighbors = []domain.Message{msg}
			}
			excerpts = append(excerpts, fmt.Sprintf("[%d] (%s)\n%s", i+1, title, joinMessages(neighbors)))
		}
	}

	result := AskResult{Hits: hits}
	if opts.NoLLM || s.chat == nil || len(excerpts) == 0 {
		return result, nil
	}

	system := "You answer questions about the user's own past coding-agent conversations, using only the numbered excerpts provided. Cite excerpts inline as [n]. If the excerpts don't contain the answer, say so plainly."
	user := strings.Join(excerpts, "\n\n---\n\n") + "\n\nQuestion: " + question
	answer, err := s.chat.Chat(ctx, system, user)
	if err != nil {
		return result, fmt.Errorf("chat: %w", err)
	}
	result.Answer = answer
	return result, nil
}

func dedupeBySession(hits []domain.VectorHit, maxPerSession int) []domain.VectorHit {
	counts := make(map[string]int)
	var out []domain.VectorHit
	for _, h := range hits {
		if counts[h.SessionID] >= maxPerSession {
			continue
		}
		counts[h.SessionID]++
		out = append(out, h)
	}
	return out
}

func joinMessages(msgs []domain.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		fmt.Fprintf(&b, "%s: %s\n", m.Role, snippet(m.Text, askExcerptMaxRunes))
	}
	return b.String()
}

func snippet(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "…"
}
