package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	setupflow "github.com/vgxness/vgxness/internal/setup"
)

const (
	RecoveryModeManaged = "managed"
	RecoveryModeFull    = "full"
)

type RecoveryPlanRequest struct {
	Provider   setupflow.Provider
	Workspace  string
	BackupRoot string
	Mode       string
}

type RecoveryPlan struct {
	Mode             string
	SourceRoot       string
	BackupRoot       string
	ArtifactCount    int
	LauncherState    string
	IntegrationState string
	HandshakeOK      bool
	HandshakeStatus  string
	Ready            bool
	Blocker          string
}

type BackupListRequest struct {
	Provider   setupflow.Provider
	Workspace  string
	BackupRoot string
}

type BackupSummary struct {
	SnapshotID string
	CreatedAt  string
	Mode       string
	FileCount  int
	TotalBytes int64
}

type BackupListResult struct {
	Snapshots []BackupSummary
}

type CreateBackupRequest struct {
	Provider   setupflow.Provider
	Workspace  string
	BackupRoot string
	Mode       string
}

type BackupResult struct {
	Mode     string
	Snapshot BackupSummary
}

type RestorePreviewRequest struct {
	Provider   setupflow.Provider
	Workspace  string
	BackupRoot string
	SnapshotID string
}

type RestorePreview struct {
	SnapshotID    string
	PreviewSHA256 string
	Missing       []string
	Identical     []string
	Conflicts     []string
}

type RestoreRequest struct {
	Provider      setupflow.Provider
	Workspace     string
	BackupRoot    string
	SnapshotID    string
	PreviewSHA256 string
}

type RestoreResult struct {
	SnapshotID string
	Created    int
	Identical  int
	Replaced   int
	Unresolved []string
}

type ProtectedReinstallRequest struct {
	Provider   setupflow.Provider
	Workspace  string
	BackupRoot string
	Mode       string
}

type ProtectedReinstallResult struct {
	Mode              string
	SnapshotID        string
	SnapshotVerified  bool
	LauncherState     string
	IntegrationState  string
	HandshakeOK       bool
	HandshakeStatus   string
	RecoveryAttempted bool
	RecoveryMissing   []string
	RecoveryGuidance  string
}

type setupView uint8

const (
	setupViewInstall setupView = iota
	setupViewRecovery
)

type recoveryOperation uint8

const (
	recoveryOperationIdle recoveryOperation = iota
	recoveryOperationLoad
	recoveryOperationPreview
	recoveryOperationCreate
	recoveryOperationRestore
	recoveryOperationReinstall
)

func (operation recoveryOperation) mutating() bool {
	return operation == recoveryOperationCreate || operation == recoveryOperationRestore || operation == recoveryOperationReinstall
}

type recoveryConfirmation uint8

const (
	recoveryConfirmNone recoveryConfirmation = iota
	recoveryConfirmCreate
	recoveryConfirmRestore
	recoveryConfirmReinstall
)

type recoveryFailure uint8

const (
	recoveryFailureNone recoveryFailure = iota
	recoveryFailureLoad
	recoveryFailureCreate
	recoveryFailurePreview
	recoveryFailureRestore
	recoveryFailureReinstall
)

type recoveryLoadedMsg struct {
	generation int
	plan       RecoveryPlan
	backups    BackupListResult
	planErr    error
	listErr    error
}

type recoveryBackupCreatedMsg struct {
	generation int
	value      BackupResult
	err        error
}

type recoveryPreviewLoadedMsg struct {
	generation int
	value      RestorePreview
	err        error
}

type recoveryRestoredMsg struct {
	generation int
	value      RestoreResult
	err        error
}

type recoveryReinstalledMsg struct {
	generation int
	value      ProtectedReinstallResult
	err        error
}

