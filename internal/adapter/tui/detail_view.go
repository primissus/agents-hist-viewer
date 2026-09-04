package tui

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"claude-code-hist-viewer/internal/domain"

	"github.com/charmbracelet/lipgloss"
)

// Marker glyphs — Unicode, no emoji.
const (
	markerUser   = "▶"
	markerClaude = "◆"
	markerThink  = "◇"
	markerTool   = "▷"
	markerResult = "◁"
	markerMore   = "▸"
	markerWarn   = "⚠"
)

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansiEscape.ReplaceAllString(s, "") }

type timeFormat int

const (
	timeFmtDateTime timeFormat = iota
	timeFmtUTC
	timeFmtDate
	timeFmtOff
)

func (f timeFormat) next() timeFormat {
	switch f {
	case timeFmtDateTime:
		return timeFmtUTC
	case timeFmtUTC:
		return timeFmtDate
	case timeFmtDate:
		return timeFmtOff
	default:
		return timeFmtDateTime
	}
}

func (f timeFormat) label() string {
	switch f {
	case timeFmtDateTime:
		return "local"
	case timeFmtUTC:
		return "utc"
	case timeFmtDate:
		return "date"
	default:
		return "off"
	}
}

func (f timeFormat) format(t time.Time) string {
	if t.IsZero() || f == timeFmtOff {
		return ""
	}
	switch f {
	case timeFmtUTC:
		return t.UTC().Format("2006-01-02 15:04 UTC")
	case timeFmtDate:
		return t.Local().Format("2006-01-02")
	default:
		return t.Local().Format("2006-01-02 15:04")
	}
}

func formatMsgTime(t time.Time, f timeFormat) string {
	ts := f.format(t)
	if ts == "" {
		return ""
	}
	return current.ScTag.Render("  " + ts)
}

func findMatchLines(content, query string) []int {
	if query == "" {
		return nil
	}
	lowerQ := strings.ToLower(query)
	var out []int
	for i, line := range strings.Split(content, "\n") {
		if strings.Contains(strings.ToLower(stripANSI(line)), lowerQ) {
			out = append(out, i)
		}
	}
	return out
}

// highlightText renders text with base style, wrapping each match in current.SearchHighlight.
func highlightText(text, query string, base lipgloss.Style) string {
	if query == "" || text == "" {
		return base.Render(text)
	}
	lower := strings.ToLower(text)
	lowerQ := strings.ToLower(query)
	if !strings.Contains(lower, lowerQ) {
		return base.Render(text)
	}
	var b strings.Builder
	pos, qLen := 0, len(query)
	for {
		idx := strings.Index(lower[pos:], lowerQ)
		if idx < 0 {
			b.WriteString(base.Render(text[pos:]))
			break
		}
		abs := pos + idx
		if abs > pos {
			b.WriteString(base.Render(text[pos:abs]))
		}
		b.WriteString(current.SearchHighlight.Render(text[abs : abs+qLen]))
		pos = abs + qLen
		if pos >= len(text) {
			break
		}
	}
	return b.String()
}

func renderDetail(m Model) string {
	s := m.detail.Session
	sticky := renderDetailSticky(s, m.width)
	sep := strings.Repeat("─", m.width)

	var footer string
	switch {
	case m.copiedMsg != "":
		footer = current.Footer.Render(m.copiedMsg)
	case m.confirmQuit:
		footer = current.Confirm.Render("Quit chv? [y/N] ")
	case m.detailSearchActive:
		footer = current.SearchBar.Render("/ " + m.detailSearch + "|  esc cancel  enter confirm")
	case m.detailSearch != "":
		if len(m.detailMatches) == 0 {
			footer = current.SearchNoMatch.Render("/" + m.detailSearch + "  — no matches  esc clear")
		} else {
			footer = current.SearchMatch.Render(fmt.Sprintf(
				"/%s  [%d/%d]  n next  N prev  esc clear",
				m.detailSearch, m.detailMatchCur+1, len(m.detailMatches),
			))
		}
	default:
		expandHint := "expand"
		if m.detailExpanded {
			expandHint = "collapse"
		}
		backHint := "back"
		if m.detailOnly {
			backHint = "quit"
		}
		filterHint := "all"
		if m.detailMsgFilter != msgFilterAll {
			filterHint = m.detailMsgFilter.label()
		}
		footer = current.Footer.Render(fmt.Sprintf(
			"/ search  m msg:%s  S copy-id  P copy-path  shift+F copy-file  ctrl+o %s  T time:%s  esc %s  ? help",
			filterHint,
			expandHint,
			m.detailTimeFmt.label(),
			backHint,
		))
	}

	var b strings.Builder
	b.WriteString(sticky)
	b.WriteString("\n")
	b.WriteString(sep)
	b.WriteString("\n")
	b.WriteString(m.vp.View())
	b.WriteString("\n")
	b.WriteString(footer)
	return b.String()
}

