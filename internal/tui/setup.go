package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/vgxness/vgxness/internal/modelcatalog"
	"github.com/vgxness/vgxness/internal/sdd"
)

const (
	defaultSetupPlan          = "medium"
	SetupModelAssignmentCount = 15
	setupDiscoveryDisclaimer  = "Local discovery proves identifier presence only; not authorization or support."
)

var setupPlans = [...]string{"low", "medium", "high", "ultra"}

type SetupRequest struct {
	Workspace            string
	Plan                 string
	ModelEfficient       string
	ModelBalanced        string
	ModelFrontier        string
	ModelEfficientEffort string
	ModelBalancedEffort  string
	ModelFrontierEffort  string
	ModelAssignments     *[SetupModelAssignmentCount]SetupModelAssignmentRequest
	ExpectedPlanDigest   string
}

type SetupModelAssignmentRequest struct {
	ArtifactKey     string
	Provider        string
	Reference       string
	RequestedEffort string
	Source          string
	Availability    string
}

type SetupModelAssignment struct {
	ArtifactKey       string
	Role              string
	Class             string
	Provider          string
	Model             string
	RequestedEffort   string
	Effort            string
	Variant           string
	Degraded          bool
	DegradationReason string
	Source            string
	Availability      string
}

type SetupCatalogModel struct {
	Provider     string
	Reference    string
	Variants     []string
	Source       string
	Availability string
}

type setupCatalogBackend interface {
	ModelCatalog(context.Context, bool) ([]SetupCatalogModel, error)
}

type setupCatalogLoadedMsg struct {
	generation int
	rows       []SetupCatalogModel
	err        error
}

type setupAgentIdentity struct{ ArtifactKey, Name, Role, Class string }

var setupAgentRows = [SetupModelAssignmentCount]setupAgentIdentity{
	{"agents/vgxness-manager.md", "manager", "manager", "core"},
	{"agents/explore.md", "explore", "research", "core"},
	{"agents/general.md", "general", "implementation", "core"},
	{"agents/vgxness-verifier.md", "verifier", "verification", "core"},
	{"agents/vgxness-review-risk.md", "review-risk", "risk", "review"},
	{"agents/vgxness-review-readability.md", "review-readability", "readability", "review"},
	{"agents/vgxness-review-reliability.md", "review-reliability", "reliability", "review"},
	{"agents/vgxness-review-resilience.md", "review-resilience", "resilience", "review"},
	{"agents/vgxness-review-refuter.md", "review-refuter", "refuter", "review"},
	{"agents/vgxness-sdd-research.md", "sdd-research", "research", "sdd"},
	{"agents/vgxness-sdd-proposal.md", "sdd-proposal", "proposal", "sdd"},
	{"agents/vgxness-sdd-spec.md", "sdd-spec", "spec", "sdd"},
	{"agents/vgxness-sdd-design.md", "sdd-design", "design", "sdd"},
	{"agents/vgxness-sdd-tasks.md", "sdd-tasks", "tasks", "sdd"},
	{"agents/vgxness-sdd-apply.md", "sdd-apply", "apply", "sdd"},
}

type SetupStep struct {
	Number      int
	Title       string
	Explanation string
	Mutates     bool
}

type SetupPlan struct {
	Digest                       string
	Provider                     string
	Steps                        []SetupStep
	SelfInstallState             string
	SelfInstallPath              string
	SelfInstallUpdateAvailable   bool
	SelfInstallRollbackAvailable bool
	SelfInstallActiveSHA256      string
	SelfInstallPreviousSHA256    string
	SelfInstallChanged           bool
	IntegrationState             string
	IntegrationPath              string
	IntegrationChanged           bool
	IntegrationRestartRequired   bool
	SkillsState                  string
	SkillsPath                   string
	SkillsFileCount              int
	SkillsChanged                bool
	SkillsUpdateNeeded           bool
	ArtifactCount                int
	ModelSchemaVersion           int
	ModelAssignments             *[SetupModelAssignmentCount]SetupModelAssignment
	ModelPlan                    string
	ModelProvider                string
	ModelEfficient               string
	ModelBalanced                string
	ModelFrontier                string
	ModelEfficientEffort         string
	ModelBalancedEffort          string
	ModelFrontierEffort          string
	ModelEfficientSource         string
	ModelBalancedSource          string
	ModelFrontierSource          string
	ModelEfficientAvailability   string
	ModelBalancedAvailability    string
	ModelFrontierAvailability    string
	HandshakeOK                  bool
	HandshakeStatus              string
	Ready                        bool
	Blocker                      string
}

type SetupResult struct {
	Plan                         SetupPlan
	SelfInstallState             string
	SelfInstallPath              string
	SelfInstallUpdateAvailable   bool
	SelfInstallRollbackAvailable bool
	SelfInstallActiveSHA256      string
	SelfInstallPreviousSHA256    string
	IntegrationState             string
	IntegrationPath              string
	SkillsState                  string
	SkillsPath                   string
	SkillsFileCount              int
	ArtifactCount                int
	HandshakeOK                  bool
	HandshakeStatus              string
	Recovery                     string
	Changed                      bool
	RestartRequired              bool
}

type setupPlanLoadedMsg struct {
	generation int
	request    SetupRequest
	value      SetupPlan
	err        error
}

type setupAppliedMsg struct {
	generation int
	value      SetupResult
	err        error
}

func (m *Model) initSetup() {
	preview := viewport.New(viewport.WithWidth(80), viewport.WithHeight(16))
	preview.SoftWrap = true
	preview.FillHeight = false
	m.setupViewport = preview
	m.setupSelected = defaultSetupPlan
	m.setupView = setupViewInstall
	m.resetRecoveryState()
}