func (m *Model) resetRecoveryState() {
	m.recoveryMode = RecoveryModeManaged
	m.recoverySelectedProvider = setupflow.ProviderOpenCode
	m.recoveryPlan = RecoveryPlan{}
	m.recoveryBackups = nil
	m.recoveryPreview = RestorePreview{}
	m.recoveryBackup = BackupResult{}
	m.recoveryRestore = RestoreResult{}
	m.recoveryReinstall = ProtectedReinstallResult{}
	m.recoveryOperation = recoveryOperationIdle
	m.recoveryConfirm = recoveryConfirmNone
	m.recoveryFailure = recoveryFailureNone
	m.recoverySnapshotIndex = 0
	m.recoveryConflictIndex = 0
	m.recoveryCancelAsked = false
	m.recoveryRefreshPending = false
	m.recoveryRefreshWarning = false
}

func (m *Model) startRecoveryOperation(operation recoveryOperation) (context.Context, int) {
	if m.cancelRecovery != nil {
		m.cancelRecovery()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancelRecovery = cancel
	m.recoveryGeneration++
	m.recoveryOperation = operation
	m.recoveryCancelAsked = false
	return ctx, m.recoveryGeneration
}

func (m *Model) finishRecoveryOperation() {
	if m.cancelRecovery != nil {
		m.cancelRecovery()
		m.cancelRecovery = nil
	}
	m.recoveryOperation = recoveryOperationIdle
	m.recoveryCancelAsked = false
}

func (m *Model) cancelRecoveryOperation() {
	if m.cancelRecovery != nil {
		m.cancelRecovery()
		m.cancelRecovery = nil
	}
	m.recoveryGeneration++
	m.recoveryOperation = recoveryOperationIdle
	m.recoveryConfirm = recoveryConfirmNone
	m.recoveryCancelAsked = false
}

func (m *Model) loadRecovery() tea.Cmd {
	m.recoveryRefreshPending = m.hasRecoveryOutcome()
	if !m.recoveryRefreshPending {
		m.recoveryRefreshWarning = false
	}
	ctx, generation := m.startRecoveryOperation(recoveryOperationLoad)
	m.recoveryConfirm = recoveryConfirmNone
	m.recoveryPreview = RestorePreview{}
	m.recoveryFailure = recoveryFailureNone
	request := RecoveryPlanRequest{Provider: m.recoveryProvider(), Workspace: m.options.Workspace, Mode: m.recoveryMode}
	listRequest := BackupListRequest{Provider: request.Provider, Workspace: m.options.Workspace}
	return func() tea.Msg {
		if m.backend == nil {
			return recoveryLoadedMsg{generation: generation, planErr: fmt.Errorf("recovery backend unavailable"), listErr: fmt.Errorf("recovery backend unavailable")}
		}
		plan, planErr := m.backend.PlanRecovery(ctx, request)
		listRequest.BackupRoot = plan.BackupRoot
		backups, listErr := m.backend.ListBackups(ctx, listRequest)
		return recoveryLoadedMsg{generation: generation, plan: plan, backups: backups, planErr: planErr, listErr: listErr}
	}
}

func (m *Model) createRecoveryBackup() tea.Cmd {
	ctx, generation := m.startRecoveryOperation(recoveryOperationCreate)
	m.recoveryConfirm = recoveryConfirmNone
	m.recoveryFailure = recoveryFailureNone
	request := CreateBackupRequest{Provider: m.recoveryProvider(), Workspace: m.options.Workspace, BackupRoot: m.recoveryPlan.BackupRoot, Mode: m.recoveryMode}
	return func() tea.Msg {
		value, err := m.backend.CreateBackup(ctx, request)
		return recoveryBackupCreatedMsg{generation: generation, value: value, err: err}
	}
}

func (m *Model) previewRecoveryRestore() tea.Cmd {
	if len(m.recoveryBackups) == 0 {
		return nil
	}
	m.clearRecoveryOutcome()
	ctx, generation := m.startRecoveryOperation(recoveryOperationPreview)
	m.recoveryFailure = recoveryFailureNone
	m.recoveryPreview = RestorePreview{}
	request := RestorePreviewRequest{Provider: m.recoveryProvider(), Workspace: m.options.Workspace, BackupRoot: m.recoveryPlan.BackupRoot, SnapshotID: m.recoveryBackups[m.recoverySnapshotIndex].SnapshotID}
	return func() tea.Msg {
		value, err := m.backend.PreviewRestore(ctx, request)
		return recoveryPreviewLoadedMsg{generation: generation, value: value, err: err}
	}
}

func (m *Model) restoreRecoverySnapshot() tea.Cmd {
	ctx, generation := m.startRecoveryOperation(recoveryOperationRestore)
	m.recoveryConfirm = recoveryConfirmNone
	m.recoveryFailure = recoveryFailureNone
	request := RestoreRequest{
		Provider: m.recoveryProvider(), Workspace: m.options.Workspace, BackupRoot: m.recoveryPlan.BackupRoot,
		SnapshotID: m.recoveryPreview.SnapshotID, PreviewSHA256: m.recoveryPreview.PreviewSHA256,
	}
	return func() tea.Msg {
		value, err := m.backend.RestoreBackup(ctx, request)
		return recoveryRestoredMsg{generation: generation, value: value, err: err}
	}
}

func (m *Model) protectedRecoveryReinstall() tea.Cmd {
	ctx, generation := m.startRecoveryOperation(recoveryOperationReinstall)
	m.recoveryConfirm = recoveryConfirmNone
	m.recoveryFailure = recoveryFailureNone
	request := ProtectedReinstallRequest{Provider: m.recoveryProvider(), Workspace: m.options.Workspace, BackupRoot: m.recoveryPlan.BackupRoot, Mode: m.recoveryMode}
	return func() tea.Msg {
		value, err := m.backend.ProtectedReinstall(ctx, request)
		return recoveryReinstalledMsg{generation: generation, value: value, err: err}
	}
}

func (m *Model) handleRecoveryLoaded(msg recoveryLoadedMsg) {
	if msg.generation != m.recoveryGeneration {
		return
	}
	m.finishRecoveryOperation()
	if msg.planErr != nil || msg.listErr != nil {
		if m.recoveryRefreshPending {
			m.recoveryRefreshWarning = true
			m.recoveryRefreshPending = false
			return
		}
		m.recoveryFailure = recoveryFailureLoad
		m.recoveryRefreshPending = false
		return
	}
	m.recoveryPlan = msg.plan
	m.recoveryBackups = append([]BackupSummary(nil), msg.backups.Snapshots...)
	if m.recoverySnapshotIndex >= len(m.recoveryBackups) {
		m.recoverySnapshotIndex = max(0, len(m.recoveryBackups)-1)
	}
	m.recoveryFailure = recoveryFailureNone
	m.recoveryRefreshWarning = false
	m.recoveryRefreshPending = false
}

func (m *Model) handleRecoveryBackupCreated(msg recoveryBackupCreatedMsg) tea.Cmd {
	if msg.generation != m.recoveryGeneration {
		return nil
	}
	m.finishRecoveryOperation()
	m.recoveryBackup = msg.value
	if msg.err != nil {
		m.recoveryFailure = recoveryFailureCreate
		return nil
	}
	m.recoveryFailure = recoveryFailureNone
	return m.loadRecovery()
}

func (m *Model) handleRecoveryPreviewLoaded(msg recoveryPreviewLoadedMsg) {
	if msg.generation != m.recoveryGeneration {
		return
	}
	m.finishRecoveryOperation()
	if msg.err != nil {
		m.recoveryFailure = recoveryFailurePreview
		return
	}
	m.recoveryPreview = cloneRestorePreview(msg.value)
	m.recoveryConflictIndex = 0
}

func (m *Model) handleRecoveryRestored(msg recoveryRestoredMsg) tea.Cmd {
	if msg.generation != m.recoveryGeneration {
		return nil
	}
	m.finishRecoveryOperation()
	m.recoveryRestore = cloneRestoreResult(msg.value)
	m.recoveryPreview = RestorePreview{}
	if msg.err != nil {
		m.recoveryFailure = recoveryFailureRestore
		return nil
	}
	m.recoveryFailure = recoveryFailureNone
	return m.loadRecovery()
}

func (m *Model) handleRecoveryReinstalled(msg recoveryReinstalledMsg) tea.Cmd {
	if msg.generation != m.recoveryGeneration {
		return nil
	}
	m.finishRecoveryOperation()
	m.recoveryReinstall = cloneProtectedReinstallResult(msg.value)
	if msg.err != nil {
		m.recoveryFailure = recoveryFailureReinstall
		return nil
	}
	m.recoveryFailure = recoveryFailureNone
	return m.loadRecovery()
}

func (m *Model) updateRecoveryKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if m.multiSetupEnabled() && m.hasSetupProvider(setupflow.ProviderOpenCode) && m.hasSetupProvider(setupflow.ProviderCodex) {
		switch msg.String() {
		case "o":
			return true, m.switchRecoveryProvider(setupflow.ProviderOpenCode)
		case "c":
			return true, m.switchRecoveryProvider(setupflow.ProviderCodex)
		}
	}
	if m.recoveryConfirm != recoveryConfirmNone {
		switch msg.String() {
		case "y":
			switch m.recoveryConfirm {
			case recoveryConfirmCreate:
				return true, m.createRecoveryBackup()
			case recoveryConfirmRestore:
				return true, m.restoreRecoverySnapshot()
			case recoveryConfirmReinstall:
				return true, m.protectedRecoveryReinstall()
			}
		case "n", "esc":
			m.recoveryConfirm = recoveryConfirmNone
			return true, nil
		}
		return true, nil
	}

	switch msg.String() {
	case "tab":
		m.cancelRecoveryOperation()
		m.setupView = setupViewInstall
		return true, m.loadSetupPlan()
	case "left", "h":
		if m.recoveryMode != RecoveryModeManaged {
			m.clearRecoveryOutcome()
			m.recoveryMode = RecoveryModeManaged
			return true, m.loadRecovery()
		}
		return true, nil
	case "right", "l":
		if m.recoveryProvider() == setupflow.ProviderCodex {
			return true, nil
		}
		if m.recoveryMode != RecoveryModeFull {
			m.clearRecoveryOutcome()
			m.recoveryMode = RecoveryModeFull
			return true, m.loadRecovery()
		}
		return true, nil
	case "b":
		if m.recoveryPlan.Ready {
			m.recoveryConfirm = recoveryConfirmCreate
		}
		return true, nil
	case "i":
		if m.recoveryPlan.Ready {
			m.recoveryConfirm = recoveryConfirmReinstall
		}
		return true, nil
	case "p":
		return true, m.previewRecoveryRestore()
	case "x":
		if m.recoveryPreview.SnapshotID != "" {
			m.recoveryConfirm = recoveryConfirmRestore
		}
		return true, nil
	case "j", "down":
		m.clearRecoveryOutcome()
		m.moveRecoverySelection(1)
		return true, nil
	case "k", "up":
		m.clearRecoveryOutcome()
		m.moveRecoverySelection(-1)
		return true, nil
	case "r":
		m.clearRecoveryOutcome()
		return true, m.loadRecovery()
	case "esc":
		if m.recoveryPreview.SnapshotID != "" {
			m.recoveryPreview = RestorePreview{}
			return true, nil
		}
		m.setRoute(routeOverview)
		return true, nil
	}
	return false, nil
}

