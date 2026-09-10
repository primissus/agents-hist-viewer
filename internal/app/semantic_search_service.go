package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"claude-code-hist-viewer/internal/domain"
)

// ErrNoEmbeddings is returned when a semantic search finds no embedded
// vectors to query against.
var ErrNoEmbeddings = errors.New("no embeddings found — run `chv embed` first")

// semanticPerSession caps semantic search to one hit per session.
const semanticPerSession = 1

type SemanticSearchOpts struct {
	Limit       int
	Vendor      domain.Vendor
	ProjectPath string
	Since       time.Time
}

type SemanticSearchService struct {
	embedder domain.Embedder
	repo     domain.EmbeddingRepository
	search   domain.SearchRepository
}

func NewSemanticSearchService(e domain.Embedder, r domain.EmbeddingRepository, s domain.SearchRepository) *SemanticSearchService {
	return &SemanticSearchService{embedder: e, repo: r, search: s}
}

// Search embeds query and returns the nearest message per session, ranked by
// cosine similarity descending.
func (s *SemanticSearchService) Search(ctx context.Context, query string, opts SemanticSearchOpts) ([]domain.SearchHit, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}

	k := opts.Limit
	if k <= 0 {
		k = defaultSearchLimit
	}

	if err := s.repo.InitEmbeddings(ctx); err != nil {
		return nil, fmt.Errorf("init embeddings: %w", err)
	}

	filter := domain.EmbedFilter{Vendor: opts.Vendor, ProjectPath: opts.ProjectPath, Since: opts.Since}
	raw, err := nearestUnits(ctx, s.embedder, s.repo, q, k, semanticPerSession, filter)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w (model %q)", ErrNoEmbeddings, s.embedder.Model())
	}

	sessionIDs := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, h := range raw {
		if !seen[h.SessionID] {
			seen[h.SessionID] = true
			sessionIDs = append(sessionIDs, h.SessionID)
		}
	}
	sessions, err := s.search.SessionsByIDs(ctx, sessionIDs)
	if err != nil {
		sessions = nil
	}

	var hits []domain.SearchHit
	for _, h := range raw {
		msg, err := s.repo.MessageByRowID(ctx, h.MessageRowID)
		if err != nil {
			continue
		}
		sess, ok := sessions[h.SessionID]
		hit := domain.SearchHit{
			SessionID:   h.SessionID,
			MessageUUID: msg.UUID,
			Role:        msg.Role,
			Kind:        msg.Kind,
			Timestamp:   msg.Timestamp,
			Snippet:     snippet(domain.CleanText(msg.Text), askSnippetMaxRunes),
			Score:       h.Score,
		}
		if ok {
			hit.SessionTitle = sess.Title
			hit.ProjectPath = sess.ProjectPath
			hit.FilePath = sess.FilePath
			hit.StartedAt = sess.StartedAt
			hit.HasTranscript = sess.HasTranscript
			hit.RecordKind = sess.RecordKind
			hit.Vendor = sess.Vendor
		}
		hits = append(hits, hit)
	}

	return hits, nil
}
