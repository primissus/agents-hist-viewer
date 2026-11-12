package transcript_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"claude-code-hist-viewer/internal/adapter/cursor/transcript"
	"claude-code-hist-viewer/internal/adapter/sqlite"
	"claude-code-hist-viewer/internal/app"
	"claude-code-hist-viewer/internal/config"
	"claude-code-hist-viewer/internal/domain"
)

func TestSessionID(t *testing.T) {
	got := transcript.SessionID("abc-123")
	if got != "cursor:abc-123" {
		t.Fatalf("got %q", got)
	}
}

func TestSessionsFromTranscript(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "Users-user1-src-demo")
	if err := os.MkdirAll(filepath.Join(proj, "agent-transcripts", "chat1"), 0755); err != nil {
		t.Fatal(err)
	}
	jsonl := filepath.Join(proj, "agent-transcripts", "chat1", "chat1.jsonl")
	content := `{"role":"user","message":{"content":[{"type":"text","text":"hello cursor unique phrase"}]}}
{"role":"assistant","message":{"content":[{"type":"text","text":"reply here"}]}}`
	if err := os.WriteFile(jsonl, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	src := transcript.NewSource(root, domain.DefaultIndexOptions)
	sessions, err := src.Sessions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	s := sessions[0]
	if s.Vendor != domain.VendorCursor {
		t.Errorf("vendor: %q", s.Vendor)
	}
	if s.ProjectPath != "/Users/user1/src/demo" {
		t.Errorf("project path: %q", s.ProjectPath)
	}
	if s.Title == "" {
		t.Error("expected title from first user message")
	}

	var msgs []domain.Message
	if err := src.Messages(t.Context(), s.ID, func(m domain.Message) error {
		msgs = append(msgs, m)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(msgs) < 2 {
		t.Fatalf("expected >=2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != domain.RoleUser {
		t.Errorf("first role: %q", msgs[0].Role)
	}
	if msgs[0].Timestamp.IsZero() {
		t.Error("expected synthesized timestamp")
	}
	_ = time.Now()
}

func TestSessionsDeriveTitleFromTaggedPrompt(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "Users-user1-src-demo")
	if err := os.MkdirAll(filepath.Join(proj, "agent-transcripts", "chat2"), 0755); err != nil {
		t.Fatal(err)
	}
	jsonl := filepath.Join(proj, "agent-transcripts", "chat2", "chat2.jsonl")
	content := "{\"role\":\"user\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"<user_query>\\nCan you show the date and hour\"}]}}\n"
	if err := os.WriteFile(jsonl, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	src := transcript.NewSource(root, domain.DefaultIndexOptions)
	sessions, err := src.Sessions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if got, want := sessions[0].Title, "Can you show the date and hour"; got != want {
		t.Fatalf("title = %q, want %q", got, want)
	}
}

func TestTranscriptCatalogMatchesFingerprintAndMeta(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "Users-user1-src-demo")
	if err := os.MkdirAll(filepath.Join(proj, "agent-transcripts", "chat-cat"), 0755); err != nil {
		t.Fatal(err)
	}
	jsonl := filepath.Join(proj, "agent-transcripts", "chat-cat", "chat-cat.jsonl")
	content := `{"role":"user","message":{"content":[{"type":"text","text":"catalog cursor phrase"}]}}
{"role":"assistant","message":{"content":[{"type":"text","text":"reply here"}]}}`
	if err := os.WriteFile(jsonl, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	src := transcript.NewSource(root, domain.DefaultIndexOptions)
	entries, err := src.TranscriptCatalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("catalog entries = %d, want 1", len(entries))
	}
	if entries[0].ID != "cursor:chat-cat" {
		t.Fatalf("entry id = %q", entries[0].ID)
	}
	if entries[0].Vendor != domain.VendorCursor {
		t.Fatalf("entry vendor = %q", entries[0].Vendor)
	}
	fp, err := src.TranscriptFingerprint(t.Context(), entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Fingerprint != fp {
		t.Fatalf("catalog fingerprint %+v != TranscriptFingerprint %+v", entries[0].Fingerprint, fp)
	}

	sessions, err := src.Sessions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	meta, err := src.SessionMeta(t.Context(), "cursor:chat-cat")
	if err != nil {
		t.Fatal(err)
	}
	if meta.ID != sessions[0].ID || meta.Title != sessions[0].Title || meta.ProjectPath != sessions[0].ProjectPath || meta.StartedAt != sessions[0].StartedAt || meta.MessageCount != sessions[0].MessageCount {
		t.Fatalf("session meta %+v != discovered session %+v", meta, sessions[0])
	}
}

type countingCursorSource struct {
	*transcript.Source
	loadCalls int
}

func (c *countingCursorSource) LoadSession(ctx context.Context, id string) (domain.Session, []domain.Message, error) {
	c.loadCalls++
	return c.Source.LoadSession(ctx, id)
}

func TestCatalogSkipsUnchangedCursorSessions(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "Users-user1-src-demo")
	if err := os.MkdirAll(filepath.Join(proj, "agent-transcripts", "chat-skip"), 0755); err != nil {
		t.Fatal(err)
	}
	jsonl := filepath.Join(proj, "agent-transcripts", "chat-skip", "chat-skip.jsonl")
	content := `{"role":"user","message":{"content":[{"type":"text","text":"cursor skip phrase"}]}}
{"role":"assistant","message":{"content":[{"type":"text","text":"reply here"}]}}`
	if err := os.WriteFile(jsonl, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tracked := &countingCursorSource{Source: transcript.NewSource(root, domain.DefaultIndexOptions)}
	repo, err := sqlite.Open(filepath.Join(t.TempDir(), "cursor.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repo.Close() })
	svc := app.NewIndexService(tracked, nil, nil, repo, domain.DefaultShrinkConfig)
	home := t.TempDir()

	stats1, err := svc.Run(t.Context(), config.IndexConfig{}, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats1.Indexed != 1 || stats1.Messages == 0 {
		t.Fatalf("first run should index, got %+v", stats1)
	}
	loads1 := tracked.loadCalls

	stats2, err := svc.Run(t.Context(), config.IndexConfig{}, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats2.Skipped != 1 || stats2.Indexed != 0 {
		t.Fatalf("second run should skip, got %+v", stats2)
	}
	if tracked.loadCalls != loads1 {
		t.Fatalf("unchanged session parsed again: loads %d->%d", loads1, tracked.loadCalls)
	}

	if err := os.WriteFile(jsonl, []byte(content+`{"role":"user","message":{"content":[{"type":"text","text":"changed cursor text"}]}}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	stats3, err := svc.Run(t.Context(), config.IndexConfig{}, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats3.Indexed != 1 {
		t.Fatalf("changed fingerprint should re-index, got %+v", stats3)
	}

	stats4, err := svc.RunWithOptions(t.Context(), config.IndexConfig{}, home, nil, app.IndexRunOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats4.Indexed != 1 {
		t.Fatalf("force should re-index, got %+v", stats4)
	}
}

func TestLoadSessionMatchesLoadFile(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "Users-user1-src-demo")
	if err := os.MkdirAll(filepath.Join(proj, "agent-transcripts", "chat-load", "subagents"), 0755); err != nil {
		t.Fatal(err)
	}
	jsonl := filepath.Join(proj, "agent-transcripts", "chat-load", "chat-load.jsonl")
	content := `{"role":"user","message":{"content":[{"type":"text","text":"hello cursor load session"}]}}
{"role":"assistant","message":{"content":[{"type":"text","text":"reply here"}]}}`
	if err := os.WriteFile(jsonl, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	sidechain := filepath.Join(proj, "agent-transcripts", "chat-load", "subagents", "agent.jsonl")
	if err := os.WriteFile(sidechain, []byte(`{"role":"assistant","message":{"content":[{"type":"text","text":"side reply"}]}}`), 0644); err != nil {
		t.Fatal(err)
	}

	src := transcript.NewSource(root, domain.DefaultIndexOptions)
	detail, err := transcript.LoadFile(t.Context(), jsonl, domain.DefaultIndexOptions)
	if err != nil {
		t.Fatal(err)
	}
	sess, msgs, err := src.LoadSession(t.Context(), "cursor:chat-load")
	if err != nil {
		t.Fatal(err)
	}

	if sess.ID != detail.Session.ID || sess.Title != detail.Session.Title ||
		sess.ProjectPath != detail.Session.ProjectPath || sess.Vendor != detail.Session.Vendor ||
		sess.MessageCount != detail.Session.MessageCount {
		t.Fatalf("LoadSession session %+v != LoadFile session %+v", sess, detail.Session)
	}
	if len(msgs) != len(detail.Messages) {
		t.Fatalf("messages = %d, want %d", len(msgs), len(detail.Messages))
	}
	var sidechainFound bool
	for i := range msgs {
		if msgs[i].UUID != detail.Messages[i].UUID || msgs[i].Text != detail.Messages[i].Text ||
			msgs[i].IsSidechain != detail.Messages[i].IsSidechain {
			t.Fatalf("message %d differs: %+v vs %+v", i, msgs[i], detail.Messages[i])
		}
		if msgs[i].IsSidechain {
			sidechainFound = true
		}
	}
	if !sidechainFound {
		t.Fatal("expected sidechain message from LoadSession")
	}
}

func TestLoadFileBuildsSessionDetail(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "Users-user1-src-demo")
	if err := os.MkdirAll(filepath.Join(proj, "agent-transcripts", "chat3", "subagents"), 0755); err != nil {
		t.Fatal(err)
	}
	jsonl := filepath.Join(proj, "agent-transcripts", "chat3", "chat3.jsonl")
	content := `{"role":"user","message":{"content":[{"type":"text","text":"hello cursor unique phrase"}]}}
{"role":"assistant","message":{"content":[{"type":"text","text":"reply here"}]}}`
	if err := os.WriteFile(jsonl, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	sidechain := filepath.Join(proj, "agent-transcripts", "chat3", "subagents", "agent.jsonl")
	if err := os.WriteFile(sidechain, []byte(`{"role":"assistant","message":{"content":[{"type":"text","text":"side reply"}]}}`), 0644); err != nil {
		t.Fatal(err)
	}

	detail, err := transcript.LoadFile(t.Context(), jsonl, domain.DefaultIndexOptions)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Session.ID != "cursor:chat3" {
		t.Fatalf("session id = %q, want cursor:chat3", detail.Session.ID)
	}
	if detail.Session.ProjectPath != "/Users/user1/src/demo" {
		t.Fatalf("project path = %q", detail.Session.ProjectPath)
	}
	if detail.Session.Title != "hello cursor unique phrase" {
		t.Fatalf("title = %q", detail.Session.Title)
	}
	if detail.Session.Vendor != domain.VendorCursor {
		t.Fatalf("vendor = %q, want cursor", detail.Session.Vendor)
	}
	if detail.Session.MessageCount != len(detail.Messages) {
		t.Fatalf("message count = %d, messages = %d", detail.Session.MessageCount, len(detail.Messages))
	}
	var sidechainFound bool
	for _, m := range detail.Messages {
		if m.ProjectPath != "/Users/user1/src/demo" {
			t.Fatalf("message project path = %q", m.ProjectPath)
		}
		if m.Timestamp.IsZero() {
			t.Fatal("expected synthesized timestamp")
		}
		if m.IsSidechain {
			sidechainFound = true
		}
	}
	if !sidechainFound {
		t.Fatal("expected sidechain message")
	}
}