func (m *Model) moveRecoverySelection(offset int) {
	if len(m.recoveryPreview.Conflicts) > 0 {
		m.recoveryConflictIndex = max(0, min(len(m.recoveryPreview.Conflicts)-1, m.recoveryConflictIndex+offset))
		return
	}
	if len(m.recoveryBackups) > 0 {
		m.recoverySnapshotIndex = max(0, min(len(m.recoveryBackups)-1, m.recoverySnapshotIndex+offset))
	}
}

func (m Model) recoveryRouteLines() []string {
	provider := m.recoveryProvider()
	lines := []string{strings.ToUpper(string(provider)) + " SETUP  Install  [Backup & Recovery]", "mode  " + strings.ToUpper(m.recoveryMode)}
	if provider != setupflow.ProviderCodex && m.recoveryMode == RecoveryModeFull {
		lines = append(lines, "! FULL BACKUP WARNING", "  May contain credentials; local storage is 0700/0600 with no encryption.")
	}
	if m.recoveryConfirm != recoveryConfirmNone {
		return append(lines, m.recoveryConfirmationLines()...)
	}
	if m.recoveryOperation.mutating() {
		operation := "controlled recovery write"
		switch m.recoveryOperation {
		case recoveryOperationCreate:
			operation = "backup creation"
		case recoveryOperationRestore:
			operation = "merge restore"
		case recoveryOperationReinstall:
			operation = "protected reinstall"
		}
		lines = append(lines, "", "... Running "+operation+"...", "Navigation and quit are locked until verification and recovery finish.")
		if m.recoveryCancelAsked {
			lines = append(lines, "! Cancellation requested. Waiting for bounded recovery...")
		}
		return lines
	}
	if m.recoveryFailure != recoveryFailureNone {
		return append(lines, m.recoveryFailureLines()...)
	}
	if m.recoveryReinstall.SnapshotID != "" {
		return m.withRecoveryRefreshWarning(append(lines, m.recoveryReinstallLines()...))
	}
	if m.recoveryRestore.SnapshotID != "" {
		title := "✓ RESTORE COMPLETE"
		if len(m.recoveryRestore.Unresolved) != 0 {
			title = "! RESTORE PARTIAL"
		}
		lines = append(lines, "", title, fmt.Sprintf("created  %d  identical  %d", m.recoveryRestore.Created, m.recoveryRestore.Identical), fmt.Sprintf("unresolved  %d", len(m.recoveryRestore.Unresolved)))
		for index, unresolved := range m.recoveryRestore.Unresolved {
			if index == 3 {
				break
			}
			lines = append(lines, "  ! "+sanitizeTerminal(unresolved))
		}
		return m.withRecoveryRefreshWarning(lines)
	}
	if m.recoveryBackup.Snapshot.SnapshotID != "" {
		return m.withRecoveryRefreshWarning(append(lines, "", "✓ BACKUP CREATED", "snapshot  "+sanitizeTerminal(m.recoveryBackup.Snapshot.SnapshotID), "mode  "+sanitizeTerminal(m.recoveryBackup.Mode)))
	}
	if m.recoveryPreview.SnapshotID != "" {
		return append(lines, m.recoveryPreviewLines()...)
	}
	if m.recoveryOperation == recoveryOperationLoad {
		lines = append(lines, "", "... Loading protected-reinstall plan and verified backups...")
	}
	readiness := "! BLOCKED"
	if m.recoveryPlan.Ready {
		readiness = "✓ READY"
	}
	lines = append(lines, "", readiness,
		"source  "+setupValue(m.recoveryPlan.SourceRoot),
		"backup  "+setupValue(m.recoveryPlan.BackupRoot),
		fmt.Sprintf("managed artifacts  %d", m.recoveryPlan.ArtifactCount),
		"launcher  "+setupValue(m.recoveryPlan.LauncherState),
		"integration  "+setupValue(m.recoveryPlan.IntegrationState),
		setupHandshake(m.recoveryPlan.HandshakeOK, m.recoveryPlan.HandshakeStatus),
	)
	if m.recoveryPlan.Blocker != "" {
		lines = append(lines, "Blocker: "+sanitizeTerminal(m.recoveryPlan.Blocker))
	}
	lines = append(lines, "", "VERIFIED SNAPSHOTS")
	if len(m.recoveryBackups) == 0 {
		return append(lines, "  No verified snapshots.")
	}
	start := max(0, min(m.recoverySnapshotIndex-1, len(m.recoveryBackups)-3))
	end := min(len(m.recoveryBackups), start+3)
	for index := start; index < end; index++ {
		item := m.recoveryBackups[index]
		marker := " "
		if index == m.recoverySnapshotIndex {
			marker = "▸"
		}
		lines = append(lines, fmt.Sprintf("%s %s  %s  %s  %d files  %s", marker, sanitizeTerminal(item.SnapshotID), sanitizeTerminal(item.CreatedAt), sanitizeTerminal(item.Mode), item.FileCount, formatBytes(item.TotalBytes)))
	}
	return lines
}

