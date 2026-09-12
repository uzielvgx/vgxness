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
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	setupflow "github.com/vgxness/vgxness/internal/setup"
)

const (
	minimumWidth  = 42
	minimumHeight = 10
)

var (
	softbricCanvas   = lipgloss.Color("#071522")
	softbricInk      = lipgloss.Color("#102231")
	softbricDelivery = lipgloss.Color("#005F5C")
	softbricBric     = lipgloss.Color("#008B87")
	softbricAqua     = lipgloss.Color("#4DD4D4")
	softbricPaper    = lipgloss.Color("#F5F7F8")

	studioAccent = lipgloss.NewStyle().Foreground(softbricAqua).Bold(true)
	studioCyan   = lipgloss.NewStyle().Foreground(softbricAqua)
	studioMuted  = lipgloss.NewStyle().Foreground(softbricAqua)
	studioPanel  = lipgloss.NewStyle().
			Foreground(softbricPaper).
			Background(softbricInk).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(softbricDelivery).
			Padding(0, 1)
	studioCard = lipgloss.NewStyle().
			Foreground(softbricPaper).
			Background(softbricInk).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(softbricDelivery).
			Padding(0, 1)
	studioFocus = lipgloss.NewStyle().
			Foreground(softbricCanvas).
			Background(softbricAqua).
			Bold(true)
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

	ModelSchemaVersion int
	ModelAssignments   *[SetupModelAssignmentCount]SetupModelAssignment
	statusGeneration   int
}

type Backend interface {
	SetupStatus(context.Context, Request) (SetupStatus, error)
	PlanSetup(context.Context, SetupRequest) (SetupPlan, error)
	ApplySetup(context.Context, SetupRequest) (SetupResult, error)
	PlanRecovery(context.Context, RecoveryPlanRequest) (RecoveryPlan, error)
	ListBackups(context.Context, BackupListRequest) (BackupListResult, error)
	CreateBackup(context.Context, CreateBackupRequest) (BackupResult, error)
	PreviewRestore(context.Context, RestorePreviewRequest) (RestorePreview, error)
	RestoreBackup(context.Context, RestoreRequest) (RestoreResult, error)
	ProtectedReinstall(context.Context, ProtectedReinstallRequest) (ProtectedReinstallResult, error)
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
	startup    bool
}

type setupStartMsg struct{}

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

const routeSetup route = iota

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

	setup                  SetupStatus
	setupErr               error
	setupLoading           bool
	setupPlan              SetupPlan
	setupResult            SetupResult
	setupProviders         []setupflow.Provider
	modelChoices           [2]modelChoice
	modelChoiceProvider    int
	modelChoiceRow         int
	modelChoiceEditing     bool
	modelChoiceInput       string
	codexPlanEdited        bool
	modelChoiceError       string
	setupMultiPlan         setupflow.MultiPlan
	setupMultiResult       setupflow.MultiResult
	setupViewport          viewport.Model
	setupSelected          string
	setupProviderCursor    int
	installationAction     installationAction
	setupPlanErr           error
	setupApplyErr          error
	setupPlanLoading       bool
	setupConfirm           bool
	setupApplying          bool
	setupCancelAsked       bool
	setupSucceeded         bool
	setupGeneration        int
	setupStatusGeneration  int
	cancelSetup            context.CancelFunc
	setupView              setupView
	setupModelEditing      bool
	setupModelSlot         int
	setupModelRefs         [3]string
	setupModelEfforts      [3]string
	setupModelVariants     [3]string
	setupModelEntryRefs    [3]string
	setupModelEntryEfforts [3]string
	setupModelEntryVars    [3]string
	setupOverrides         bool
	setupEntryOverrides    bool
	setupPreviewRequest    SetupRequest
	setupPreviewed         bool
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

	recoverySelectedProvider setupflow.Provider

	setupAssignmentRows         [SetupModelAssignmentCount]SetupModelAssignmentRequest
	setupAssignmentEntryRows    [SetupModelAssignmentCount]SetupModelAssignmentRequest
	setupAssignmentsSeeded      bool
	setupAssignmentsExact       bool
	setupAssignmentsEntry       bool
	setupAssignmentsEdited      bool
	setupAssignmentsEntryEdited bool
	setupCatalog                []SetupCatalogModel
	setupCatalogErr             error
	setupCatalogLoading         bool
	setupCatalogGeneration      int
	cancelSetupCatalog          context.CancelFunc
	setupCatalogQuery           string
	setupCatalogSearching       bool
	setupCatalogResultIndex     int
	setupEditorPlan             SetupPlan
	setupEditorRequest          SetupRequest
	setupEditorPreviewed        bool

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
		sectionItem{route: routeSetup, title: "Installation", description: "install, repair, configure"},
	}, delegate, 24, 1)
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
		route: routeSetup, focus: focusContent, sections: sections,
		setupGeneration: 1,
		spinner:         spin, help: help.New(), keys: newKeyMap(),
	}
	model.initSetup()
	return model
}

