package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"claude-code-hist-viewer/internal/domain"
)

// overflowHitItem is a test-only list item that returns raw multiline titles.
type overflowHitItem struct {
	title string
	desc  string
}

func (h overflowHitItem) FilterValue() string { return h.title }
func (h overflowHitItem) Title() string       { return h.title }
func (h overflowHitItem) Description() string { return h.desc }

func TestClipResultsBody(t *testing.T) {
	body := strings.Repeat("line\n", 10)
	got := clipResultsBody(body, 40, 3)
	lines := strings.Split(got, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) != 3 {
		t.Fatalf("clipResultsBody() kept %d lines, want 3", len(lines))
	}
}

func TestRenderResultsStickyChromeAlwaysPresent(t *testing.T) {
	m := NewApp(nil, "")
	m.width = 100
	m.height = 24
	m.loading = false
	m.isHome = true
	m = m.syncResultsLayout()

	items := make([]list.Item, 8)
	for i := range items {
		items[i] = overflowHitItem{
			title: "<user_query>\nOverflow title line\nAnother overflow line",
			desc:  "/Users/user1/src/demo",
		}
	}
	m.list.SetItems(items)

	view := renderResults(m)
	lines := strings.Split(view, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) != m.height {
		t.Fatalf("rendered %d lines, want %d", len(lines), m.height)
	}
	if got := ansi.Strip(lines[0]); !strings.HasPrefix(got, "> search") {
		t.Fatalf("row 1 = %q, want search bar", got)
	}
	if got := ansi.Strip(lines[1]); !strings.HasPrefix(got, "─") {
		t.Fatalf("row 2 = %q, want separator", got)
	}
	if !strings.Contains(ansi.Strip(lines[2]), "Recent threads") {
		t.Fatalf("row 3 = %q, want subtitle", lines[2])
	}
}

// assertSearchInputVisible guards against the home-screen regression where a faint
// or low-contrast palette color made the search row invisible on pale terminals.
func assertSearchInputVisible(t *testing.T, inp textinput.Model) {
	t.Helper()

	wantFG := current.SearchBar.GetForeground()
	if wantFG == nil {
		t.Fatal("search bar theme must define a foreground color")
	}

	styles := []struct {
		name  string
		style lipgloss.Style
	}{
		{"prompt", inp.PromptStyle},
		{"text", inp.TextStyle},
		{"placeholder", inp.PlaceholderStyle},
		{"completion", inp.CompletionStyle},
		{"cursor text", inp.Cursor.TextStyle},
	}
	for _, s := range styles {
		if s.style.GetFaint() {
			t.Fatalf("%s style must not be faint", s.name)
		}
		if got := s.style.GetForeground(); got != wantFG {
			t.Fatalf("%s foreground = %v, want search bar foreground %v", s.name, got, wantFG)
		}
	}
}

func TestNewAppSearchInputUsesVisibleSearchBarStyle(t *testing.T) {
	m := NewApp(nil, "")
	assertSearchInputVisible(t, m.input)
}

func TestRenderResultsShowsEmptyHomeSearchBar(t *testing.T) {
	m := NewApp(nil, "")
	m.width = 80
	m.height = 10
	m.loading = false
	m.isHome = true
	m = m.syncResultsLayout()

	assertSearchInputVisible(t, m.input)

	view := renderResults(m)
	lines := strings.Split(view, "\n")
	if len(lines) < 3 {
		t.Fatalf("rendered view too short: %q", view)
	}

	if got := ansi.Strip(lines[0]); !strings.HasPrefix(got, "> search") {
		t.Fatalf("search row = %q, want prompt and placeholder", got)
	}
	if got := ansi.Strip(lines[1]); !strings.HasPrefix(got, "─") {
		t.Fatalf("separator row = %q, want horizontal rule", got)
	}
}

func TestRenderResultsFitsWindowHeightWithMultilineTitles(t *testing.T) {
	m := NewApp(nil, "")
	m.width = 100
	m.height = 24
	m.loading = false
	m.isHome = true

	multiline := "<user_query>\nCan you show the date and hour of each message"
	var hits []domain.SearchHit
	for i := 0; i < 20; i++ {
		hits = append(hits, domain.SearchHit{
			SessionTitle: multiline,
			ProjectPath:  "/p",
			SessionID:    fmt.Sprintf("id-%d", i),
		})
	}
	m.hits = hits
	m = m.applyFiltersAndSort()
	m = m.syncResultsLayout()

	item := m.list.Items()[0].(hitItem)
	if strings.Contains(item.Title(), "\n") {
		t.Fatalf("display title must be single-line, got %q", item.Title())
	}

	view := renderResults(m)
	lines := strings.Split(view, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) != m.height {
		t.Fatalf("rendered %d lines, want %d", len(lines), m.height)
	}
}

