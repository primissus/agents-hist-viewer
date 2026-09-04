package tui

import (
	"context"
	"sort"
	"strings"

	appSvc "claude-code-hist-viewer/internal/app"
	"claude-code-hist-viewer/internal/domain"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

type sortMode int

const (
	sortRelevance   sortMode = iota
	sortLastMsg              // Timestamp desc
	sortCreation             // StartedAt desc
	sortProjectName          // ProjectPath asc
	sortThreadName           // SessionTitle asc
	numSortModes
)

func (s sortMode) label() string {
	switch s {
	case sortLastMsg:
		return "last msg"
	case sortCreation:
		return "created"
	case sortProjectName:
		return "project"
	case sortThreadName:
		return "thread"
	default:
		return "relevance"
	}
}

type groupMode int

const (
	groupOff groupMode = iota
	groupPath
	groupVendor
	groupVendorPath
	numGroupModes
)

func (g groupMode) label() string {
	switch g {
	case groupPath:
		return "path"
	case groupVendor:
		return "vendor"
	case groupVendorPath:
		return "vendor+path"
	default:
		return "off"
	}
}

type viewState int

const (
	viewResults viewState = iota
	viewDetail
	viewFilter
	viewHelp
)

type searchDoneMsg struct {
	hits  []domain.SearchHit
	query string
}

type homeDoneMsg struct{ hits []domain.SearchHit }
type detailDoneMsg struct{ detail domain.SessionDetail }
type errMsg struct{ err error }

// hitItem wraps SearchHit to implement list.DefaultItem.
type hitItem struct {
	hit       domain.SearchHit
	listWidth int
}

func (h hitItem) FilterValue() string { return h.hit.SessionTitle }
func (h hitItem) Title() string {
	t := h.hit.SessionTitle
	if derived := domain.DeriveTitle(t, 60); derived != "" {
		t = derived
	} else {
		t = strings.ReplaceAll(t, "\n", " ")
		t = strings.TrimSpace(t)
	}
	if t == "" {
		t = h.hit.SessionID
	}
	t += hitTags(h.hit)
	return t
}

func hitTags(h domain.SearchHit) string {
	parts := []string{domain.VendorLabel(h.Vendor)}
	switch h.RecordKind {
	case domain.RecordPlan:
		parts = append(parts, "plan")
	case domain.RecordChat:
		if !h.HasTranscript {
			parts = append(parts, "prompt-only")
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " [" + strings.Join(parts, " ") + "]"
}

func (h hitItem) Description() string {
	lim := limitsForWidth(h.listWidth)
	ts := ""
	if !h.hit.Timestamp.IsZero() {
		ts = "  " + h.hit.Timestamp.Format("2006-01-02")
	}
	sid := shortID(h.hit.SessionID)
	suffix := ts + "  " + sid
	proj := truncatePath(h.hit.ProjectPath, resultProjectLimit(h.listWidth, suffix, lim))
	return proj + suffix
}

type groupHeaderItem struct {
	title string
}

func (g groupHeaderItem) FilterValue() string { return g.title }
func (g groupHeaderItem) Title() string       { return g.title }
func (g groupHeaderItem) Description() string { return "" }

// shortID returns the first 8 characters of a session UUID.
func shortID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

// Model is the Bubble Tea TUI model.
type Model struct {
	svc             *appSvc.SearchService
	keys            keyMap
	state           viewState
	input           textinput.Model
	list            list.Model
	vp              viewport.Model
	hits            []domain.SearchHit
	filteredHits    []domain.SearchHit
	detail          domain.SessionDetail
	detailOnly      bool
	lastQuery       string
	isHome          bool
	searched        bool
	confirmQuit     bool
	detailExpanded  bool
	detailTimeFmt   timeFormat
	detailMsgFilter msgFilter
	loading         bool
	err             string
	width           int
	height          int
	sortMode        sortMode
	groupMode       groupMode
	filterProj      string
	filterKind      domain.RecordKind // empty = all
	filterVendor    domain.Vendor     // empty = all
	filterCat       filterCategory
	filterOpts      []string
	filterCur       int
	filterOffset    int
	histPath        string
	history         []string
	histCur         int    // -1 = not navigating; ≥0 = index into history
	histDraft       string // saved input text while navigating history

	detailSearch       string
	detailSearchActive bool
	detailMatches      []int
	detailMatchCur     int
	copiedMsg          string
	helpFromState      viewState
}

func NewApp(svc *appSvc.SearchService, histPath string) Model {
	inp := textinput.New()
	inp.Placeholder = "search…"
	inp.CharLimit = 200
	inp.Width = 80
	inp.PromptStyle = current.SearchBar
	inp.TextStyle = current.SearchBar
	inp.PlaceholderStyle = current.SearchBar
	inp.CompletionStyle = current.SearchBar
	inp.Cursor.TextStyle = current.SearchBar

	delegate := newListDelegate(current)
	lst := list.New([]list.Item{}, delegate, 80, 20)
	lst.SetShowHelp(false)
	lst.SetShowTitle(false)
	lst.SetShowStatusBar(false)
	lst.SetFilteringEnabled(false)

	vp := viewport.New(80, 20)

	return Model{
		svc:           svc,
		keys:          defaultKeyMap,
		input:         inp,
		list:          lst,
		vp:            vp,
		loading:       true,
		detailTimeFmt: timeFmtDateTime,
		histPath:      histPath,
		history:       loadHistory(histPath),
		histCur:       -1,
	}
}

func NewDetailApp(detail domain.SessionDetail) Model {
	m := NewApp(nil, "")
	m.state = viewDetail
	m.loading = false
	m.detail = detail
	m.detailOnly = true
	m.width = 80
	m.height = 24
	m.vp.Width = 80
	m.vp.Height = m.height - detailChromeLines(detail.Session)
	m.vp.SetContent(renderDetailViewport(detail, m.width, false, "", m.detailTimeFmt, m.detailMsgFilter))
	return m
}

const (
	homeRecentLimit   = 100
	searchResultLimit = 500
)

func (m Model) Init() tea.Cmd {
	if m.detailOnly || m.svc == nil {
		return nil
	}
	return doLoadRecent(m.svc, m.filterKind, m.filterProj, m.filterVendor)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m = m.syncResultsLayout()
		m = m.applyFiltersAndSort()
		m.vp.Width = msg.Width
		m.vp.Height = msg.Height - detailChromeLines(m.detail.Session)
		if len(m.detail.Messages) > 0 {
			content := renderDetailViewport(m.detail, msg.Width, m.detailExpanded, m.detailSearch, m.detailTimeFmt, m.detailMsgFilter)
			m.vp.SetContent(content)
			if m.detailSearch != "" {
				m.detailMatches = findMatchLines(content, m.detailSearch)
			}
		}
		return m, nil

	case homeDoneMsg:
		m.loading = false
		m.isHome = true
		m.hits = msg.hits
		m = m.applyFiltersAndSort()
		m = m.syncResultsLayout()
		return m, nil

	case searchDoneMsg:
		m.loading = false
		m.isHome = false
		m.hits = msg.hits
		m.lastQuery = msg.query
		m.searched = true
		m = m.applyFiltersAndSort()
		m = m.syncResultsLayout()
		return m, nil

	case detailDoneMsg:
		m.loading = false
		m.detail = msg.detail
		m.detailExpanded = false
		m.detailSearch = ""
		m.detailSearchActive = false
		m.detailMatches = nil
		m.detailMatchCur = -1
		if m.height > 0 {
			m.vp.Height = m.height - detailChromeLines(msg.detail.Session)
		}
		m.vp.SetContent(renderDetailViewport(msg.detail, m.width, false, "", m.detailTimeFmt, m.detailMsgFilter))
		m.vp.GotoTop()
		m.state = viewDetail
		return m, nil

	case errMsg:
		m.loading = false
		m.err = msg.err.Error()
		return m, nil

	case tea.KeyMsg:
		m.copiedMsg = ""
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.confirmQuit {
			if msg.String() == "y" || msg.String() == "Y" {
				return m, tea.Quit
			}
			m.confirmQuit = false
			return m, nil
		}
		switch m.state {
		case viewResults:
			return m.updateResults(msg)
		case viewDetail:
			return m.updateDetail(msg)
		case viewFilter:
			return m.updateFilter(msg)
		case viewHelp:
			return m.updateHelp(msg)
		}
	}
	return m, nil
}

func (m Model) updateResults(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.input.Focused() {
		switch {
		case msg.String() == "ctrl+c":
			return m, tea.Quit
		case key.Matches(msg, m.keys.Esc):
			m.input.Blur()
			m.histCur = -1
			return m, nil
		case msg.String() == "up":
			if len(m.history) == 0 {
				return m, nil
			}
			if m.histCur == -1 {
				m.histDraft = m.input.Value()
				m.histCur = len(m.history) - 1
			} else if m.histCur > 0 {
				m.histCur--
			}
			m.input.SetValue(m.history[m.histCur])
			return m, nil
		case msg.String() == "down":
			if m.histCur == -1 {
				return m, nil
			}
			if m.histCur < len(m.history)-1 {
				m.histCur++
				m.input.SetValue(m.history[m.histCur])
			} else {
				m.histCur = -1
				m.input.SetValue(m.histDraft)
			}
			return m, nil
		case key.Matches(msg, m.keys.Enter):
			query := m.input.Value()
			m.input.Blur()
			if query == "" {
				return m, nil
			}
			m.history = appendHistory(m.histPath, query, m.history)
			m.histCur = -1
			m.histDraft = ""
			m.loading = true
			m.err = ""
			return m, doSearch(m.svc, query)
		default:
			m.histCur = -1
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
	}

	switch {
	case key.Matches(msg, m.keys.Quit), key.Matches(msg, m.keys.Esc):
		m.confirmQuit = true
		return m, nil
	case msg.String() == "/":
		cmd := m.input.Focus()
		return m, cmd
	case key.Matches(msg, m.keys.Filter):
		if len(m.hits) == 0 {
			return m, nil
		}
		m.filterCat = filterPath
		syncFilterCursor(&m)
		m.state = viewFilter
		return m, nil
	case key.Matches(msg, m.keys.Sort):
		m.sortMode = (m.sortMode + 1) % numSortModes
		m = m.applyFiltersAndSort()
		return m, nil
	case key.Matches(msg, m.keys.Group):
		m.groupMode = (m.groupMode + 1) % numGroupModes
		m = m.applyFiltersAndSort()
		return m, nil
	case key.Matches(msg, m.keys.ClearSearch):
		if !m.searched {
			return m, nil
		}
		m.searched = false
		m.lastQuery = ""
		m.input.SetValue("")
		m.loading = true
		m.err = ""
		return m, doLoadRecent(m.svc, m.filterKind, m.filterProj, m.filterVendor)
	case key.Matches(msg, m.keys.TypeFilter):
		switch m.filterKind {
		case "":
			m.filterKind = domain.RecordChat
		case domain.RecordChat:
			m.filterKind = domain.RecordPlan
		default:
			m.filterKind = ""
		}
		if m.isHome {
			m.loading = true
			m.err = ""
			return m, doLoadRecent(m.svc, m.filterKind, m.filterProj, m.filterVendor)
		}
		m = m.applyFiltersAndSort()
		return m, nil
	case key.Matches(msg, m.keys.Enter):
		if sel := m.list.SelectedItem(); sel != nil {
			item, ok := sel.(hitItem)
			if !ok {
				return m, nil
			}
			m.loading = true
			m.err = ""
			return m, doLoadDetail(m.svc, item.hit.SessionID)
		}
		return m, nil
	case msg.String() == "S":
		if sel := m.list.SelectedItem(); sel != nil {
			item, ok := sel.(hitItem)
			if !ok {
				return m, nil
			}
			id := item.hit.SessionID
			m.copiedMsg = "Copied: " + id
			return m, doCopyToClipboard(id)
		}
		return m, nil
	case msg.String() == "P":
		if sel := m.list.SelectedItem(); sel != nil {
			item, ok := sel.(hitItem)
			if !ok {
				return m, nil
			}
			path := item.hit.ProjectPath
			m.copiedMsg = "Copied: " + path
			return m, doCopyToClipboard(path)
		}
		return m, nil
	case key.Matches(msg, m.keys.CopyFilePath):
		if sel := m.list.SelectedItem(); sel != nil {
			item, ok := sel.(hitItem)
			if !ok {
				return m, nil
			}
			path := item.hit.FilePath
			if path == "" {
				return m, nil
			}
			m.copiedMsg = "Copied: " + path
			return m, doCopyToClipboard(path)
		}
		return m, nil
	case msg.String() == "?":
		m.helpFromState = viewResults
		m.state = viewHelp
		return m, nil
	default:
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}
}

func (m Model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.detailSearchActive {
		return m.updateDetailSearch(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		m.confirmQuit = true
		return m, nil
	case key.Matches(msg, m.keys.Back):
		if m.detailSearch != "" {
			m.detailSearch = ""
			m.detailSearchActive = false
			m.detailMatches = nil
			m.detailMatchCur = -1
			content := renderDetailViewport(m.detail, m.width, m.detailExpanded, "", m.detailTimeFmt, m.detailMsgFilter)
			m.vp.SetContent(content)
			return m, nil
		}
		if m.detailOnly {
			return m, tea.Quit
		}
		m.state = viewResults
		return m, nil
	case msg.String() == "/":
		m.detailSearchActive = true
		return m, nil
	case msg.String() == "n":
		if len(m.detailMatches) > 0 {
			m.detailMatchCur = (m.detailMatchCur + 1) % len(m.detailMatches)
			m.vp.YOffset = m.detailMatches[m.detailMatchCur]
		}
		return m, nil
	case msg.String() == "N":
		if len(m.detailMatches) > 0 {
			n := len(m.detailMatches)
			m.detailMatchCur = (m.detailMatchCur - 1 + n) % n
			m.vp.YOffset = m.detailMatches[m.detailMatchCur]
		}
		return m, nil
	case msg.String() == "S":
		id := m.detail.Session.ID
		m.copiedMsg = "Copied: " + id
		return m, doCopyToClipboard(id)
	case msg.String() == "P":
		path := m.detail.Session.ProjectPath
		m.copiedMsg = "Copied: " + path
		return m, doCopyToClipboard(path)
	case key.Matches(msg, m.keys.CopyFilePath):
		path := m.detail.Session.FilePath
		if path == "" {
			return m, nil
		}
		m.copiedMsg = "Copied: " + path
		return m, doCopyToClipboard(path)
	case msg.String() == "?":
		m.helpFromState = viewDetail
		m.state = viewHelp
		return m, nil
	case key.Matches(msg, m.keys.Top):
		m.vp.GotoTop()
		return m, nil
	case key.Matches(msg, m.keys.Bottom):
		m.vp.GotoBottom()
		return m, nil
	case key.Matches(msg, m.keys.MsgFilter):
		m.detailMsgFilter = m.detailMsgFilter.next()
		content := renderDetailViewport(m.detail, m.width, m.detailExpanded, m.detailSearch, m.detailTimeFmt, m.detailMsgFilter)
		m.vp.SetContent(content)
		if m.detailSearch != "" {
			m.detailMatches = findMatchLines(content, m.detailSearch)
			if m.detailMatchCur >= len(m.detailMatches) {
				m.detailMatchCur = 0
			}
		}
		return m, nil
	case key.Matches(msg, m.keys.Expand):
		m.detailExpanded = !m.detailExpanded
		content := renderDetailViewport(m.detail, m.width, m.detailExpanded, m.detailSearch, m.detailTimeFmt, m.detailMsgFilter)
		m.vp.SetContent(content)
		if m.detailSearch != "" {
			m.detailMatches = findMatchLines(content, m.detailSearch)
			if m.detailMatchCur >= len(m.detailMatches) {
				m.detailMatchCur = 0
			}
		}
		return m, nil
	case key.Matches(msg, m.keys.TimeFormat):
		m.detailTimeFmt = m.detailTimeFmt.next()
		content := renderDetailViewport(m.detail, m.width, m.detailExpanded, m.detailSearch, m.detailTimeFmt, m.detailMsgFilter)
		m.vp.SetContent(content)
		if m.detailSearch != "" {
			m.detailMatches = findMatchLines(content, m.detailSearch)
			if m.detailMatchCur >= len(m.detailMatches) {
				m.detailMatchCur = 0
			}
		}
		return m, nil
	case key.Matches(msg, m.keys.ScrollHalfUp):
		m.vp.HalfViewUp()
		return m, nil
	case key.Matches(msg, m.keys.ScrollHalfDown):
		m.vp.HalfViewDown()
		return m, nil
	case key.Matches(msg, m.keys.NavUp):
		m.vp.LineUp(1)
		return m, nil
	case key.Matches(msg, m.keys.NavDown):
		m.vp.LineDown(1)
		return m, nil
	default:
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
}

func (m Model) updateDetailSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.detailSearchActive = false
		return m, nil
	case "enter":
		m.detailSearchActive = false
		if len(m.detailMatches) > 0 {
			m.vp.YOffset = m.detailMatches[m.detailMatchCur]
		}
		return m, nil
	case "backspace":
		if len(m.detailSearch) > 0 {
			runes := []rune(m.detailSearch)
			m.detailSearch = string(runes[:len(runes)-1])
			m = m.recomputeDetailMatches()
		}
		return m, nil
	default:
		if len(msg.Runes) > 0 {
			m.detailSearch += string(msg.Runes)
			m = m.recomputeDetailMatches()
		}
		return m, nil
	}
}

func (m Model) updateHelp(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.state = m.helpFromState
	return m, nil
}

func (m Model) recomputeDetailMatches() Model {
	content := renderDetailViewport(m.detail, m.width, m.detailExpanded, m.detailSearch, m.detailTimeFmt, m.detailMsgFilter)
	m.vp.SetContent(content)
	if m.detailSearch == "" {
		m.detailMatches = nil
		m.detailMatchCur = -1
		return m
	}
	m.detailMatches = findMatchLines(content, m.detailSearch)
	if len(m.detailMatches) > 0 {
		m.detailMatchCur = 0
		m.vp.YOffset = m.detailMatches[0]
	} else {
		m.detailMatchCur = -1
	}
	return m
}

func (m Model) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		m.confirmQuit = true
		return m, nil
	case key.Matches(msg, m.keys.Esc), key.Matches(msg, m.keys.Back):
		m.state = viewResults
		return m, nil
	case msg.String() == "tab", key.Matches(msg, m.keys.FilterNext):
		m.filterCat = m.filterCat.next()
		syncFilterCursor(&m)
		return m, nil
	case msg.String() == "shift+tab", key.Matches(msg, m.keys.FilterPrev):
		m.filterCat = m.filterCat.prev()
		syncFilterCursor(&m)
		return m, nil
	case key.Matches(msg, m.keys.ClearFilter):
		clearFilterCategory(&m)
		if m.isHome {
			m.loading = true
			m.err = ""
			return m, doLoadRecent(m.svc, m.filterKind, m.filterProj, m.filterVendor)
		}
		m = m.applyFiltersAndSort()
		return m, nil
	case key.Matches(msg, m.keys.NavUp):
		if m.filterCur > 0 {
			m.filterCur--
			m.clampFilterWindow()
		}
		return m, nil
	case key.Matches(msg, m.keys.NavDown):
		if m.filterCur < len(m.filterOpts)-1 {
			m.filterCur++
			m.clampFilterWindow()
		}
		return m, nil
	case key.Matches(msg, m.keys.ScrollHalfUp):
		window := m.filterWindow()
		if m.filterOffset > 0 {
			m.filterOffset -= window
			if m.filterOffset < 0 {
				m.filterOffset = 0
			}
		}
		return m, nil
	case key.Matches(msg, m.keys.ScrollHalfDown):
		window := m.filterWindow()
		maxOffset := len(m.filterOpts) - window
		if maxOffset < 0 {
			maxOffset = 0
		}
		if m.filterOffset < maxOffset {
			m.filterOffset += window
			if m.filterOffset > maxOffset {
				m.filterOffset = maxOffset
			}
		}
		return m, nil
	case key.Matches(msg, m.keys.Enter):
		applyFilterSelection(&m)
		if m.isHome {
			m.loading = true
			m.err = ""
			m.state = viewResults
			return m, doLoadRecent(m.svc, m.filterKind, m.filterProj, m.filterVendor)
		}
		m = m.applyFiltersAndSort()
		m.state = viewResults
		return m, nil
	}
	return m, nil
}

// applyFiltersAndSort filters hits (search results only) and sorts.
func (m Model) syncResultsLayout() Model {
	if m.width > 0 && m.height > 0 {
		listH := m.height - resultsChromeLines
		if listH < 1 {
			listH = 1
		}
		m.input.Width = m.width
		m.list.SetSize(m.width, listH)
	}
	return m
}

// applyFiltersAndSort filters hits (search results only) and sorts.
func (m Model) applyFiltersAndSort() Model {
	filtered := m.hits
	if !m.isHome {
		if m.filterProj != "" {
			var out []domain.SearchHit
			for _, h := range filtered {
				if h.ProjectPath == m.filterProj {
					out = append(out, h)
				}
			}
			filtered = out
		}
		if m.filterKind != "" {
			var out []domain.SearchHit
			for _, h := range filtered {
				if h.RecordKind == m.filterKind {
					out = append(out, h)
				}
			}
			filtered = out
		}
		if m.filterVendor != "" {
			var out []domain.SearchHit
			for _, h := range filtered {
				if h.Vendor == m.filterVendor {
					out = append(out, h)
				}
			}
			filtered = out
		}
	}
	return m.applySortTo(filtered)
}

func (m Model) applySortTo(filtered []domain.SearchHit) Model {

	sorted := make([]domain.SearchHit, len(filtered))
	copy(sorted, filtered)

	switch m.sortMode {
	case sortLastMsg:
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Timestamp.After(sorted[j].Timestamp)
		})
	case sortCreation:
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].StartedAt.After(sorted[j].StartedAt)
		})
	case sortProjectName:
		sort.Slice(sorted, func(i, j int) bool {
			if sorted[i].ProjectPath != sorted[j].ProjectPath {
				return sorted[i].ProjectPath < sorted[j].ProjectPath
			}
			return sorted[i].SessionTitle < sorted[j].SessionTitle
		})
	case sortThreadName:
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].SessionTitle < sorted[j].SessionTitle
		})
	}

	m.filteredHits = sorted
	items := m.buildResultItems(sorted)
	m.list.SetItems(items)
	return m
}

