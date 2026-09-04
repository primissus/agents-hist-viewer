package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Search         key.Binding
	NavUp          key.Binding
	NavDown        key.Binding
	Enter          key.Binding
	Back           key.Binding
	Top            key.Binding
	Bottom         key.Binding
	Quit           key.Binding
	Esc            key.Binding
	Filter         key.Binding
	FilterNext     key.Binding
	FilterPrev     key.Binding
	ClearFilter    key.Binding
	TypeFilter     key.Binding
	Sort           key.Binding
	Group          key.Binding
	ClearSearch    key.Binding
	ScrollHalfUp   key.Binding
	ScrollHalfDown key.Binding
	Expand         key.Binding
	TimeFormat     key.Binding
	MsgFilter      key.Binding
	CopyFilePath   key.Binding
}

var defaultKeyMap = keyMap{
	Search:         key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
	NavUp:          key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("↑/k", "up")),
	NavDown:        key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("↓/j", "down")),
	Enter:          key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
	Back:           key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	Top:            key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "top")),
	Bottom:         key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "bottom")),
	Quit:           key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	Esc:            key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	Filter:         key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "filters")),
	FilterNext:     key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "next filter")),
	FilterPrev:     key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "prev filter")),
	ClearFilter:    key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "clear filter")),
	TypeFilter:     key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "cycle type")),
	Sort:           key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "cycle sort")),
	Group:          key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "cycle group")),
	ClearSearch:    key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "clear search")),
	ScrollHalfUp:   key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "half-page up")),
	ScrollHalfDown: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "half-page down")),
	Expand:         key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("ctrl+o", "expand/collapse")),
	TimeFormat:     key.NewBinding(key.WithKeys("T"), key.WithHelp("T", "local/UTC/date/off")),
	MsgFilter:      key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "cycle msg type")),
	CopyFilePath:   key.NewBinding(key.WithKeys("shift+f", "F"), key.WithHelp("shift+F", "copy file path")),
}
