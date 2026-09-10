package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"claude-code-hist-viewer/internal/adapter/sqlite"
	"claude-code-hist-viewer/internal/app"
	"claude-code-hist-viewer/internal/domain"
)

// rowidsBySeq indexes a session's not-yet-embedded units by Seq, so tests can
// assign specific vectors to specific fixture messages without depending on
// message UUIDs (EmbedUnit only carries Seq, not UUID).
func rowidsBySeq(t *testing.T, r *sqlite.Repo) map[int]int64 {
	t.Helper()
	units, err := r.UnembeddedUnits(context.Background(), "fake-model", domain.EmbedFilter{Force: true}, 100)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[int]int64, len(units))
	for _, u := range units {
		out[u.Seq] = u.MessageRowID
	}
	return out
}

func seedAskSession(t *testing.T, r *sqlite.Repo) map[int]int64 {
	ctx := context.Background()
	if err := r.Init(ctx); err != nil {
		t.Fatal(err)
	}

	s := domain.Session{ID: "s1", Title: "Homebrew setup", HasTranscript: true,
		StartedAt: time.Now(), EndedAt: time.Now(), Vendor: domain.VendorClaude}
	msgs := []domain.Message{
		{UUID: "u1", SessionID: "s1", Role: domain.RoleUser, Kind: domain.KindText, Text: "how do I set up a homebrew tap for my project", Sequence: 0},
		{UUID: "u2", SessionID: "s1", Role: domain.RoleAssistant, Kind: domain.KindText, Text: "you create a tap repo and a formula file", Sequence: 1},
		{UUID: "u3", SessionID: "s1", Role: domain.RoleUser, Kind: domain.KindText, Text: "totally unrelated question about pizza toppings", Sequence: 2},
	}
	if err := r.ReplaceSession(ctx, s, msgs); err != nil {
		t.Fatal(err)
	}
	if err := r.InitEmbeddings(ctx); err != nil {
		t.Fatal(err)
	}
	return rowidsBySeq(t, r)
}

func TestAskServiceRetrievesAndAnswers(t *testing.T) {
	ctx := context.Background()
	r := openRepo(t)
	bySeq := seedAskSession(t, r)

	// Homebrew question (seq 0) and its answer (seq 1) get vectors close to
	// the query; the unrelated pizza question (seq 2) gets a distant one.
	if err := r.PutEmbeddings(ctx, []domain.Embedding{
		{MessageRowID: bySeq[0], SessionID: "s1", Vector: []float32{1, 0, 0, 0}, Model: "fake-model"},
		{MessageRowID: bySeq[1], SessionID: "s1", Vector: []float32{0.9, 0.1, 0, 0}, Model: "fake-model"},
		{MessageRowID: bySeq[2], SessionID: "s1", Vector: []float32{0, 0, 0, 1}, Model: "fake-model"},
	}); err != nil {
		t.Fatal(err)
	}

	embedder := &fakeEmbedder{model: "fake-model", dim: 4, embedFn: func(texts []string) ([][]float32, error) {
		return [][]float32{{1, 0, 0, 0}}, nil
	}}
	chat := &fakeChatModel{answer: "you create a tap and a formula [1]"}
	svc := app.NewAskService(embedder, chat, r, r)

	result, err := svc.Ask(ctx, "how did I set up homebrew", app.AskOptions{K: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hits) == 0 {
		t.Fatal("expected at least one hit")
	}
	if result.Hits[0].SessionID != "s1" {
		t.Fatalf("expected top hit from s1, got %q", result.Hits[0].SessionID)
	}
	if result.Answer != "you create a tap and a formula [1]" {
		t.Fatalf("unexpected answer: %q", result.Answer)
	}
	if !strings.Contains(chat.lastUser, "[1]") {
		t.Fatalf("expected excerpt numbering in chat prompt, got: %q", chat.lastUser)
	}
}

func TestAskServiceNoLLMSkipsChat(t *testing.T) {
	ctx := context.Background()
	r := openRepo(t)
	bySeq := seedAskSession(t, r)

	if err := r.PutEmbeddings(ctx, []domain.Embedding{
		{MessageRowID: bySeq[0], SessionID: "s1", Vector: []float32{1, 0, 0, 0}, Model: "fake-model"},
	}); err != nil {
		t.Fatal(err)
	}

	embedder := &fakeEmbedder{model: "fake-model", dim: 4, embedFn: func(texts []string) ([][]float32, error) {
		return [][]float32{{1, 0, 0, 0}}, nil
	}}
	chat := &fakeChatModel{answer: "should not be called"}
	svc := app.NewAskService(embedder, chat, r, r)

	result, err := svc.Ask(ctx, "question", app.AskOptions{NoLLM: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Answer != "" {
		t.Fatalf("expected no answer in --no-llm mode, got %q", result.Answer)
	}
	if chat.lastUser != "" {
		t.Fatal("chat model should not have been called in --no-llm mode")
	}
}

type fakeChatModel struct {
	lastSystem, lastUser string
	answer               string
	err                  error
	calls                int
}

func (f *fakeChatModel) Chat(_ context.Context, system, user string) (string, error) {
	f.calls++
	f.lastSystem, f.lastUser = system, user
	if f.err != nil {
		return "", f.err
	}
	return f.answer, nil
}