func (m Model) Init() tea.Cmd {
	return func() tea.Msg { return setupStartMsg{} }
}

func (m Model) loadStartupStatus() tea.Cmd {
	return func() tea.Msg {
		if m.backend == nil {
			return setupLoadedMsg{generation: m.generation, startup: true, err: fmt.Errorf("setup backend unavailable")}
		}
		value, err := m.backend.SetupStatus(m.ctx, Request{Workspace: m.options.Workspace})
		return setupLoadedMsg{generation: m.generation, value: value, err: err, startup: true}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case setupStartMsg:
		m.setupLoading = true
		return m, tea.Batch(m.loadStartupStatus(), m.spinner.Tick)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.SetWidth(max(1, msg.Width))
		m.resizeSetup()
		return m, nil
	case tea.KeyPressMsg:
		if m.setupApplying || m.recoveryOperation.mutating() {
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
			m.cancelSetupOperation()
			m.cancelRecoveryOperation()
			return m, tea.Quit
		}
		if m.tooSmall() {
			if key.Matches(msg, m.keys.Quit) {
				m.cancelSetupOperation()
				m.cancelRecoveryOperation()
				return m, tea.Quit
			}
			return m, nil
		}
		if m.focus != focusNavigation {
			if handled, cmd := m.updateSetupKey(msg); handled {
				return m, cmd
			}
		}
		if key.Matches(msg, m.keys.Quit) {
			m.cancelSetupOperation()
			m.cancelRecoveryOperation()
			return m, tea.Quit
		}
		if key.Matches(msg, m.keys.Help) {
			m.help.ShowAll = !m.help.ShowAll
			return m, nil
		}
		switch {
		case key.Matches(msg, m.keys.Sections):
			return m, nil
		}
	case setupLoadedMsg:
		if msg.generation != m.generation || msg.value.statusGeneration != m.setupStatusGeneration {
			return m, nil
		}
		m.setup, m.setupErr, m.setupLoading = cloneSetupStatus(msg.value), msg.err, false
		if msg.startup && msg.err == nil {
			m.setRoute(routeSetup)
			return m, m.loadSetupCatalog(false)
		}
		return m, nil
	case setupPlanLoadedMsg:
		m.handleSetupPlanLoaded(msg)
		return m, nil
	case setupAppliedMsg:
		m.handleSetupApplied(msg)
		return m, nil
	case setupCatalogLoadedMsg:
		m.handleSetupCatalogLoaded(msg)
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
			"VGXNESS / INSTALLATION STUDIO",
			strings.Repeat("─", width),
			"! Resize required",
			fmt.Sprintf("  Need at least %dx%d; current terminal is %dx%d.", minimumWidth, minimumHeight, m.width, m.height),
			"  [q] quit",
		}
		return fit(lines, width, m.height)
	}

	lines := m.brandHeader()
	panelWidth := max(1, width-2)
	body := studioPanel.Width(panelWidth).Render(strings.Join(m.renderSetupRoute(), "\n"))
	lines = append(lines, strings.Split(body, "\n")...)
	if !(m.multiSetupEnabled() && m.setupView == setupViewReview) {
		lines = append(lines, studioMuted.Render(m.setupHelp()))
	}
	return lipgloss.NewStyle().Background(softbricCanvas).Width(width).Render(fit(lines, width, m.height))
}

