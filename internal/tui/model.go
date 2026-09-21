package tui

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/textinput"
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

type route uint8

const routeSetup route = iota

type Model struct {
	ctx     context.Context
	backend Backend
	options Options

	width      int
	height     int
	generation int
	route      route

	setup                  SetupStatus
	setupErr               error
	setupLoading           bool
	setupPlan              SetupPlan
	setupResult            SetupResult
	setupProviders         []setupflow.Provider
	modelChoices           [2]modelChoice
	modelChoiceProvider    int
	modelChoiceRow         int
	modelChoiceModeChoice  int
	modelChoiceEditing     bool
	manualInput            textinput.Model
	pickerOpen             bool
	pickerStep             pickerStep
	pickerProvider         string
	pickerFilter           textinput.Model
	pickerIndex            int
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
	setupCatalogAttempted       bool
	cancelSetupCatalog          context.CancelFunc
	setupCatalogQuery           string
	setupCatalogSearching       bool
	setupCatalogResultIndex     int
	piCatalog                   []SetupCatalogModel
	piCatalogErr                error
	piCatalogLoading            bool
	piCatalogGeneration         int
	piCatalogAttempted          bool
	cancelPiCatalog             context.CancelFunc
	setupEditorPlan             SetupPlan
	setupEditorRequest          SetupRequest
	setupEditorPreviewed        bool
}

func NewModel(ctx context.Context, backend Backend, options Options) Model {
	if ctx == nil {
		ctx = context.Background()
	}
	model := Model{
		ctx: ctx, backend: backend, options: options, generation: 1,
		route: routeSetup, setupGeneration: 1,
		manualInput:  newPickerInput("", ""),
		pickerFilter: newPickerInput("> ", "filter"),
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
		return m, m.loadStartupStatus()
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resizeSetup()
		return m, nil
	case tea.PasteMsg:
		switch {
		case m.pickerOpen:
			var cmd tea.Cmd
			m.pickerFilter, cmd = m.pickerFilter.Update(msg)
			m.pickerIndex = 0
			return m, cmd
		case m.modelChoiceEditing:
			var cmd tea.Cmd
			m.manualInput, cmd = m.manualInput.Update(msg)
			return m, cmd
		}
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
			if msg.String() == "q" || msg.String() == "ctrl+c" {
				m.cancelSetupOperation()
				m.cancelRecoveryOperation()
				return m, tea.Quit
			}
			return m, nil
		}
		if handled, cmd := m.updateSetupKey(msg); handled {
			return m, cmd
		}
		if msg.String() == "q" {
			m.cancelSetupOperation()
			m.cancelRecoveryOperation()
			return m, tea.Quit
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
	if !m.pickerOpen && !(m.multiSetupEnabled() && m.setupView == setupViewReview) {
		lines = append(lines, studioMuted.Render(m.setupHelp()))
	}
	base := lipgloss.NewStyle().Background(softbricCanvas).Width(width).Render(fit(lines, width, m.height))
	if m.pickerOpen {
		return m.overlayPicker(base, width, m.height)
	}
	return base
}

// headerWorkspace keeps the workspace on one header line by trimming the path
// from the left, which preserves the most specific trailing directories.
func (m Model) headerWorkspace(maxWidth int) string {
	path := sanitizeTerminal(m.options.Workspace)
	runes := []rune(path)
	if maxWidth < 4 {
		maxWidth = 4
	}
	if len(runes) > maxWidth {
		path = "…" + string(runes[len(runes)-(maxWidth-1):])
	}
	return studioMuted.Render("workspace  ") + path
}

func (m Model) brandHeader() []string {
	if m.wide() && m.setupView == setupViewHome && !m.setupModelEditing {
		const prefix = "INSTALLATION STUDIO  ·  LOCAL SETUP CONSOLE   workspace  "
		return append(softbricBanner(),
			studioAccent.Render("INSTALLATION STUDIO")+studioMuted.Render("  ·  LOCAL SETUP CONSOLE")+"   "+m.headerWorkspace(m.width-lipgloss.Width(prefix)),
		)
	}
	const prefix = "Install · reinstall · configure   │   workspace  "
	return []string{
		studioAccent.Render("VGXNESS / INSTALLATION STUDIO"),
		studioCyan.Render("Install · reinstall · configure") + studioMuted.Render("   │   ") + m.headerWorkspace(m.width-lipgloss.Width(prefix)),
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

func (m Model) tooSmall() bool {
	return m.width < minimumWidth || m.height < minimumHeight
}

func (m Model) wide() bool {
	return m.width >= 100
}

func (m *Model) setRoute(next route) {
	m.route = next
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
