package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/opencodebackup"
	setupflow "github.com/vgxness/vgxness/internal/setup"
	"github.com/vgxness/vgxness/internal/tui"
)

type recordingRecoveryRuntime struct {
	planRequest      setupflow.ProtectedReinstallRequest
	listRequest      setupflow.BackupListRequest
	createRequest    setupflow.BackupRequest
	previewRequest   setupflow.RestorePreviewRequest
	restoreRequest   setupflow.RestoreRequest
	reinstallRequest setupflow.ProtectedReinstallRequest
	calls            []string
	conflicts        []string
	unresolved       []string
	missing          []string
	restoreErr       error
}

func (runtime *recordingRecoveryRuntime) PlanProtectedReinstall(_ context.Context, request setupflow.ProtectedReinstallRequest) (setupflow.ProtectedReinstallPlan, error) {
	runtime.calls = append(runtime.calls, "plan")
	runtime.planRequest = request
	return setupflow.ProtectedReinstallPlan{
		Mode: request.Mode, SourceRoot: "/config/opencode", BackupRoot: "/backups", ManagedArtifactCount: 17,
		Ready: true, Integration: integration.Result{State: integration.StateInstalled},
		Handshake: integration.Handshake{OK: true, Status: integration.HandshakeHealthy},
	}, nil
}

func (runtime *recordingRecoveryRuntime) ListBackups(_ context.Context, request setupflow.BackupListRequest) (setupflow.BackupListResult, error) {
	runtime.calls = append(runtime.calls, "list")
	runtime.listRequest = request
	return setupflow.BackupListResult{Backups: []opencodebackup.Summary{{
		SnapshotID: "20260729T120000.000000000Z-0123456789abcdef", CreatedAt: time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC),
		Mode: opencodebackup.ModeManaged, EntryCount: 17, TotalBytes: 2048,
	}}}, nil
}

func (runtime *recordingRecoveryRuntime) CreateBackup(_ context.Context, request setupflow.BackupRequest) (setupflow.BackupResult, error) {
	runtime.calls = append(runtime.calls, "create")
	runtime.createRequest = request
	summary := opencodebackup.Summary{SnapshotID: "20260729T120000.000000000Z-0123456789abcdef", Mode: request.Mode, EntryCount: 17}
	return setupflow.BackupResult{Mode: request.Mode, Summary: summary, Snapshot: opencodebackup.Snapshot{Manifest: opencodebackup.Manifest{SnapshotID: summary.SnapshotID, Mode: request.Mode}, Summary: summary}}, nil
}

func (runtime *recordingRecoveryRuntime) PreviewRestore(_ context.Context, request setupflow.RestorePreviewRequest) (setupflow.RestorePreviewResult, error) {
	runtime.calls = append(runtime.calls, "preview")
	runtime.previewRequest = request
	runtime.conflicts = []string{"agents/conflict.md", "plugins/conflict.ts"}
	return setupflow.RestorePreviewResult{SnapshotID: request.SnapshotID, Preview: opencodebackup.RestorePreview{
		SnapshotID: request.SnapshotID, SHA256: strings.Repeat("a", 64), Missing: []string{"agents/missing.md"}, Identical: []string{"agents/same.md"}, Conflicts: runtime.conflicts,
	}}, nil
}

func (runtime *recordingRecoveryRuntime) Restore(_ context.Context, request setupflow.RestoreRequest) (setupflow.RestoreResult, error) {
	runtime.calls = append(runtime.calls, "restore")
	runtime.restoreRequest = request
	runtime.unresolved = []string{"plugins/conflict.ts"}
	return setupflow.RestoreResult{SnapshotID: request.SnapshotID, Result: opencodebackup.RestoreResult{SnapshotID: request.SnapshotID, Created: 1, Unresolved: runtime.unresolved}}, runtime.restoreErr
}

