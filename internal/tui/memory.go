package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type MemorySummary struct {
	ID        string
	Title     string
	Preview   string
	Type      string
	State     string
	UpdatedAt time.Time
}

type memoryListItem struct {
	summary MemorySummary
}

func (item memoryListItem) Title() string {
	title := item.summary.Title
	if title == "" {
		title = item.summary.ID
	}
	return sanitizeTerminal(title)
}

func (item memoryListItem) Description() string { return sanitizeTerminal(item.summary.Preview) }

func (item memoryListItem) FilterValue() string {
	return sanitizeTerminal(strings.Join([]string{item.Title(), item.summary.Preview, item.summary.Type, item.summary.ID}, " "))
}

type MemorySearch struct {
	Workspace string
	Query     string
	Limit     int
}

type MemoryLookup struct {
	Workspace string
	ID        string
}

type MemoryDetail struct {
	ID             string
	Title          string
	Content        string
	Project        string
	Scope          string
	Type           string
	TopicKey       string
	Session        string
	Producer       string
	SourceProvider string
	SourceID       string
	State          string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	References     []string
}

type memorySearchLoadedMsg struct {
	generation int
	value      []MemorySummary
	err        error
}

type memoryDetailLoadedMsg struct {
	generation int
	id         string
	value      MemoryDetail
	err        error
}

var _ list.DefaultItem = memoryListItem{}

func (m *Model) initMemory() {
	search := textinput.New()
	search.Prompt = "/ "
	search.Placeholder = "search project memory"
	search.CharLimit = 256
	search.SetWidth(40)

	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = true
	delegate.SetHeight(2)
	delegate.SetSpacing(1)
	results := list.New(nil, delegate, 40, 12)
	results.SetShowTitle(false)
	results.SetShowFilter(false)
	results.SetShowHelp(false)
	results.SetShowPagination(false)
	results.SetShowStatusBar(false)
	results.SetFilteringEnabled(false)
	results.DisableQuitKeybindings()

	detail := viewport.New(viewport.WithWidth(40), viewport.WithHeight(12))
	detail.SoftWrap = true
	detail.FillHeight = false

	m.memorySearch = search
	m.memoryList = results
	m.memoryViewport = detail
}

func (m *Model) focusMemory(focus focusArea) tea.Cmd {
	m.focus = focus
	m.memorySearch.Blur()
	if focus == focusMemorySearch {
		return m.memorySearch.Focus()
	}
	return nil
}

func (m *Model) updateMemoryKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch m.focus {
	case focusMemorySearch:
		switch msg.String() {
		case "enter":
			return true, m.searchMemory()
		case "esc":
			m.focusMemory(focusMemoryList)
			return true, nil
		case "tab":
			m.focusMemory(focusMemoryList)
			return true, nil
		}
		var cmd tea.Cmd
		m.memorySearch, cmd = m.memorySearch.Update(msg)
		return true, cmd
	case focusMemoryList:
		switch msg.String() {
		case "/":
			return true, m.focusMemory(focusMemorySearch)
		case "enter":
			return true, m.loadMemoryDetail()
		case "esc":
			m.setRoute(routeOverview)
			return true, nil
		case "tab":
			return true, m.focusMemory(focusMemorySearch)
		}
		var cmd tea.Cmd
		m.memoryList, cmd = m.memoryList.Update(msg)
		return true, cmd
	case focusMemoryDetail:
		switch msg.String() {
		case "/":
			return true, m.focusMemory(focusMemorySearch)
		case "esc", "tab":
			m.focusMemory(focusMemoryList)
			return true, nil
		}
		var cmd tea.Cmd
		m.memoryViewport, cmd = m.memoryViewport.Update(msg)
		return true, cmd
	}
	return false, nil
}

func (m *Model) searchMemory() tea.Cmd {
	query := strings.TrimSpace(m.memorySearch.Value())
	if query == "" {
		m.memoryStatus = "! Enter a search term, then press Enter."
		return nil
	}
	ctx, generation := m.beginMemoryOperation()
	m.memorySearching = true
	m.memorySearched = true
	m.memoryDetailLoad = false
	m.memoryDetail = MemoryDetail{}
	m.memoryDetailReady = false
	m.memoryViewport.SetContent("")
	m.memoryStatus = "... Searching project memory"
	request := MemorySearch{Workspace: m.options.Workspace, Query: query, Limit: 20}
	return func() tea.Msg {
		if m.backend == nil {
			return memorySearchLoadedMsg{generation: generation, err: fmt.Errorf("memory backend unavailable")}
		}
		value, err := m.backend.Search(ctx, request)
		return memorySearchLoadedMsg{generation: generation, value: value, err: err}
	}
}

