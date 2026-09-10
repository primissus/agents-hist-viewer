package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"claude-code-hist-viewer/internal/adapter/sqlite"
	"claude-code-hist-viewer/internal/app"
	"claude-code-hist-viewer/internal/domain"
)

// seedSemanticSession seeds a session with two messages ("target" at seq 0,
// "distractor" at seq 1) and returns the message rowids keyed by Seq, so
// tests can assign specific vectors to specific fixture messages.
func seedSemanticSession(t *testing.T, r *sqlite.Repo, id string, vendor domain.Vendor, filePath, projectPath string) map[int]int64 {
	t.Helper()
	ctx := context.Background()
	if err := r.Init(ctx); err != nil {
		t.Fatal(err)
	}

	s := domain.Session{
		ID: id, Title: "Session " + id, FilePath: filePath, ProjectPath: projectPath,
		HasTranscript: true, StartedAt: time.Now(), EndedAt: time.Now(), Vendor: vendor,
	}
	msgs := []domain.Message{
		{UUID: id + "-u1", SessionID: id, Role: domain.RoleUser, Kind: domain.KindText, Text: "target message about homebrew taps", Sequence: 0, Timestamp: time.Now()},
		{UUID: id + "-u2", SessionID: id, Role: domain.RoleAssistant, Kind: domain.KindText, Text: "distractor message about something else", Sequence: 1, Timestamp: time.Now()},
	}
	if err := r.ReplaceSession(ctx, s, msgs); err != nil {
		t.Fatal(err)
	}
	if err := r.InitEmbeddings(ctx); err != nil {
		t.Fatal(err)
	}
	return rowidsBySeq(t, r)
}

func TestSemanticSearchOneHitPerSessionDescending(t *testing.T) {
	ctx := context.Background()
	r := openRepo(t)

	bySeq1 := seedSemanticSession(t, r, "sem1", domain.VendorClaude, "/f1.jsonl", "/proj1")
	bySeq2 := seedSemanticSession(t, r, "sem2", domain.VendorClaude, "/f2.jsonl", "/proj2")

	if err := r.PutEmbeddings(ctx, []domain.Embedding{
		{MessageRowID: bySeq1[0], SessionID: "sem1", Vector: []float32{0.9, 0.1, 0, 0}, Model: "fake-model"},
		{MessageRowID: bySeq1[1], SessionID: "sem1", Vector: []float32{0, 0, 0, 1}, Model: "fake-model"},
		{MessageRowID: bySeq2[0], SessionID: "sem2", Vector: []float32{1, 0, 0, 0}, Model: "fake-model"},
		{MessageRowID: bySeq2[1], SessionID: "sem2", Vector: []float32{0, 0, 0, 1}, Model: "fake-model"},
	}); err != nil {
		t.Fatal(err)
	}

	embedder := &fakeEmbedder{model: "fake-model", dim: 4, embedFn: func(texts []string) ([][]float32, error) {
		return [][]float32{{1, 0, 0, 0}}, nil
	}}
	svc := app.NewSemanticSearchService(embedder, r, r)

	hits, err := svc.Search(ctx, "homebrew taps", app.SemanticSearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected one hit per session (2), got %d", len(hits))
	}
	if hits[0].SessionID != "sem2" {
		t.Fatalf("expected sem2 (exact match) to rank first, got %q", hits[0].SessionID)
	}
	if hits[0].Score < hits[1].Score {
		t.Fatalf("expected descending scores, got %v then %v", hits[0].Score, hits[1].Score)
	}
	if hits[0].SessionTitle == "" || hits[0].ProjectPath == "" || hits[0].FilePath == "" {
		t.Fatalf("expected session fields populated, got %+v", hits[0])
	}
	seen := map[string]bool{}
	for _, h := range hits {
		if seen[h.SessionID] {
			t.Fatalf("expected at most one hit per session, got duplicate for %q", h.SessionID)
		}
		seen[h.SessionID] = true
	}
}

