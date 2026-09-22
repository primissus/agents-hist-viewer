package transcript_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"claude-code-hist-viewer/internal/adapter/transcript"
	"claude-code-hist-viewer/internal/domain"
)

func TestTranscriptCatalogMatchesFingerprint(t *testing.T) {
	root := writeClaudeFixture(t, "sess-catalog")

	src := transcript.NewSource(root, domain.DefaultIndexOptions)
	entries, err := src.TranscriptCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("catalog entries = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.ID != "sess-catalog" {
		t.Fatalf("entry id = %q", entry.ID)
	}
	if entry.Vendor != domain.VendorClaude {
		t.Fatalf("entry vendor = %q", entry.Vendor)
	}

	fp, err := src.TranscriptFingerprint(context.Background(), entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Fingerprint != fp {
		t.Fatalf("catalog fingerprint %+v != TranscriptFingerprint %+v", entry.Fingerprint, fp)
	}
}

func TestTranscriptCatalogFingerprintTracksSidechain(t *testing.T) {
	root := writeClaudeFixture(t, "sess-sidechain")
	src := transcript.NewSource(root, domain.DefaultIndexOptions)

	before, err := src.TranscriptCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 {
		t.Fatalf("catalog entries = %d, want 1", len(before))
	}

	scDir := filepath.Join(root, "proj", "sess-sidechain", "subagents")
	if err := os.MkdirAll(scDir, 0755); err != nil {
		t.Fatal(err)
	}
	scPath := filepath.Join(scDir, "agent-a.jsonl")
	if err := os.WriteFile(scPath, []byte(`{"type":"user","sessionId":"sess-sidechain","isSidechain":true,"timestamp":"2026-01-02T03:04:07Z","message":{"role":"user","content":"sidechain text"}}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	after, err := src.TranscriptCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if before[0].Fingerprint.Hash == after[0].Fingerprint.Hash {
		t.Fatal("expected sidechain file to change the catalog fingerprint")
	}
}

func TestSessionMetaMatchesSessions(t *testing.T) {
	root := writeClaudeFixture(t, "sess-meta")
	src := transcript.NewSource(root, domain.DefaultIndexOptions)

	sessions, err := src.Sessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}

	meta, err := src.SessionMeta(context.Background(), "sess-meta")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(meta, sessions[0]) {
		t.Fatalf("session meta %+v != discovered session %+v", meta, sessions[0])
	}
}

func TestMessagesAfterCatalogUsesCachedPath(t *testing.T) {
	root := writeClaudeFixture(t, "sess-msg")
	src := transcript.NewSource(root, domain.DefaultIndexOptions)

	if _, err := src.TranscriptCatalog(context.Background()); err != nil {
		t.Fatal(err)
	}
	var msgs []domain.Message
	if err := src.Messages(context.Background(), "sess-msg", func(m domain.Message) error {
		msgs = append(msgs, m)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want 2", len(msgs))
	}
}

func writeClaudeFixture(t *testing.T, sessionID string) string {
	t.Helper()
	root := t.TempDir()
	projDir := filepath.Join(root, "proj")
	if err := os.MkdirAll(projDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := `{"type":"custom-title","sessionId":"` + sessionID + `","customTitle":"Fixture Title"}
{"type":"user","sessionId":"` + sessionID + `","timestamp":"2026-01-02T03:04:05Z","cwd":"/Users/user1/src/demo","message":{"role":"user","content":"hello claude fixture phrase"}}
{"type":"assistant","sessionId":"` + sessionID + `","timestamp":"2026-01-02T03:04:06Z","cwd":"/Users/user1/src/demo","message":{"role":"assistant","content":[{"type":"text","text":"reply here"}]}}
`
	if err := os.WriteFile(filepath.Join(projDir, sessionID+".jsonl"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return root
}
