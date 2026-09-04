package transcript_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"claude-code-hist-viewer/internal/adapter/cursor/transcript"
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