func (m Model) recoveryConfirmationLines() []string {
	switch m.recoveryConfirm {
	case recoveryConfirmCreate:
		return []string{"", "! CONFIRM CREATE BACKUP", "Create a " + strings.ToUpper(m.recoveryMode) + " snapshot? [y] yes  [n/Esc] cancel"}
	case recoveryConfirmRestore:
		return []string{"", "! CONFIRM MERGE RESTORE", "Restore missing files only; conflicts remain unresolved for manual repair.", "[y] restore missing  [n/Esc] cancel"}
	case recoveryConfirmReinstall:
		return []string{"", "! CONFIRM PROTECTED REINSTALL", "Mode " + strings.ToUpper(m.recoveryMode) + "; a verified snapshot is mandatory before provider mutation.", "[y] reinstall  [n/Esc] cancel"}
	}
	return nil
}

func (m Model) recoveryPreviewLines() []string {
	lines := []string{"", "RESTORE PREVIEW", "snapshot  " + sanitizeTerminal(m.recoveryPreview.SnapshotID), fmt.Sprintf("missing  %d  identical  %d  conflicts  %d", len(m.recoveryPreview.Missing), len(m.recoveryPreview.Identical), len(m.recoveryPreview.Conflicts)), "Conflicts remain unresolved and require manual repair:"}
	for index, conflict := range m.recoveryPreview.Conflicts {
		cursor := " "
		if index == m.recoveryConflictIndex {
			cursor = "▸"
		}
		lines = append(lines, cursor+" ! "+sanitizeTerminal(conflict))
		if len(lines) >= 12 {
			break
		}
	}
	return append(lines, "[x] restore missing files; conflicts are never overwritten")
}

