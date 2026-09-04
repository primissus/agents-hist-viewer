package tui

import (
	"strings"
	"testing"
	"time"

	"claude-code-hist-viewer/internal/domain"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestRenderDetailStickyShowsTitleIDAndFile(t *testing.T) {
	path := "/home/user/.claude/projects/proj/sess-abc.jsonl"
	m := Model{
		width:  120,
		height: 24,
		detail: domain.SessionDetail{
			Session: domain.Session{
				ID:          "sess-abc-full-id",
				Title:       "My thread title",
				ProjectPath: "/home/user/proj",
				GitBranch:   "main",
				FilePath:    path,
				StartedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				EndedAt:     time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
			},
		},
	}

	view := renderDetail(m)
	plain := ansi.Strip(view)
	firstLine, _, ok := strings.Cut(plain, "\n")
	if !ok {
		t.Fatal("detail view missing lines")
	}
	if !strings.Contains(firstLine, "My thread title") {
		t.Fatalf("sticky header missing title in %q", firstLine)
	}
	if !strings.Contains(firstLine, "Claude Code") {
		t.Fatalf("sticky header missing source label in %q", firstLine)
	}
	if !strings.Contains(firstLine, "sess-abc") {
		t.Fatalf("sticky header missing short session ID in %q", firstLine)
	}
	if !strings.Contains(firstLine, "sess-abc.jsonl") {
		t.Fatalf("sticky header missing filename in %q", firstLine)
	}
}

func TestRenderDetailStickyShowsClaudeDesktopSource(t *testing.T) {
	line := ansi.Strip(renderDetailSticky(domain.Session{
		ID:     "sess-desktop",
		Title:  "Desktop thread",
		Vendor: domain.VendorClaudeDesktop,
	}, 100))
	if !strings.Contains(line, "Claude Desktop") {
		t.Fatalf("sticky header missing desktop source label in %q", line)
	}
}

func TestRenderDetailScrollHeaderShowsFullPath(t *testing.T) {
	path := "/home/user/.claude/projects/proj/sess-abc.jsonl"
	content := renderDetailViewport(domain.SessionDetail{
		Session: domain.Session{
			ID:          "sess-abc",
			Title:       "My thread",
			ProjectPath: "/home/user/proj",
			FilePath:    path,
			StartedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			EndedAt:     time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		},
	}, 120, false, "", timeFmtDateTime, msgFilterAll)

	plain := ansi.Strip(content)
	if !strings.Contains(plain, path) {
		t.Fatalf("scrollable header missing full file path in:\n%s", plain)
	}
}

func TestRenderDetailStickyOmitsFileWhenEmpty(t *testing.T) {
	line := ansi.Strip(renderDetailSticky(domain.Session{
		ID:    "sess-only",
		Title: "No file",
	}, 80))
	if !strings.Contains(line, "sess-onl") {
		t.Fatalf("sticky header missing short session id in %q", line)
	}
	if strings.Contains(line, ".jsonl") {
		t.Fatalf("sticky header should not show file path, got %q", line)
	}
	if !strings.Contains(line, "—") {
		t.Fatalf("sticky header should show placeholder for missing file, got %q", line)
	}
}

func TestDetailChromeLines(t *testing.T) {
	if got := detailChromeLines(domain.Session{}); got != 3 {
		t.Fatalf("detail chrome = %d, want 3", got)
	}
	if got := detailChromeLines(domain.Session{FilePath: "/x"}); got != 3 {
		t.Fatalf("detail chrome with file path = %d, want 3", got)
	}
}

func TestRenderMessageToolResultNotLabeledYou(t *testing.T) {
	out := ansi.Strip(renderMessage(domain.Message{
		Role: domain.RoleUser,
		Kind: domain.KindToolResult,
		Text: "tool output",
	}, false, "", timeFmtOff))
	if strings.Contains(out, "You") {
		t.Fatalf("tool_result should not render as You, got %q", out)
	}
	if !strings.Contains(out, "result") {
		t.Fatalf("tool_result should render as result, got %q", out)
	}
}

func TestRenderMessageSidechainTaskNotLabeledYou(t *testing.T) {
	out := ansi.Strip(renderMessage(domain.Message{
		Role:        domain.RoleUser,
		Kind:        domain.KindText,
		Text:        "delegated subagent task",
		IsSidechain: true,
	}, false, "", timeFmtOff))
	if strings.Contains(out, "You") {
		t.Fatalf("sidechain user text should not render as You, got %q", out)
	}
	if !strings.Contains(out, "Task") {
		t.Fatalf("sidechain user text should render as Task, got %q", out)
	}
}

func TestMessageMatchesFilter(t *testing.T) {
	msg := domain.Message{Role: domain.RoleUser, Kind: domain.KindText, IsSidechain: true}
	if !messageMatchesFilter(msg, msgFilterTask) {
		t.Fatal("expected sidechain user text to match task filter")
	}
	if messageMatchesFilter(msg, msgFilterYou) {
		t.Fatal("sidechain user text should not match you filter")
	}
}

func TestNewDetailAppEscQuits(t *testing.T) {
	m := NewDetailApp(domain.SessionDetail{
		Session: domain.Session{
			ID:        "sess-abc",
			Title:     "Direct view",
			FilePath:  "/tmp/sess-abc.jsonl",
			StartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			EndedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		Messages: []domain.Message{{
			SessionID: "sess-abc",
			Role:      domain.RoleUser,
			Kind:      domain.KindText,
			Text:      "hello",
		}},
	})
	if cmd := m.Init(); cmd != nil {
		t.Fatal("detail-only app should not load recent sessions")
	}
	if !strings.Contains(ansi.Strip(renderDetail(m)), "esc quit") {
		t.Fatal("detail-only footer should show esc quit")
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	if _, ok := next.(Model); !ok {
		t.Fatalf("model type = %T, want tui.Model", next)
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("command message = %T, want tea.QuitMsg", msg)
	}
}
