package tui

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	minimumWidth  = 42
	minimumHeight = 10
)

type Request struct {
	Workspace string
}

type Inspection struct {
	Root      string
	Database  string
	Migration int
}

type SetupStatus struct {
	Provider         string
	Ready            bool
	Blocker          string
	SelfInstallState string
	SelfInstallPath  string
	IntegrationState string
	IntegrationPath  string
	SkillsState      string
	SkillsPath       string
	SkillsFileCount  int
	ArtifactCount    int
	HandshakeOK      bool
	HandshakeStatus  string
	ModelPlan        string
}

type Backend interface {
	Inspect(context.Context, Request) (Inspection, error)
	SetupStatus(context.Context, Request) (SetupStatus, error)
	PlanSetup(context.Context, SetupRequest) (SetupPlan, error)
	ApplySetup(context.Context, SetupRequest) (SetupResult, error)
	PlanRecovery(context.Context, RecoveryPlanRequest) (RecoveryPlan, error)
	ListBackups(context.Context, BackupListRequest) (BackupListResult, error)
	CreateBackup(context.Context, CreateBackupRequest) (BackupResult, error)
	PreviewRestore(context.Context, RestorePreviewRequest) (RestorePreview, error)
	RestoreBackup(context.Context, RestoreRequest) (RestoreResult, error)
	ProtectedReinstall(context.Context, ProtectedReinstallRequest) (ProtectedReinstallResult, error)
	Recent(context.Context, Request) ([]MemorySummary, error)
	Search(context.Context, MemorySearch) ([]MemorySummary, error)
	GetMemory(context.Context, MemoryLookup) (MemoryDetail, error)
}

type Options struct {
	Workspace string
}

type inspectionLoadedMsg struct {
	generation int
	value      Inspection
	err        error
}

type setupLoadedMsg struct {
	generation int
	value      SetupStatus
	err        error
}

type memoriesLoadedMsg struct {
	generation int
	value      []MemorySummary
	err        error
}

type keyMap struct {
	Sections key.Binding
	Select   key.Binding
	Back     key.Binding
	Refresh  key.Binding
	Help     key.Binding
	Quit     key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Sections: key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "sections")),
		Select:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Refresh:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

func (keys keyMap) ShortHelp() []key.Binding {
	return []key.Binding{keys.Sections, keys.Back, keys.Refresh, keys.Help, keys.Quit}
}

func (keys keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{keys.Sections, keys.Select, keys.Back}, {keys.Refresh, keys.Help, keys.Quit}}
}

type route uint8

const (
	routeOverview route = iota
	routeSystem
	routeMemory
	routeSetup
)

func (current route) title() string {
	switch current {
	case routeSystem:
		return "SYSTEM"
	case routeMemory:
		return "MEMORY"
	case routeSetup:
		return "SETUP"
	default:
		return "OVERVIEW"
	}
}

type focusArea uint8

const (
	focusContent focusArea = iota
	focusNavigation
	focusMemorySearch
	focusMemoryList
	focusMemoryDetail
)

type sectionItem struct {
	route       route
	title       string
	description string
}

func (item sectionItem) Title() string       { return item.title }
func (item sectionItem) Description() string { return item.description }
func (item sectionItem) FilterValue() string { return item.title }

