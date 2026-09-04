package tui

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"claude-code-hist-viewer/internal/adapter/tui/palette"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// ANSI palette slots for fallback when OSC 4 query is unavailable.
const (
	slotUser   = 2
	slotAccent = 4 // blue — theme accent
	slotTool   = 3
	slotThink  = 5
	slotClaude = 6 // cyan — secondary
	slotMuted  = 8
	slotError  = 9
	slotOK     = 10
	slotWarn   = 11
)

// Theme holds lipgloss styles derived from the terminal palette.
type Theme struct {
	Footer, Error          lipgloss.Style
	PromptHint, HomeHeader lipgloss.Style
	Confirm                lipgloss.Style

	UserLabel, ClaudeLabel, ThinkLabel, ToolLabel, ResultLabel lipgloss.Style
	UserBody, ClaudeBody, ThinkBody, ToolBody                  lipgloss.Style

	Orphan, Header, ScTag lipgloss.Style

	SearchHighlight, SearchBar, SearchSep, SearchNoMatch, SearchMatch lipgloss.Style

	HelpBox, HelpSection, HelpKey lipgloss.Style

	FilterActive, FilterCursor lipgloss.Style

	GroupHeader lipgloss.Style
}

var current = DefaultTheme()

// InitTheme queries the terminal palette (OSC 4) and sets package-level styles.
// Falls back to ANSI slots when query is unsupported. Set CHV_NO_COLOR_QUERY=1 to skip.
func InitTheme(_ *os.File) Theme {
	indices := make([]int, 16)
	for i := range indices {
		indices[i] = i
	}

	colors, err := palette.QueryTerminal(indices)
	palette.DrainStdin()
	if err == nil && len(colors) == 16 {
		var p [16]string
		complete := true
		for i := 0; i < 16; i++ {
			c, ok := colors[i]
			if !ok {
				complete = false
				break
			}
			p[i] = c
		}
		if complete {
			current = ThemeFromPalette(p)
			return current
		}
	}

	current = DefaultTheme()
	return current
}

// DefaultTheme builds styles from ANSI palette indices.
func DefaultTheme() Theme {
	return buildTheme(func(slot int) lipgloss.Color {
		return lipgloss.Color(strconv.Itoa(slot))
	})
}

// ThemeFromPalette builds styles from OSC 4 hex colors (indices 0–15).
func ThemeFromPalette(p [16]string) Theme {
	return buildTheme(func(slot int) lipgloss.Color {
		if p[slot] != "" {
			return lipgloss.Color(p[slot])
		}
		return lipgloss.Color(strconv.Itoa(slot))
	})
}

func buildTheme(c func(int) lipgloss.Color) Theme {
	return Theme{
		Footer:     lipgloss.NewStyle().Faint(true),
		Error:      lipgloss.NewStyle().Foreground(c(slotError)),
		PromptHint: lipgloss.NewStyle().Faint(true),
		HomeHeader: lipgloss.NewStyle().Faint(true),
		Confirm:    lipgloss.NewStyle().Bold(true).Foreground(c(slotWarn)),

		UserLabel:   lipgloss.NewStyle().Bold(true).Foreground(c(slotUser)),
		ClaudeLabel: lipgloss.NewStyle().Bold(true).Foreground(c(slotClaude)),
		ThinkLabel:  lipgloss.NewStyle().Foreground(c(slotThink)).Faint(true),
		ToolLabel:   lipgloss.NewStyle().Foreground(c(slotTool)),
		ResultLabel: lipgloss.NewStyle().Foreground(c(slotMuted)),

		UserBody:   lipgloss.NewStyle(),
		ClaudeBody: lipgloss.NewStyle(),
		ThinkBody:  lipgloss.NewStyle().Faint(true),
		ToolBody:   lipgloss.NewStyle().Foreground(c(slotMuted)),

		Orphan: lipgloss.NewStyle().Foreground(c(slotWarn)).Bold(true),
		Header: lipgloss.NewStyle().Bold(true),
		ScTag:  lipgloss.NewStyle().Faint(true),

		SearchHighlight: lipgloss.NewStyle().Reverse(true).Bold(true),
		SearchBar: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{
			Light: "#1a1a1a",
			Dark:  "#dddddd",
		}),
		SearchSep: lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
			Light: "#A49FA5",
			Dark:  "#777777",
		}),
		SearchNoMatch: lipgloss.NewStyle().Foreground(c(slotError)),
		SearchMatch:   lipgloss.NewStyle().Foreground(c(slotOK)),

		HelpBox:     lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(c(slotMuted)).Padding(1, 3),
		HelpSection: lipgloss.NewStyle().Bold(true).Foreground(c(slotAccent)),
		HelpKey:     lipgloss.NewStyle().Foreground(c(slotTool)),

		FilterActive: lipgloss.NewStyle().Bold(true).Foreground(c(slotUser)),
		FilterCursor: lipgloss.NewStyle().Bold(true),
		GroupHeader:  lipgloss.NewStyle().Bold(true).Foreground(c(slotAccent)).Padding(0, 0, 0, 2),
	}
}

type groupAwareDelegate struct {
	list.DefaultDelegate
	groupHeader lipgloss.Style
}

func (d groupAwareDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	header, ok := item.(groupHeaderItem)
	if !ok {
		d.DefaultDelegate.Render(w, m, index, item)
		return
	}

	width := m.Width() - d.groupHeader.GetPaddingLeft() - d.groupHeader.GetPaddingRight()
	if width < 1 {
		width = 1
	}
	title := ansi.Truncate(header.Title(), width, "…")
	title = d.groupHeader.Render(title)
	fmt.Fprintf(w, "%s\n%s", title, "") //nolint: errcheck
}

func newListDelegate(t Theme) groupAwareDelegate {
	d := list.NewDefaultDelegate()
	s := list.DefaultItemStyles{}

	s.NormalTitle = lipgloss.NewStyle().Padding(0, 0, 0, 2)
	s.NormalDesc = lipgloss.NewStyle().Faint(true).Padding(0, 0, 0, 2)

	accent := t.HelpSection.GetForeground()
	if accent == nil {
		accent = lipgloss.Color(strconv.Itoa(slotAccent))
	}
	s.SelectedTitle = lipgloss.NewStyle().
		Bold(true).
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(accent).
		Padding(0, 0, 0, 1)
	s.SelectedDesc = lipgloss.NewStyle().Faint(true).Padding(0, 0, 0, 2)

	s.DimmedTitle = lipgloss.NewStyle().Faint(true).Padding(0, 0, 0, 2)
	s.DimmedDesc = lipgloss.NewStyle().Faint(true).Padding(0, 0, 0, 2)
	s.FilterMatch = lipgloss.NewStyle().Underline(true)

	d.Styles = s
	return groupAwareDelegate{DefaultDelegate: d, groupHeader: t.GroupHeader}
}
