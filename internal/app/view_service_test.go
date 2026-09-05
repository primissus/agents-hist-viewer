package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"claude-code-hist-viewer/internal/domain"
)

func TestViewServiceAutoDetectsClaude(t *testing.T) {
	path := writeJSONL(t, `{"type":"user","sessionId":"sess-1","message":{"role":"user","content":"hi"}}`)
	var called string
	svc := NewViewService(
		func(ctx context.Context, p string, opts domain.IndexOptions) (domain.SessionDetail, error) {
			called = "claude"
			if !filepath.IsAbs(p) {
				t.Fatalf("loader path is not absolute: %s", p)
			}
			return domain.SessionDetail{Session: domain.Session{ID: "sess-1"}}, nil
		},
		func(ctx context.Context, p string, opts domain.IndexOptions) (domain.SessionDetail, error) {
			called = "cursor"
			return domain.SessionDetail{}, nil
		},
		domain.DefaultIndexOptions,
	)

	detail, err := svc.LoadJSONL(t.Context(), path, ViewFormatAuto)
	if err != nil {
		t.Fatal(err)
	}
	if called != "claude" {
		t.Fatalf("called %q, want claude", called)
	}
	if detail.Session.ID != "sess-1" {
		t.Fatalf("session id = %q", detail.Session.ID)
	}
}

func TestViewServiceAutoDetectsCursor(t *testing.T) {
	path := writeJSONL(t, `{"role":"user","message":{"content":[{"type":"text","text":"hi"}]}}`)
	var called string
	svc := NewViewService(
		func(ctx context.Context, p string, opts domain.IndexOptions) (domain.SessionDetail, error) {
			called = "claude"
			return domain.SessionDetail{}, nil
		},
		func(ctx context.Context, p string, opts domain.IndexOptions) (domain.SessionDetail, error) {
			called = "cursor"
			return domain.SessionDetail{Session: domain.Session{ID: "cursor:chat"}}, nil
		},
		domain.DefaultIndexOptions,
	)

	detail, err := svc.LoadJSONL(t.Context(), path, ViewFormatAuto)
	if err != nil {
		t.Fatal(err)
	}
	if called != "cursor" {
		t.Fatalf("called %q, want cursor", called)
	}
	if detail.Session.ID != "cursor:chat" {
		t.Fatalf("session id = %q", detail.Session.ID)
	}
}

func TestViewServiceRejectsNonJSONL(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "chat.txt")
	if err := os.WriteFile(path, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	svc := NewViewService(nil, nil, domain.DefaultIndexOptions)
	if _, err := svc.LoadJSONL(t.Context(), path, ViewFormatAuto); err == nil {
		t.Fatal("expected non-jsonl error")
	}
}

func TestParseViewFormat(t *testing.T) {
	if got, err := ParseViewFormat("CLAUDE"); err != nil || got != ViewFormatClaude {
		t.Fatalf("ParseViewFormat(CLAUDE) = %q, %v", got, err)
	}
	if got, err := ParseViewFormat("codex"); err != nil || got != ViewFormatCodex {
		t.Fatalf("ParseViewFormat(codex) = %q, %v", got, err)
	}
	if _, err := ParseViewFormat("bogus"); err == nil {
		t.Fatal("expected unsupported format error")
	}
}

func TestViewServiceAutoDetectsCodex(t *testing.T) {
	path := writeJSONL(t, `{"timestamp":"2026-09-01T10:00:00Z","type":"session_meta","payload":{"id":"thread_1"}}`)
	var called string
	svc := NewViewServiceWithCodex(
		func(ctx context.Context, p string, opts domain.IndexOptions) (domain.SessionDetail, error) {
			called = "claude"
			return domain.SessionDetail{}, nil
		},
		func(ctx context.Context, p string, opts domain.IndexOptions) (domain.SessionDetail, error) {
			called = "cursor"
			return domain.SessionDetail{}, nil
		},
		func(ctx context.Context, p string, opts domain.IndexOptions) (domain.SessionDetail, error) {
			called = "codex"
			return domain.SessionDetail{Session: domain.Session{ID: "codex:thread_1"}}, nil
		},
		domain.DefaultIndexOptions,
	)

	detail, err := svc.LoadJSONL(t.Context(), path, ViewFormatAuto)
	if err != nil {
		t.Fatal(err)
	}
	if called != "codex" {
		t.Fatalf("called %q, want codex", called)
	}
	if detail.Session.ID != "codex:thread_1" {
		t.Fatalf("session id = %q", detail.Session.ID)
	}
}

func TestViewServiceCodexRequiresLoader(t *testing.T) {
	path := writeJSONL(t, `{"timestamp":"2026-09-01T10:00:00Z","type":"session_meta","payload":{"id":"thread_1"}}`)
	svc := NewViewService(nil, nil, domain.DefaultIndexOptions)
	if _, err := svc.LoadJSONL(t.Context(), path, ViewFormatCodex); err == nil {
		t.Fatal("expected codex loader not configured error")
	}
}

func writeJSONL(t *testing.T, content string) string {
	t.Helper()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "chat.jsonl")
	if err := os.WriteFile(path, []byte(content+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}
