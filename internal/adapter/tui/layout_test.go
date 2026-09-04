package tui

import (
	"strings"
	"testing"
	"time"

	"claude-code-hist-viewer/internal/domain"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestLimitsForWidthBreakpoints(t *testing.T) {
	narrow := limitsForWidth(59)
	wide := limitsForWidth(60)
	if narrow.StickyTitle != 14 {
		t.Fatalf("width 59 StickyTitle = %d, want 14", narrow.StickyTitle)
	}
	if wide.StickyTitle != 20 {
		t.Fatalf("width 60 StickyTitle = %d, want 20", wide.StickyTitle)
	}
	if limitsForWidth(79).StickyTitle != 20 {
		t.Fatalf("width 79 should stay at 60-tier limits")
	}
	if limitsForWidth(80).StickyTitle != 28 {
		t.Fatalf("width 80 StickyTitle = %d, want 28", limitsForWidth(80).StickyTitle)
	}
	if limitsForWidth(200).StickyTitle != 52 {
		t.Fatalf("width 200 StickyTitle = %d, want 52", limitsForWidth(200).StickyTitle)
	}
}

func TestFitStickyFieldsFitsTerminalWidth(t *testing.T) {
	longTitle := strings.Repeat("T", 80)
	longPath := "/very/long/path/to/project/" + strings.Repeat("x", 80) + ".jsonl"
	for _, width := range []int{50, 80, 120} {
		t.Run("", func(t *testing.T) {
			lim := limitsForWidth(width)
			title, id, file := fitStickyFields(longTitle, "abcdefgh-1234", longPath, width, lim)
			line := joinSticky(title, id, file)
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("width %d: line width %d exceeds budget: %q", width, got, line)
			}
		})
	}
}

func TestRenderDetailStickyRespectsWidth(t *testing.T) {
	s := domain.Session{
		ID:       "sess-abc-full-id",
		Title:    strings.Repeat("Long thread title ", 8),
		FilePath: "/home/user/.claude/projects/" + strings.Repeat("deep/", 12) + "sess.jsonl",
	}
	for _, width := range []int{60, 100, 160} {
		t.Run("", func(t *testing.T) {
			line := ansi.Strip(renderDetailSticky(s, width))
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("width %d: sticky line width %d: %q", width, got, line)
			}
		})
	}
}

func TestHitItemDescriptionRespectsListWidth(t *testing.T) {
	item := hitItem{
		listWidth: 80,
		hit: domain.SearchHit{
			ProjectPath: "/very/long/project/path/" + strings.Repeat("p", 60),
			SessionID:   "12345678-abcd",
			Timestamp:   time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		},
	}
	desc := item.Description()
	if got := lipgloss.Width(desc); got > 80 {
		t.Fatalf("description width %d exceeds list width 80: %q", got, desc)
	}
}

func TestFitResultsFooterClampsHint(t *testing.T) {
	hint := strings.Repeat("hint ", 40)
	got := fitResultsFooter(hint, 60)
	if lipgloss.Width(got) > 60 {
		t.Fatalf("footer hint width %d exceeds 60", lipgloss.Width(got))
	}
}
