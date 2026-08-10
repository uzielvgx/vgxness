package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

const defaultSetupPlan = "medium"

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
	ExpectedPlanDigest   string
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
}

func (m *Model) finishSetupOperation() {
	if m.cancelSetup != nil {
		m.cancelSetup()
		m.cancelSetup = nil
	}
}

func (m *Model) handleSetupPlanLoaded(msg setupPlanLoadedMsg) {
	if msg.generation != m.setupGeneration || setupPreviewRequest(msg.request) != setupPreviewRequest(m.setupRequest()) {
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
		m.setup = SetupStatus{
			Provider: msg.value.Plan.Provider, Ready: true,
			SelfInstallState: msg.value.SelfInstallState, SelfInstallPath: msg.value.SelfInstallPath,
			IntegrationState: msg.value.IntegrationState, IntegrationPath: msg.value.IntegrationPath,
			ArtifactCount: msg.value.ArtifactCount,
			HandshakeOK:   msg.value.HandshakeOK, HandshakeStatus: msg.value.HandshakeStatus,
			ModelPlan: msg.value.Plan.ModelPlan,
		}
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
		m.setupModelEntryRefs, m.setupModelEntryEfforts, m.setupEntryOverrides = m.setupModelRefs, m.setupModelEfforts, m.setupOverrides
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
	if m.setupPlanLoading || !m.setupPreviewed || m.setupPlan.Digest == "" || m.setupPreviewRequest != m.setupRequest() || m.setupPlanErr != nil || m.setupApplyErr != nil || m.setupSucceeded || !m.setupPlan.Ready || (m.setupOverrides && m.modelEditorError() != "") {
		return false
	}
	action := classifySetup(m.setupPlan)
	return action == "initial install" || action == "reinstall/update"
}

func (m Model) setupRequest() SetupRequest {
	request := SetupRequest{Workspace: m.options.Workspace, Plan: m.setupSelected}
	if m.setupOverrides {
		request.ModelEfficient, request.ModelBalanced, request.ModelFrontier = m.setupModelRefs[0], m.setupModelRefs[1], m.setupModelRefs[2]
		if m.modelProfileMixed() {
			request.ModelEfficientEffort, request.ModelBalancedEffort, request.ModelFrontierEffort = m.setupModelEfforts[0], m.setupModelEfforts[1], m.setupModelEfforts[2]
		}
	}
	return request
}

func (m *Model) updateModelEditorKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
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
		m.setupModelEfforts[m.setupModelSlot] = nextSetupEffort(m.setupModelEfforts[m.setupModelSlot])
		return true, nil
	case "backspace":
		value := []rune(m.setupModelRefs[m.setupModelSlot])
		if len(value) > 0 {
			m.setupModelRefs[m.setupModelSlot] = string(value[:len(value)-1])
		}
		m.ensureMixedEfforts()
		return true, nil
	}
	if msg.Text != "" {
		m.setupModelRefs[m.setupModelSlot] += msg.Text
		m.ensureMixedEfforts()
		return true, nil
	}
	return true, nil
}

func (m *Model) ensureMixedEfforts() {
	if m.modelProfileMixed() {
		for index, effort := range m.setupModelEfforts {
			if !validSetupPlan(effort) {
				m.setupModelEfforts[index] = "medium"
			}
		}
	}
}

func nextSetupEffort(value string) string {
	for index, effort := range setupPlans {
		if value == effort {
			return setupPlans[(index+1)%len(setupPlans)]
		}
	}
	return "medium"
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
	return m.setupOverrides
}
func (m Model) modelEditorError() string {
	for _, ref := range m.setupModelRefs {
		if !validSetupModelReference(ref) {
			return "Each slot needs a provider/model reference."
		}
	}
	if m.modelProfileMixed() {
		for _, effort := range m.setupModelEfforts {
			if !validSetupPlan(effort) {
				return "Mixed profiles require low, medium, high, or ultra for every slot."
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
	provider, model, found := strings.Cut(reference, "/")
	return found && len(reference) <= 256 && strings.TrimSpace(reference) == reference && validSetupModelPart(provider, false) && validSetupModelPart(model, true)
}

func validSetupModelPart(value string, slash bool) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.' || slash && character == '/' {
			continue
		}
		return false
	}
	return true
}

func (m *Model) selectSetupPlan(offset int) tea.Cmd {
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
	lines := []string{"OPENCODE SETUP", "selected plan  " + sanitizeTerminal(m.setupSelected)}
	if m.setupModelEditing {
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
		lines = append(lines, setupPlanModelProfile(result.Plan)...)
		if result.RestartRequired {
			lines = append(lines, "! Restart OpenCode to load the selected model plan.")
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
		if m.modelProfileChanged() {
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
		return "[y] apply  [n/Esc] cancel  No write occurs until y"
	case m.setupModelEditing:
		return "[j/k] slot  type/edit ref  [Tab] effort  [Enter] save  [Esc] cancel"
	case m.setupSucceeded:
		return "[r] reload preview  [Esc] Overview  [q] quit"
	case m.setupApplyErr != nil:
		return "[r] inspect/refresh  [Esc] Overview"
	default:
		if classifySetup(m.setupPlan) == "blocked/recovery" {
			return "[Tab] Recovery  [r] refresh preflight  [Esc] Overview"
		}
		if classifySetup(m.setupPlan) == "no changes" {
			return "[r] refresh preflight  [Tab] Backup & Recovery  [Esc] Overview"
		}
		return "[m] model profile  [Tab] Recovery  [h/l] plan  [a] apply  [j/k] scroll"
	}
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
		lines = append(lines, fmt.Sprintf("%s %-9s %s  effort=%s  availability=%s", marker, name, setupValue(m.setupModelRefs[index]), setupValue(m.setupModelEfforts[index]), setupValue(availability)))
	}
	if err := m.modelEditorError(); err != "" {
		lines = append(lines, "✕ "+err)
	}
	return append(lines, "", "Enter previews valid inputs; Esc restores entry values.")
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

func setupPlanModelProfile(plan SetupPlan) []string {
	if plan.ModelEfficient == "" && plan.ModelBalanced == "" && plan.ModelFrontier == "" {
		return nil
	}
	return []string{
		"model efficient  " + setupValue(plan.ModelEfficient) + "  effort=" + setupValue(plan.ModelEfficientEffort) + "  source=" + setupValue(plan.ModelEfficientSource) + "  availability=" + setupValue(plan.ModelEfficientAvailability),
		"model balanced   " + setupValue(plan.ModelBalanced) + "  effort=" + setupValue(plan.ModelBalancedEffort) + "  source=" + setupValue(plan.ModelBalancedSource) + "  availability=" + setupValue(plan.ModelBalancedAvailability),
		"model frontier   " + setupValue(plan.ModelFrontier) + "  effort=" + setupValue(plan.ModelFrontierEffort) + "  source=" + setupValue(plan.ModelFrontierSource) + "  availability=" + setupValue(plan.ModelFrontierAvailability),
	}
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
	return value
}

func cloneSetupResult(value SetupResult) SetupResult {
	value.Plan = cloneSetupPlan(value.Plan)
	return value
}
