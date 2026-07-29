package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func (fakeBackend) PlanRecovery(context.Context, RecoveryPlanRequest) (RecoveryPlan, error) {
	return RecoveryPlan{}, nil
}
func (fakeBackend) ListBackups(context.Context, BackupListRequest) (BackupListResult, error) {
	return BackupListResult{}, nil
}
func (fakeBackend) CreateBackup(context.Context, CreateBackupRequest) (BackupResult, error) {
	return BackupResult{}, nil
}
func (fakeBackend) PreviewRestore(context.Context, RestorePreviewRequest) (RestorePreview, error) {
	return RestorePreview{}, nil
}
func (fakeBackend) RestoreBackup(context.Context, RestoreRequest) (RestoreResult, error) {
	return RestoreResult{}, nil
}
func (fakeBackend) ProtectedReinstall(context.Context, ProtectedReinstallRequest) (ProtectedReinstallResult, error) {
	return ProtectedReinstallResult{}, nil
}

type recordingRecoveryBackend struct {
	fakeBackend
	planRequests      []RecoveryPlanRequest
	listRequests      []BackupListRequest
	createRequests    []CreateBackupRequest
	previewRequests   []RestorePreviewRequest
	restoreRequests   []RestoreRequest
	reinstallRequests []ProtectedReinstallRequest
	plan              RecoveryPlan
	list              BackupListResult
	backup            BackupResult
	preview           RestorePreview
	restore           RestoreResult
	reinstall         ProtectedReinstallResult
	err               error
	listErr           error
}

func (backend *recordingRecoveryBackend) PlanRecovery(_ context.Context, request RecoveryPlanRequest) (RecoveryPlan, error) {
	backend.planRequests = append(backend.planRequests, request)
	return backend.plan, backend.err
}
func (backend *recordingRecoveryBackend) ListBackups(_ context.Context, request BackupListRequest) (BackupListResult, error) {
	backend.listRequests = append(backend.listRequests, request)
	if backend.listErr != nil {
		return BackupListResult{}, backend.listErr
	}
	return backend.list, backend.err
}
func (backend *recordingRecoveryBackend) CreateBackup(_ context.Context, request CreateBackupRequest) (BackupResult, error) {
	backend.createRequests = append(backend.createRequests, request)
	return backend.backup, backend.err
}
func (backend *recordingRecoveryBackend) PreviewRestore(_ context.Context, request RestorePreviewRequest) (RestorePreview, error) {
	backend.previewRequests = append(backend.previewRequests, request)
	return backend.preview, backend.err
}
func (backend *recordingRecoveryBackend) RestoreBackup(_ context.Context, request RestoreRequest) (RestoreResult, error) {
	backend.restoreRequests = append(backend.restoreRequests, request)
	return backend.restore, backend.err
}
func (backend *recordingRecoveryBackend) ProtectedReinstall(_ context.Context, request ProtectedReinstallRequest) (ProtectedReinstallResult, error) {
	backend.reinstallRequests = append(backend.reinstallRequests, request)
	return backend.reinstall, backend.err
}

func TestSetupRecoveryTabDefaultsManagedLoadsRealStateAndSelectsSnapshots(t *testing.T) {
	backend := readyRecoveryBackend()
	model := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model, _ = openSetupRoute(t, model)

	updated, cmd := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	model = updated.(Model)
	if cmd == nil || model.recoveryMode != RecoveryModeManaged || model.setupView != setupViewRecovery {
		t.Fatalf("Tab did not enter managed recovery: mode=%q view=%v cmd=%v", model.recoveryMode, model.setupView, cmd)
	}
	model = updateModel(t, model, cmd())
	if len(backend.planRequests) != 1 || backend.planRequests[0].Mode != RecoveryModeManaged || len(backend.listRequests) != 1 {
		t.Fatalf("recovery load requests plan=%+v list=%+v", backend.planRequests, backend.listRequests)
	}
	view := model.View().Content
	for _, expected := range []string{"CONTROLLED WRITE", "Backup & Recovery", "MANAGED", "/config/opencode", "/backups", "15", "20260729T120000", "2.0 KiB"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("recovery view missing %q:\n%s", expected, view)
		}
	}
	model = updateModel(t, model, keyPress("j"))
	if model.recoverySnapshotIndex != 1 {
		t.Fatalf("j did not select next snapshot: %d", model.recoverySnapshotIndex)
	}
	assertMaximumWidth(t, view, 80)
}

