package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

const defaultSetupPlan = "medium"

var setupPlans = [...]string{"low", "medium", "high"}

type SetupRequest struct {
	Workspace string
	Plan      string
}

type SetupStep struct {
	Number      int
	Title       string
	Explanation string
	Mutates     bool
}

type SetupPlan struct {
	Provider         string
	Steps            []SetupStep
	SelfInstallState string
	SelfInstallPath  string
	IntegrationState string
	IntegrationPath  string
	ArtifactCount    int
	ModelPlan        string
	ModelProvider    string
	ModelEfficient   string
	ModelBalanced    string
	ModelFrontier    string
	HandshakeOK      bool
	HandshakeStatus  string
	Ready            bool
	Blocker          string
}

type SetupResult struct {
	Plan             SetupPlan
	SelfInstallState string
	SelfInstallPath  string
	IntegrationState string
	IntegrationPath  string
	ArtifactCount    int
	HandshakeOK      bool
	HandshakeStatus  string
	Recovery         string
	Changed          bool
	RestartRequired  bool
}

type setupPlanLoadedMsg struct {
	generation int
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
}

func (m *Model) loadSetupPlan() tea.Cmd {
	ctx, generation := m.startSetupOperation()
	m.setupPlanLoading = true
	m.setupConfirm = false
	m.setupCancelAsked = false
	m.setupSucceeded = false
	m.setupApplyErr = nil
	m.setupPlanErr = nil
	m.setupViewport.GotoTop()
	request := SetupRequest{Workspace: m.options.Workspace, Plan: m.setupSelected}
	return func() tea.Msg {
		if m.backend == nil {
			return setupPlanLoadedMsg{generation: generation, err: fmt.Errorf("setup backend unavailable")}
		}
		value, err := m.backend.PlanSetup(ctx, request)
		return setupPlanLoadedMsg{generation: generation, value: value, err: err}
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
	request := SetupRequest{Workspace: m.options.Workspace, Plan: m.setupSelected}
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
}

func (m *Model) finishSetupOperation() {
	if m.cancelSetup != nil {
		m.cancelSetup()
		m.cancelSetup = nil
	}
}

func (m *Model) handleSetupPlanLoaded(msg setupPlanLoadedMsg) {
	if msg.generation != m.setupGeneration {
		return
	}
	m.finishSetupOperation()
	m.setupPlanLoading = false
	m.setupPlan = cloneSetupPlan(msg.value)
	m.setupPlanErr = msg.err
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
	if m.setupConfirm {
		switch msg.String() {
		case "y":
			return true, m.applySetup()
		case "n", "esc":
			m.setupConfirm = false
			return true, nil
		}
	}

	switch msg.String() {
	case "left", "h":
		return true, m.selectSetupPlan(-1)
	case "right", "l":
		return true, m.selectSetupPlan(1)
	case "a":
		if !m.setupPlanLoading && m.setupPlanErr == nil && m.setupPlan.Ready {
			m.setupConfirm = true
		}
		return true, nil
	case "r":
		return true, m.loadSetupPlan()
	case "esc":
		m.setRoute(routeOverview)
		return true, nil
	case "j", "k", "up", "down", "pgup", "pgdown", "home", "end":
		var cmd tea.Cmd
		m.setupViewport, cmd = m.setupViewport.Update(msg)
		return true, cmd
	}
	return false, nil
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
	return value == "low" || value == "medium" || value == "high"
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
	preview.SetContent(strings.Join(m.setupRouteLines(), "\n"))
	return strings.Split(strings.TrimRight(preview.View(), "\n"), "\n")
}

func (m Model) setupRouteLines() []string {
	lines := []string{"OPENCODE SETUP", "selected plan  " + sanitizeTerminal(m.setupSelected)}
	switch {
	case m.setupApplying:
		lines = append(lines, "", "... Applying verified six-step plan...", "No per-step progress is reported by the setup service.")
		if m.setupCancelAsked {
			lines = append(lines, "! Cancellation requested. Waiting for setup recovery...")
		}
		return lines
	case m.setupSucceeded:
		result := m.setupResult
		changed := "no"
		if result.Changed {
			changed = "yes"
		}
		lines = append(lines,
			"✓ SETUP COMPLETE",
			"changed  "+changed,
			"launcher  "+setupValue(result.SelfInstallState)+"  "+setupValue(result.SelfInstallPath),
			"integration  "+setupValue(result.IntegrationState)+"  "+setupValue(result.IntegrationPath),
			fmt.Sprintf("artifacts  %d", result.ArtifactCount),
			setupHandshake(result.HandshakeOK, result.HandshakeStatus),
		)
		if result.RestartRequired {
			lines = append(lines, "! Restart OpenCode to load the selected model plan.")
		} else {
			lines = append(lines, "Restart OpenCode if it is already running.")
		}
		return lines
	case m.setupApplyErr != nil:
		lines = append(lines, "✕ SETUP FAILED", "Action: review recovery guidance, then press a to retry.")
		if m.setupResult.Recovery != "" {
			lines = append(lines, "Recovery: "+sanitizeTerminal(m.setupResult.Recovery))
		}
		return lines
	case m.setupPlanLoading:
		return append(lines, "", "... Loading verified setup preview...")
	case m.setupPlanErr != nil:
		return append(lines, "✕ SETUP PREVIEW UNAVAILABLE", "Action: check local setup prerequisites, then press r to retry.")
	}

	plan := m.setupPlan
	readiness := "! BLOCKED"
	if plan.Ready {
		readiness = "✓ READY TO APPLY"
	}
	lines = append(lines,
		readiness,
		"launcher  "+setupValue(plan.SelfInstallState)+"  "+setupValue(plan.SelfInstallPath),
		"integration  "+setupValue(plan.IntegrationState)+"  "+setupValue(plan.IntegrationPath),
		fmt.Sprintf("artifacts  %d", plan.ArtifactCount),
		"model efficient  "+setupValue(plan.ModelEfficient),
		"model balanced   "+setupValue(plan.ModelBalanced),
		"model frontier   "+setupValue(plan.ModelFrontier),
		setupHandshake(plan.HandshakeOK, plan.HandshakeStatus),
	)
	if plan.Blocker != "" {
		lines = append(lines, "Blocker: "+sanitizeTerminal(plan.Blocker))
	}
	if m.setupConfirm {
		lines = append(lines, "", "! CONFIRM SETUP", "Apply this verified six-step plan? [y] yes  [n/Esc] cancel")
	}
	lines = append(lines, "", fmt.Sprintf("SIX-STEP PLAN (%d artifacts)", plan.ArtifactCount))
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
	switch {
	case m.setupApplying:
		return "Applying: navigation and quit locked  [ctrl+c] emergency cancel"
	case m.setupConfirm:
		return "[y] apply  [n/Esc] cancel  No write occurs until y"
	case m.setupSucceeded:
		return "[r] reload preview  [Esc] Overview  [q] quit"
	case m.setupApplyErr != nil:
		return "[a] retry with confirmation  [r] reload  [Esc] Overview"
	default:
		return "[h/l or ←/→] plan  [a] apply  [j/k] scroll  [r] reload  [Esc] Overview"
	}
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