func (runtime *recordingRecoveryRuntime) ProtectedReinstall(_ context.Context, request setupflow.ProtectedReinstallRequest) (setupflow.ProtectedReinstallResult, error) {
	runtime.calls = append(runtime.calls, "reinstall")
	runtime.reinstallRequest = request
	runtime.missing = []string{"agents/missing.md"}
	return setupflow.ProtectedReinstallResult{
		Mode: request.Mode, SnapshotVerified: true,
		Snapshot:    opencodebackup.Snapshot{Summary: opencodebackup.Summary{SnapshotID: "20260729T120000.000000000Z-0123456789abcdef", Mode: request.Mode, EntryCount: 17}},
		Integration: integration.Result{State: integration.StateInstalled},
		Handshake:   integration.Handshake{OK: true, Status: integration.HandshakeHealthy},
		Recovery:    setupflow.RecoveryAttempt{Attempted: true, Missing: runtime.missing, Guidance: "Inspect retained snapshot."},
	}, nil
}

func TestTUIRecoveryBackendMapsModesPathsAndClonesSlices(t *testing.T) {
	runtime := &recordingRecoveryRuntime{}
	backend := tuiBackend{recovery: runtime}
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace", "..", "project")
	expectedWorkspace := filepath.Clean(workspace)
	backupRoot := filepath.Join(root, "backups")

	plan, err := backend.PlanRecovery(context.Background(), tui.RecoveryPlanRequest{Workspace: workspace, BackupRoot: backupRoot, Mode: "full"})
	if err != nil || !plan.Ready || plan.Mode != "full" || plan.ArtifactCount != 17 || !plan.HandshakeOK || plan.HandshakeStatus != integration.HandshakeHealthy.String() {
		t.Fatalf("PlanRecovery()=%+v, %v", plan, err)
	}
	if runtime.planRequest.Mode != opencodebackup.ModeFull || runtime.planRequest.BackupRoot != backupRoot || runtime.planRequest.Options.Workspace != expectedWorkspace {
		t.Fatalf("plan request=%+v", runtime.planRequest)
	}

	listed, err := backend.ListBackups(context.Background(), tui.BackupListRequest{Workspace: workspace, BackupRoot: backupRoot})
	if err != nil || len(listed.Snapshots) != 1 || listed.Snapshots[0].CreatedAt != "2026-07-29T12:00:00Z" || runtime.listRequest.BackupRoot != backupRoot {
		t.Fatalf("ListBackups()=%+v, %v", listed, err)
	}
	created, err := backend.CreateBackup(context.Background(), tui.CreateBackupRequest{Workspace: workspace, BackupRoot: backupRoot, Mode: "managed"})
	if err != nil || created.Snapshot.Mode != "managed" || runtime.createRequest.Mode != opencodebackup.ModeManaged || runtime.createRequest.BackupRoot != backupRoot {
		t.Fatalf("CreateBackup()=%+v request=%+v err=%v", created, runtime.createRequest, err)
	}

	id := "20260729T120000.000000000Z-0123456789abcdef"
	preview, err := backend.PreviewRestore(context.Background(), tui.RestorePreviewRequest{Workspace: workspace, BackupRoot: backupRoot, SnapshotID: id})
	if err != nil || len(preview.Conflicts) != 2 || runtime.previewRequest.BackupRoot != backupRoot {
		t.Fatalf("PreviewRestore()=%+v, %v", preview, err)
	}
	preview.Conflicts[0] = "changed"
	if runtime.conflicts[0] == "changed" {
		t.Fatal("preview conflicts alias setup result")
	}
	restored, err := backend.RestoreBackup(context.Background(), tui.RestoreRequest{Workspace: workspace, BackupRoot: backupRoot, SnapshotID: id, PreviewSHA256: strings.Repeat("a", 64)})
	if err != nil || restored.Created != 1 || runtime.restoreRequest.BackupRoot != backupRoot {
		t.Fatalf("RestoreBackup()=%+v request=%+v err=%v", restored, runtime.restoreRequest, err)
	}
	restored.Unresolved[0] = "changed"
	if runtime.unresolved[0] == "changed" {
		t.Fatal("restore unresolved paths alias setup result")
	}
	reinstalled, err := backend.ProtectedReinstall(context.Background(), tui.ProtectedReinstallRequest{Workspace: workspace, BackupRoot: backupRoot, Mode: "full"})
	if err != nil || !reinstalled.SnapshotVerified || reinstalled.Mode != "full" || runtime.reinstallRequest.Mode != opencodebackup.ModeFull || runtime.reinstallRequest.BackupRoot != backupRoot || !reinstalled.HandshakeOK || reinstalled.HandshakeStatus != integration.HandshakeHealthy.String() {
		t.Fatalf("ProtectedReinstall()=%+v request=%+v err=%v", reinstalled, runtime.reinstallRequest, err)
	}
	reinstalled.RecoveryMissing[0] = "changed"
	if runtime.missing[0] == "changed" {
		t.Fatal("recovery paths alias setup result")
	}
}