type Model struct {
	ctx        context.Context
	loadCtx    context.Context
	cancelLoad context.CancelFunc
	backend    Backend
	options    Options

	width      int
	height     int
	generation int
	route      route
	focus      focusArea
	sections   list.Model

	inspection             Inspection
	inspectionErr          error
	inspectionLoading      bool
	setup                  SetupStatus
	setupErr               error
	setupLoading           bool
	memories               []MemorySummary
	memoriesErr            error
	memoriesLoading        bool
	memorySearch           textinput.Model
	memoryList             list.Model
	memoryViewport         viewport.Model
	memoryDetail           MemoryDetail
	memoryDetailReady      bool
	memoryStatus           string
	memorySearching        bool
	memorySearched         bool
	memoryDetailLoad       bool
	memoryGeneration       int
	cancelMemory           context.CancelFunc
	setupPlan              SetupPlan
	setupResult            SetupResult
	setupViewport          viewport.Model
	setupSelected          string
	setupPlanErr           error
	setupApplyErr          error
	setupPlanLoading       bool
	setupConfirm           bool
	setupApplying          bool
	setupCancelAsked       bool
	setupSucceeded         bool
	setupGeneration        int
	cancelSetup            context.CancelFunc
	setupView              setupView
	recoveryMode           string
	recoveryPlan           RecoveryPlan
	recoveryBackups        []BackupSummary
	recoveryPreview        RestorePreview
	recoveryBackup         BackupResult
	recoveryRestore        RestoreResult
	recoveryReinstall      ProtectedReinstallResult
	recoveryOperation      recoveryOperation
	recoveryConfirm        recoveryConfirmation
	recoveryFailure        recoveryFailure
	recoverySnapshotIndex  int
	recoveryConflictIndex  int
	recoveryGeneration     int
	recoveryCancelAsked    bool
	recoveryRefreshPending bool
	recoveryRefreshWarning bool
	cancelRecovery         context.CancelFunc

	spinner spinner.Model
	help    help.Model
	keys    keyMap
}

func NewModel(ctx context.Context, backend Backend, options Options) Model {
	if ctx == nil {
		ctx = context.Background()
	}
	spin := spinner.New()
	spin.Spinner = spinner.MiniDot
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetHeight(1)
	delegate.SetSpacing(0)
	sections := list.New([]list.Item{
		sectionItem{route: routeOverview, title: "Overview", description: "workspace summary"},
		sectionItem{route: routeSystem, title: "System", description: "read-only health"},
		sectionItem{route: routeMemory, title: "Memory", description: "search and inspect"},
		sectionItem{route: routeSetup, title: "Setup", description: "controlled OpenCode write"},
	}, delegate, 24, 4)
	sections.SetShowTitle(false)
	sections.SetShowFilter(false)
	sections.SetShowHelp(false)
	sections.SetShowPagination(false)
	sections.SetShowStatusBar(false)
	sections.DisableQuitKeybindings()
	loadCtx, cancelLoad := context.WithCancel(ctx)
	model := Model{
		ctx: ctx, loadCtx: loadCtx, cancelLoad: cancelLoad,
		backend: backend, options: options, generation: 1,
		route: routeOverview, focus: focusContent, sections: sections,
		inspectionLoading: true, setupLoading: true, memoriesLoading: true,
		spinner: spin, help: help.New(), keys: newKeyMap(),
	}
	model.initMemory()
	model.initSetup()
	return model
}

func (m Model) Init() tea.Cmd {
	return m.load()
}