func (m *Model) loadMemoryDetail() tea.Cmd {
	item, ok := m.memoryList.SelectedItem().(memoryListItem)
	if !ok || item.summary.ID == "" {
		m.memoryStatus = "! Select a memory, then press Enter."
		return nil
	}
	ctx, generation := m.beginMemoryOperation()
	m.memoryDetailLoad = true
	m.memorySearching = false
	m.memoryDetail = MemoryDetail{}
	m.memoryDetailReady = false
	m.memoryViewport.SetContent("")
	m.memoryStatus = "... Loading memory detail"
	request := MemoryLookup{Workspace: m.options.Workspace, ID: item.summary.ID}
	return func() tea.Msg {
		if m.backend == nil {
			return memoryDetailLoadedMsg{generation: generation, id: item.summary.ID, err: fmt.Errorf("memory backend unavailable")}
		}
		value, err := m.backend.GetMemory(ctx, request)
		return memoryDetailLoadedMsg{generation: generation, id: item.summary.ID, value: value, err: err}
	}
}

func (m *Model) beginMemoryOperation() (context.Context, int) {
	m.cancelMemoryOperation()
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancelMemory = cancel
	return ctx, m.memoryGeneration
}

func (m *Model) cancelMemoryOperation() {
	if m.cancelMemory != nil {
		m.cancelMemory()
		m.cancelMemory = nil
	}
	m.memoryGeneration++
	m.memorySearching = false
	m.memoryDetailLoad = false
}

func (m *Model) finishMemoryOperation() {
	if m.cancelMemory != nil {
		m.cancelMemory()
		m.cancelMemory = nil
	}
}

func (m *Model) handleMemorySearchLoaded(msg memorySearchLoadedMsg) {
	if msg.generation != m.memoryGeneration {
		return
	}
	m.finishMemoryOperation()
	m.memorySearching = false
	if msg.err != nil {
		m.memorySearched = false
		if len(m.memories) > 0 {
			m.setMemoryItems(m.memories)
		}
		m.memoryStatus = "✕ Search unavailable. Check the terms and try again."
		return
	}
	m.setMemoryItems(msg.value)
	m.memoryDetail = MemoryDetail{}
	m.memoryDetailReady = false
	if len(msg.value) == 0 {
		m.memoryStatus = "! No matching project memories. Try different terms."
	} else {
		m.memoryStatus = fmt.Sprintf("✓ %d matching memories", len(msg.value))
	}
	m.focusMemory(focusMemoryList)
}

func (m *Model) handleMemoryDetailLoaded(msg memoryDetailLoadedMsg) {
	if msg.generation != m.memoryGeneration {
		return
	}
	m.finishMemoryOperation()
	m.memoryDetailLoad = false
	if msg.err != nil || msg.value.ID != "" && msg.value.ID != msg.id {
		m.memoryStatus = "✕ Detail unavailable. Select the memory and try again."
		m.focusMemory(focusMemoryList)
		return
	}
	m.memoryDetail = msg.value
	m.memoryDetailReady = true
	m.memoryStatus = ""
	m.memoryViewport.SetContent(renderMemoryDetail(msg.value))
	m.memoryViewport.GotoTop()
	m.focusMemory(focusMemoryDetail)
}

func (m *Model) setMemoryItems(items []MemorySummary) {
	values := make([]list.Item, len(items))
	for index := range items {
		values[index] = memoryListItem{summary: items[index]}
	}
	m.memoryList.SetItems(values)
	if len(values) > 0 {
		m.memoryList.Select(0)
	}
}

func (m *Model) resizeMemory() {
	width := m.memoryBodyWidth()
	height := max(3, m.height-8)
	searchWidth := max(1, width-2)
	listWidth, detailWidth := width, width
	if m.wide() {
		listWidth = max(24, (width-1)*2/5)
		detailWidth = max(1, width-listWidth-1)
	}
	m.memorySearch.SetWidth(searchWidth)
	m.memoryList.SetSize(listWidth, max(3, height-3))
	m.memoryViewport.SetWidth(detailWidth)
	m.memoryViewport.SetHeight(max(3, height-2))
}

func (m Model) memoryBodyWidth() int {
	if m.wide() {
		return max(1, m.width-27)
	}
	return max(1, m.width)
}

