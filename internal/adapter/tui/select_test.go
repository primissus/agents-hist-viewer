package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"claude-code-hist-viewer/internal/domain"
)

func seedResultsModel(hits []domain.SearchHit) Model {
	m := NewApp(nil, "")
	m.width = 100
	m.height = 24
	m.loading = false
	m.isHome = true
	m.hits = hits
	m = m.applyFiltersAndSort()
	m = m.syncResultsLayout()
	return m
}

func TestToggleSelectMarksAndUnmarksItem(t *testing.T) {
	m := seedResultsModel([]domain.SearchHit{
		{SessionTitle: "one", SessionID: "id-1", ProjectPath: "/p", FilePath: "/p/one.jsonl"},
	})
	m.list.Select(0)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if cmd != nil {
		t.Fatalf("space returned a command: %v", cmd)
	}
	m = updated.(Model)
	if !m.selected["id-1"] {
		t.Fatal("expected id-1 to be selected after space")
	}
	item := m.list.Items()[0].(hitItem)
	if !strings.HasPrefix(item.Title(), "[x] ") {
		t.Fatalf("title = %q, want [x] prefix", item.Title())
	}

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if cmd != nil {
		t.Fatalf("second space returned a command: %v", cmd)
	}
	m = updated.(Model)
	if m.selected["id-1"] {
		t.Fatal("expected id-1 to be deselected after second space")
	}
	item = m.list.Items()[0].(hitItem)
	if strings.Contains(item.Title(), "[x]") || strings.Contains(item.Title(), "[ ]") {
		t.Fatalf("title = %q, want no marker once selection empty", item.Title())
	}
}

func TestToggleSelectOnGroupHeaderIsNoop(t *testing.T) {
	m := seedResultsModel([]domain.SearchHit{
		{SessionTitle: "one", SessionID: "id-1", ProjectPath: "/a"},
		{SessionTitle: "two", SessionID: "id-2", ProjectPath: "/b"},
	})
	m.groupMode = groupPath
	m = m.applyFiltersAndSort()
	m.list.Select(0) // group header for /a

	if _, ok := m.list.SelectedItem().(groupHeaderItem); !ok {
		t.Fatalf("expected cursor on group header, got %T", m.list.SelectedItem())
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if cmd != nil {
		t.Fatalf("space on header returned a command: %v", cmd)
	}
	m2 := updated.(Model)
	if len(m2.selected) != 0 {
		t.Fatalf("selected = %v, want empty after space on group header", m2.selected)
	}
}

func TestSelectAllTogglesEverything(t *testing.T) {
	m := seedResultsModel([]domain.SearchHit{
		{SessionTitle: "one", SessionID: "id-1", ProjectPath: "/p"},
		{SessionTitle: "two", SessionID: "id-2", ProjectPath: "/p"},
	})

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	if cmd != nil {
		t.Fatalf("ctrl+a returned a command: %v", cmd)
	}
	m = updated.(Model)
	if len(m.selected) != 2 || !m.selected["id-1"] || !m.selected["id-2"] {
		t.Fatalf("selected = %v, want both ids selected", m.selected)
	}

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	if cmd != nil {
		t.Fatalf("second ctrl+a returned a command: %v", cmd)
	}
	m = updated.(Model)
	if len(m.selected) != 0 {
		t.Fatalf("selected = %v, want empty after second ctrl+a", m.selected)
	}
}

func TestEscClearsSelectionInsteadOfQuitConfirm(t *testing.T) {
	m := seedResultsModel([]domain.SearchHit{
		{SessionTitle: "one", SessionID: "id-1", ProjectPath: "/p"},
	})
	m.list.Select(0)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(Model)
	if len(m.selected) == 0 {
		t.Fatal("setup: expected a selection before esc")
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatalf("esc with selection returned a command: %v", cmd)
	}
	m = updated.(Model)
	if len(m.selected) != 0 {
		t.Fatalf("selected = %v, want empty after esc", m.selected)
	}
	if m.confirmQuit {
		t.Fatal("esc with a selection must not trigger quit-confirm")
	}
}

func TestEscTriggersQuitConfirmWhenSelectionEmpty(t *testing.T) {
	m := seedResultsModel([]domain.SearchHit{
		{SessionTitle: "one", SessionID: "id-1", ProjectPath: "/p"},
	})

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatalf("esc returned a command: %v", cmd)
	}
	m = updated.(Model)
	if !m.confirmQuit {
		t.Fatal("esc with empty selection should trigger quit-confirm")
	}
}

func TestSelectedPathLinesFormatsAndSkips(t *testing.T) {
	m := seedResultsModel([]domain.SearchHit{
		{SessionTitle: "one", SessionID: "id-1", ProjectPath: "/p", FilePath: "/p/one.jsonl"},
		{SessionTitle: "two", SessionID: "id-2", ProjectPath: "/q", FilePath: ""},
		{SessionTitle: "three", SessionID: "id-3", ProjectPath: "/r", FilePath: "/r/three.jsonl"},
	})
	m.selected = map[string]bool{"id-1": true, "id-2": true, "id-3": true}

	lines, skipped := m.selectedPathLines()
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
	}
	want := []string{"/p/one.jsonl\t/p", "/r/three.jsonl\t/r"}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("lines = %#v, want %#v", lines, want)
	}

	if got := m.copiedPathsMsg(len(lines), skipped); got != "Copied 2 paths (1 skipped: no file)" {
		t.Fatalf("copiedPathsMsg = %q", got)
	}
	if got := m.copiedPathsMsg(1, 0); got != "Copied 1 path" {
		t.Fatalf("copiedPathsMsg(1,0) = %q", got)
	}
	if got := m.copiedPathsMsg(0, 1); got != "No file paths to copy" {
		t.Fatalf("copiedPathsMsg(0,1) = %q", got)
	}
}

func TestCopyPathsFallsBackToCursorItemWhenNothingSelected(t *testing.T) {
	m := seedResultsModel([]domain.SearchHit{
		{SessionTitle: "one", SessionID: "id-1", ProjectPath: "/p", FilePath: "/p/one.jsonl"},
		{SessionTitle: "two", SessionID: "id-2", ProjectPath: "/q", FilePath: "/q/two.jsonl"},
	})
	m.list.Select(1)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Y'}})
	if cmd == nil {
		t.Fatal("Y with a cursor item should return a copy command")
	}
	m = updated.(Model)
	if m.copiedMsg != "Copied 1 path" {
		t.Fatalf("copiedMsg = %q", m.copiedMsg)
	}
}

func TestSearchDoneMsgClearsSelection(t *testing.T) {
	m := seedResultsModel([]domain.SearchHit{
		{SessionTitle: "one", SessionID: "id-1", ProjectPath: "/p"},
	})
	m.selected = map[string]bool{"id-1": true}

	updated, _ := m.Update(searchDoneMsg{hits: m.hits, query: "q"})
	m = updated.(Model)
	if len(m.selected) != 0 {
		t.Fatalf("selected = %v, want empty after searchDoneMsg", m.selected)
	}
}

func TestHomeDoneMsgClearsSelection(t *testing.T) {
	m := seedResultsModel([]domain.SearchHit{
		{SessionTitle: "one", SessionID: "id-1", ProjectPath: "/p"},
	})
	m.selected = map[string]bool{"id-1": true}

	updated, _ := m.Update(homeDoneMsg{hits: m.hits})
	m = updated.(Model)
	if len(m.selected) != 0 {
		t.Fatalf("selected = %v, want empty after homeDoneMsg", m.selected)
	}
}
