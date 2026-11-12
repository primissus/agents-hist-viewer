package transcript_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"claude-code-hist-viewer/internal/adapter/transcript"
	"claude-code-hist-viewer/internal/domain"
)

const samplePath = "../../../testdata/sample.jsonl"

func setupSource(t *testing.T) *transcript.Source {
	t.Helper()
	// Put sample.jsonl in a fake projects/<proj>/<sessionId>.jsonl structure.
	tmp := t.TempDir()
	projDir := filepath.Join(tmp, "proj1")
	if err := os.MkdirAll(projDir, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(samplePath)
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "sess-abc.jsonl"), data, 0644); err != nil {
		t.Fatal(err)
	}
	return transcript.NewSource(tmp, domain.DefaultIndexOptions)
}

func TestMetaLineExcluded(t *testing.T) {
	src := setupSource(t)
	var msgs []domain.Message
	if err := src.Messages(context.Background(), "sess-abc", func(m domain.Message) error {
		msgs = append(msgs, m)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		if m.Text == "<local-command-caveat>meta line should be filtered</local-command-caveat>" {
			t.Error("isMeta line should have been excluded")
		}
	}
}

func TestImageBlockExcluded(t *testing.T) {
	src := setupSource(t)
	var msgs []domain.Message
	if err := src.Messages(context.Background(), "sess-abc", func(m domain.Message) error {
		msgs = append(msgs, m)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		if m.Kind == domain.KindImage {
			t.Error("image block should be dropped by Shrink")
		}
	}
}

func TestToolUseBlockPresentAndTruncated(t *testing.T) {
	// Use a tiny cap to force truncation.
	tmp := t.TempDir()
	projDir := filepath.Join(tmp, "p")
	if err := os.MkdirAll(projDir, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(samplePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "sess-abc.jsonl"), data, 0644); err != nil {
		t.Fatal(err)
	}
	opts := domain.IndexOptions{
		Shrink: domain.ShrinkConfig{Enabled: true, ToolPayloadCap: 5, DropImages: true},
		Depth:  domain.IndexDepthDeep,
	}
	src := transcript.NewSource(tmp, opts)

	var found bool
	if err := src.Messages(context.Background(), "sess-abc", func(m domain.Message) error {
		if m.Kind == domain.KindToolUse {
			found = true
			if len([]rune(m.Text)) > 5+len("…[truncated]")+5 {
				t.Errorf("tool_use text not truncated: %q", m.Text)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Error("expected at least one tool_use block")
	}
}

func TestCustomTitlePreferredOverAiTitle(t *testing.T) {
	src := setupSource(t)
	sessions, err := src.Sessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) == 0 {
		t.Fatal("no sessions found")
	}
	if sessions[0].Title != "Custom Title Preferred" {
		t.Errorf("expected custom title, got %q", sessions[0].Title)
	}
}

func TestSidechainBlockFlagged(t *testing.T) {
	src := setupSource(t)
	var found bool
	if err := src.Messages(context.Background(), "sess-abc", func(m domain.Message) error {
		if m.IsSidechain {
			found = true
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Error("expected at least one sidechain message")
	}
}

func TestStringContentProducesTextMessage(t *testing.T) {
	src := setupSource(t)
	var found bool
	if err := src.Messages(context.Background(), "sess-abc", func(m domain.Message) error {
		if m.Kind == domain.KindText && m.Text == "hello world plain string" {
			found = true
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Error("expected text message from string content")
	}
}

func TestQuickIndexSkipsNonUserBlocks(t *testing.T) {
	tmp := t.TempDir()
	projDir := filepath.Join(tmp, "proj1")
	if err := os.MkdirAll(projDir, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(samplePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "sess-abc.jsonl"), data, 0644); err != nil {
		t.Fatal(err)
	}
	opts := domain.IndexOptions{Shrink: domain.DefaultShrinkConfig, Depth: domain.IndexDepthQuick}
	src := transcript.NewSource(tmp, opts)

	var msgs []domain.Message
	if err := src.Messages(context.Background(), "sess-abc", func(m domain.Message) error {
		msgs = append(msgs, m)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		switch m.Kind {
		case domain.KindText:
			if m.Role != domain.RoleUser {
				t.Fatalf("quick index kept non-user text: %+v", m)
			}
		case domain.KindToolResult:
			// ok
		default:
			t.Fatalf("quick index kept unexpected kind %s", m.Kind)
		}
	}
	if len(msgs) == 0 {
		t.Fatal("expected quick index messages")
	}
}

func TestLoadSessionMatchesLoadFile(t *testing.T) {
	src := setupSource(t)
	filePath := filepath.Join(mustSourceRoot(t, src), "proj1", "sess-abc.jsonl")

	detail, err := transcript.LoadFile(context.Background(), filePath, domain.DefaultIndexOptions)
	if err != nil {
		t.Fatal(err)
	}
	sess, msgs, err := src.LoadSession(context.Background(), "sess-abc")
	if err != nil {
		t.Fatal(err)
	}

	if sess.ID != detail.Session.ID || sess.Title != detail.Session.Title ||
		sess.ProjectPath != detail.Session.ProjectPath || sess.Vendor != detail.Session.Vendor ||
		sess.RecordKind != detail.Session.RecordKind || sess.HasTranscript != detail.Session.HasTranscript ||
		sess.MessageCount != detail.Session.MessageCount {
		t.Fatalf("LoadSession session %+v != LoadFile session %+v", sess, detail.Session)
	}
	if !sess.StartedAt.Equal(detail.Session.StartedAt) || !sess.EndedAt.Equal(detail.Session.EndedAt) {
		t.Fatalf("timestamps differ: %v/%v vs %v/%v", sess.StartedAt, sess.EndedAt, detail.Session.StartedAt, detail.Session.EndedAt)
	}
	if len(msgs) != len(detail.Messages) {
		t.Fatalf("messages = %d, want %d", len(msgs), len(detail.Messages))
	}
	for i := range msgs {
		a, b := msgs[i], detail.Messages[i]
		if a.UUID != b.UUID || a.Text != b.Text || a.Sequence != b.Sequence ||
			a.Role != b.Role || a.Kind != b.Kind || a.IsSidechain != b.IsSidechain {
			t.Fatalf("message %d differs:\n got %+v\nwant %+v", i, a, b)
		}
	}
}

func mustSourceRoot(t *testing.T, src *transcript.Source) string {
	t.Helper()
	// setupSource creates the fixture under a temp dir; find it through the
	// session the source already knows about.
	sessions, err := src.Sessions(context.Background())
	if err != nil || len(sessions) == 0 {
		t.Fatalf("sessions: %v %v", sessions, err)
	}
	return filepath.Dir(filepath.Dir(sessions[0].FilePath))
}

func TestLoadFileBuildsSessionDetail(t *testing.T) {
	detail, err := transcript.LoadFile(context.Background(), samplePath, domain.DefaultIndexOptions)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Session.ID != "sess-abc" {
		t.Fatalf("session id = %q, want sess-abc", detail.Session.ID)
	}
	if detail.Session.Title != "Custom Title Preferred" {
		t.Fatalf("title = %q, want custom title", detail.Session.Title)
	}
	if detail.Session.Vendor != domain.VendorClaude {
		t.Fatalf("vendor = %q, want claude", detail.Session.Vendor)
	}
	if detail.Session.RecordKind != domain.RecordChat {
		t.Fatalf("record kind = %q, want chat", detail.Session.RecordKind)
	}
	if !detail.Session.HasTranscript {
		t.Fatal("expected transcript flag")
	}
	if detail.Session.MessageCount != len(detail.Messages) {
		t.Fatalf("message count = %d, messages = %d", detail.Session.MessageCount, len(detail.Messages))
	}
	if len(detail.Messages) == 0 {
		t.Fatal("expected messages")
	}
	if detail.Messages[0].SessionID != "sess-abc" {
		t.Fatalf("message session id = %q, want sess-abc", detail.Messages[0].SessionID)
	}
}