func (m Model) renderMemory() []string {
	if m.wide() {
		return m.renderWideMemory()
	}
	if m.focus == focusMemoryDetail && m.memoryDetailReady {
		return []string{
			"MEMORY / DETAIL  DETAIL FOCUS",
			"j/k or arrows scroll · Esc returns to results",
			m.memoryViewport.View(),
		}
	}
	lines := []string{"MEMORY", m.memoryFocusLabel(), "Search  " + m.memorySearch.View()}
	if m.memoryStatus != "" {
		lines = append(lines, m.memoryStatus)
	}
	if m.memoriesLoading && len(m.memoryList.Items()) == 0 {
		return append(lines, "... Loading recent project memories")
	}
	if m.memoriesErr != nil && len(m.memoryList.Items()) == 0 {
		return append(lines, "✕ Memory unavailable. Press r to retry.")
	}
	if len(m.memoryList.Items()) == 0 {
		return append(lines, "! No project memories to show. Use / to search.")
	}
	return append(lines, strings.Split(strings.TrimRight(m.memoryList.View(), "\n"), "\n")...)
}

func (m Model) renderWideMemory() []string {
	width := m.memoryBodyWidth()
	listWidth := max(24, (width-1)*2/5)
	detailWidth := max(1, width-listWidth-1)
	header := "MEMORY  " + m.memoryFocusLabel()
	lines := []string{ansi.Truncate(header, width, ""), ansi.Truncate("Search  "+m.memorySearch.View(), width, "")}
	if m.memoryStatus != "" {
		lines = append(lines, ansi.Truncate(m.memoryStatus, width, ""))
	}
	left := []string{"RESULTS"}
	if len(m.memoryList.Items()) == 0 {
		left = append(left, "! No memories to show")
	} else {
		left = append(left, strings.Split(strings.TrimRight(m.memoryList.View(), "\n"), "\n")...)
	}
	right := []string{"DETAIL"}
	if m.memoryDetailReady {
		right = append(right, strings.Split(strings.TrimRight(m.memoryViewport.View(), "\n"), "\n")...)
	} else if m.memoryDetailLoad {
		right = append(right, "... Loading selected memory")
	} else {
		right = append(right, "Select a memory and press Enter.")
	}
	rows := max(len(left), len(right))
	for index := 0; index < rows; index++ {
		leftLine, rightLine := "", ""
		if index < len(left) {
			leftLine = left[index]
		}
		if index < len(right) {
			rightLine = right[index]
		}
		lines = append(lines, padLine(leftLine, listWidth)+"│"+ansi.Truncate(rightLine, detailWidth, ""))
	}
	return lines
}

func (m Model) memoryFocusLabel() string {
	switch m.focus {
	case focusMemorySearch:
		return "SEARCH FOCUS"
	case focusMemoryDetail:
		return "DETAIL FOCUS"
	default:
		return "LIST FOCUS"
	}
}

func (m Model) memoryHelp() string {
	switch m.focus {
	case focusMemorySearch:
		return "[Enter] search  [Tab/Esc] results  [ctrl+c] quit"
	case focusMemoryDetail:
		return "[j/k] scroll  [Tab/Esc] results  [/] search  [q] quit"
	default:
		return "[j/k] move  [Enter] detail  [/] search  [Tab] focus  [Esc] Overview"
	}
}

func renderMemoryDetail(detail MemoryDetail) string {
	value := func(input string) string {
		if input == "" {
			return "-"
		}
		return sanitizeTerminal(input)
	}
	lines := []string{
		value(detail.Title),
		"ID              " + value(detail.ID),
		"",
		"CONTENT",
		value(detail.Content),
		"",
		"METADATA",
		"Project         " + value(detail.Project),
		"Scope           " + value(detail.Scope),
		"Type            " + value(detail.Type),
		"Topic           " + value(detail.TopicKey),
		"State           " + value(detail.State),
		"Session         " + value(detail.Session),
		"Producer        " + value(detail.Producer),
		"Source provider " + value(detail.SourceProvider),
		"Source ID       " + value(detail.SourceID),
		"Created         " + formatMemoryTime(detail.CreatedAt),
		"Updated         " + formatMemoryTime(detail.UpdatedAt),
	}
	if len(detail.References) == 0 {
		lines = append(lines, "", "REFERENCES", "-")
	} else {
		lines = append(lines, "", "REFERENCES")
		for _, reference := range detail.References {
			lines = append(lines, "- "+sanitizeTerminal(reference))
		}
	}
	return strings.Join(lines, "\n")
}

func formatMemoryTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.UTC().Format(time.RFC3339)
}