func TestRenderResultsFitsWindowHeight(t *testing.T) {
	for _, height := range []int{10, 24, 40} {
		t.Run(fmt.Sprintf("height-%d", height), func(t *testing.T) {
			m := NewApp(nil, "")
			m.width = 100
			m.height = height
			m.loading = false
			m.isHome = true
			var hits []domain.SearchHit
			for i := 0; i < 20; i++ {
				hits = append(hits, domain.SearchHit{
					SessionTitle: "title",
					ProjectPath:  "/p",
					SessionID:    "id",
				})
			}
			m.hits = hits
			m = m.applyFiltersAndSort()
			m = m.syncResultsLayout()

			view := renderResults(m)
			lines := strings.Split(view, "\n")
			if lines[len(lines)-1] == "" {
				lines = lines[:len(lines)-1]
			}
			if len(lines) != height {
				t.Fatalf("height %d: rendered %d lines", height, len(lines))
			}
		})
	}
}

func TestGroupedResultsInsertPathHeaders(t *testing.T) {
	m := NewApp(nil, "")
	m.width = 100
	m.height = 24
	m.loading = false
	m.isHome = true
	m.groupMode = groupPath
	m.hits = []domain.SearchHit{
		{SessionTitle: "first", ProjectPath: "/b", SessionID: "id-1"},
		{SessionTitle: "second", ProjectPath: "/a", SessionID: "id-2"},
		{SessionTitle: "third", ProjectPath: "/a", SessionID: "id-3"},
		{SessionTitle: "fourth", SessionID: "id-4"},
	}

	m = m.applyFiltersAndSort()

	if len(m.filteredHits) != 4 {
		t.Fatalf("filteredHits len = %d, want 4", len(m.filteredHits))
	}
	items := m.list.Items()
	if len(items) != 7 {
		t.Fatalf("items len = %d, want 7", len(items))
	}
	wantHeaders := []struct {
		index int
		title string
	}{
		{0, "(no path)"},
		{2, "/a"},
		{5, "/b"},
	}
	for _, want := range wantHeaders {
		header, ok := items[want.index].(groupHeaderItem)
		if !ok {
			t.Fatalf("item %d = %T, want groupHeaderItem", want.index, items[want.index])
		}
		if header.Title() != want.title {
			t.Fatalf("header %d = %q, want %q", want.index, header.Title(), want.title)
		}
	}
}

func TestGroupedResultsVendorPathOrdering(t *testing.T) {
	m := NewApp(nil, "")
	m.width = 100
	m.height = 24
	m.loading = false
	m.isHome = true
	m.groupMode = groupVendorPath
	m.hits = []domain.SearchHit{
		{SessionTitle: "cursor", ProjectPath: "/z", SessionID: "id-1", Vendor: domain.VendorCursor},
		{SessionTitle: "claude b", ProjectPath: "/b", SessionID: "id-2", Vendor: domain.VendorClaude},
		{SessionTitle: "claude a", ProjectPath: "/a", SessionID: "id-3", Vendor: domain.VendorClaude},
	}

	m = m.applyFiltersAndSort()

	items := m.list.Items()
	var headers []string
	for _, item := range items {
		if header, ok := item.(groupHeaderItem); ok {
			headers = append(headers, header.Title())
		}
	}
	want := []string{
		"Claude Code: /a",
		"Claude Code: /b",
		"Cursor: /z",
	}
	if strings.Join(headers, "\n") != strings.Join(want, "\n") {
		t.Fatalf("headers = %#v, want %#v", headers, want)
	}
}

func TestGroupedResultHeaderActionsNoop(t *testing.T) {
	m := NewApp(nil, "")
	m.width = 100
	m.height = 24
	m.loading = false
	m.isHome = true
	m.groupMode = groupVendor
	m.hits = []domain.SearchHit{
		{SessionTitle: "title", ProjectPath: "/p", SessionID: "id", Vendor: domain.VendorClaude},
	}
	m = m.applyFiltersAndSort()
	m.list.Select(0)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("enter on group header returned a command")
	}
	m = updated.(Model)
	if m.loading {
		t.Fatal("enter on group header set loading")
	}

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	if cmd != nil {
		t.Fatal("copy on group header returned a command")
	}
	m = updated.(Model)
	if m.copiedMsg != "" {
		t.Fatalf("copy on group header set copiedMsg = %q", m.copiedMsg)
	}
}
