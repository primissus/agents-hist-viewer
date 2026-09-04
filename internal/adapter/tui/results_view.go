package tui

import (
	"fmt"
	"strings"

	"claude-code-hist-viewer/internal/domain"

	"github.com/charmbracelet/lipgloss"
)

// resultsChromeLines is fixed overhead in the results view: search, separator,
// subtitle, and footer.
const resultsChromeLines = 4

func renderSearchBar(m Model) string {
	if m.input.Focused() || m.input.Value() != "" {
		return m.input.View()
	}
	bar := current.SearchBar
	line := bar.Render("> ") + bar.Render(m.input.Placeholder)
	if pad := m.width - lipgloss.Width(line); pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	return line
}

func renderResultsStickyChrome(m Model) string {
	var b strings.Builder
	b.WriteString(renderSearchBar(m))
	b.WriteString("\n")
	b.WriteString(current.SearchSep.Render(strings.Repeat("─", m.width)))
	b.WriteString("\n")
	switch {
	case m.isHome:
		b.WriteString(current.HomeHeader.Render("  Recent threads"))
	case m.searched:
		b.WriteString(current.Footer.Render("  Results for: " + m.lastQuery))
	}
	return b.String()
}

func clipResultsBody(s string, width, height int) string {
	if height < 1 {
		height = 1
	}
	style := lipgloss.NewStyle().Height(height).MaxHeight(height)
	if width > 0 {
		style = style.Width(width)
	}
	return style.Render(s)
}

func renderResultsBody(m Model, contentH int) string {
	switch {
	case !m.searched && !m.isHome && !m.loading:
		return lipgloss.Place(m.width, contentH,
			lipgloss.Center, lipgloss.Center,
			current.PromptHint.Render("Press / to search"))
	case m.searched && len(m.hits) == 0:
		msg := fmt.Sprintf("No matches for %q.", m.lastQuery)
		return lipgloss.Place(m.width, contentH,
			lipgloss.Center, lipgloss.Center, msg)
	case len(m.filteredHits) == 0 && (m.filterProj != "" || m.filterKind != "" || m.filterVendor != ""):
		return lipgloss.Place(m.width, contentH,
			lipgloss.Center, lipgloss.Center,
			"No results for current filters. Press f to change.")
	default:
		return m.list.View()
	}
}

func renderResultsFooter(m Model) string {
	switch {
	case m.copiedMsg != "":
		return current.Footer.Render(m.copiedMsg)
	case m.confirmQuit:
		return current.Confirm.Render("Quit chv? [y/N] ")
	case m.err != "":
		return current.Error.Render("Error: " + m.err)
	case m.loading:
		return current.Footer.Render("Loading…")
	default:
		lim := limitsForWidth(m.width)
		hint := "/ search  S copy-id  P copy-path  shift+F copy-file  f filters  t type  s sort  g group  ? help"
		if m.searched {
			hint = "/ search  c clear  S copy-id  P copy-path  shift+F copy-file  f filters  t type  s sort  g group  ? help"
		}
		if m.isHome || m.searched {
			total := len(m.hits)
			filtered := len(m.filteredHits)
			if m.filterProj != "" {
				hint += fmt.Sprintf("  (%d/%d  path: %s)", filtered, total, truncatePath(m.filterProj, lim.FooterFilter))
			} else if m.filterKind != "" {
				hint += fmt.Sprintf("  (%d/%d  type: %s)", filtered, total, m.filterKind)
			} else if m.filterVendor != "" {
				hint += fmt.Sprintf("  (%d/%d  vendor: %s)", filtered, total, domain.VendorLabel(m.filterVendor))
			} else if m.isHome {
				hint += fmt.Sprintf("  (%d recent)", total)
			} else {
				hint += fmt.Sprintf("  (%d results)", total)
			}
			if m.sortMode != sortRelevance {
				hint += "  sort:" + m.sortMode.label()
			}
			if m.groupMode != groupOff {
				hint += "  group:" + m.groupMode.label()
			}
		}
		if m.width < 80 && m.width > 0 && lipgloss.Width(hint) > m.width {
			if idx := strings.LastIndex(hint, "  group:"); idx >= 0 {
				hint = hint[:idx]
			}
		}
		if m.width < 80 && m.width > 0 && lipgloss.Width(hint) > m.width {
			if idx := strings.LastIndex(hint, "  sort:"); idx >= 0 {
				hint = hint[:idx]
			}
		}
		if m.width < 80 && m.width > 0 && lipgloss.Width(hint) > m.width {
			if idx := strings.LastIndex(hint, "  ("); idx >= 0 {
				hint = hint[:idx]
			}
		}
		hint = fitResultsFooter(hint, m.width)
		return current.Footer.Render(hint)
	}
}

func renderResults(m Model) string {
	contentH := m.height - resultsChromeLines
	if contentH < 1 {
		contentH = 1
	}

	var b strings.Builder
	b.WriteString(renderResultsStickyChrome(m))
	b.WriteString("\n")
	b.WriteString(clipResultsBody(renderResultsBody(m, contentH), m.width, contentH))
	b.WriteString("\n")
	b.WriteString(renderResultsFooter(m))
	return b.String()
}