func (m Model) recoveryReinstallLines() []string {
	verified := "no"
	if m.recoveryReinstall.SnapshotVerified {
		verified = "yes"
	}
	lines := []string{"", "✓ REINSTALL COMPLETE", "retained snapshot  " + setupValue(m.recoveryReinstall.SnapshotID), "snapshot verified  " + verified, "launcher  " + setupValue(m.recoveryReinstall.LauncherState), "integration  " + setupValue(m.recoveryReinstall.IntegrationState), setupHandshake(m.recoveryReinstall.HandshakeOK, m.recoveryReinstall.HandshakeStatus)}
	if m.recoveryReinstall.RecoveryGuidance != "" {
		lines = append(lines, "Recovery: "+sanitizeTerminal(m.recoveryReinstall.RecoveryGuidance))
	}
	return lines
}

func (m Model) recoveryFailureLines() []string {
	title := "RECOVERY LOAD FAILED"
	switch m.recoveryFailure {
	case recoveryFailureCreate:
		title = "BACKUP FAILED"
	case recoveryFailurePreview:
		title = "RESTORE PREVIEW FAILED"
	case recoveryFailureRestore:
		title = "RESTORE FAILED"
	case recoveryFailureReinstall:
		title = "REINSTALL FAILED"
	}
	lines := []string{"", "✕ " + title, "Action: review local readiness and retry; no raw backend details are shown."}
	if m.recoveryReinstall.SnapshotID != "" {
		lines = append(lines, "retained snapshot  "+sanitizeTerminal(m.recoveryReinstall.SnapshotID))
	}
	if m.recoveryFailure == recoveryFailureCreate && m.recoveryBackup.Snapshot.SnapshotID != "" {
		lines = append(lines, "retained snapshot  "+sanitizeTerminal(m.recoveryBackup.Snapshot.SnapshotID))
	}
	if m.recoveryFailure == recoveryFailureRestore && m.recoveryRestore.SnapshotID != "" && (m.recoveryRestore.Created > 0 || len(m.recoveryRestore.Unresolved) > 0) {
		lines = append(lines, "! RESTORE PARTIAL", fmt.Sprintf("created  %d  identical  %d  unresolved  %d", m.recoveryRestore.Created, m.recoveryRestore.Identical, len(m.recoveryRestore.Unresolved)))
	}
	if m.recoveryFailure == recoveryFailureReinstall {
		lines = append(lines,
			"integration  "+setupValue(m.recoveryReinstall.IntegrationState),
			setupHandshake(m.recoveryReinstall.HandshakeOK, m.recoveryReinstall.HandshakeStatus),
		)
	}
	if m.recoveryReinstall.RecoveryGuidance != "" {
		lines = append(lines, "Recovery: "+sanitizeTerminal(m.recoveryReinstall.RecoveryGuidance))
	}
	return lines
}