func (m *Model) loadSetupCatalog(refresh bool) tea.Cmd {
	m.cancelSetupCatalogLoad()
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancelSetupCatalog = cancel
	m.setupCatalogGeneration++
	m.setupCatalogLoading = true
	m.setupCatalogErr = nil
	return m.setupCatalogCommand(ctx, refresh, m.setupCatalogGeneration)
}

func (m Model) setupCatalogCommand(ctx context.Context, refresh bool, generation int) tea.Cmd {
	return func() tea.Msg {
		backend, ok := m.backend.(setupCatalogBackend)
		if !ok {
			return setupCatalogLoadedMsg{generation: generation, err: fmt.Errorf("model catalog unavailable")}
		}
		rows, err := backend.ModelCatalog(ctx, refresh)
		return setupCatalogLoadedMsg{generation: generation, rows: append([]SetupCatalogModel(nil), rows...), err: err}
	}
}

func (m *Model) handleSetupCatalogLoaded(msg setupCatalogLoadedMsg) {
	if msg.generation != m.setupCatalogGeneration {
		return
	}
	m.cancelSetupCatalogLoad()
	m.setupCatalogLoading = false
	m.setupCatalogErr = msg.err
	if msg.err == nil {
		m.setupCatalog = append([]SetupCatalogModel(nil), msg.rows...)
	}
}

func (m *Model) cancelSetupCatalogLoad() {
	if m.cancelSetupCatalog != nil {
		m.cancelSetupCatalog()
		m.cancelSetupCatalog = nil
	}
}

func (m *Model) loadSetupPlan() tea.Cmd {
	ctx, generation := m.startSetupOperation()
	m.setupPlanLoading = true
	m.setupConfirm = false
	m.setupCancelAsked = false
	m.setupSucceeded = false
	m.setupPreviewed = false
	m.setupApplyErr = nil
	m.setupPlanErr = nil
	m.setupViewport.GotoTop()
	request := m.setupRequest()
	return func() tea.Msg {
		if m.backend == nil {
			return setupPlanLoadedMsg{generation: generation, request: request, err: fmt.Errorf("setup backend unavailable")}
		}
		value, err := m.backend.PlanSetup(ctx, request)
		return setupPlanLoadedMsg{generation: generation, request: request, value: value, err: err}
	}
}

func (m *Model) applySetup() tea.Cmd {
	ctx, generation := m.startSetupOperation()
	m.setupStatusGeneration++
	m.setupConfirm = false
	m.setupApplying = true
	m.setupCancelAsked = false
	m.setupSucceeded = false
	m.setupApplyErr = nil
	m.setupResult = SetupResult{}
	m.setupViewport.GotoTop()
	request := m.setupRequest()
	request.ExpectedPlanDigest = m.setupPlan.Digest
	return func() tea.Msg {
		if m.backend == nil {
			return setupAppliedMsg{generation: generation, err: fmt.Errorf("setup backend unavailable")}
		}
		value, err := m.backend.ApplySetup(ctx, request)
		return setupAppliedMsg{generation: generation, value: value, err: err}
	}
}

func (m *Model) startSetupOperation() (context.Context, int) {
	if m.cancelSetup != nil {
		m.cancelSetup()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancelSetup = cancel
	m.setupGeneration++
	return ctx, m.setupGeneration
}

func (m *Model) cancelSetupOperation() {
	if m.cancelSetup != nil {
		m.cancelSetup()
		m.cancelSetup = nil
	}
	m.setupGeneration++
	m.setupPlanLoading = false
	m.setupApplying = false
	m.setupCancelAsked = false
	m.setupConfirm = false
	m.setupPreviewed = false
	m.cancelSetupCatalogLoad()
	m.setupCatalogGeneration++
	m.setupCatalogLoading = false
}

func (m *Model) finishSetupOperation() {
	if m.cancelSetup != nil {
		m.cancelSetup()
		m.cancelSetup = nil
	}
}

func (m *Model) handleSetupPlanLoaded(msg setupPlanLoadedMsg) {
	if msg.generation != m.setupGeneration || !setupRequestsEqual(msg.request, m.setupRequest()) {
		return
	}
	m.finishSetupOperation()
	m.setupPlanLoading = false
	m.setupPlanErr = msg.err
	if msg.err == nil {
		m.setupPlan = cloneSetupPlan(msg.value)
		m.setupPreviewRequest = msg.request
		m.setupPreviewed = true
		if !m.setupOverrides {
			m.setupModelRefs = [3]string{msg.value.ModelEfficient, msg.value.ModelBalanced, msg.value.ModelFrontier}
			m.setupModelEfforts = [3]string{msg.value.ModelEfficientEffort, msg.value.ModelBalancedEffort, msg.value.ModelFrontierEffort}
		}
		if !m.setupAssignmentsExact {
			m.seedSetupAssignments(msg.value)
		}
		m.setupPreviewRequest = m.setupRequest()
	}
	m.setupViewport.GotoTop()
}

func (m *Model) handleSetupApplied(msg setupAppliedMsg) {
	if msg.generation != m.setupGeneration {
		return
	}
	m.finishSetupOperation()
	m.setupApplying = false
	m.setupResult = cloneSetupResult(msg.value)
	m.setupApplyErr = msg.err
	m.setupSucceeded = msg.err == nil
	m.setupCancelAsked = false
	if msg.err == nil {
		m.setup = cloneSetupStatus(SetupStatus{
			Provider: msg.value.Plan.Provider, Ready: true,
			SelfInstallState: msg.value.SelfInstallState, SelfInstallPath: msg.value.SelfInstallPath,
			IntegrationState: msg.value.IntegrationState, IntegrationPath: msg.value.IntegrationPath,
			ArtifactCount: msg.value.ArtifactCount,
			HandshakeOK:   msg.value.HandshakeOK, HandshakeStatus: msg.value.HandshakeStatus,
			ModelPlan:          msg.value.Plan.ModelPlan,
			ModelSchemaVersion: msg.value.Plan.ModelSchemaVersion, ModelAssignments: msg.value.Plan.ModelAssignments,
		})
		m.setupErr = nil
		m.setupLoading = false
	}
	m.setupViewport.GotoTop()
}

func (m *Model) updateSetupKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if m.setupView == setupViewRecovery {
		return m.updateRecoveryKey(msg)
	}
	if m.setupConfirm {
		switch msg.String() {
		case "y":
			if m.setupApplyAllowed() {
				return true, m.applySetup()
			}
			m.setupConfirm = false
			return true, nil
		case "n", "esc":
			m.setupConfirm = false
			return true, nil
		case "j", "k":
		default:
			return true, nil
		}
	}
	if m.setupModelEditing {
		return m.updateModelEditorKey(msg)
	}

	switch msg.String() {
	case "tab":
		m.cancelSetupOperation()
		m.setupView = setupViewRecovery
		m.resetRecoveryState()
		return true, m.loadRecovery()
	case "left", "h":
		return true, m.selectSetupPlan(-1)
	case "right", "l":
		return true, m.selectSetupPlan(1)
	case "a":
		if m.setupApplyAllowed() {
			m.setupConfirm = true
		}
		return true, nil
	case "m":
		m.setupModelEditing = true
		m.setupModelSlot = 0
		m.setupAssignmentEntryRows, m.setupAssignmentsEntry = m.setupAssignmentRows, m.setupAssignmentsExact
		m.setupAssignmentsEntryEdited = m.setupAssignmentsEdited
		m.setupModelEntryRefs, m.setupModelEntryEfforts, m.setupEntryOverrides = m.setupModelRefs, m.setupModelEfforts, m.setupOverrides
		m.setupEditorPlan, m.setupEditorRequest, m.setupEditorPreviewed = cloneSetupPlan(m.setupPlan), cloneSetupRequest(m.setupPreviewRequest), m.setupPreviewed
		return true, nil
	case "r":
		return true, m.loadSetupPlan()
	case "esc":
		m.setRoute(routeOverview)
		return true, nil
	case "j", "k", "up", "down", "pgup", "pgdown", "home", "end":
		m.setupViewport.SetContent(strings.Join(m.setupRouteLines(), "\n"))
		var cmd tea.Cmd
		m.setupViewport, cmd = m.setupViewport.Update(msg)
		return true, cmd
	}
	return false, nil
}