func TestSetupRecoveryFullModeWarnsAndReloadsPlan(t *testing.T) {
	backend := readyRecoveryBackend()
	model := enterRecovery(t, backend)
	updated, cmd := model.Update(keyPress("l"))
	model = updated.(Model)
	if cmd == nil || model.recoveryMode != RecoveryModeFull {
		t.Fatalf("full mode did not reload: mode=%q cmd=%v", model.recoveryMode, cmd)
	}
	view := model.View().Content
	for _, expected := range []string{"FULL", "credentials", "0700/0600", "no encryption"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("full warning missing %q:\n%s", expected, view)
		}
	}
	model = updateModel(t, model, cmd())
	if backend.planRequests[len(backend.planRequests)-1].Mode != RecoveryModeFull {
		t.Fatalf("mode reload request=%+v", backend.planRequests)
	}
	assertMaximumWidth(t, view, 80)
}

func TestSetupRecoveryBackupAndReinstallRequireYConfirmation(t *testing.T) {
	backend := readyRecoveryBackend()
	model := enterRecovery(t, backend)

	model = updateModel(t, model, keyPress("b"))
	if len(backend.createRequests) != 0 || !strings.Contains(model.View().Content, "CONFIRM CREATE BACKUP") {
		t.Fatal("backup confirmation did not gate write")
	}
	model = updateModel(t, model, keyPress("n"))
	if len(backend.createRequests) != 0 {
		t.Fatal("n created backup")
	}
	model = updateModel(t, model, keyPress("b"))
	updated, cmd := model.Update(keyPress("y"))
	model = updated.(Model)
	if cmd == nil || len(backend.createRequests) != 0 {
		t.Fatalf("backup started incorrectly: cmd=%v requests=%v", cmd, backend.createRequests)
	}
	model = updateModel(t, model, cmd())
	if len(backend.createRequests) != 1 || !strings.Contains(model.View().Content, "BACKUP CREATED") {
		t.Fatalf("backup result missing: requests=%+v view=%s", backend.createRequests, model.View().Content)
	}

	model = updateModel(t, model, keyPress("i"))
	if len(backend.reinstallRequests) != 0 || !strings.Contains(model.View().Content, "verified snapshot is mandatory") {
		t.Fatal("reinstall confirmation did not gate write")
	}
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if len(backend.reinstallRequests) != 0 {
		t.Fatal("Esc started reinstall")
	}
	model = updateModel(t, model, keyPress("i"))
	updated, cmd = model.Update(keyPress("y"))
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("confirmed reinstall returned no command")
	}
	model = updateModel(t, model, cmd())
	if len(backend.reinstallRequests) != 1 || !strings.Contains(model.View().Content, "REINSTALL COMPLETE") || !strings.Contains(model.View().Content, "retained snapshot") {
		t.Fatalf("reinstall result missing: requests=%+v view=%s", backend.reinstallRequests, model.View().Content)
	}
}

