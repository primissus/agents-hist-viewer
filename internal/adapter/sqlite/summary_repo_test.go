package sqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"claude-code-hist-viewer/internal/adapter/sqlite"
	"claude-code-hist-viewer/internal/domain"

	_ "modernc.org/sqlite"
)

func TestSummaryRoundtripAndUpsertOverwrite(t *testing.T) {
	r := openTemp(t)
	ctx := context.Background()

	if err := r.InitSummaries(ctx); err != nil {
		t.Fatal(err)
	}

	s := makeSession("s1", "session one")
	if err := r.ReplaceSession(ctx, s, nil); err != nil {
		t.Fatal(err)
	}

	sum := domain.Summary{SessionID: "s1", Model: "chat-model", SourceHash: "hash1", Text: "first summary"}
	if err := r.PutSummary(ctx, sum); err != nil {
		t.Fatal(err)
	}

	got, ok, err := r.GetSummary(ctx, "s1", "chat-model")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected summary to be found")
	}
	if got.Text != "first summary" || got.SourceHash != "hash1" {
		t.Fatalf("unexpected summary: %+v", got)
	}

	sum2 := domain.Summary{SessionID: "s1", Model: "chat-model", SourceHash: "hash2", Text: "second summary"}
	if err := r.PutSummary(ctx, sum2); err != nil {
		t.Fatal(err)
	}
	got2, ok, err := r.GetSummary(ctx, "s1", "chat-model")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected summary to still be found after upsert")
	}
	if got2.Text != "second summary" || got2.SourceHash != "hash2" {
		t.Fatalf("expected upsert to overwrite, got: %+v", got2)
	}
}

func TestSummarySurvivesUnrelatedReplaceSession(t *testing.T) {
	r := openTemp(t)
	ctx := context.Background()

	if err := r.InitSummaries(ctx); err != nil {
		t.Fatal(err)
	}

	s1 := makeSession("s1", "session one")
	s2 := makeSession("s2", "session two")
	if err := r.ReplaceSession(ctx, s1, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.ReplaceSession(ctx, s2, nil); err != nil {
		t.Fatal(err)
	}

	sum := domain.Summary{SessionID: "s1", Model: "chat-model", SourceHash: "hash1", Text: "summary for s1"}
	if err := r.PutSummary(ctx, sum); err != nil {
		t.Fatal(err)
	}

	// Re-index (ReplaceSession) an unrelated session, s2.
	msgs := []domain.Message{makeMsg("s2", "m1", 0, "some new text")}
	if err := r.ReplaceSession(ctx, s2, msgs); err != nil {
		t.Fatal(err)
	}

	got, ok, err := r.GetSummary(ctx, "s1", "chat-model")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected s1's summary to survive an unrelated ReplaceSession for s2")
	}
	if got.Text != "summary for s1" {
		t.Fatalf("unexpected summary text: %q", got.Text)
	}
}

func TestInitSummariesSweepsOrphans(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	r, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	ctx := context.Background()
	if err := r.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}

	if err := r.InitSummaries(ctx); err != nil {
		t.Fatal(err)
	}

	s1 := makeSession("s1", "session one")
	if err := r.ReplaceSession(ctx, s1, nil); err != nil {
		t.Fatal(err)
	}
	sum := domain.Summary{SessionID: "s1", Model: "chat-model", SourceHash: "hash1", Text: "summary for s1"}
	if err := r.PutSummary(ctx, sum); err != nil {
		t.Fatal(err)
	}

	// Delete the session directly via a second connection, simulating it
	// being dropped from a re-index (there's no exported delete-only path).
	raw, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, "s1"); err != nil {
		t.Fatal(err)
	}

	if err := r.InitSummaries(ctx); err != nil {
		t.Fatal(err)
	}

	_, ok, err := r.GetSummary(ctx, "s1", "chat-model")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected orphaned summary to be swept by InitSummaries")
	}
}