func TestSemanticSearchLimitTruncates(t *testing.T) {
	ctx := context.Background()
	r := openRepo(t)

	var embeddings []domain.Embedding
	sessionIDs := []string{"lim1", "lim2", "lim3"}
	for _, id := range sessionIDs {
		bySeq := seedSemanticSession(t, r, id, domain.VendorClaude, "/f-"+id, "/proj-"+id)
		embeddings = append(embeddings,
			domain.Embedding{MessageRowID: bySeq[0], SessionID: id, Vector: []float32{1, 0, 0, 0}, Model: "fake-model"},
			domain.Embedding{MessageRowID: bySeq[1], SessionID: id, Vector: []float32{0, 0, 0, 1}, Model: "fake-model"},
		)
	}
	if err := r.PutEmbeddings(ctx, embeddings); err != nil {
		t.Fatal(err)
	}

	embedder := &fakeEmbedder{model: "fake-model", dim: 4, embedFn: func(texts []string) ([][]float32, error) {
		return [][]float32{{1, 0, 0, 0}}, nil
	}}
	svc := app.NewSemanticSearchService(embedder, r, r)

	hits, err := svc.Search(ctx, "homebrew taps", app.SemanticSearchOpts{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected limit to truncate to 2 hits, got %d", len(hits))
	}
}

func TestSemanticSearchNoEmbeddings(t *testing.T) {
	ctx := context.Background()
	r := openRepo(t)
	if err := r.Init(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.InitEmbeddings(ctx); err != nil {
		t.Fatal(err)
	}

	embedder := &fakeEmbedder{model: "fake-model", dim: 4, embedFn: func(texts []string) ([][]float32, error) {
		return [][]float32{{1, 0, 0, 0}}, nil
	}}
	svc := app.NewSemanticSearchService(embedder, r, r)

	_, err := svc.Search(ctx, "anything", app.SemanticSearchOpts{})
	if !errors.Is(err, app.ErrNoEmbeddings) {
		t.Fatalf("expected ErrNoEmbeddings, got %v", err)
	}
}

func TestSemanticSearchVendorFilter(t *testing.T) {
	ctx := context.Background()
	r := openRepo(t)

	bySeqClaude := seedSemanticSession(t, r, "vend-claude", domain.VendorClaude, "/fc.jsonl", "/projc")
	bySeqCursor := seedSemanticSession(t, r, "vend-cursor", domain.VendorCursor, "/fu.jsonl", "/proju")

	if err := r.PutEmbeddings(ctx, []domain.Embedding{
		{MessageRowID: bySeqClaude[0], SessionID: "vend-claude", Vector: []float32{1, 0, 0, 0}, Model: "fake-model"},
		{MessageRowID: bySeqClaude[1], SessionID: "vend-claude", Vector: []float32{0, 0, 0, 1}, Model: "fake-model"},
		{MessageRowID: bySeqCursor[0], SessionID: "vend-cursor", Vector: []float32{1, 0, 0, 0}, Model: "fake-model"},
		{MessageRowID: bySeqCursor[1], SessionID: "vend-cursor", Vector: []float32{0, 0, 0, 1}, Model: "fake-model"},
	}); err != nil {
		t.Fatal(err)
	}

	embedder := &fakeEmbedder{model: "fake-model", dim: 4, embedFn: func(texts []string) ([][]float32, error) {
		return [][]float32{{1, 0, 0, 0}}, nil
	}}
	svc := app.NewSemanticSearchService(embedder, r, r)

	hits, err := svc.Search(ctx, "homebrew taps", app.SemanticSearchOpts{Vendor: domain.VendorCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected exactly one hit from cursor vendor, got %d", len(hits))
	}
	if hits[0].SessionID != "vend-cursor" {
		t.Fatalf("expected hit from vend-cursor, got %q", hits[0].SessionID)
	}
	if hits[0].Vendor != domain.VendorCursor {
		t.Fatalf("expected vendor cursor, got %q", hits[0].Vendor)
	}
}

func TestSemanticSearchEmptyQuery(t *testing.T) {
	ctx := context.Background()
	r := openRepo(t)
	if err := r.Init(ctx); err != nil {
		t.Fatal(err)
	}

	embedder := &fakeEmbedder{model: "fake-model", dim: 4}
	svc := app.NewSemanticSearchService(embedder, r, r)

	hits, err := svc.Search(ctx, "   ", app.SemanticSearchOpts{})
	if err != nil {
		t.Fatalf("expected no error for empty query, got %v", err)
	}
	if hits != nil {
		t.Fatalf("expected nil hits for empty query, got %+v", hits)
	}
}