func TestSetupRecoveryPreviewKeepsConflictsManualAndRestoresMissingOnly(t *testing.T) {
	backend := readyRecoveryBackend()
	model := enterRecovery(t, backend)
	updated, cmd := model.Update(keyPress("p"))
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("preview returned no command")
	}
	model = updateModel(t, model, cmd())
	view := model.View().Content
	for _, expected := range []string{"RESTORE PREVIEW", "missing  1", "identical  1", "conflicts  2", "agents/conflict.md", "manual"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("preview missing %q:\n%s", expected, view)
		}
	}
	model = updateModel(t, model, keyPress("x"))
	updated, cmd = model.Update(keyPress("y"))
	model = updated.(Model)
	model = updateModel(t, model, cmd())
	if len(backend.restoreRequests) != 1 {
		t.Fatalf("default restore authorized conflicts: %+v", backend.restoreRequests)
	}
	if view := model.View().Content; !strings.Contains(view, "RESTORE PARTIAL") || strings.Contains(view, "RESTORE COMPLETE") {
		t.Fatalf("unresolved restore rendered as complete:\n%s", view)
	}
}

func TestSetupRecoveryOutcomeSurvivesAutomaticRefreshFailure(t *testing.T) {
	backend := readyRecoveryBackend()
	model := enterRecovery(t, backend)
	model = updateModel(t, model, keyPress("b"))
	updated, createCmd := model.Update(keyPress("y"))
	model = updated.(Model)
	updated, refreshCmd := model.Update(createCmd())
	model = updated.(Model)
	if refreshCmd == nil {
		t.Fatal("successful backup did not request automatic refresh")
	}
	backend.listErr = errors.New("refresh failed")
	model = updateModel(t, model, refreshCmd())
	view := model.View().Content
	if !strings.Contains(view, "BACKUP CREATED") || !strings.Contains(view, "refresh warning") || strings.Contains(view, "RECOVERY LOAD FAILED") {
		t.Fatalf("refresh failure hid successful outcome:\n%s", view)
	}
}

func TestSetupRecoveryIgnoresStaleMessagesLocksMutationAndBlocksSmallConfirmation(t *testing.T) {
	backend := readyRecoveryBackend()
	model := enterRecovery(t, backend)
	current := model.recoveryGeneration
	model = updateModel(t, model, recoveryLoadedMsg{generation: current - 1, plan: RecoveryPlan{SourceRoot: "STALE"}})
	if model.recoveryPlan.SourceRoot == "STALE" {
		t.Fatal("stale recovery load replaced current plan")
	}

	operationCtx, cancel := context.WithCancel(context.Background())
	model.recoveryOperation = recoveryOperationReinstall
	model.cancelRecovery = cancel
	for _, key := range []tea.KeyPressMsg{keyPress("q"), keyPress("g"), keyPress("r"), keyPress("b"), tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}), tea.KeyPressMsg(tea.Key{Code: tea.KeyTab})} {
		updated, cmd := model.Update(key)
		model = updated.(Model)
		if cmd != nil || model.recoveryOperation != recoveryOperationReinstall || model.route != routeSetup {
			t.Fatalf("mutation key escaped lock: %q op=%v route=%v cmd=%v", key.String(), model.recoveryOperation, model.route, cmd)
		}
	}
	updated, cmd := model.Update(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	model = updated.(Model)
	if cmd != nil || !model.recoveryCancelAsked || !strings.Contains(model.View().Content, "Cancellation requested") {
		t.Fatal("ctrl+c cancellation was not visible")
	}
	select {
	case <-operationCtx.Done():
	default:
		t.Fatal("ctrl+c did not cancel recovery operation")
	}

	model.recoveryOperation = recoveryOperationIdle
	model.recoveryCancelAsked = false
	model = updateModel(t, model, keyPress("b"))
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 30, Height: 8})
	updated, cmd = model.Update(keyPress("y"))
	model = updated.(Model)
	if cmd != nil || len(backend.createRequests) != 0 {
		t.Fatal("too-small terminal confirmed recovery mutation")
	}
}