func (m Model) buildResultItems(hits []domain.SearchHit) []list.Item {
	if m.groupMode == groupOff {
		items := make([]list.Item, len(hits))
		for i, h := range hits {
			items[i] = hitItem{hit: h, listWidth: m.width}
		}
		return items
	}

	grouped := append([]domain.SearchHit(nil), hits...)
	sort.SliceStable(grouped, func(i, j int) bool {
		left := m.groupSortKey(grouped[i])
		right := m.groupSortKey(grouped[j])
		return left < right
	})

	items := make([]list.Item, 0, len(grouped))
	lastGroup := ""
	for _, h := range grouped {
		group := m.groupLabel(h)
		if group != lastGroup {
			items = append(items, groupHeaderItem{title: group})
			lastGroup = group
		}
		items = append(items, hitItem{hit: h, listWidth: m.width})
	}
	return items
}

func (m Model) groupSortKey(h domain.SearchHit) string {
	switch m.groupMode {
	case groupPath:
		return groupPathLabel(h)
	case groupVendor:
		return domain.VendorLabel(h.Vendor)
	case groupVendorPath:
		return domain.VendorLabel(h.Vendor) + "\x00" + groupPathLabel(h)
	default:
		return ""
	}
}

func (m Model) groupLabel(h domain.SearchHit) string {
	switch m.groupMode {
	case groupPath:
		return groupPathLabel(h)
	case groupVendor:
		return domain.VendorLabel(h.Vendor)
	case groupVendorPath:
		return domain.VendorLabel(h.Vendor) + ": " + groupPathLabel(h)
	default:
		return ""
	}
}

