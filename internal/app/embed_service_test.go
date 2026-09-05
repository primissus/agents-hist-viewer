package app_test

import (
	"context"
	"testing"
	"time"

	"claude-code-hist-viewer/internal/app"
	"claude-code-hist-viewer/internal/domain"
)

// fakeEmbedder returns a fixed-length deterministic vector per input string
// (length-based, distinct enough for tests without a real model), unless
// embedFn is set to override the response entirely (e.g. for a query vector).
type fakeEmbedder struct {
	model   string
	dim     int
	calls   [][]string
	embedFn func(texts []string) ([][]float32, error)
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.calls = append(f.calls, texts)
	if f.embedFn != nil {
		return f.embedFn(texts)
	}
	vecs := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, f.dim)
		for j := range v {
			v[j] = float32(len(t) + j)
		}
		vecs[i] = v
	}
	return vecs, nil
}
func (f *fakeEmbedder) Model() string { return f.model }
func (f *fakeEmbedder) Dim() int      { return f.dim }

func newFakeEmbedder() *fakeEmbedder { return &fakeEmbedder{model: "fake-model", dim: 4} }

func TestEmbedServiceSkipsShortAndNonBashUnits(t *testing.T) {
	ctx := context.Background()
	r := openRepo(t)

	if err := r.Init(ctx); err != nil {
		t.Fatal(err)
	}
	s := domain.Session{ID: "s1", Title: "t", HasTranscript: true, StartedAt: time.Now(), EndedAt: time.Now(), Vendor: domain.VendorClaude}
	msgs := []domain.Message{
		{UUID: "u1", SessionID: "s1", Role: domain.RoleUser, Kind: domain.KindText, Text: "a real user question here", Sequence: 0},
		{UUID: "u2", SessionID: "s1", Role: domain.RoleUser, Kind: domain.KindText, Text: "short", Sequence: 1},
		{UUID: "u3", SessionID: "s1", Role: domain.RoleAssistant, Kind: domain.KindToolUse, ToolName: "Read",
			Text: `Read {"file_path":"/tmp/x"}`, Sequence: 2},
		{UUID: "u4", SessionID: "s1", Role: domain.RoleAssistant, Kind: domain.KindToolUse, ToolName: "Bash",
			Text: `Bash {"command":"go build ./..."}`, Sequence: 3},
	}
	if err := r.ReplaceSession(ctx, s, msgs); err != nil {
		t.Fatal(err)
	}

	embedder := newFakeEmbedder()
	svc := app.NewEmbedService(embedder, r)
	stats, err := svc.Run(ctx, app.EmbedOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Embedded != 2 {
		t.Fatalf("expected 2 embedded (u1 + Bash u4), got %d (skipped=%d)", stats.Embedded, stats.Skipped)
	}
	// The non-Bash tool_use (u3) is excluded at the SQL level (embeddableWhere)
	// and never reaches the service, so only the too-short text (u2) counts here.
	if stats.Skipped != 1 {
		t.Fatalf("expected 1 skipped (short text), got %d", stats.Skipped)
	}

	items, err := r.EmbeddingsWithContext(ctx, "fake-model", domain.EmbedFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 stored embeddings, got %d", len(items))
	}
}

func TestEmbedServiceIsResumable(t *testing.T) {
	ctx := context.Background()
	r := openRepo(t)

	if err := r.Init(ctx); err != nil {
		t.Fatal(err)
	}
	s := domain.Session{ID: "s1", Title: "t", HasTranscript: true, StartedAt: time.Now(), EndedAt: time.Now(), Vendor: domain.VendorClaude}
	msgs := []domain.Message{
		{UUID: "u1", SessionID: "s1", Role: domain.RoleUser, Kind: domain.KindText, Text: "a real user question here", Sequence: 0},
	}
	if err := r.ReplaceSession(ctx, s, msgs); err != nil {
		t.Fatal(err)
	}

	embedder := newFakeEmbedder()
	svc := app.NewEmbedService(embedder, r)
	if _, err := svc.Run(ctx, app.EmbedOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	// Second run should find nothing new to embed.
	stats, err := svc.Run(ctx, app.EmbedOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Embedded != 0 {
		t.Fatalf("expected 0 newly embedded on rerun, got %d", stats.Embedded)
	}

	// Force re-embeds regardless of existing state.
	stats, err = svc.Run(ctx, app.EmbedOptions{Force: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Embedded != 1 {
		t.Fatalf("expected 1 re-embedded with Force, got %d", stats.Embedded)
	}
}