func (m Model) setupApplyAllowed() bool {
	if m.setupPlanLoading || !m.setupPreviewed || m.setupPlan.Digest == "" || !setupRequestsEqual(m.setupPreviewRequest, m.setupRequest()) || m.setupPlanErr != nil || m.setupApplyErr != nil || m.setupSucceeded || !m.setupPlan.Ready || ((m.setupOverrides || m.setupAssignmentsExact) && m.modelEditorError() != "") {
		return false
	}
	action := classifySetup(m.setupPlan)
	return action == "initial install" || action == "reinstall/update"
}

func (m Model) setupRequest() SetupRequest {
	request := SetupRequest{Workspace: m.options.Workspace, Plan: m.setupSelected}
	if m.setupAssignmentsExact {
		rows := m.setupAssignmentRows
		request.ModelAssignments = &rows
		return request
	}
	if m.setupOverrides {
		request.ModelEfficient, request.ModelBalanced, request.ModelFrontier = m.setupModelRefs[0], m.setupModelRefs[1], m.setupModelRefs[2]
		if m.modelProfileMixed() {
			request.ModelEfficientEffort, request.ModelBalancedEffort, request.ModelFrontierEffort = m.setupModelEfforts[0], m.setupModelEfforts[1], m.setupModelEfforts[2]
		}
	}
	return request
}

func (m *Model) updateModelEditorKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if m.setupAssignmentsSeeded {
		return m.updateAssignmentEditorKey(msg)
	}
	switch msg.String() {
	case "esc":
		m.setupModelRefs, m.setupModelEfforts, m.setupOverrides = m.setupModelEntryRefs, m.setupModelEntryEfforts, m.setupEntryOverrides
		m.setupModelEditing = false
		return true, nil
	case "enter":
		if m.modelEditorError() == "" {
			m.setupModelEditing = false
			m.setupOverrides = true
			return true, m.loadSetupPlan()
		}
		return true, nil
	case "up", "k":
		m.setupModelSlot = (m.setupModelSlot + 2) % 3
		return true, nil
	case "down", "j":
		m.setupModelSlot = (m.setupModelSlot + 1) % 3
		return true, nil
	case "tab", "right", "l":
		m.setupModelEfforts[m.setupModelSlot] = nextSetupEffort(m.setupModelEfforts[m.setupModelSlot], setupSupportedEfforts(m.setupModelRefs[m.setupModelSlot]))
		return true, nil
	case "backspace":
		value := []rune(m.setupModelRefs[m.setupModelSlot])
		if len(value) > 0 {
			m.setupModelRefs[m.setupModelSlot] = string(value[:len(value)-1])
		}
		m.ensureMixedEffort(m.setupModelSlot)
		return true, nil
	}
	if msg.Text != "" {
		m.setupModelRefs[m.setupModelSlot] += msg.Text
		m.ensureMixedEffort(m.setupModelSlot)
		return true, nil
	}
	return true, nil
}