func renderDetailSticky(s domain.Session, width int) string {
	lim := limitsForWidth(width)
	title, id, file := fitStickyFields(detailSourceTitle(s), s.ID, s.FilePath, width, lim)
	line := joinSticky(title, id, file)
	if width > 0 && lipgloss.Width(line) > width {
		line = fitPlainWidth(line, width)
	}
	return current.Header.Render(line)
}

func detailSourceTitle(s domain.Session) string {
	label := domain.VendorLabel(s.Vendor)
	if strings.TrimSpace(s.Title) == "" {
		return label
	}
	return label + ": " + s.Title
}

func renderDetailScrollHeader(s domain.Session, width int) string {
	lim := limitsForWidth(width)
	branch := s.GitBranch
	if branch == "" {
		branch = "—"
	}
	var b strings.Builder
	b.WriteString(current.Footer.Render(fmt.Sprintf(
		"%s  |  %s  |  %s → %s",
		truncatePath(s.ProjectPath, lim.ScrollProject),
		branch,
		s.StartedAt.Format("2006-01-02"),
		s.EndedAt.Format("2006-01-02"),
	)))
	if s.FilePath != "" {
		b.WriteString("\n")
		b.WriteString(current.Footer.Render(truncatePath(s.FilePath, scrollFileLimit(width, lim))))
	}
	return b.String()
}

type msgFilter int

const (
	msgFilterAll msgFilter = iota
	msgFilterYou
	msgFilterTask
	msgFilterClaude
	msgFilterThinking
	msgFilterToolUse
	msgFilterToolResult
	msgFilterSidechain
	numMsgFilters
)

func (f msgFilter) label() string {
	switch f {
	case msgFilterYou:
		return "you"
	case msgFilterTask:
		return "task"
	case msgFilterClaude:
		return "claude"
	case msgFilterThinking:
		return "thinking"
	case msgFilterToolUse:
		return "tool"
	case msgFilterToolResult:
		return "result"
	case msgFilterSidechain:
		return "sidechain"
	default:
		return "all"
	}
}

func (f msgFilter) next() msgFilter {
	return (f + 1) % numMsgFilters
}

func messageMatchesFilter(msg domain.Message, f msgFilter) bool {
	switch f {
	case msgFilterAll:
		return true
	case msgFilterYou:
		return msg.Role == domain.RoleUser && msg.Kind == domain.KindText && !msg.IsSidechain
	case msgFilterTask:
		return msg.IsSidechain && msg.Role == domain.RoleUser && msg.Kind == domain.KindText
	case msgFilterClaude:
		return msg.Role == domain.RoleAssistant && msg.Kind == domain.KindText
	case msgFilterThinking:
		return msg.Kind == domain.KindThinking
	case msgFilterToolUse:
		return msg.Kind == domain.KindToolUse
	case msgFilterToolResult:
		return msg.Kind == domain.KindToolResult
	case msgFilterSidechain:
		return msg.IsSidechain
	default:
		return true
	}
}

