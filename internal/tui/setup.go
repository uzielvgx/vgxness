package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/vgxness/vgxness/internal/modelcatalog"
	setupflow "github.com/vgxness/vgxness/internal/setup"
)

const (
	defaultSetupPlan          = "medium"
	SetupModelAssignmentCount = 15
	setupDiscoveryDisclaimer  = "Local discovery proves identifier presence only; not authorization or support."
)

var setupPlans = [...]string{"low", "medium", "high", "ultra"}

type SetupRequest struct {
	Workspace              string
	Plan                   string
	ModelEfficient         string
	ModelBalanced          string
	ModelFrontier          string
	ModelEfficientEffort   string
	ModelBalancedEffort    string
	ModelFrontierEffort    string
	ModelEfficientVariant  string
	ModelBalancedVariant   string
	ModelFrontierVariant   string
	ModelVariantsSpecified bool
	ModelAssignments       *[SetupModelAssignmentCount]SetupModelAssignmentRequest
	ExpectedPlanDigest     string
}

// MultiSetupRequest is the optional composite setup contract owned by the TUI.
type MultiSetupRequest struct {
	Setup              SetupRequest
	Providers          []setupflow.Provider
	ExpectedPlanDigest string
	Verified           []setupflow.ProviderResult
}

type MultiSetupBackend interface {
	PlanMultiSetup(context.Context, MultiSetupRequest) (setupflow.MultiPlan, error)
	ApplyMultiSetup(context.Context, MultiSetupRequest) (setupflow.MultiResult, error)
}

type SetupModelAssignmentRequest struct {
	ArtifactKey      string
	Provider         string
	Reference        string
	RequestedEffort  string
	Variant          string
	VariantSpecified bool
	Source           string
	Availability     string
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
	VariantSpecified  bool
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
	ModelEfficientVariant        string
	ModelBalancedVariant         string
	ModelFrontierVariant         string
	ModelVariantsSpecified       bool
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
	multi      *setupflow.MultiPlan
}

type setupAppliedMsg struct {
	generation int
	value      SetupResult
	err        error
	multi      *setupflow.MultiResult
}

