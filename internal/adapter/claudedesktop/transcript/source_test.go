package transcript_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	desktoptranscript "claude-code-hist-viewer/internal/adapter/claudedesktop/transcript"
	"claude-code-hist-viewer/internal/domain"
)

func TestSessionsUseDesktopMetadataAndLinkedTranscript(t *testing.T) {
	root, claudeRoot := writeDesktopFixture(t, "sess-desktop", "Desktop Title")
	src := desktoptranscript.NewSource(root, claudeRoot, domain.DefaultIndexOptions)

	sessions, err := src.Sessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	sess := sessions[0]
	if sess.ID != "sess-desktop" {
		t.Fatalf("session id = %q, want linked cliSessionId", sess.ID)
	}
	if sess.Title != "Desktop Title" {
		t.Fatalf("title = %q, want Desktop Title", sess.Title)
	}
	if sess.ProjectPath != "/tmp/desktop-project" {
		t.Fatalf("project = %q", sess.ProjectPath)
	}
	if sess.Vendor != domain.VendorClaudeDesktop {
		t.Fatalf("vendor = %q, want claude-desktop", sess.Vendor)
	}
	if !sess.HasTranscript || sess.FilePath == "" {
		t.Fatalf("expected linked transcript, got %+v", sess)
	}
	if sess.StartedAt != time.UnixMilli(1770000000000).UTC() {
		t.Fatalf("started = %s", sess.StartedAt)
	}
}

func TestMessagesDelegateToClaudeTranscript(t *testing.T) {
	root, claudeRoot := writeDesktopFixture(t, "sess-messages", "Message Title")
	src := desktoptranscript.NewSource(root, claudeRoot, domain.DefaultIndexOptions)

	var msgs []domain.Message
	if err := src.Messages(context.Background(), "sess-messages", func(m domain.Message) error {
		msgs = append(msgs, m)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected messages")
	}
	if msgs[0].SessionID != "sess-messages" {
		t.Fatalf("message session = %q", msgs[0].SessionID)
	}
	if !strings.Contains(msgs[0].Text, "desktop searchable phrase") {
		t.Fatalf("message text = %q", msgs[0].Text)
	}
}

func TestTranscriptFingerprintIncludesDesktopMetadata(t *testing.T) {
	root, claudeRoot := writeDesktopFixture(t, "sess-fingerprint", "Before")
	src := desktoptranscript.NewSource(root, claudeRoot, domain.DefaultIndexOptions)

	fp1, err := src.TranscriptFingerprint(context.Background(), "sess-fingerprint")
	if err != nil {
		t.Fatal(err)
	}

	localPath := filepath.Join(root, "claude-code-sessions", "org", "account", "local_sess-fingerprint.json")
	data, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "Before", "After", 1))
	if err := os.WriteFile(localPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	fp2, err := src.TranscriptFingerprint(context.Background(), "sess-fingerprint")
	if err != nil {
		t.Fatal(err)
	}
	if fp1.Hash == fp2.Hash {
		t.Fatal("expected metadata change to affect fingerprint")
	}
	if fp1.Path != localPath {
		t.Fatalf("fingerprint path = %q, want local metadata path", fp1.Path)
	}
}

func writeDesktopFixture(t *testing.T, sessionID, title string) (string, string) {
	t.Helper()
	root := t.TempDir()
	claudeRoot := filepath.Join(t.TempDir(), "projects")

	projDir := filepath.Join(claudeRoot, "proj")
	if err := os.MkdirAll(projDir, 0755); err != nil {
		t.Fatal(err)
	}
	jsonl := `{"type":"user","sessionId":"` + sessionID + `","timestamp":"2026-01-02T03:04:05Z","cwd":"/tmp/from-transcript","message":{"role":"user","content":"desktop searchable phrase"}}` + "\n"
	if err := os.WriteFile(filepath.Join(projDir, sessionID+".jsonl"), []byte(jsonl), 0644); err != nil {
		t.Fatal(err)
	}

	localDir := filepath.Join(root, "claude-code-sessions", "org", "account")
	if err := os.MkdirAll(localDir, 0755); err != nil {
		t.Fatal(err)
	}
	local := `{
		"sessionId":"local-` + sessionID + `",
		"cliSessionId":"` + sessionID + `",
		"cwd":"/tmp/desktop-project",
		"title":"` + title + `",
		"createdAt":1770000000000,
		"lastActivityAt":1770000060000
	}`
	if err := os.WriteFile(filepath.Join(localDir, "local_"+sessionID+".json"), []byte(local), 0644); err != nil {
		t.Fatal(err)
	}
	return root, claudeRoot
}