func (m *Model) updateAssignmentEditorKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.setupAssignmentRows, m.setupAssignmentsExact, m.setupAssignmentsEdited = m.setupAssignmentEntryRows, m.setupAssignmentsEntry, m.setupAssignmentsEntryEdited
		m.setupModelEditing = false
		m.restoreSetupEditorPreview()
		return true, nil
	case "enter":
		if m.modelEditorError() == "" {
			m.setupModelEditing = false
			return true, m.loadSetupPlan()
		}
	case "up", "k":
		m.setupModelSlot = (m.setupModelSlot + SetupModelAssignmentCount - 1) % SetupModelAssignmentCount
	case "down", "j":
		m.setupModelSlot = (m.setupModelSlot + 1) % SetupModelAssignmentCount
	case "left", "h":
		m.selectCatalogModel(-1)
	case "right", "l":
		m.selectCatalogModel(1)
	case "[":
		m.selectAssignmentEffort(-1)
	case "]":
		m.selectAssignmentEffort(1)
	case "r":
		return true, m.loadSetupCatalog(true)
	case "q":
		return false, nil
	}
	return true, nil
}

func (m *Model) selectCatalogModel(offset int) {
	if len(m.setupCatalog) == 0 {
		return
	}
	current := m.setupAssignmentRows[m.setupModelSlot].Reference
	index := 0
	for candidate, row := range m.setupCatalog {
		if row.Reference == current {
			index = candidate
			break
		}
	}
	index = (index + offset + len(m.setupCatalog)) % len(m.setupCatalog)
	selected := m.setupCatalog[index]
	row := &m.setupAssignmentRows[m.setupModelSlot]
	row.Provider, row.Reference = selected.Provider, selected.Reference
	row.Source, row.Availability = selected.Source, selected.Availability
	if efforts := setupSupportedEfforts(row.Reference); len(efforts) == 0 {
		row.RequestedEffort = ""
	} else if !supportsSetupEffort(efforts, row.RequestedEffort) {
		row.RequestedEffort = efforts[0]
	}
	m.markAssignmentEdit()
}

func (m *Model) selectAssignmentEffort(offset int) {
	row := &m.setupAssignmentRows[m.setupModelSlot]
	next := cycleSetupEffort(row.RequestedEffort, setupSupportedEfforts(row.Reference), offset)
	if next == row.RequestedEffort {
		return
	}
	row.RequestedEffort = next
	m.markAssignmentEdit()
}

func (m *Model) markAssignmentEdit() {
	m.setupAssignmentsExact = true
	m.setupAssignmentsEdited = true
	m.setupPreviewed = false
	m.setupConfirm = false
	m.setupPlan.Digest = ""
}

func (m *Model) resetSetupAssignments() {
	m.setupAssignmentRows = [SetupModelAssignmentCount]SetupModelAssignmentRequest{}
	m.setupAssignmentEntryRows = [SetupModelAssignmentCount]SetupModelAssignmentRequest{}
	m.setupAssignmentsSeeded, m.setupAssignmentsExact, m.setupAssignmentsEntry = false, false, false
	m.setupAssignmentsEdited, m.setupAssignmentsEntryEdited = false, false
	m.setupEditorPlan, m.setupEditorRequest, m.setupEditorPreviewed = SetupPlan{}, SetupRequest{}, false
}

func (m *Model) seedSetupAssignments(plan SetupPlan) {
	ordered, ok := orderedSetupAssignments(plan.ModelAssignments)
	if !ok {
		return
	}
	var rows [SetupModelAssignmentCount]SetupModelAssignmentRequest
	for index, assignment := range ordered {
		rows[index] = SetupModelAssignmentRequest{
			ArtifactKey: assignment.ArtifactKey, Provider: assignment.Provider, Reference: assignment.Model,
			RequestedEffort: assignment.RequestedEffort, Source: assignment.Source, Availability: assignment.Availability,
		}
	}
	m.setupAssignmentRows = rows
	m.setupAssignmentsSeeded = true
	m.setupAssignmentsExact = plan.ModelSchemaVersion >= 3
}

func (m *Model) restoreSetupEditorPreview() {
	if setupRequestsEqual(m.setupEditorRequest, m.setupRequest()) {
		m.setupPlan, m.setupPreviewRequest, m.setupPreviewed = cloneSetupPlan(m.setupEditorPlan), cloneSetupRequest(m.setupEditorRequest), m.setupEditorPreviewed
	}
}

func (m *Model) ensureMixedEffort(index int) {
	if !m.modelProfileMixed() {
		return
	}
	efforts := setupSupportedEfforts(m.setupModelRefs[index])
	if len(efforts) == 0 {
		m.setupModelEfforts[index] = ""
		return
	}
	if !supportsSetupEffort(efforts, m.setupModelEfforts[index]) {
		m.setupModelEfforts[index] = efforts[0]
	}
}

func nextSetupEffort(value string, supported []string) string {
	return cycleSetupEffort(value, supported, 1)
}

func cycleSetupEffort(value string, supported []string, offset int) string {
	if len(supported) == 0 {
		return value
	}
	for index, effort := range supported {
		if value == effort {
			return supported[(index+offset+len(supported))%len(supported)]
		}
	}
	if offset < 0 {
		return supported[len(supported)-1]
	}
	return supported[0]
}

func setupSupportedEfforts(reference string) []string {
	efforts := sdd.CatalogSupportedEfforts(reference)
	result := make([]string, len(efforts))
	for index, effort := range efforts {
		result[index] = string(effort)
	}
	return result
}

func supportsSetupEffort(supported []string, effort string) bool {
	for _, candidate := range supported {
		if candidate == effort {
			return true
		}
	}
	return false
}

func (m Model) modelProfileMixed() bool {
	provider := setupModelProvider(m.setupModelRefs[0])
	return provider == "" || provider != setupModelProvider(m.setupModelRefs[1]) || provider != setupModelProvider(m.setupModelRefs[2])
}