func (m *Model) initSetup() {
	preview := viewport.New(viewport.WithWidth(80), viewport.WithHeight(16))
	preview.SoftWrap = true
	preview.FillHeight = false
	m.setupViewport = preview
	m.setupSelected = defaultSetupPlan
	m.setupView = setupViewInstall
	if _, ok := m.backend.(MultiSetupBackend); ok {
		m.setupProviders = []setupflow.Provider{setupflow.ProviderOpenCode}
	}
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
	multiRequest := m.multiSetupRequest()
	return func() tea.Msg {
		if backend, ok := m.backend.(MultiSetupBackend); ok {
			value, err := backend.PlanMultiSetup(ctx, multiRequest)
			return setupPlanLoadedMsg{generation: generation, request: request, multi: &value, err: err}
		}
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
	multiRequest := m.multiSetupRequest()
	multiRequest.ExpectedPlanDigest = m.setupMultiPlan.Digest
	return func() tea.Msg {
		if backend, ok := m.backend.(MultiSetupBackend); ok {
			value, err := backend.ApplyMultiSetup(ctx, multiRequest)
			return setupAppliedMsg{generation: generation, multi: &value, err: err}
		}
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
		if msg.multi != nil {
			m.setupMultiPlan = *msg.multi
			m.setupPlan = SetupPlan{Digest: msg.multi.Digest, Ready: msg.multi.Ready, Blocker: msg.multi.Blocker, ModelPlan: m.setupSelected}
			m.setupPreviewRequest, m.setupPreviewed = m.setupRequest(), true
			m.setupViewport.GotoTop()
			return
		}
		m.setupPlan = cloneSetupPlan(msg.value)
		m.setupPreviewRequest = msg.request
		m.setupPreviewed = true
		if !m.setupOverrides {
			m.setupModelRefs = [3]string{msg.value.ModelEfficient, msg.value.ModelBalanced, msg.value.ModelFrontier}
			m.setupModelEfforts = [3]string{msg.value.ModelEfficientEffort, msg.value.ModelBalancedEffort, msg.value.ModelFrontierEffort}
			m.setupModelVariants = [3]string{msg.value.ModelEfficientVariant, msg.value.ModelBalancedVariant, msg.value.ModelFrontierVariant}
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
	if msg.multi != nil {
		m.setupMultiResult = *msg.multi
		m.setupApplyErr = msg.err
		m.setupSucceeded = msg.err == nil
		m.setupCancelAsked = false
		m.setupViewport.GotoTop()
		return
	}
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
	case "o", "c":
		if m.multiSetupEnabled() {
			provider := setupflow.ProviderOpenCode
			if msg.String() == "c" {
				provider = setupflow.ProviderCodex
			}
			m.toggleSetupProvider(provider)
			return true, m.loadSetupPlan()
		}
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
		if m.multiSetupEnabled() && !m.hasSetupProvider(setupflow.ProviderOpenCode) {
			return true, nil
		}
		m.setupModelEditing = true
		m.setupModelSlot = 0
		m.setupAssignmentEntryRows, m.setupAssignmentsEntry = m.setupAssignmentRows, m.setupAssignmentsExact
		m.setupAssignmentsEntryEdited = m.setupAssignmentsEdited
		m.setupModelEntryRefs, m.setupModelEntryEfforts, m.setupModelEntryVars, m.setupEntryOverrides = m.setupModelRefs, m.setupModelEfforts, m.setupModelVariants, m.setupOverrides
		m.setupEditorPlan, m.setupEditorRequest, m.setupEditorPreviewed = cloneSetupPlan(m.setupPlan), cloneSetupRequest(m.setupPreviewRequest), m.setupPreviewed
		return true, m.loadSetupCatalog(false)
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

func (m Model) multiSetupEnabled() bool { _, ok := m.backend.(MultiSetupBackend); return ok }
func (m Model) hasSetupProvider(provider setupflow.Provider) bool {
	for _, value := range m.setupProviders {
		if value == provider {
			return true
		}
	}
	return false
}
func (m *Model) toggleSetupProvider(provider setupflow.Provider) {
	m.setupConfirm, m.setupSucceeded, m.setupApplyErr = false, false, nil
	m.setupResult, m.setupMultiResult = SetupResult{}, setupflow.MultiResult{}
	if m.hasSetupProvider(provider) {
		if len(m.setupProviders) == 1 {
			return
		}
		for index, value := range m.setupProviders {
			if value == provider {
				m.setupProviders = append(m.setupProviders[:index], m.setupProviders[index+1:]...)
				if m.hasSetupProvider(setupflow.ProviderCodex) && !m.hasSetupProvider(setupflow.ProviderOpenCode) {
					m.recoveryMode = RecoveryModeManaged
				}
				return
			}
		}
	}
	m.setupProviders = append(m.setupProviders, provider)
	if m.hasSetupProvider(setupflow.ProviderCodex) && !m.hasSetupProvider(setupflow.ProviderOpenCode) {
		m.recoveryMode = RecoveryModeManaged
	}
}
func (m Model) multiSetupRequest() MultiSetupRequest {
	verified := make([]setupflow.ProviderResult, 0, len(m.setupMultiResult.Providers))
	for _, outcome := range m.setupMultiResult.Providers {
		if outcome.Verified {
			verified = append(verified, outcome)
		}
	}
	return MultiSetupRequest{Setup: m.setupRequest(), Providers: append([]setupflow.Provider(nil), m.setupProviders...), Verified: verified}
}

func (m Model) setupApplyAllowed() bool {
	if m.multiSetupEnabled() {
		return !m.setupPlanLoading && m.setupPreviewed && m.setupMultiPlan.Digest != "" && setupRequestsEqual(m.setupPreviewRequest, m.setupRequest()) && m.setupPlanErr == nil && m.setupApplyErr == nil && !m.setupSucceeded && m.setupMultiPlan.Ready && m.setupMultiPlan.Blocker == "" && (m.setupMultiPlan.Changed || m.multiSetupHasUnverifiedProvider()) && (!m.hasSetupProvider(setupflow.ProviderOpenCode) || !m.modelProfileChanged() || m.modelEditorError() == "")
	}
	if m.setupPlanLoading || !m.setupPreviewed || m.setupPlan.Digest == "" || !setupRequestsEqual(m.setupPreviewRequest, m.setupRequest()) || m.setupPlanErr != nil || m.setupApplyErr != nil || m.setupSucceeded || !m.setupPlan.Ready || ((m.setupOverrides || m.setupAssignmentsExact || m.setupAssignmentsSeeded && m.setupCatalogAvailable()) && m.modelEditorError() != "") {
		return false
	}
	action := classifySetup(m.setupPlan)
	return action == "initial install" || action == "reinstall/update"
}

func (m Model) multiSetupHasUnverifiedProvider() bool {
	verified := make(map[setupflow.Provider]bool, len(m.setupMultiResult.Providers))
	for _, outcome := range m.setupMultiResult.Providers {
		if outcome.Verified {
			verified[outcome.Provider] = true
		}
	}
	for _, provider := range m.setupMultiPlan.Providers {
		if !verified[provider.Provider] {
			return true
		}
	}
	return false
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
		request.ModelEfficientEffort, request.ModelBalancedEffort, request.ModelFrontierEffort = m.setupModelEfforts[0], m.setupModelEfforts[1], m.setupModelEfforts[2]
		request.ModelEfficientVariant, request.ModelBalancedVariant, request.ModelFrontierVariant = m.setupModelVariants[0], m.setupModelVariants[1], m.setupModelVariants[2]
		request.ModelVariantsSpecified = true
	}
	return request
}

func (m *Model) updateModelEditorKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if m.setupAssignmentsSeeded {
		return m.updateAssignmentEditorKey(msg)
	}
	switch msg.String() {
	case "esc":
		m.setupModelRefs, m.setupModelEfforts, m.setupModelVariants, m.setupOverrides = m.setupModelEntryRefs, m.setupModelEntryEfforts, m.setupModelEntryVars, m.setupEntryOverrides
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
		m.setupModelVariants[m.setupModelSlot] = nextSetupVariant(m.setupModelVariants[m.setupModelSlot], m.setupVariantsForModel(m.setupModelRefs[m.setupModelSlot]))
		return true, nil
	case "backspace":
		value := []rune(m.setupModelRefs[m.setupModelSlot])
		if len(value) > 0 {
			m.setupModelRefs[m.setupModelSlot] = string(value[:len(value)-1])
		}
		m.ensureModelVariant(m.setupModelSlot)
		return true, nil
	}
	if msg.Text != "" {
		m.setupModelRefs[m.setupModelSlot] += msg.Text
		m.ensureModelVariant(m.setupModelSlot)
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
		m.selectAssignmentVariant(-1)
	case "]":
		m.selectAssignmentVariant(1)
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
	variants := m.setupVariantsForModel(row.Reference)
	row.VariantSpecified = true
	if len(variants) == 0 {
		row.Variant = ""
	} else if !supportsSetupVariant(variants, row.Variant) {
		row.Variant = variants[0]
	}
	m.markAssignmentEdit()
}

func (m *Model) selectAssignmentVariant(offset int) {
	row := &m.setupAssignmentRows[m.setupModelSlot]
	next := cycleSetupVariant(row.Variant, m.setupVariantsForModel(row.Reference), offset)
	if next == row.Variant {
		return
	}
	row.Variant, row.VariantSpecified = next, true
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
		variant := assignment.Variant
		if !assignment.VariantSpecified {
			variant = ""
		}
		rows[index] = SetupModelAssignmentRequest{
			ArtifactKey: assignment.ArtifactKey, Provider: assignment.Provider, Reference: assignment.Model,
			RequestedEffort: assignment.RequestedEffort, Variant: variant, VariantSpecified: assignment.VariantSpecified, Source: assignment.Source, Availability: assignment.Availability,
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

func (m *Model) ensureModelVariant(index int) {
	variants := m.setupVariantsForModel(m.setupModelRefs[index])
	if len(variants) == 0 {
		m.setupModelVariants[index] = ""
		return
	}
	if !supportsSetupVariant(variants, m.setupModelVariants[index]) {
		m.setupModelVariants[index] = variants[0]
	}
}

func nextSetupVariant(value string, supported []string) string {
	return cycleSetupVariant(value, supported, 1)
}

func cycleSetupVariant(value string, supported []string, offset int) string {
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

func supportsSetupVariant(supported []string, variant string) bool {
	for _, candidate := range supported {
		if candidate == variant {
			return true
		}
	}
	return false
}

func (m Model) setupVariantsForModel(reference string) []string {
	for _, row := range m.setupCatalog {
		if row.Reference == reference {
			return append([]string(nil), row.Variants...)
		}
	}
	return nil
}

func (m Model) knownSetupModel(reference string) bool {
	for _, row := range m.setupCatalog {
		if row.Reference == reference {
			return true
		}
	}
	return false
}

func (m Model) setupCatalogAvailable() bool {
	return m.setupCatalogGeneration != 0 && !m.setupCatalogLoading && (m.setupCatalogErr == nil || len(m.setupCatalog) != 0)
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
			if row.ArtifactKey != setupAgentRows[index].ArtifactKey || !validSetupModelReference(row.Reference) || row.Provider != setupModelProvider(row.Reference) || !m.knownSetupModel(row.Reference) {
				return "Every agent needs a discovered provider/model and variant."
			}
			variants := m.setupVariantsForModel(row.Reference)
			if len(variants) > 0 && row.Variant != "" && !supportsSetupVariant(variants, row.Variant) || len(variants) == 0 && row.Variant != "" {
				return "Known models require a discovered variant or provider default."
			}
		}
		return ""
	}
	for _, ref := range m.setupModelRefs {
		if !validSetupModelReference(ref) {
			return "Each slot needs a provider/model reference."
		}
	}
	for index, ref := range m.setupModelRefs {
		if !m.knownSetupModel(ref) {
			return "Model variant metadata is not available."
		}
		variants := m.setupVariantsForModel(ref)
		if len(variants) > 0 && !supportsSetupVariant(variants, m.setupModelVariants[index]) || len(variants) == 0 && m.setupModelVariants[index] != "" {
			return "Known models require a discovered variant or provider default."
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
	if m.multiSetupEnabled() {
		return m.multiSetupRouteLines()
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
			lines = append(lines, "! Discovery proves identifier presence only; no remote probe was made.")
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

func (m Model) multiSetupRouteLines() []string {
	providers := ""
	for _, provider := range []setupflow.Provider{setupflow.ProviderOpenCode, setupflow.ProviderCodex} {
		marker := " "
		if m.hasSetupProvider(provider) {
			marker = "✓"
		}
		providers += marker + " " + string(provider) + "  "
	}
	lines := []string{"MULTI-PROVIDER SETUP", "providers  " + strings.TrimSpace(providers), "shared plan  " + sanitizeTerminal(m.setupSelected)}
	if m.hasSetupProvider(setupflow.ProviderCodex) {
		lines = append(lines, "! Codex: shared plan only; OpenCode custom slots do not apply.")
	}
	if m.setupPlanLoading {
		return append(lines, "", "... Loading verified provider preview...")
	}
	if m.setupApplying {
		return append(lines, "", "... Applying shared work then providers in order...")
	}
	if m.setupApplyErr != nil {
		lines = append(lines, "✕ SETUP PARTIAL/FAILED")
		lines = append(lines, "Reason: "+sanitizeTerminal(m.setupApplyErr.Error()))
		for _, row := range m.setupMultiResult.Providers {
			lines = append(lines, providerOutcomeLine(row))
		}
		if m.setupMultiResult.Shared.Recovery != "" {
			lines = append(lines, "Recovery: "+sanitizeTerminal(m.setupMultiResult.Shared.Recovery))
		}
		return append(lines, "Action: [r] replan/retry; verified unchanged providers are skipped.")
	}
	if m.setupSucceeded {
		lines = append(lines, "✓ SETUP COMPLETE")
		for _, row := range m.setupMultiResult.Providers {
			lines = append(lines, providerOutcomeLine(row))
		}
		return lines
	}
	plan := m.setupMultiPlan
	state := "! BLOCKED"
	if plan.Ready {
		state = "✓ READY TO APPLY"
	}
	if plan.Ready && !plan.Changed {
		state = "✓ NO CHANGES"
	}
	lines = append(lines, state, "digest  "+setupValue(plan.Digest))
	for _, row := range plan.Providers {
		glyph := "!"
		if row.Ready {
			glyph = "✓"
		}
		status := "needs install"
		if row.Installed && !row.Changed {
			status = "installed"
		}
		lines = append(lines, glyph+" "+string(row.Provider)+"  "+status)
		if row.Blocker != "" {
			lines = append(lines, "  blocker  "+sanitizeTerminal(row.Blocker))
		}
	}
	if plan.Blocker != "" {
		lines = append(lines, "Blocker: "+sanitizeTerminal(plan.Blocker))
	}
	if m.setupConfirm {
		lines = append(lines, "", "! CONFIRM MULTI-PROVIDER APPLY", "Apply shared work once, then providers? [y] yes  [n/Esc] cancel")
	}
	return lines
}

func providerOutcomeLine(row setupflow.ProviderResult) string {
	glyph := "✕"
	if row.Verified {
		glyph = "✓"
	}
	if row.Skipped {
		glyph = "✓"
	}
	value := "unverified"
	if row.Verified {
		value = "verified"
	}
	if row.Skipped {
		value = "verified unchanged; skipped"
	}
	return glyph + " " + string(row.Provider) + "  " + value
}

func (m Model) setupHelp() string {
	if m.setupView == setupViewRecovery {
		return m.recoveryHelp()
	}
	if m.multiSetupEnabled() {
		if m.setupApplying {
			return "Applying: navigation and quit locked  [ctrl+c] emergency cancel"
		}
		if m.setupConfirm {
			return "[y] apply  [n/Esc] cancel  No write occurs until y"
		}
		help := "[o] OpenCode  [c] Codex  [h/l] shared plan  [r] refresh"
		if m.hasSetupProvider(setupflow.ProviderOpenCode) {
			help += "  [m] model profile  [Tab] Recovery"
		} else {
			help += "  [Tab] verification/retry"
		}
		if m.setupApplyAllowed() {
			help += "  [a] apply"
		}
		return help
	}
	switch {
	case m.setupApplying:
		return "Applying: navigation and quit locked  [ctrl+c] emergency cancel"
	case m.setupConfirm:
		return "[j/k] scroll  [y] apply  [n/Esc] cancel  No write occurs until y"
	case m.setupModelEditing:
		if m.setupAssignmentsSeeded {
			help := "[↑↓/j/k] row  [←→] model"
			if len(m.setupVariantsForModel(m.setupAssignmentRows[m.setupModelSlot].Reference)) > 0 {
				help += "  [[/]] variant"
			} else if m.knownSetupModel(m.setupAssignmentRows[m.setupModelSlot].Reference) {
				help += "  provider default"
			} else {
				help += "  variant not available"
			}
			return help + "  [r] refresh  [Enter] preview  [Esc] cancel  [q] quit"
		}
		help := "[j/k] slot  type/edit ref"
		if len(m.setupVariantsForModel(m.setupModelRefs[m.setupModelSlot])) > 0 {
			help += "  [Tab] variant"
		} else if m.knownSetupModel(m.setupModelRefs[m.setupModelSlot]) {
			help += "  provider default"
		} else {
			help += "  variant not available"
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
	lines := []string{"AGENT ASSIGNMENT MATRIX · 15 agents", "agent                  class/role          provider/model · variant"}
	for index, identity := range setupAgentRows {
		marker := " "
		if index == m.setupModelSlot {
			marker = "▸"
		}
		row := m.setupAssignmentRows[index]
		reference := setupValue(row.Reference)
		variant := truncateSetupRunes(setupValue(row.Variant), 12)
		maxReference := max(12, m.setupViewport.Width()-48-lipgloss.Width(variant))
		reference = truncateSetupRunes(reference, maxReference)
		lines = append(lines, fmt.Sprintf("%s %-22s %-19s %s · %s", marker, identity.Name, identity.Class+"/"+identity.Role, reference, variant))
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
	lines = append(lines, "allowed variants  "+m.setupVariantAvailability(m.setupAssignmentRows[m.setupModelSlot].Reference))
	if err := m.modelEditorError(); err != "" {
		lines = append(lines, "✕ "+err)
	}
	return lines
}

func truncateSetupRunes(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum-1]) + "…"
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
		lines = append(lines, fmt.Sprintf("%s %-9s %s  variant=%s  allowed=%s  availability=%s", marker, name, setupValue(m.setupModelRefs[index]), setupValue(m.setupModelVariants[index]), m.setupVariantAvailability(m.setupModelRefs[index]), setupValue(availability)))
	}
	if err := m.modelEditorError(); err != "" {
		lines = append(lines, "✕ "+err)
	}
	return append(lines, "", "Enter previews valid inputs; Esc restores entry values.")
}

func (m Model) setupVariantAvailability(reference string) string {
	supported := m.setupVariantsForModel(reference)
	if len(supported) > 0 {
		return strings.Join(supported, ", ")
	}
	if m.knownSetupModel(reference) {
		return "provider default"
	}
	return "not available"
}

func (m Model) modelProfileSummary() []string {
	lines := []string{"Model profile:"}
	for index, name := range []string{"efficient", "balanced", "frontier"} {
		lines = append(lines, fmt.Sprintf("  %s=%s variant=%s", name, setupValue(m.setupModelRefs[index]), setupValue(m.setupModelVariants[index])))
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
		"model efficient  " + setupValue(plan.ModelEfficient) + "  variant=" + setupValue(plan.ModelEfficientVariant) + "  source=" + setupValue(plan.ModelEfficientSource) + "  availability=" + setupValue(plan.ModelEfficientAvailability),
		"model balanced   " + setupValue(plan.ModelBalanced) + "  variant=" + setupValue(plan.ModelBalancedVariant) + "  source=" + setupValue(plan.ModelBalancedSource) + "  availability=" + setupValue(plan.ModelBalancedAvailability),
		"model frontier   " + setupValue(plan.ModelFrontier) + "  variant=" + setupValue(plan.ModelFrontierVariant) + "  source=" + setupValue(plan.ModelFrontierSource) + "  availability=" + setupValue(plan.ModelFrontierAvailability),
	}
}

func setupAssignmentRequestProfile(rows [SetupModelAssignmentCount]SetupModelAssignmentRequest, width int) []string {
	lines := []string{"EXACT AGENT ASSIGNMENTS"}
	for index, row := range rows {
		lines = appendSetupAssignmentProfile(lines, setupAgentRows[index].Name, row.Reference, []string{
			"variant=" + row.Variant, "source=" + row.Source, "availability=" + row.Availability,
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