func (m Model) recoveryHelp() string {
	if m.recoveryOperation.mutating() {
		return "Controlled write locked  [ctrl+c] request cancellation and wait"
	}
	if m.recoveryConfirm != recoveryConfirmNone {
		return "[y] confirm controlled write  [n/Esc] cancel  No write occurs until y"
	}
	if m.recoveryPreview.SnapshotID != "" {
		return "[j/k] inspect conflicts  [x] restore missing only  [Esc] close preview"
	}
	modeHelp := "[h/l] mode  "
	if m.recoveryProvider() == setupflow.ProviderCodex {
		modeHelp = "managed-only  "
	}
	switchHelp := ""
	if m.multiSetupEnabled() && m.hasSetupProvider(setupflow.ProviderOpenCode) && m.hasSetupProvider(setupflow.ProviderCodex) {
		switchHelp = "[o/c] provider  "
	}
	return "[Tab] Install  [r] refresh  " + switchHelp + modeHelp + "[j/k] snapshot  [p] preview  controlled writes: [b] backup [i] reinstall"
}

func (m Model) recoveryProvider() setupflow.Provider {
	if m.multiSetupEnabled() && m.hasSetupProvider(setupflow.ProviderCodex) && (!m.hasSetupProvider(setupflow.ProviderOpenCode) || m.recoverySelectedProvider == setupflow.ProviderCodex) {
		return setupflow.ProviderCodex
	}
	return setupflow.ProviderOpenCode
}