func setupModelProvider(reference string) string {
	provider, _, _ := strings.Cut(reference, "/")
	return provider
}
func (m Model) modelProfileChanged() bool {
	return m.setupOverrides || m.setupAssignmentsEdited
}
func (m Model) modelEditorError() string {
	if m.setupAssignmentsSeeded {
		for index, row := range m.setupAssignmentRows {
			if row.ArtifactKey != setupAgentRows[index].ArtifactKey || !validSetupModelReference(row.Reference) || row.Provider != setupModelProvider(row.Reference) {
				return "Every agent needs a valid catalog provider/model and effort."
			}
			supported := setupSupportedEfforts(row.Reference)
			if len(supported) == 0 {
				return "Model effort metadata is not available."
			}
			if !sdd.Effort(row.RequestedEffort).Valid() || !supportsSetupEffort(supported, row.RequestedEffort) {
				return "Known models require an effort declared by the SDD catalog."
			}
		}
		return ""
	}
	for _, ref := range m.setupModelRefs {
		if !validSetupModelReference(ref) {
			return "Each slot needs a provider/model reference."
		}
	}
	for _, ref := range m.setupModelRefs {
		if len(setupSupportedEfforts(ref)) == 0 {
			return "Model effort metadata is not available."
		}
	}
	if m.modelProfileMixed() {
		for index, effort := range m.setupModelEfforts {
			supported := setupSupportedEfforts(m.setupModelRefs[index])
			if len(supported) == 0 {
				return "Model effort metadata is not available."
			}
			if !sdd.Effort(effort).Valid() || !supportsSetupEffort(supported, effort) {
				return "Known models require an effort declared by the SDD catalog."
			}
		}
	} else {
		for _, effort := range m.setupModelEfforts {
			if effort != "" {
				return "Per-slot efforts require mixed providers."
			}
		}
	}
	return ""
}

func validSetupModelReference(reference string) bool {
	_, valid := modelcatalog.ValidReference(reference)
	return valid
}

func (m *Model) selectSetupPlan(offset int) tea.Cmd {
	if m.setupAssignmentsExact {
		return nil
	}
	index := setupPlanIndex(m.setupSelected)
	next := max(0, min(len(setupPlans)-1, index+offset))
	if next == index {
		return nil
	}
	m.setupSelected = setupPlans[next]
	return m.loadSetupPlan()
}

func setupPlanIndex(value string) int {
	for index, plan := range setupPlans {
		if value == plan {
			return index
		}
	}
	return 1
}

func validSetupPlan(value string) bool {
	return value == "low" || value == "medium" || value == "high" || value == "ultra"
}

func (m *Model) resizeSetup() {
	width := m.width
	if m.wide() {
		width -= 27
	}
	m.setupViewport.SetWidth(max(1, width))
	m.setupViewport.SetHeight(max(3, m.height-5))
}

func (m Model) renderSetupRoute() []string {
	preview := m.setupViewport
	offset := preview.YOffset()
	preview.SetContent(strings.Join(m.setupRouteLines(), "\n"))
	preview.SetYOffset(offset)
	return strings.Split(strings.TrimRight(preview.View(), "\n"), "\n")
}

