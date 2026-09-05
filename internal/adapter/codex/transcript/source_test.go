package transcript_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-code-hist-viewer/internal/adapter/codex/transcript"
	"claude-code-hist-viewer/internal/domain"
)

func writeRollout(t *testing.T, root, dir, name, content string) string {
	t.Helper()
	full := filepath.Join(root, dir)
	if err := os.MkdirAll(full, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(full, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

const sampleRollout = `{"timestamp":"2026-09-01T10:00:00.000Z","type":"session_meta","payload":{"id":"thread_abc123","cwd":"/Users/me/src/app","git":{"branch":"main","sha":"deadbeef"},"lineage":{}}}
{"timestamp":"2026-09-01T10:00:01.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello codex unique phrase"}]}}
{"timestamp":"2026-09-01T10:00:02.000Z","type":"response_item","payload":{"type":"reasoning","summary":[{"type":"summary_text","text":"thinking about it"}]}}
{"timestamp":"2026-09-01T10:00:03.000Z","type":"response_item","payload":{"type":"function_call","call_id":"call_1","name":"bash","arguments":"{\"command\":\"ls\"}"}}
{"timestamp":"2026-09-01T10:00:04.000Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call_1","output":"file1 file2"}}
{"timestamp":"2026-09-01T10:00:05.000Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}}
{"timestamp":"2026-09-01T10:00:06.000Z","type":"event_msg","payload":{"type":"agent_message","message":"ignored"}}
`

func TestSessionID(t *testing.T) {
	if got := transcript.SessionID("thread_1"); got != "codex:thread_1" {
		t.Fatalf("got %q", got)
	}
}

func TestSessionsFromRollout(t *testing.T) {
	root := t.TempDir()
	writeRollout(t, root, "2026/09/01", "rollout-1-uuid.jsonl", sampleRollout)

	src := transcript.NewSource(root, domain.DefaultIndexOptions)
	sessions, err := src.Sessions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	s := sessions[0]
	if s.ID != "codex:thread_abc123" {
		t.Errorf("id: %q", s.ID)
	}
	if s.Vendor != domain.VendorCodex {
		t.Errorf("vendor: %q", s.Vendor)
	}
	if s.ProjectPath != "/Users/me/src/app" {
		t.Errorf("project path: %q", s.ProjectPath)
	}
	if s.GitBranch != "main" {
		t.Errorf("branch: %q", s.GitBranch)
	}
	if s.Title == "" {
		t.Error("expected title from first user message")
	}
}

func TestMessagesMapKindsAndPairTools(t *testing.T) {
	root := t.TempDir()
	writeRollout(t, root, "2026/09/01", "rollout-1-uuid.jsonl", sampleRollout)

	src := transcript.NewSource(root, domain.DefaultIndexOptions)
	var msgs []domain.Message
	if err := src.Messages(t.Context(), "codex:thread_abc123", func(m domain.Message) error {
		msgs = append(msgs, m)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	kinds := map[domain.BlockKind]int{}
	for _, m := range msgs {
		kinds[m.Kind]++
		if m.ProjectPath != "/Users/me/src/app" {
			t.Errorf("message project path: %q", m.ProjectPath)
		}
	}
	if kinds[domain.KindText] < 2 {
		t.Errorf("expected >=2 text messages, got %d", kinds[domain.KindText])
	}
	if kinds[domain.KindThinking] != 1 {
		t.Errorf("expected 1 thinking message, got %d", kinds[domain.KindThinking])
	}
	if kinds[domain.KindToolUse] != 1 {
		t.Errorf("expected 1 tool use, got %d", kinds[domain.KindToolUse])
	}
	if kinds[domain.KindToolResult] != 1 {
		t.Errorf("expected 1 tool result, got %d", kinds[domain.KindToolResult])
	}

	var toolUse, toolResult *domain.Message
	for i := range msgs {
		switch msgs[i].Kind {
		case domain.KindToolUse:
			toolUse = &msgs[i]
		case domain.KindToolResult:
			toolResult = &msgs[i]
		}
	}
	if toolUse == nil || toolUse.ToolName != "bash" {
		t.Errorf("tool use name: %+v", toolUse)
	}
	if toolResult == nil || toolResult.ToolName != "bash" {
		t.Errorf("tool result should inherit paired tool name, got %+v", toolResult)
	}
	if toolResult == nil || !strings.Contains(toolResult.Text, "file1") {
		t.Errorf("tool result text: %+v", toolResult)
	}
}

func TestLoadFileBuildsSessionDetail(t *testing.T) {
	root := t.TempDir()
	path := writeRollout(t, root, "2026/09/01", "rollout-1-uuid.jsonl", sampleRollout)

	detail, err := transcript.LoadFile(t.Context(), path, domain.DefaultIndexOptions)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Session.ID != "codex:thread_abc123" {
		t.Fatalf("session id = %q", detail.Session.ID)
	}
	if detail.Session.Vendor != domain.VendorCodex {
		t.Fatalf("vendor = %q", detail.Session.Vendor)
	}
	if detail.Session.Title != "hello codex unique phrase" {
		t.Fatalf("title = %q", detail.Session.Title)
	}
	if detail.Session.MessageCount != len(detail.Messages) {
		t.Fatalf("message count = %d, messages = %d", detail.Session.MessageCount, len(detail.Messages))
	}
}

func TestMissingRootYieldsNoSessions(t *testing.T) {
	src := transcript.NewSource(filepath.Join(t.TempDir(), "nope"), domain.DefaultIndexOptions)
	sessions, err := src.Sessions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected no sessions, got %d", len(sessions))
	}
}

func TestFingerprintStableForFile(t *testing.T) {
	root := t.TempDir()
	writeRollout(t, root, "2026/09/01", "rollout-1-uuid.jsonl", sampleRollout)
	src := transcript.NewSource(root, domain.DefaultIndexOptions)

	fp, err := src.TranscriptFingerprint(t.Context(), "codex:thread_abc123")
	if err != nil {
		t.Fatal(err)
	}
	if fp.Path == "" || fp.Hash == "" {
		t.Fatalf("fingerprint: %+v", fp)
	}
	if !strings.HasPrefix(fp.Hash, "codex-transcript-meta-v1:") {
		t.Fatalf("hash prefix: %q", fp.Hash)
	}
	if _, err := src.TranscriptFingerprint(t.Context(), "codex:missing"); err == nil {
		t.Fatal("expected error for missing session")
	}
}