func TestSetupRecoveryFailureHidesRawErrorAndShowsGuidance(t *testing.T) {
	backend := readyRecoveryBackend()
	model := enterRecovery(t, backend)
	model.recoveryGeneration++
	model.recoveryOperation = recoveryOperationReinstall
	model = updateModel(t, model, recoveryReinstalledMsg{generation: model.recoveryGeneration, value: ProtectedReinstallResult{SnapshotID: "snapshot-1", SnapshotVerified: true, IntegrationState: "installed", HandshakeStatus: "unhealthy", RecoveryGuidance: "Integration is installed; repair the handshake using the retained snapshot."}, err: errors.New("SECRET CONTENT")})
	view := model.View().Content
	for _, expected := range []string{"REINSTALL FAILED", "retained snapshot", "integration  installed", "unhealthy", "repair the handshake"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("recovery failure missing %q:\n%s", expected, view)
		}
	}
	if strings.Contains(view, "SECRET CONTENT") {
		t.Fatalf("unsafe recovery failure view:\n%s", view)
	}
	assertMaximumWidth(t, view, 80)
}

func TestSetupRecoveryErrorRendersAccumulatedRestoreOutcome(t *testing.T) {
	backend := readyRecoveryBackend()
	model := enterRecovery(t, backend)
	model.recoveryGeneration++
	model.recoveryOperation = recoveryOperationRestore
	model = updateModel(t, model, recoveryRestoredMsg{
		generation: model.recoveryGeneration,
		value:      RestoreResult{SnapshotID: "snapshot-1", Created: 1, Unresolved: []string{"agents/conflict.md"}},
		err:        errors.New("SECRET durability detail"),
	})
	view := model.View().Content
	if !strings.Contains(view, "RESTORE PARTIAL") || !strings.Contains(view, "created  1") || !strings.Contains(view, "unresolved  1") || strings.Contains(view, "SECRET") {
		t.Fatalf("partial restore outcome was lost:\n%s", view)
	}
}

func readyRecoveryBackend() *recordingRecoveryBackend {
	return &recordingRecoveryBackend{
		plan: RecoveryPlan{Mode: RecoveryModeManaged, SourceRoot: "/config/opencode", BackupRoot: "/backups", ArtifactCount: 15, LauncherState: "installed", IntegrationState: "installed", HandshakeOK: true, HandshakeStatus: "healthy", Ready: true},
		list: BackupListResult{Snapshots: []BackupSummary{
			{SnapshotID: "20260729T120000.000000000Z-0123456789abcdef", CreatedAt: "2026-07-29T12:00:00Z", Mode: RecoveryModeManaged, FileCount: 15, TotalBytes: 2048},
			{SnapshotID: "20260728T120000.000000000Z-fedcba9876543210", CreatedAt: "2026-07-28T12:00:00Z", Mode: RecoveryModeFull, FileCount: 20, TotalBytes: 4096},
		}},
		backup:    BackupResult{Mode: RecoveryModeManaged, Snapshot: BackupSummary{SnapshotID: "20260729T120000.000000000Z-0123456789abcdef", Mode: RecoveryModeManaged, FileCount: 15}},
		preview:   RestorePreview{SnapshotID: "20260729T120000.000000000Z-0123456789abcdef", PreviewSHA256: strings.Repeat("a", 64), Missing: []string{"agents/missing.md"}, Identical: []string{"agents/same.md"}, Conflicts: []string{"agents/conflict.md", "plugins/conflict.ts"}},
		restore:   RestoreResult{SnapshotID: "20260729T120000.000000000Z-0123456789abcdef", Created: 1, Unresolved: []string{"plugins/conflict.ts"}},
		reinstall: ProtectedReinstallResult{Mode: RecoveryModeManaged, SnapshotID: "20260729T120000.000000000Z-0123456789abcdef", SnapshotVerified: true, LauncherState: "installed", IntegrationState: "installed", HandshakeOK: true, HandshakeStatus: "healthy", RecoveryGuidance: "Retained snapshot is available."},
	}
}

func enterRecovery(t *testing.T, backend *recordingRecoveryBackend) Model {
	t.Helper()
	model := NewModel(context.Background(), backend, Options{Workspace: "/workspace"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	model, _ = openSetupRoute(t, model)
	updated, cmd := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("entering recovery did not load state")
	}
	return updateModel(t, model, cmd())
}