func (m Model) setupRouteLines() []string {
	if m.setupView == setupViewRecovery {
		return m.recoveryRouteLines()
	}
	header := "selected plan  " + sanitizeTerminal(m.setupSelected)
	if m.setupAssignmentsExact {
		header = "per-agent assignments"
	}
	lines := []string{"OPENCODE SETUP", header}
	if m.setupModelEditing {
		if m.setupAssignmentsSeeded {
			return append(lines, m.modelAssignmentLines()...)
		}
		return append(lines, m.modelEditorLines()...)
	}
	switch {
	case m.setupApplying:
		lines = append(lines, "", "APPLYING SETUP · VERIFIED PLAN", "Waiting for the setup service; step progress is unavailable.")
		if m.setupCancelAsked {
			lines = append(lines, "! Cancellation requested. Waiting for setup recovery...")
		}
		return lines
	case m.setupSucceeded:
		result := m.setupResult
		outcome := setupResultOutcome(result)
		lines = append(lines,
			"✓ "+strings.ToUpper(outcome)+" COMPLETE",
			"changed  "+map[bool]string{true: "yes", false: "no"}[result.Changed],
			"launcher  "+setupValue(result.SelfInstallState)+"  "+setupValue(result.SelfInstallPath),
			"integration  "+setupValue(result.IntegrationState)+"  "+setupValue(result.IntegrationPath),
			"shared skills  "+setupValue(result.SkillsState)+"  "+setupValue(result.SkillsPath),
			fmt.Sprintf("artifacts  %d", result.ArtifactCount),
			setupHandshake(result.HandshakeOK, result.HandshakeStatus),
		)
		lines = append(lines, setupSelfInstallDetails(result.SelfInstallUpdateAvailable, result.SelfInstallRollbackAvailable, result.SelfInstallActiveSHA256, result.SelfInstallPreviousSHA256)...)
		lines = append(lines, setupPlanModelProfile(result.Plan, m.setupViewport.Width())...)
		if result.RestartRequired {
			target := "selected model plan"
			if m.setupAssignmentsExact {
				target = "per-agent assignments"
			}
			lines = append(lines, "! Restart OpenCode to load the "+target+".")
		} else {
			lines = append(lines, "Restart OpenCode if it is already running.")
		}
		return lines
	case m.setupApplyErr != nil:
		lines = append(lines, "✕ SETUP FAILED")
		if setupResultHasKnownState(m.setupResult) {
			lines = append(lines,
				"launcher  "+setupValue(m.setupResult.SelfInstallState)+"  "+setupValue(m.setupResult.SelfInstallPath),
				"integration  "+setupValue(m.setupResult.IntegrationState)+"  "+setupValue(m.setupResult.IntegrationPath),
				"shared skills  "+setupValue(m.setupResult.SkillsState)+"  "+setupValue(m.setupResult.SkillsPath),
			)
			lines = append(lines, setupSelfInstallDetails(m.setupResult.SelfInstallUpdateAvailable, m.setupResult.SelfInstallRollbackAvailable, m.setupResult.SelfInstallActiveSHA256, m.setupResult.SelfInstallPreviousSHA256)...)
		}
		if m.setupResult.Recovery != "" {
			lines = append(lines, "Recovery: "+sanitizeTerminal(m.setupResult.Recovery), "Action: inspect and refresh before retry.")
		} else if setupResultHasKnownState(m.setupResult) {
			lines = append(lines, "! The operation may be partial or unverified.", "Action: inspect and refresh before retry.")
		} else {
			lines = append(lines, "Action: refresh the preflight before retrying.")
		}
		return lines
	case m.setupPlanLoading:
		return append(lines, "", "... Loading verified setup preview...")
	case m.setupPlanErr != nil:
		return append(lines, "✕ SETUP PREVIEW UNAVAILABLE", "Action: check local setup prerequisites, then press r to retry.")
	}

	plan := m.setupPlan
	action := classifySetup(plan)
	readiness := "! BLOCKED / RECOVERY"
	if action == "initial install" || action == "reinstall/update" {
		readiness = "✓ READY TO APPLY: " + strings.ToUpper(action)
	}
	if action == "no changes" {
		readiness = "✓ NO CHANGES NEEDED"
	}
	lines = append(lines,
		readiness,
		"action  "+action,
		"digest  "+setupValue(plan.Digest),
		"launcher  "+setupValue(plan.SelfInstallState)+"  "+setupValue(plan.SelfInstallPath),
		"integration  "+setupValue(plan.IntegrationState)+"  "+setupValue(plan.IntegrationPath),
		"shared skills  "+setupValue(plan.SkillsState)+"  "+setupValue(plan.SkillsPath),
		fmt.Sprintf("artifacts  %d", plan.ArtifactCount),
		"model efficient  "+setupValue(plan.ModelEfficient),
		"model balanced   "+setupValue(plan.ModelBalanced),
		"model frontier   "+setupValue(plan.ModelFrontier),
		setupHandshake(plan.HandshakeOK, plan.HandshakeStatus),
	)
	if err := m.modelEditorError(); err != "" && (m.setupModelEditing || m.setupOverrides) {
		lines = append(lines, "✕ Model profile: "+err)
	}
	lines = append(lines, setupSelfInstallDetails(plan.SelfInstallUpdateAvailable, plan.SelfInstallRollbackAvailable, plan.SelfInstallActiveSHA256, plan.SelfInstallPreviousSHA256)...)
	if plan.Blocker != "" {
		lines = append(lines, "Blocker: "+sanitizeTerminal(plan.Blocker))
	}
	if action == "no changes" {
		lines = append(lines, "no changes detected by preflight", "Apply is disabled. Press r to refresh the preflight.")
	}
	if m.setupConfirm {
		lines = append(lines, "", "! CONFIRM "+strings.ToUpper(action), "No files change before y.")
		if m.setupAssignmentsExact {
			lines = append(lines, setupAssignmentRequestProfile(m.setupAssignmentRows, m.setupViewport.Width())...)
		} else if m.modelProfileChanged() {
			if m.modelProfileMixed() {
				lines = append(lines, "! Custom slots may have unknown availability; no remote probe was made.")
			}
			lines = append(lines, m.modelProfileSummary()...)
		}
		lines = append(lines, fmt.Sprintf("Apply this verified %d-step plan? [y] yes  [n/Esc] cancel", len(plan.Steps)))
	}
	lines = append(lines, "", fmt.Sprintf("%d-STEP PLAN (%d artifacts)", len(plan.Steps), plan.ArtifactCount))
	for _, step := range plan.Steps {
		marker := "check"
		if step.Mutates {
			marker = "write"
		}
		lines = append(lines, fmt.Sprintf("%d. %s  [%s]", step.Number, sanitizeTerminal(step.Title), marker))
		if m.wide() && step.Explanation != "" {
			lines = append(lines, "   "+sanitizeTerminal(step.Explanation))
		}
	}
	return lines
}

func (m Model) setupHelp() string {
	if m.setupView == setupViewRecovery {
		return m.recoveryHelp()
	}
	switch {
	case m.setupApplying:
		return "Applying: navigation and quit locked  [ctrl+c] emergency cancel"
	case m.setupConfirm:
		return "[j/k] scroll  [y] apply  [n/Esc] cancel  No write occurs until y"
	case m.setupModelEditing:
		if m.setupAssignmentsSeeded {
			help := "[↑↓/j/k] row  [←→] model"
			if len(setupSupportedEfforts(m.setupAssignmentRows[m.setupModelSlot].Reference)) > 0 {
				help += "  [[/]] effort"
			} else {
				help += "  effort not available"
			}
			return help + "  [r] refresh  [Enter] preview  [Esc] cancel  [q] quit"
		}
		help := "[j/k] slot  type/edit ref"
		if len(setupSupportedEfforts(m.setupModelRefs[m.setupModelSlot])) > 0 {
			help += "  [Tab] effort"
		} else {
			help += "  effort not available"
		}
		return help + "  [Enter] save  [Esc] cancel"
	case m.setupSucceeded:
		return "[j/k] scroll  [r] reload preview  [Esc] Overview  [q] quit"
	case m.setupApplyErr != nil:
		return "[r] inspect/refresh  [Esc] Overview"
	default:
		if m.setupAssignmentsExact {
			help := "[m] edit assignments  [Tab] Recovery  [r] refresh preflight"
			if m.setupApplyAllowed() {
				help += "  [a] apply"
			}
			return help + "  [j/k] scroll"
		}
		if classifySetup(m.setupPlan) == "blocked/recovery" {
			return "[Tab] Recovery  [r] refresh preflight  [Esc] Overview"
		}
		if classifySetup(m.setupPlan) == "no changes" {
			return "[r] refresh preflight  [Tab] Backup & Recovery  [Esc] Overview"
		}
		return "[m] model profile  [Tab] Recovery  [h/l] plan  [a] apply  [j/k] scroll"
	}
}