func renderDetailViewport(detail domain.SessionDetail, width int, expanded bool, searchQuery string, timeFmt timeFormat, msgF msgFilter) string {
	var b strings.Builder
	b.WriteString(renderDetailScrollHeader(detail.Session, width))
	b.WriteString("\n\n")
	b.WriteString(renderMessages(detail, width, expanded, searchQuery, timeFmt, msgF))
	return b.String()
}

// detailChromeLines is fixed overhead in the thread view: sticky header, separator, footer.
func detailChromeLines(domain.Session) int {
	return 3
}

func renderMessages(detail domain.SessionDetail, _ int, expanded bool, searchQuery string, timeFmt timeFormat, msgF msgFilter) string {
	var b strings.Builder

	if !detail.Session.HasTranscript && detail.Session.RecordKind == domain.RecordChat {
		b.WriteString(current.Orphan.Render(markerWarn + "  transcript no longer on disk — showing typed prompts only"))
		b.WriteString("\n\n")
	}

	shown := 0
	for _, msg := range detail.Messages {
		if !messageMatchesFilter(msg, msgF) {
			continue
		}
		shown++
		b.WriteString(renderMessage(msg, expanded, searchQuery, timeFmt))
		b.WriteString("\n\n")
	}

	if msgF != msgFilterAll && shown == 0 {
		b.WriteString(current.Orphan.Render(fmt.Sprintf("No %s messages in this thread.", msgF.label())))
		b.WriteString("\n\n")
	}

	return b.String()
}

func renderMessage(msg domain.Message, expanded bool, searchQuery string, timeFmt timeFormat) string {
	sc := ""
	if msg.IsSidechain {
		sc = current.ScTag.Render(" [sidechain]")
	}
	ts := formatMsgTime(msg.Timestamp, timeFmt)

	switch msg.Kind {
	case domain.KindThinking:
		if msg.Text == "" {
			return current.ThinkLabel.Render(markerThink+" thinking (redacted)") + ts + sc
		}
		if !expanded {
			n := len([]rune(msg.Text))
			return current.ThinkLabel.Render(fmt.Sprintf("%s thinking  %s %d chars", markerThink, markerMore, n)) + ts + sc
		}
		label := current.ThinkLabel.Render(markerThink + " thinking")
		return label + ts + sc + "\n" + highlightText(msg.Text, searchQuery, current.ThinkBody)

	case domain.KindToolUse:
		name := msg.ToolName
		if name == "" {
			name = "tool"
		}
		if !expanded {
			return current.ToolLabel.Render(fmt.Sprintf("%s %s  %s", markerTool, name, markerMore)) + ts + sc
		}
		label := current.ToolLabel.Render(markerTool + " " + name)
		return label + ts + sc + "\n" + highlightText(msg.Text, searchQuery, current.ToolBody)

	case domain.KindToolResult:
		if !expanded {
			return current.ResultLabel.Render(fmt.Sprintf("%s result  %s", markerResult, markerMore)) + ts + sc
		}
		label := current.ResultLabel.Render(markerResult + " result")
		return label + ts + sc + "\n" + highlightText(msg.Text, searchQuery, current.ToolBody)

	case domain.KindText:
		if msg.Role == domain.RoleUser {
			labelText := "You"
			if msg.IsSidechain {
				labelText = "Task"
			}
			label := current.UserLabel.Render(markerUser + " " + labelText)
			text := msg.Text
			if !expanded {
				text = truncate(text, 300)
			}
			return label + ts + sc + "\n" + highlightText(text, searchQuery, current.UserBody)
		}
		label := current.ClaudeLabel.Render(markerClaude + " Claude")
		text := msg.Text
		if !expanded {
			text = truncate(text, 1000)
		}
		return label + ts + sc + "\n" + highlightText(text, searchQuery, current.ClaudeBody)

	default:
		label := current.ClaudeLabel.Render(markerClaude + " Claude")
		text := msg.Text
		if !expanded {
			text = truncate(text, 1000)
		}
		return label + ts + sc + "\n" + highlightText(text, searchQuery, current.ClaudeBody)
	}
}