func groupPathLabel(h domain.SearchHit) string {
	if h.ProjectPath == "" {
		return "(no path)"
	}
	return h.ProjectPath
}

func (m Model) View() string {
	switch m.state {
	case viewResults:
		return renderResults(m)
	case viewDetail:
		return renderDetail(m)
	case viewFilter:
		return renderFilter(m)
	case viewHelp:
		return renderHelp(m)
	}
	return ""
}

func doLoadRecent(svc *appSvc.SearchService, kind domain.RecordKind, project string, vendor domain.Vendor) tea.Cmd {
	return func() tea.Msg {
		hits, err := svc.RecentSessions(context.Background(), domain.RecentQuery{
			Limit:       homeRecentLimit,
			RecordKind:  kind,
			ProjectPath: project,
			Vendor:      vendor,
		})
		if err != nil {
			return errMsg{err}
		}
		return homeDoneMsg{hits: hits}
	}
}

func doSearch(svc *appSvc.SearchService, query string) tea.Cmd {
	return func() tea.Msg {
		hits, err := svc.Search(context.Background(), query, searchResultLimit, domain.SearchOpts{})
		if err != nil {
			return errMsg{err}
		}
		return searchDoneMsg{hits: hits, query: query}
	}
}

func doLoadDetail(svc *appSvc.SearchService, id string) tea.Cmd {
	return func() tea.Msg {
		detail, err := svc.Session(context.Background(), id)
		if err != nil {
			return errMsg{err}
		}
		return detailDoneMsg{detail: detail}
	}
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) > max {
		return string(runes[:max]) + "…"
	}
	return s
}

func truncatePath(s string, max int) string {
	runes := []rune(s)
	if len(runes) > max {
		return "…" + string(runes[len(runes)-(max-1):])
	}
	return s
}