func TestTUIRecoveryBackendRejectsInvalidInputsBeforeDomainCall(t *testing.T) {
	runtime := &recordingRecoveryRuntime{}
	backend := tuiBackend{recovery: runtime}
	id := "20260729T120000.000000000Z-0123456789abcdef"
	for name, call := range map[string]func() error{
		"mode": func() error {
			_, err := backend.PlanRecovery(context.Background(), tui.RecoveryPlanRequest{Workspace: "/workspace", Mode: "other"})
			return err
		},
		"snapshot": func() error {
			_, err := backend.PreviewRestore(context.Background(), tui.RestorePreviewRequest{Workspace: "/workspace", SnapshotID: "../snapshot"})
			return err
		},
		"digest": func() error {
			_, err := backend.RestoreBackup(context.Background(), tui.RestoreRequest{Workspace: "/workspace", SnapshotID: id, PreviewSHA256: "bad"})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); err == nil {
				t.Fatal("invalid request was accepted")
			}
		})
	}
	if len(runtime.calls) != 0 {
		t.Fatalf("invalid requests reached setup domain: %v", runtime.calls)
	}
}

func TestTUIRecoveryBackendReturnsSafeErrors(t *testing.T) {
	backend := tuiBackend{recovery: failingRecoveryRuntime{err: errors.New("SECRET file contents")}}
	_, err := backend.ListBackups(context.Background(), tui.BackupListRequest{Workspace: "/workspace"})
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("unsafe error=%v", err)
	}
}

func TestTUIRecoveryBackendPreservesPartialRestoreWithSafeError(t *testing.T) {
	runtime := &recordingRecoveryRuntime{restoreErr: errors.New("SECRET durability detail")}
	backend := tuiBackend{recovery: runtime}
	result, err := backend.RestoreBackup(context.Background(), tui.RestoreRequest{
		Workspace: "/workspace", SnapshotID: "20260729T120000.000000000Z-0123456789abcdef", PreviewSHA256: strings.Repeat("a", 64),
	})
	if result.Created != 1 || len(result.Unresolved) != 1 || err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("RestoreBackup()=%+v, %v", result, err)
	}
}

type failingRecoveryRuntime struct{ err error }

func (runtime failingRecoveryRuntime) PlanProtectedReinstall(context.Context, setupflow.ProtectedReinstallRequest) (setupflow.ProtectedReinstallPlan, error) {
	return setupflow.ProtectedReinstallPlan{}, runtime.err
}
func (runtime failingRecoveryRuntime) ListBackups(context.Context, setupflow.BackupListRequest) (setupflow.BackupListResult, error) {
	return setupflow.BackupListResult{}, runtime.err
}
func (runtime failingRecoveryRuntime) CreateBackup(context.Context, setupflow.BackupRequest) (setupflow.BackupResult, error) {
	return setupflow.BackupResult{}, runtime.err
}
func (runtime failingRecoveryRuntime) PreviewRestore(context.Context, setupflow.RestorePreviewRequest) (setupflow.RestorePreviewResult, error) {
	return setupflow.RestorePreviewResult{}, runtime.err
}
func (runtime failingRecoveryRuntime) Restore(context.Context, setupflow.RestoreRequest) (setupflow.RestoreResult, error) {
	return setupflow.RestoreResult{}, runtime.err
}
func (runtime failingRecoveryRuntime) ProtectedReinstall(context.Context, setupflow.ProtectedReinstallRequest) (setupflow.ProtectedReinstallResult, error) {
	return setupflow.ProtectedReinstallResult{}, runtime.err
}
