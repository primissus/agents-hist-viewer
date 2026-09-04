package tui

import (
	"github.com/charmbracelet/lipgloss"
)

const stickySep = "  |  "

type fieldLimits struct {
	StickyTitle   int
	StickyFile    int
	ScrollProject int
	ScrollFile    int
	ResultProject int
	FooterFilter  int
}

type widthBreakpoint struct {
	minWidth int
	limits   fieldLimits
}

var widthBreakpoints = []widthBreakpoint{
	{minWidth: 0, limits: fieldLimits{14, 16, 18, 24, 28, 12}},
	{minWidth: 60, limits: fieldLimits{20, 22, 26, 32, 38, 16}},
	{minWidth: 80, limits: fieldLimits{28, 30, 34, 44, 50, 20}},
	{minWidth: 100, limits: fieldLimits{36, 38, 42, 56, 62, 24}},
	{minWidth: 120, limits: fieldLimits{44, 46, 50, 68, 74, 28}},
	{minWidth: 160, limits: fieldLimits{52, 56, 60, 90, 90, 32}},
}

func limitsForWidth(width int) fieldLimits {
	lim := widthBreakpoints[0].limits
	for _, bp := range widthBreakpoints {
		if width >= bp.minWidth {
			lim = bp.limits
		}
	}
	return lim
}

func stickySeparatorWidth() int {
	return lipgloss.Width(stickySep)
}

func joinSticky(title, id, file string) string {
	return title + stickySep + id + stickySep + file
}

func fitStickyFields(rawTitle, sessionID, rawFile string, width int, lim fieldLimits) (string, string, string) {
	title := truncate(rawTitle, lim.StickyTitle)
	if title == "" {
		title = shortID(sessionID)
	}
	id := shortID(sessionID)
	file := "—"
	fileLim := lim.StickyFile
	if rawFile != "" {
		file = truncatePath(rawFile, fileLim)
	}

	titleLim := lim.StickyTitle
	if width <= 0 {
		return title, id, file
	}

	for lipgloss.Width(joinSticky(title, id, file)) > width {
		shrunk := false
		if rawFile != "" && fileLim > 4 {
			fileLim--
			file = truncatePath(rawFile, fileLim)
			shrunk = true
		} else if rawTitle != "" && titleLim > 4 {
			titleLim--
			title = truncate(rawTitle, titleLim)
			shrunk = true
		} else if titleLim > 4 && title != shortID(sessionID) {
			titleLim--
			title = truncate(title, titleLim)
			shrunk = true
		}
		if !shrunk {
			break
		}
	}
	return title, id, file
}

func fitPlainWidth(s string, width int) string {
	if width <= 0 || lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	for len(runes) > 1 && lipgloss.Width(string(runes)) > width {
		runes = runes[:len(runes)-1]
	}
	out := string(runes)
	if lipgloss.Width(out) > width {
		return truncate(out, width)
	}
	if lipgloss.Width(out+"…") <= width {
		return out + "…"
	}
	return truncate(out, width)
}

func scrollFileLimit(width int, lim fieldLimits) int {
	if lim.ScrollFile > 0 {
		return lim.ScrollFile
	}
	if width > 2 {
		return width - 2
	}
	return width
}

func resultProjectLimit(listWidth int, suffix string, lim fieldLimits) int {
	projMax := lim.ResultProject
	if listWidth <= 0 {
		return projMax
	}
	available := listWidth - lipgloss.Width(suffix) - 4
	if available < projMax {
		projMax = available
	}
	if projMax < 8 {
		return 8
	}
	return projMax
}

func fitResultsFooter(hint string, width int) string {
	if width <= 0 || lipgloss.Width(hint) <= width {
		return hint
	}
	return fitPlainWidth(hint, width)
}
