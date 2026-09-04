package app

import (
	"context"
	"strings"

	"claude-code-hist-viewer/internal/domain"
)

const defaultSearchLimit = 20

type SearchService struct {
	repo domain.SearchRepository
}

func NewSearchService(repo domain.SearchRepository) *SearchService {
	return &SearchService{repo: repo}
}

func (s *SearchService) Search(ctx context.Context, query string, limit int, opts domain.SearchOpts) ([]domain.SearchHit, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	compiled, err := domain.CompileSearchQuery(q, opts)
	if err != nil {
		return nil, err
	}
	if compiled == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	return s.repo.Search(ctx, compiled, limit)
}

func (s *SearchService) Session(ctx context.Context, id string) (domain.SessionDetail, error) {
	return s.repo.SessionByID(ctx, id)
}

func (s *SearchService) RecentSessions(ctx context.Context, q domain.RecentQuery) ([]domain.SearchHit, error) {
	if q.Limit <= 0 {
		q.Limit = defaultSearchLimit
	}
	return s.repo.RecentSessions(ctx, q)
}