func (m Model) modelAssignmentLines() []string {
	lines := []string{"AGENT ASSIGNMENT MATRIX · 15 agents", "agent                  class/role          provider/model · effort"}
	for index, identity := range setupAgentRows {
		marker := " "
		if index == m.setupModelSlot {
			marker = "▸"
		}
		row := m.setupAssignmentRows[index]
		reference := setupValue(row.Reference)
		maxReference := max(12, m.setupViewport.Width()-54)
		if len([]rune(reference)) > maxReference {
			reference = string([]rune(reference)[:maxReference-1]) + "…"
		}
		lines = append(lines, fmt.Sprintf("%s %-22s %-19s %s · %s", marker, identity.Name, identity.Class+"/"+identity.Role, reference, setupValue(row.RequestedEffort)))
	}
	switch {
	case m.setupCatalogLoading:
		lines = append(lines, "... Loading local model catalog...")
	case m.setupCatalogErr != nil:
		lines = append(lines, "✕ Model catalog unavailable. [r] Retry explicit refresh.")
	case len(m.setupCatalog) == 0:
		lines = append(lines, "! No locally cached models. [r] Refresh explicitly.")
	default:
		lines = appendSetupWrapped(lines, "", fmt.Sprintf("✓ %d catalog models · %s", len(m.setupCatalog), setupDiscoveryDisclaimer), m.setupViewport.Width())
	}
	lines = append(lines, "allowed efforts  "+setupEffortAvailability(m.setupAssignmentRows[m.setupModelSlot].Reference))
	if err := m.modelEditorError(); err != "" {
		lines = append(lines, "✕ "+err)
	}
	return lines
}

func (m Model) modelEditorLines() []string {
	lines := []string{"MODEL PROFILE EDITOR", "Refs are local inputs; custom availability is unknown (no remote probe)."}
	for index, name := range []string{"efficient", "balanced", "frontier"} {
		marker := " "
		if index == m.setupModelSlot {
			marker = "▸"
		}
		availability := m.setupPlan.ModelEfficientAvailability
		if index == 1 {
			availability = m.setupPlan.ModelBalancedAvailability
		}
		if index == 2 {
			availability = m.setupPlan.ModelFrontierAvailability
		}
		if m.modelProfileChanged() {
			availability = "unknown"
		}
		lines = append(lines, fmt.Sprintf("%s %-9s %s  effort=%s  allowed=%s  availability=%s", marker, name, setupValue(m.setupModelRefs[index]), setupValue(m.setupModelEfforts[index]), setupEffortAvailability(m.setupModelRefs[index]), setupValue(availability)))
	}
	if err := m.modelEditorError(); err != "" {
		lines = append(lines, "✕ "+err)
	}
	return append(lines, "", "Enter previews valid inputs; Esc restores entry values.")
}

func setupEffortAvailability(reference string) string {
	supported := setupSupportedEfforts(reference)
	if len(supported) == 0 {
		return "not available"
	}
	return strings.Join(supported, ", ")
}

func (m Model) modelProfileSummary() []string {
	lines := []string{"Model profile:"}
	for index, name := range []string{"efficient", "balanced", "frontier"} {
		lines = append(lines, fmt.Sprintf("  %s=%s effort=%s", name, setupValue(m.setupModelRefs[index]), setupValue(m.setupModelEfforts[index])))
	}
	return lines
}

func setupPreviewRequest(request SetupRequest) SetupRequest {
	request.ExpectedPlanDigest = ""
	return request
}

func setupRequestsEqual(left, right SetupRequest) bool {
	left, right = setupPreviewRequest(left), setupPreviewRequest(right)
	if left.ModelAssignments == nil || right.ModelAssignments == nil {
		return left == right
	}
	leftRows, rightRows := *left.ModelAssignments, *right.ModelAssignments
	left.ModelAssignments, right.ModelAssignments = nil, nil
	return left == right && leftRows == rightRows
}

func setupPlanModelProfile(plan SetupPlan, width int) []string {
	if plan.ModelAssignments != nil {
		lines := []string{"EXACT AGENT ASSIGNMENTS"}
		ordered, ok := orderedSetupAssignments(plan.ModelAssignments)
		if !ok {
			return append(lines, "! Invalid agent identity set; exact profile not applied.")
		}
		for index, row := range ordered {
			lines = appendSetupAssignmentProfile(lines, setupAgentRows[index].Name, row.Model, []string{
				"requested=" + row.RequestedEffort, "effective=" + row.Effort, "variant=" + row.Variant,
				"source=" + row.Source, "availability=" + row.Availability,
			}, width)
			if row.Degraded {
				lines = appendSetupWrapped(lines, "    degraded=", row.DegradationReason, width)
			}
		}
		lines = appendSetupWrapped(lines, "", setupDiscoveryDisclaimer, width)
		return lines
	}
	if plan.ModelEfficient == "" && plan.ModelBalanced == "" && plan.ModelFrontier == "" {
		return nil
	}
	return []string{
		"model efficient  " + setupValue(plan.ModelEfficient) + "  effort=" + setupValue(plan.ModelEfficientEffort) + "  source=" + setupValue(plan.ModelEfficientSource) + "  availability=" + setupValue(plan.ModelEfficientAvailability),
		"model balanced   " + setupValue(plan.ModelBalanced) + "  effort=" + setupValue(plan.ModelBalancedEffort) + "  source=" + setupValue(plan.ModelBalancedSource) + "  availability=" + setupValue(plan.ModelBalancedAvailability),
		"model frontier   " + setupValue(plan.ModelFrontier) + "  effort=" + setupValue(plan.ModelFrontierEffort) + "  source=" + setupValue(plan.ModelFrontierSource) + "  availability=" + setupValue(plan.ModelFrontierAvailability),
	}
}

