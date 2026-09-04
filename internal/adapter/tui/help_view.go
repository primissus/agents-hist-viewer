package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func renderHelp(m Model) string {
	row := func(key, desc string) string {
		return "  " + current.HelpKey.Render(key) + strings.Repeat(" ", max(1, 14-len(key))) + desc
	}
	detailEsc := "back / clear search"
	if m.detailOnly && m.helpFromState == viewDetail {
		detailEsc = "quit / clear search"
	}

	lines := []string{
		current.Header.Render("Keyboard Shortcuts"),
		"",
		current.HelpSection.Render("Results"),
		row("/", "search"),
		row("↑↓ in search", "history"),
		row("enter", "open thread"),
		row("↑↓  j k", "navigate"),
		row("f", "filters (path / type / vendor)"),
		row("t", "cycle type (all/chat/plan)"),
		row("s", "cycle sort"),
		row("g", "cycle group (off/path/vendor/vendor+path)"),
		row("c", "clear search (back to recent)"),
		row("S", "copy session ID"),
		row("P", "copy project path"),
		row("shift+F  F", "copy file path"),
		row("q  esc", "quit"),
		row("ctrl+c", "quit immediately"),
		"",
		current.HelpSection.Render("Filter view"),
		row("←/→  h/l", "switch category"),
		row("tab  shift+tab", "switch category"),
		row("↑↓  j k", "navigate"),
		row("u  d", "page up / down"),
		row("enter", "apply and close"),
		row("c", "clear active category"),
		row("esc", "close without applying"),
		row("q", "quit"),
		"",
		current.HelpSection.Render("Thread"),
		row("/", "in-thread search"),
		row("n  N", "next / prev match"),
		row("ctrl+o", "expand / collapse"),
		row("m", "cycle message type filter"),
		row("T", "cycle time format (local/UTC/date/off)"),
		row("u  d", "half-page up / down"),
		row("j  k", "scroll line"),
		row("g  G", "top / bottom"),
		row("S", "copy session ID"),
		row("P", "copy project path"),
		row("shift+F  F", "copy file path"),
		row("esc", detailEsc),
		row("q", "quit"),
		row("ctrl+c", "quit immediately"),
		"",
		current.Footer.Render("any key to dismiss"),
	}

	box := current.HelpBox.Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