func (m Model) brandHeader() []string {
	workspace := studioMuted.Render("workspace  ") + sanitizeTerminal(m.options.Workspace)
	if m.wide() && m.setupView == setupViewHome {
		return append(softbricBanner(),
			studioAccent.Render("INSTALLATION STUDIO")+studioMuted.Render("  ·  LOCAL SETUP CONSOLE")+"   "+workspace,
		)
	}
	return []string{
		studioAccent.Render("VGXNESS / INSTALLATION STUDIO"),
		studioCyan.Render("Install · reinstall · configure") + studioMuted.Render("   │   ") + workspace,
	}
}

func softbricBanner() []string {
	columns := [][]string{
		{"██╗   ██╗", "╚██╗ ██╔╝", " ╚████╔╝ ", "  ╚██╔╝  ", "   ██║   ", "   ╚═╝   "},       // V
		{" ██████╗", "██╔════╝", "██║  ███╗", "██║   ██║", "╚██████╔╝", " ╚═════╝ "},         // G
		{"██╗  ██╗", "╚██╗██╔╝", " ╚███╔╝ ", " ██╔██╗ ", "██╔╝ ██╗", "╚═╝  ╚═╝"},             // X
		{"███╗   ██╗", "████╗  ██║", "██╔██╗ ██║", "██║╚██╗██║", "██║ ╚████║", "╚═╝  ╚═══╝"}, // N
		{"███████╗", "██╔════╝", "█████╗  ", "██╔══╝  ", "███████╗", "╚══════╝"},             // E
		{"███████╗", "██╔════╝", "███████╗", "╚════██║", "███████║", "╚══════╝"},             // S
		{"███████╗", "██╔════╝", "███████╗", "╚════██║", "███████║", "╚══════╝"},             // S
	}
	lines := make([]string, len(columns[0]))
	for row := range lines {
		parts := make([]string, len(columns))
		for column := range columns {
			parts[column] = padLine(columns[column][row], lipgloss.Width(columns[column][0]))
		}
		lines[row] = strings.Join(parts, " ")
	}
	gradient := lipgloss.Blend1D(len(lines), softbricAqua, softbricBric)
	for index, line := range lines {
		lines[index] = lipgloss.NewStyle().Foreground(gradient[index]).Bold(true).Render(line)
	}
	return lines
}

func (m Model) renderRoute() []string {
	return m.renderSetupRoute()
}

func (m Model) loading() bool {
	return m.setupLoading || m.setupPlanLoading || m.setupApplying || m.recoveryOperation != recoveryOperationIdle
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
	m.route = next
	m.sections.Select(int(next))
	if next == routeSetup {
		m.setupView = setupViewHome
		m.setupSelected = defaultSetupPlan
		if validSetupPlan(m.setup.ModelPlan) {
			m.setupSelected = m.setup.ModelPlan
		}
		m.setupPlan = SetupPlan{}
		m.setupResult = SetupResult{}
		m.setupMultiPlan = setupflow.MultiPlan{}
		m.setupMultiResult = setupflow.MultiResult{}
		m.setupPlanErr = nil
		m.setupApplyErr = nil
		m.setupSucceeded = false
		m.setupConfirm = false
		m.setupModelEditing = false
		m.setupOverrides = false
		m.setupModelRefs = [3]string{}
		m.setupModelEfforts = [3]string{}
		m.setupModelVariants = [3]string{}
		m.setupPreviewRequest = SetupRequest{}
		m.setupPreviewed = false
		m.resetSetupAssignments()
		m.seedSetupAssignments(SetupPlan{ModelSchemaVersion: m.setup.ModelSchemaVersion, ModelAssignments: m.setup.ModelAssignments})
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