func (m Model) load() tea.Cmd {
	generation := m.generation
	request := Request{Workspace: m.options.Workspace}
	inspect := func() tea.Msg {
		if m.backend == nil {
			return inspectionLoadedMsg{generation: generation, err: fmt.Errorf("inspection backend unavailable")}
		}
		value, err := m.backend.Inspect(m.loadCtx, request)
		return inspectionLoadedMsg{generation: generation, value: value, err: err}
	}
	setupStatus := func() tea.Msg {
		if m.backend == nil {
			return setupLoadedMsg{generation: generation, err: fmt.Errorf("setup backend unavailable")}
		}
		value, err := m.backend.SetupStatus(m.loadCtx, request)
		return setupLoadedMsg{generation: generation, value: value, err: err}
	}
	memories := func() tea.Msg {
		if m.backend == nil {
			return memoriesLoadedMsg{generation: generation, err: fmt.Errorf("memory backend unavailable")}
		}
		value, err := m.backend.Recent(m.loadCtx, request)
		return memoriesLoadedMsg{generation: generation, value: value, err: err}
	}
	return tea.Batch(inspect, setupStatus, memories, m.spinner.Tick)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.SetWidth(max(1, msg.Width))
		m.resizeSections()
		m.resizeMemory()
		m.resizeSetup()
		return m, nil
	case tea.KeyPressMsg:
		if m.route == routeSetup && (m.setupApplying || m.recoveryOperation.mutating()) {
			if msg.String() == "ctrl+c" && m.cancelSetup != nil {
				if m.setupApplying {
					m.cancelSetup()
					m.setupCancelAsked = true
				}
			}
			if msg.String() == "ctrl+c" && m.cancelRecovery != nil && m.recoveryOperation.mutating() {
				m.cancelRecovery()
				m.recoveryCancelAsked = true
			}
			return m, nil
		}
		if msg.String() == "ctrl+c" {
			m.cancelCurrentLoad()
			m.cancelMemoryOperation()
			m.cancelSetupOperation()
			m.cancelRecoveryOperation()
			return m, tea.Quit
		}
		if m.route == routeSetup && m.tooSmall() {
			if key.Matches(msg, m.keys.Quit) {
				m.cancelCurrentLoad()
				m.cancelMemoryOperation()
				m.cancelSetupOperation()
				m.cancelRecoveryOperation()
				return m, tea.Quit
			}
			return m, nil
		}
		if m.route == routeMemory && m.focus == focusMemorySearch {
			if handled, cmd := m.updateMemoryKey(msg); handled {
				return m, cmd
			}
		}
		if m.route == routeSetup && m.focus != focusNavigation {
			if handled, cmd := m.updateSetupKey(msg); handled {
				return m, cmd
			}
		}
		if key.Matches(msg, m.keys.Quit) {
			m.cancelCurrentLoad()
			m.cancelMemoryOperation()
			m.cancelSetupOperation()
			m.cancelRecoveryOperation()
			return m, tea.Quit
		}
		if key.Matches(msg, m.keys.Help) {
			m.help.ShowAll = !m.help.ShowAll
			return m, nil
		}
		if m.focus == focusNavigation {
			switch {
			case key.Matches(msg, m.keys.Back):
				m.focus = focusContent
				return m, nil
			case key.Matches(msg, m.keys.Select):
				if item, ok := m.sections.SelectedItem().(sectionItem); ok {
					m.setRoute(item.route)
					if item.route == routeSetup {
						return m, m.loadSetupPlan()
					}
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.sections, cmd = m.sections.Update(msg)
			return m, cmd
		}
		switch {
		case key.Matches(msg, m.keys.Sections) && !m.tooSmall():
			m.sections.Select(int(m.route))
			m.focus = focusNavigation
			return m, nil
		case key.Matches(msg, m.keys.Refresh) && !m.tooSmall():
			m.cancelCurrentLoad()
			m.cancelMemoryOperation()
			m.cancelSetupOperation()
			m.cancelRecoveryOperation()
			m.memorySearched = false
			m.memoryDetail = MemoryDetail{}
			m.memoryDetailReady = false
			m.memoryStatus = ""
			m.loadCtx, m.cancelLoad = context.WithCancel(m.ctx)
			m.generation++
			m.inspectionLoading, m.setupLoading, m.memoriesLoading = true, true, true
			m.inspectionErr, m.setupErr, m.memoriesErr = nil, nil, nil
			return m, m.load()
		}
		if m.route == routeMemory {
			if handled, cmd := m.updateMemoryKey(msg); handled {
				return m, cmd
			}
		}
		if key.Matches(msg, m.keys.Back) && m.route != routeOverview {
			m.setRoute(routeOverview)
			return m, nil
		}
	case inspectionLoadedMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		m.inspection, m.inspectionErr, m.inspectionLoading = msg.value, msg.err, false
		m.cancelCompletedLoad()
		return m, nil
	case setupLoadedMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		m.setup, m.setupErr, m.setupLoading = msg.value, msg.err, false
		m.cancelCompletedLoad()
		return m, nil
	case memoriesLoadedMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		m.memories = append([]MemorySummary(nil), msg.value...)
		m.memoriesErr, m.memoriesLoading = msg.err, false
		if !m.memorySearched {
			m.setMemoryItems(msg.value)
		}
		m.cancelCompletedLoad()
		return m, nil
	case memorySearchLoadedMsg:
		m.handleMemorySearchLoaded(msg)
		return m, nil
	case memoryDetailLoadedMsg:
		m.handleMemoryDetailLoaded(msg)
		return m, nil
	case setupPlanLoadedMsg:
		m.handleSetupPlanLoaded(msg)
		return m, nil
	case setupAppliedMsg:
		m.handleSetupApplied(msg)
		return m, nil
	case recoveryLoadedMsg:
		m.handleRecoveryLoaded(msg)
		return m, nil
	case recoveryBackupCreatedMsg:
		return m, m.handleRecoveryBackupCreated(msg)
	case recoveryPreviewLoadedMsg:
		m.handleRecoveryPreviewLoaded(msg)
		return m, nil
	case recoveryRestoredMsg:
		return m, m.handleRecoveryRestored(msg)
	case recoveryReinstalledMsg:
		return m, m.handleRecoveryReinstalled(msg)
	case spinner.TickMsg:
		if !m.loading() {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true
	view.WindowTitle = "VGXNESS Console"
	return view
}

func (m Model) render() string {
	width := max(1, m.width)
	if m.tooSmall() {
		lines := []string{
			"VGXNESS / OVERVIEW / READ ONLY",
			strings.Repeat("─", width),
			"! Resize required",
			fmt.Sprintf("  Need at least %dx%d; current terminal is %dx%d.", minimumWidth, minimumHeight, m.width, m.height),
			"  [q] quit",
		}
		return fit(lines, width, m.height)
	}

	mode := "READ ONLY"
	if m.route == routeSetup {
		mode = "CONTROLLED WRITE"
	}
	lines := []string{
		"VGXNESS / " + m.route.title() + " / " + mode,
		"workspace  " + sanitizeTerminal(m.options.Workspace),
		strings.Repeat("─", width),
	}
	content := m.renderRoute()
	if m.focus == focusNavigation && !m.wide() {
		content = m.renderNavigation()
	} else if m.wide() {
		content = m.renderWide(content, width)
	}
	lines = append(lines, content...)
	footer := m.help.View(m.keys)
	if m.route == routeMemory && m.focus != focusNavigation {
		footer = m.memoryHelp()
	} else if m.route == routeSetup && m.focus != focusNavigation {
		footer = m.setupHelp()
	}
	lines = append(lines, strings.Repeat("─", width), footer)
	return fit(lines, width, m.height)
}

func (m Model) renderRoute() []string {
	switch m.route {
	case routeSystem:
		return m.renderSystem()
	case routeMemory:
		return m.renderMemory()
	case routeSetup:
		return m.renderSetupRoute()
	}
	lines := []string{"STORAGE & CHRONICLE"}
	lines = append(lines, m.renderInspection()...)
	lines = append(lines, "", "SETUP")
	lines = append(lines, m.renderSetup()...)
	lines = append(lines, "", "RECENT PROJECT MEMORY")
	return append(lines, m.renderMemories()...)
}

func (m Model) renderSystem() []string {
	lines := []string{
		"SYSTEM HEALTH",
		"scope  quick read-only inspection · deep compatibility inventory is CLI-only",
		"",
		"QUICK CHECK",
	}
	lines = append(lines, m.renderInspection()...)
	lines = append(lines, "", "SETUP & ADAPTER")
	lines = append(lines, m.renderSetup()...)
	lines = append(lines, "", "[Esc] return to Overview")
	return lines
}

func (m Model) renderNavigation() []string {
	lines := []string{"SECTIONS", "j/k move · Enter open · Esc close", ""}
	return append(lines, strings.Split(strings.TrimRight(m.sections.View(), "\n"), "\n")...)
}

func (m Model) renderWide(content []string, width int) []string {
	const sidebarWidth = 26
	bodyWidth := max(1, width-sidebarWidth-1)
	title, hint := "SECTIONS  [g] focus", ""
	if m.focus == focusNavigation {
		title, hint = "SECTIONS  NAV FOCUS", "j/k move · Enter open"
	}
	navigation := append([]string{title, hint}, strings.Split(strings.TrimRight(m.sections.View(), "\n"), "\n")...)
	rows := max(len(navigation), len(content))
	result := make([]string, 0, rows)
	for index := 0; index < rows; index++ {
		left, right := "", ""
		if index < len(navigation) {
			left = navigation[index]
		}
		if index < len(content) {
			right = content[index]
		}
		result = append(result, padLine(left, sidebarWidth)+"│"+ansi.Truncate(right, bodyWidth, ""))
	}
	return result
}

func (m Model) renderInspection() []string {
	if m.inspectionLoading {
		return []string{m.spinner.View() + " Loading storage inspection..."}
	}
	if m.inspectionErr != nil {
		return []string{
			"✕ Inspection unavailable",
			"  Action: run `vgxness status` for bounded diagnostics.",
		}
	}
	value := m.inspection
	lines := []string{
		fmt.Sprintf("✓ Storage available · schema v%d", value.Migration),
		"  root      " + sanitizeTerminal(value.Root),
		"  database  " + sanitizeTerminal(value.Database),
	}
	return lines
}

func (m Model) renderSetup() []string {
	if m.setupLoading {
		return []string{m.spinner.View() + " Loading setup status..."}
	}
	if m.setupErr != nil {
		return []string{
			"✕ Setup unavailable",
			"  Action: run `vgxness setup opencode --status`.",
		}
	}
	value := m.setup
	state := "! Setup requires attention"
	if value.Ready {
		state = "✓ Setup ready"
	}
	lines := []string{state}
	if value.SelfInstallState != "" || value.SelfInstallPath != "" {
		lines = append(lines, "  launcher     "+sanitizeTerminal(value.SelfInstallState)+"  "+sanitizeTerminal(value.SelfInstallPath))
	}
	if value.IntegrationState != "" || value.IntegrationPath != "" {
		lines = append(lines, "  integration  "+sanitizeTerminal(value.IntegrationState)+"  "+sanitizeTerminal(value.IntegrationPath))
	}
	if value.ArtifactCount > 0 {
		lines = append(lines, fmt.Sprintf("  artifacts    %d", value.ArtifactCount))
	}
	if value.HandshakeStatus != "" {
		glyph := "✕"
		if value.HandshakeOK {
			glyph = "✓"
		}
		lines = append(lines, "  "+glyph+" OpenCode handshake · "+sanitizeTerminal(value.HandshakeStatus))
	}
	if value.ModelPlan != "" {
		lines = append(lines, "  model plan   "+sanitizeTerminal(value.ModelPlan))
	}
	if value.Blocker != "" {
		lines = append(lines, "  Next: "+sanitizeTerminal(value.Blocker))
	}
	return lines
}

func (m Model) renderMemories() []string {
	if m.memoriesLoading {
		return []string{m.spinner.View() + " Loading recent memories..."}
	}
	if m.memoriesErr != nil {
		return []string{
			"✕ Memory unavailable",
			"  Action: verify memory storage, then press r to retry.",
		}
	}
	if len(m.memories) == 0 {
		return []string{"! No active project memories found."}
	}
	lines := make([]string, 0, len(m.memories))
	for _, item := range m.memories {
		kind := sanitizeTerminal(item.Type)
		if kind == "" {
			kind = "memory"
		}
		lines = append(lines, "✓ "+sanitizeTerminal(item.Title)+"  ["+kind+"]  "+sanitizeTerminal(item.ID))
	}
	return lines
}

func (m Model) loading() bool {
	return m.inspectionLoading || m.setupLoading || m.memoriesLoading || m.setupPlanLoading || m.setupApplying || m.recoveryOperation != recoveryOperationIdle
}

func (m *Model) cancelCurrentLoad() {
	if m.cancelLoad != nil {
		m.cancelLoad()
		m.cancelLoad = nil
	}
}

func (m *Model) cancelCompletedLoad() {
	if !m.loading() {
		m.cancelCurrentLoad()
	}
}

func (m Model) tooSmall() bool {
	return m.width < minimumWidth || m.height < minimumHeight
}

func (m Model) wide() bool {
	return m.width >= 100
}

func (m *Model) resizeSections() {
	width := max(1, m.width-4)
	if m.wide() {
		width = 24
	}
	m.sections.SetSize(width, 4)
}

func (m *Model) setRoute(next route) {
	if m.route == routeMemory && next != routeMemory {
		m.cancelMemoryOperation()
	}
	if m.route == routeSetup && next != routeSetup {
		m.cancelSetupOperation()
		m.cancelRecoveryOperation()
	}
	previous := m.route
	m.route = next
	m.sections.Select(int(next))
	if next == routeMemory {
		m.focusMemory(focusMemoryList)
		return
	}
	if next == routeSetup && previous != routeSetup {
		m.setupView = setupViewInstall
		m.setupSelected = defaultSetupPlan
		if validSetupPlan(m.setup.ModelPlan) {
			m.setupSelected = m.setup.ModelPlan
		}
		m.setupPlan = SetupPlan{}
		m.setupResult = SetupResult{}
		m.setupPlanErr = nil
		m.setupApplyErr = nil
		m.setupSucceeded = false
		m.setupConfirm = false
		m.setupViewport.GotoTop()
		m.resetRecoveryState()
	}
	m.focus = focusContent
}

func padLine(value string, width int) string {
	value = ansi.Truncate(value, width, "")
	return value + strings.Repeat(" ", max(0, width-lipgloss.Width(value)))
}

func fit(lines []string, width, height int) string {
	width = max(1, width)
	var rendered []string
	for _, line := range lines {
		wrapped := lipgloss.Wrap(line, width, "")
		for _, part := range strings.Split(wrapped, "\n") {
			rendered = append(rendered, ansi.Truncate(part, width, ""))
		}
	}
	if height > 0 && len(rendered) > height {
		if height < 2 {
			return strings.Join(rendered[:height], "\n")
		}
		footer := append([]string(nil), rendered[len(rendered)-2:]...)
		rendered = append(rendered[:height-len(footer)], footer...)
	}
	return strings.Join(rendered, "\n")
}

func sanitizeTerminal(value string) string {
	var result strings.Builder
	for _, r := range value {
		switch r {
		case '\n':
			result.WriteString(`\n`)
		case '\r':
			result.WriteString(`\r`)
		case '\t':
			result.WriteString(`\t`)
		case '\x1b':
			result.WriteString(`\x1b`)
		case '\x7f':
			result.WriteString(`\x7f`)
		default:
			if unicode.IsControl(r) || isBidiControl(r) {
				if r <= 0xff {
					fmt.Fprintf(&result, `\x%02x`, r)
				} else {
					fmt.Fprintf(&result, `\u%04x`, r)
				}
				continue
			}
			result.WriteRune(r)
		}
	}
	return result.String()
}

func isBidiControl(r rune) bool {
	return r == '\u061c' || r == '\u200e' || r == '\u200f' ||
		r >= '\u202a' && r <= '\u202e' || r >= '\u2066' && r <= '\u2069'
}