func setupAssignmentRequestProfile(rows [SetupModelAssignmentCount]SetupModelAssignmentRequest, width int) []string {
	lines := []string{"EXACT AGENT ASSIGNMENTS"}
	for index, row := range rows {
		lines = appendSetupAssignmentProfile(lines, setupAgentRows[index].Name, row.Reference, []string{
			"requested=" + row.RequestedEffort, "source=" + row.Source, "availability=" + row.Availability,
		}, width)
	}
	lines = appendSetupWrapped(lines, "", setupDiscoveryDisclaimer, width)
	return lines
}

func appendSetupAssignmentProfile(lines []string, name, reference string, details []string, width int) []string {
	name, reference = sanitizeTerminal(name), sanitizeTerminal(reference)
	line := "  " + name + "  model=" + reference
	width = max(20, width)
	if lipgloss.Width(line) <= width {
		return appendSetupDetails(append(lines, line), details, width)
	}
	lines = append(lines, "  "+name)
	lines = appendSetupWrapped(lines, "    model=", reference, width)
	return appendSetupDetails(lines, details, width)
}

func appendSetupDetails(lines, details []string, width int) []string {
	line := "    "
	for _, detail := range details {
		detail = sanitizeTerminal(detail)
		if lipgloss.Width(line)+1+lipgloss.Width(detail) > width && strings.TrimSpace(line) != "" {
			lines = append(lines, line)
			line = "    " + detail
			continue
		}
		if strings.TrimSpace(line) != "" {
			line += " "
		}
		line += detail
	}
	if strings.TrimSpace(line) != "" {
		lines = append(lines, line)
	}
	return lines
}

func appendSetupWrapped(lines []string, prefix, value string, width int) []string {
	wrapped := lipgloss.Wrap(prefix+sanitizeTerminal(value), max(20, width), "")
	return append(lines, strings.Split(wrapped, "\n")...)
}

func orderedSetupAssignments(rows *[SetupModelAssignmentCount]SetupModelAssignment) ([SetupModelAssignmentCount]SetupModelAssignment, bool) {
	var ordered [SetupModelAssignmentCount]SetupModelAssignment
	if rows == nil {
		return ordered, false
	}
	for _, row := range rows {
		index := -1
		for candidate, identity := range setupAgentRows {
			if row.ArtifactKey == identity.ArtifactKey && row.Role == identity.Role && row.Class == identity.Class {
				index = candidate
				break
			}
		}
		if index < 0 || ordered[index].ArtifactKey != "" {
			return [SetupModelAssignmentCount]SetupModelAssignment{}, false
		}
		ordered[index] = row
	}
	return ordered, true
}

func setupResultHasKnownState(result SetupResult) bool {
	return result.SelfInstallState != "" || result.SelfInstallPath != "" || result.IntegrationState != "" || result.IntegrationPath != "" || result.SkillsState != "" || result.SkillsPath != "" || result.SelfInstallActiveSHA256 != "" || result.SelfInstallPreviousSHA256 != "" || result.SelfInstallRollbackAvailable
}

func classifySetup(plan SetupPlan) string {
	if !plan.Ready || plan.Blocker != "" || plan.SelfInstallState == "drifted" || plan.SelfInstallState == "recovery_pending" || plan.IntegrationState == "drifted" || plan.SkillsState == "drifted" || plan.SkillsState == "conflict" {
		return "blocked/recovery"
	}
	if !plan.HandshakeOK {
		return "blocked/recovery"
	}
	if plan.SelfInstallState == "absent" {
		return "initial install"
	}
	if plan.SelfInstallState == "installed" && plan.IntegrationState == "installed" && plan.SkillsState == "installed" && !plan.SelfInstallChanged && !plan.SelfInstallUpdateAvailable && !plan.IntegrationChanged && !plan.IntegrationRestartRequired && !plan.SkillsChanged && !plan.SkillsUpdateNeeded {
		return "no changes"
	}
	return "reinstall/update"
}

func setupResultOutcome(result SetupResult) string {
	if !result.Changed {
		return "no changes"
	}
	if result.Plan.SelfInstallState == "absent" {
		return "initial install"
	}
	return "reinstalled/updated"
}

func setupSelfInstallDetails(update, rollback bool, activeSHA, previousSHA string) []string {
	if !update && !rollback && activeSHA == "" && previousSHA == "" {
		return nil
	}
	lines := []string{"self update  " + map[bool]string{true: "yes", false: "no"}[update]}
	if activeSHA != "" {
		lines = append(lines, "active SHA  "+setupValue(activeSHA))
	}
	if previousSHA != "" {
		lines = append(lines, "previous SHA  "+setupValue(previousSHA))
	}
	lines = append(lines, "rollback  "+map[bool]string{true: "available", false: "unavailable"}[rollback])
	return lines
}

func setupValue(value string) string {
	if value == "" {
		return "-"
	}
	return sanitizeTerminal(value)
}

func setupHandshake(ok bool, status string) string {
	marker := "✕"
	if ok {
		marker = "✓"
	}
	return marker + " OpenCode handshake  " + setupValue(status)
}

func cloneSetupPlan(value SetupPlan) SetupPlan {
	value.Steps = append([]SetupStep(nil), value.Steps...)
	if value.ModelAssignments != nil {
		assignments := *value.ModelAssignments
		value.ModelAssignments = &assignments
	}
	return value
}

func cloneSetupStatus(value SetupStatus) SetupStatus {
	if value.ModelAssignments != nil {
		assignments := *value.ModelAssignments
		value.ModelAssignments = &assignments
	}
	return value
}

func cloneSetupRequest(value SetupRequest) SetupRequest {
	if value.ModelAssignments != nil {
		rows := *value.ModelAssignments
		value.ModelAssignments = &rows
	}
	return value
}

func cloneSetupResult(value SetupResult) SetupResult {
	value.Plan = cloneSetupPlan(value.Plan)
	return value
}
