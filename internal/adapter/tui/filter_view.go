package tui

import (
	"fmt"
	"sort"
	"strings"

	"claude-code-hist-viewer/internal/domain"
)

type filterCategory int

const (
	filterPath filterCategory = iota
	filterType
	filterVendor
	numFilterCategories
)

func (c filterCategory) label() string {
	switch c {
	case filterType:
		return "Type"
	case filterVendor:
		return "Vendor"
	default:
		return "Path"
	}
}

func (c filterCategory) next() filterCategory {
	return (c + 1) % numFilterCategories
}

func (c filterCategory) prev() filterCategory {
	return (c - 1 + numFilterCategories) % numFilterCategories
}

func buildFilterOpts(m Model) []string {
	switch m.filterCat {
	case filterType:
		return []string{"", string(domain.RecordChat), string(domain.RecordPlan)}
	case filterVendor:
		return []string{"", string(domain.VendorClaude), string(domain.VendorClaudeDesktop), string(domain.VendorCursor), string(domain.VendorOpencode), string(domain.VendorCodex)}
	default:
		seen := make(map[string]bool)
		var opts []string
		opts = append(opts, "")
		var paths []string
		for _, h := range m.hits {
			if h.ProjectPath != "" && !seen[h.ProjectPath] {
				seen[h.ProjectPath] = true
				paths = append(paths, h.ProjectPath)
			}
		}
		sort.Strings(paths)
		opts = append(opts, paths...)
		return opts
	}
}

func filterOptLabel(cat filterCategory, value string) string {
	switch cat {
	case filterType:
		switch domain.RecordKind(value) {
		case domain.RecordChat:
			return "Chat"
		case domain.RecordPlan:
			return "Plan"
		default:
			return "All types"
		}
	case filterVendor:
		switch domain.Vendor(value) {
		case domain.VendorClaude, domain.VendorClaudeDesktop, domain.VendorCursor, domain.VendorOpencode, domain.VendorCodex:
			return domain.VendorLabel(domain.Vendor(value))
		default:
			return "All vendors"
		}
	default:
		if value == "" {
			return "All paths"
		}
		return truncatePath(value, 120)
	}
}

func activeFilterValue(m Model) string {
	switch m.filterCat {
	case filterType:
		return string(m.filterKind)
	case filterVendor:
		return string(m.filterVendor)
	default:
		return m.filterProj
	}
}

func syncFilterCursor(m *Model) {
	active := activeFilterValue(*m)
	m.filterOpts = buildFilterOpts(*m)
	m.filterCur = 0
	for i, opt := range m.filterOpts {
		if opt == active {
			m.filterCur = i
			break
		}
	}
	m.filterOffset = 0
	m.clampFilterWindow()
}

func (m *Model) clampFilterWindow() {
	window := m.filterWindow()
	if window <= 0 {
		m.filterOffset = 0
		return
	}
	if m.filterCur < m.filterOffset {
		m.filterOffset = m.filterCur
	}
	if m.filterCur >= m.filterOffset+window {
		m.filterOffset = m.filterCur - window + 1
	}
}

func (m Model) filterWindow() int {
	h := m.height - 6
	if h < 1 {
		return 1
	}
	return h
}

func applyFilterSelection(m *Model) {
	if len(m.filterOpts) == 0 {
		return
	}
	val := m.filterOpts[m.filterCur]
	switch m.filterCat {
	case filterType:
		m.filterKind = domain.RecordKind(val)
	case filterVendor:
		m.filterVendor = domain.Vendor(val)
	default:
		m.filterProj = val
	}
}

func clearFilterCategory(m *Model) {
	switch m.filterCat {
	case filterType:
		m.filterKind = ""
	case filterVendor:
		m.filterVendor = ""
	default:
		m.filterProj = ""
	}
	syncFilterCursor(m)
}

func renderFilter(m Model) string {
	var b strings.Builder

	header := fmt.Sprintf("Filters — %s", m.filterCat.label())
	title := current.Header.Render(header)
	active := activeFilterValue(m)
	if active != "" {
		title += current.Footer.Render("  active: " + filterOptLabel(m.filterCat, active))
	}
	b.WriteString(title)
	b.WriteString("\n")

	cats := make([]string, int(numFilterCategories))
	for i := 0; i < int(numFilterCategories); i++ {
		c := filterCategory(i)
		label := c.label()
		if c == m.filterCat {
			cats[i] = current.FilterActive.Render("[" + label + "]")
		} else {
			cats[i] = label
		}
	}
	b.WriteString(strings.Join(cats, "  "))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", m.width))
	b.WriteString("\n")

	window := m.filterWindow()
	end := m.filterOffset + window
	if end > len(m.filterOpts) {
		end = len(m.filterOpts)
	}
	for i := m.filterOffset; i < end; i++ {
		opt := m.filterOpts[i]
		label := filterOptLabel(m.filterCat, opt)
		cursor := "  "
		if i == m.filterCur {
			cursor = "▶ "
		}
		line := cursor + label
		switch {
		case i == m.filterCur && opt == active:
			b.WriteString(current.FilterActive.Render(line))
		case i == m.filterCur:
			b.WriteString(current.FilterCursor.Render(line))
		case opt == active:
			b.WriteString(current.FilterActive.Render(line))
		default:
			b.WriteString(line)
		}
		b.WriteString("\n")
	}

	if len(m.filterOpts) > window {
		b.WriteString(current.Footer.Render(fmt.Sprintf("  showing %d-%d of %d\n",
			m.filterOffset+1, end, len(m.filterOpts))))
	}

	b.WriteString("\n")
	if m.confirmQuit {
		b.WriteString(current.Confirm.Render("Quit chv? [y/N] "))
	} else {
		b.WriteString(current.Footer.Render("←/→ category  ↑↓/jk nav  u/d page  enter apply  c clear  esc close  q quit"))
	}

	return b.String()
}