func (m *Model) switchRecoveryProvider(provider setupflow.Provider) tea.Cmd {
	if !m.hasSetupProvider(provider) || m.recoveryProvider() == provider {
		return nil
	}
	m.cancelRecoveryOperation()
	m.clearRecoveryOutcome()
	m.recoveryPlan = RecoveryPlan{}
	m.recoveryBackups = nil
	m.recoveryPreview = RestorePreview{}
	m.recoverySnapshotIndex, m.recoveryConflictIndex = 0, 0
	m.recoveryFailure, m.recoveryRefreshPending, m.recoveryRefreshWarning = recoveryFailureNone, false, false
	m.recoverySelectedProvider = provider
	if provider == setupflow.ProviderCodex {
		m.recoveryMode = RecoveryModeManaged
	}
	return m.loadRecovery()
}

func (m *Model) clearRecoveryOutcome() {
	m.recoveryBackup = BackupResult{}
	m.recoveryRestore = RestoreResult{}
	m.recoveryReinstall = ProtectedReinstallResult{}
}

func (m Model) hasRecoveryOutcome() bool {
	return m.recoveryBackup.Snapshot.SnapshotID != "" || m.recoveryRestore.SnapshotID != "" || m.recoveryReinstall.SnapshotID != ""
}

func (m Model) withRecoveryRefreshWarning(lines []string) []string {
	if m.recoveryRefreshWarning {
		return append(lines, "! Automatic refresh warning: the completed outcome is retained; press r to retry refresh.")
	}
	return lines
}

func cloneRestorePreview(value RestorePreview) RestorePreview {
	value.Missing = append([]string(nil), value.Missing...)
	value.Identical = append([]string(nil), value.Identical...)
	value.Conflicts = append([]string(nil), value.Conflicts...)
	return value
}

func cloneRestoreResult(value RestoreResult) RestoreResult {
	value.Unresolved = append([]string(nil), value.Unresolved...)
	return value
}

func cloneProtectedReinstallResult(value ProtectedReinstallResult) ProtectedReinstallResult {
	value.RecoveryMissing = append([]string(nil), value.RecoveryMissing...)
	return value
}

func formatBytes(value int64) string {
	if value < 1024 {
		return fmt.Sprintf("%d B", value)
	}
	if value < 1024*1024 {
		return fmt.Sprintf("%.1f KiB", float64(value)/1024)
	}
	return fmt.Sprintf("%.1f MiB", float64(value)/(1024*1024))
}
