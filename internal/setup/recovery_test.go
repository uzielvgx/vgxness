package setup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/launcher"
	"github.com/vgxness/vgxness/internal/opencodebackup"
	"github.com/vgxness/vgxness/internal/selfinstall"
)

func TestManagedProtectionUsesCodexRecovery(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	p := newManagedProtection(ProviderCodex, integration.Options{ConfigDir: root}, home).(*managedProtection)
	result, err := p.Protect(context.Background())
	if err != nil || !result.Skipped || result.Source == nil {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("managed"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err = p.Protect(context.Background())
	if err != nil || !result.Verified || result.ID == "" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "share", "vgxness", "backups", "codex", result.ID)); err != nil {
		t.Fatal(err)
	}
}

func TestProtectedReinstallCreatesAndVerifiesBackupBeforeMutation(t *testing.T) {
	calls := []string{}
	launcherStatus := managedLauncherForRecoveryTest(t)
	layout := recoveryLayout(t.TempDir())
	managed := &recoveryManaged{status: integration.Result{State: integration.StateInstalled}, layout: layout, calls: &calls}
	backups := &recoveryBackups{snapshot: recoverySnapshot(layout.Root, opencodebackup.ModeManaged), calls: &calls}
	service := recoveryService(launcherStatus, managed, backups, &recoveryProber{responses: []integration.Handshake{{OK: true, Status: integration.HandshakeHealthy}, {OK: true, Status: integration.HandshakeHealthy}}}, &calls)

	result, err := service.ProtectedReinstall(context.Background(), ProtectedReinstallRequest{
		Options: Options{Workspace: t.TempDir()}, Mode: opencodebackup.ModeManaged, BackupRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.SnapshotVerified || result.Snapshot.Manifest.SnapshotID == "" || result.Mode != opencodebackup.ModeManaged || result.Integration.State != integration.StateInstalled || result.Launcher.ActiveSHA256 != launcherStatus.ActiveSHA256 || !result.Handshake.OK || result.Handshake.Status != integration.HandshakeHealthy {
		t.Fatalf("unexpected result: %+v", result)
	}
	create := indexOfRecoveryCall(calls, "backup-create:managed")
	verify := indexOfRecoveryCall(calls, "backup-verify")
	reinstall := indexOfRecoveryCall(calls, "integration-reinstall")
	if create < 0 || verify <= create || reinstall <= verify {
		t.Fatalf("backup was not verified before reinstall: %v", calls)
	}
	if backups.options.SourceRoot != layout.Root || len(backups.options.ManagedPaths) != 17 || backups.options.Launcher == nil || backups.options.Launcher.ActiveSHA256 != launcherStatus.ActiveSHA256 {
		t.Fatalf("backup options do not bind managed layout and launcher: %+v", backups.options)
	}
}

func TestProtectedReinstallBackupFailurePreventsMutationAndPropagatesMode(t *testing.T) {
	for name, test := range map[string]struct{ requested, want opencodebackup.Mode }{
		"default managed": {requested: "", want: opencodebackup.ModeManaged},
		"managed":         {requested: opencodebackup.ModeManaged, want: opencodebackup.ModeManaged},
		"full":            {requested: opencodebackup.ModeFull, want: opencodebackup.ModeFull},
	} {
		t.Run(name, func(t *testing.T) {
			launcherStatus := managedLauncherForRecoveryTest(t)
			layout := recoveryLayout(t.TempDir())
			managed := &recoveryManaged{status: integration.Result{State: integration.StateInstalled}, layout: layout}
			backupErr := errors.New("backup failed")
			backups := &recoveryBackups{createErr: backupErr}
			service := recoveryService(launcherStatus, managed, backups, healthyRecoveryProber(), nil)
			result, err := service.ProtectedReinstall(context.Background(), ProtectedReinstallRequest{Options: Options{Workspace: t.TempDir()}, Mode: test.requested, BackupRoot: t.TempDir()})
			if !errors.Is(err, backupErr) || managed.reinstallCalls != 0 || result.Mode != test.want || backups.createMode != test.want || result.Snapshot.Manifest.SnapshotID != "" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestBackupPublicationErrorPreservesSnapshotAndPreventsReinstall(t *testing.T) {
	launcherStatus := managedLauncherForRecoveryTest(t)
	layout := recoveryLayout(t.TempDir())
	managed := &recoveryManaged{status: integration.Result{State: integration.StateInstalled}, layout: layout}
	retained := recoverySnapshot(layout.Root, opencodebackup.ModeManaged)
	createErr := errors.New("publication durability failed")
	backups := &recoveryBackups{snapshot: retained, createErr: createErr}
	service := recoveryService(launcherStatus, managed, backups, healthyRecoveryProber(), nil)

	created, err := service.CreateBackup(context.Background(), BackupRequest{Options: Options{Workspace: t.TempDir()}, Mode: opencodebackup.ModeManaged, BackupRoot: t.TempDir()})
	if !errors.Is(err, createErr) || created.Snapshot.Manifest.SnapshotID != retained.Manifest.SnapshotID {
		t.Fatalf("CreateBackup()=%+v, %v", created, err)
	}
	result, err := service.ProtectedReinstall(context.Background(), ProtectedReinstallRequest{Options: Options{Workspace: t.TempDir()}, Mode: opencodebackup.ModeManaged, BackupRoot: t.TempDir()})
	if !errors.Is(err, createErr) || result.Snapshot.Manifest.SnapshotID != retained.Manifest.SnapshotID || managed.reinstallCalls != 0 {
		t.Fatalf("ProtectedReinstall()=%+v, %v reinstallCalls=%d", result, err, managed.reinstallCalls)
	}
}

func TestProtectedReinstallDetectsLauncherMutationAndRetainsSnapshot(t *testing.T) {
	launcherStatus := managedLauncherForRecoveryTest(t)
	layout := recoveryLayout(t.TempDir())
	managed := &recoveryManaged{status: integration.Result{State: integration.StateInstalled}, layout: layout}
	managed.onReinstall = func() {
		activePath := launcher.VersionPath(launcherStatus.DataDir, launcherStatus.ActiveSHA256)
		if err := os.WriteFile(activePath, []byte("changed active binary"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	backups := &recoveryBackups{snapshot: recoverySnapshot(layout.Root, opencodebackup.ModeManaged)}
	service := recoveryService(launcherStatus, managed, backups, healthyRecoveryProber(), nil)
	result, err := service.ProtectedReinstall(context.Background(), ProtectedReinstallRequest{Options: Options{Workspace: t.TempDir()}, Mode: opencodebackup.ModeManaged, BackupRoot: t.TempDir()})
	if !errors.Is(err, ErrVerification) || !result.SnapshotVerified || result.Snapshot.Manifest.SnapshotID == "" {
		t.Fatalf("launcher mutation result=%+v err=%v", result, err)
	}
}

func TestProtectedReinstallRetainsSnapshotAcrossFailures(t *testing.T) {
	for name, test := range map[string]struct {
		err       error
		responses []integration.Handshake
		want      error
	}{
		"provider":  {errors.New("provider failed"), []integration.Handshake{{OK: true, Status: integration.HandshakeHealthy}}, nil},
		"cancelled": {context.Canceled, []integration.Handshake{{OK: true, Status: integration.HandshakeHealthy}}, context.Canceled},
		"handshake": {nil, []integration.Handshake{{OK: true, Status: integration.HandshakeHealthy}, {Status: integration.HandshakeUnavailable}}, ErrVerification},
	} {
		t.Run(name, func(t *testing.T) {
			launcherStatus := managedLauncherForRecoveryTest(t)
			layout := recoveryLayout(t.TempDir())
			managed := &recoveryManaged{status: integration.Result{State: integration.StateInstalled}, layout: layout, reinstallErr: test.err}
			backups := &recoveryBackups{snapshot: recoverySnapshot(layout.Root, opencodebackup.ModeManaged)}
			service := recoveryService(launcherStatus, managed, backups, &recoveryProber{responses: test.responses}, nil)
			result, err := service.ProtectedReinstall(context.Background(), ProtectedReinstallRequest{Options: Options{Workspace: t.TempDir()}, Mode: opencodebackup.ModeManaged, BackupRoot: t.TempDir()})
			if err == nil || result.Snapshot.Manifest.SnapshotID == "" || !result.SnapshotVerified {
				t.Fatalf("snapshot was not retained: result=%+v err=%v", result, err)
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("error=%v, want %v", err, test.want)
			}
		})
	}
}

func TestProtectedReinstallHandshakeFailureReturnsExplicitGuidance(t *testing.T) {
	launcherStatus := managedLauncherForRecoveryTest(t)
	layout := recoveryLayout(t.TempDir())
	managed := &recoveryManaged{status: integration.Result{State: integration.StateInstalled}, layout: layout}
	backups := &recoveryBackups{snapshot: recoverySnapshot(layout.Root, opencodebackup.ModeManaged)}
	service := recoveryService(launcherStatus, managed, backups, &recoveryProber{responses: []integration.Handshake{{OK: true, Status: integration.HandshakeHealthy}, {Status: integration.HandshakeIncompatible}}}, nil)
	result, err := service.ProtectedReinstall(context.Background(), ProtectedReinstallRequest{Options: Options{Workspace: t.TempDir()}, Mode: opencodebackup.ModeManaged, BackupRoot: t.TempDir()})
	if !errors.Is(err, ErrVerification) || result.Integration.State != integration.StateInstalled || result.Snapshot.Manifest.SnapshotID == "" || result.Handshake.Status != integration.HandshakeIncompatible || !strings.Contains(result.Recovery.Guidance, "integración") || !strings.Contains(result.Recovery.Guidance, "instantánea") || !strings.Contains(result.Recovery.Guidance, "conexión") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestRestorePreservesPartialResultWithError(t *testing.T) {
	launcherStatus := managedLauncherForRecoveryTest(t)
	layout := recoveryLayout(t.TempDir())
	managed := &recoveryManaged{status: integration.Result{State: integration.StateInstalled}, layout: layout}
	restoreErr := errors.New("durability failed after publication")
	backups := &recoveryBackups{restoreResult: opencodebackup.RestoreResult{SnapshotID: "snapshot-1", Created: 1, Unresolved: []string{"agents/conflict"}}, restoreErr: restoreErr}
	service := recoveryService(launcherStatus, managed, backups, &recoveryProber{}, nil)
	result, err := service.Restore(context.Background(), RestoreRequest{Options: Options{Workspace: t.TempDir()}, SnapshotID: "snapshot-1", PreviewSHA256: strings.Repeat("a", 64)})
	if !errors.Is(err, restoreErr) || result.Result.Created != 1 || !reflect.DeepEqual(result.Result.Unresolved, []string{"agents/conflict"}) {
		t.Fatalf("Restore()=%+v, %v", result, err)
	}
}

func TestProtectedReinstallRecoveryRestoresOnlyMissingManagedPaths(t *testing.T) {
	launcherStatus := managedLauncherForRecoveryTest(t)
	layout := recoveryLayout(t.TempDir())
	missingManaged := layout.Artifacts[3].RelativePath
	managed := &recoveryManaged{status: integration.Result{State: integration.StateInstalled}, layout: layout, reinstallErr: integration.ErrRecovery}
	ctx, cancel := context.WithCancel(context.Background())
	managed.onReinstall = cancel
	backups := &recoveryBackups{
		snapshot:      recoverySnapshot(layout.Root, opencodebackup.ModeFull),
		preview:       opencodebackup.RestorePreview{SnapshotID: "snapshot-1", SHA256: strings.Repeat("b", 64), Missing: []string{missingManaged, "unrelated-missing"}},
		restoreResult: opencodebackup.RestoreResult{SnapshotID: "snapshot-1", Created: 1},
	}
	service := recoveryService(launcherStatus, managed, backups, healthyRecoveryProber(), nil)
	result, err := service.ProtectedReinstall(ctx, ProtectedReinstallRequest{Options: Options{Workspace: t.TempDir()}, Mode: opencodebackup.ModeFull, BackupRoot: t.TempDir()})
	if !errors.Is(err, integration.ErrRecovery) || !result.Recovery.Attempted || result.Recovery.Result.Created != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if !reflect.DeepEqual(backups.restoreRequest.IncludePaths, []string{missingManaged}) || len(backups.restoreRequest.ReplaceConflicts) != 0 {
		t.Fatalf("recovery restore request = %+v", backups.restoreRequest)
	}
	if !strings.Contains(result.Recovery.Guidance, "archivos administrados ausentes") || !strings.Contains(result.Recovery.Guidance, "no se sobrescribieron") {
		t.Fatalf("recovery guidance=%q", result.Recovery.Guidance)
	}
	if backups.previewCtxErr != nil || backups.restoreCtxErr != nil {
		t.Fatalf("bounded recovery inherited cancellation: preview=%v restore=%v", backups.previewCtxErr, backups.restoreCtxErr)
	}
}

func TestProtectedReinstallPlanBlocksInvalidModeRootAndInventory(t *testing.T) {
	launcherStatus := managedLauncherForRecoveryTest(t)
	layout := recoveryLayout(t.TempDir())
	managed := &recoveryManaged{status: integration.Result{State: integration.StateInstalled, ArtifactCount: 17}, layout: layout}
	service := recoveryService(launcherStatus, managed, &recoveryBackups{}, healthyRecoveryProber(), nil)
	if _, err := service.PlanProtectedReinstall(context.Background(), ProtectedReinstallRequest{Options: Options{Workspace: t.TempDir()}, Mode: opencodebackup.Mode("invalid"), BackupRoot: t.TempDir()}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid mode error = %v", err)
	}

	managed.layout.Artifacts = managed.layout.Artifacts[:16]
	plan, err := service.PlanProtectedReinstall(context.Background(), ProtectedReinstallRequest{Options: Options{Workspace: t.TempDir()}, Mode: opencodebackup.ModeManaged, BackupRoot: t.TempDir()})
	if err != nil || plan.Ready || plan.Blocker != "El inventario de artefactos administrados de OpenCode no es válido." {
		t.Fatalf("invalid inventory plan=%+v err=%v", plan, err)
	}

	managed.layout = layout
	service.backups = func(opencodebackup.Options) (BackupEngine, error) { return nil, opencodebackup.ErrInvalid }
	plan, err = service.PlanProtectedReinstall(context.Background(), ProtectedReinstallRequest{Options: Options{Workspace: t.TempDir()}, Mode: opencodebackup.ModeManaged, BackupRoot: filepath.Join(layout.Root, "nested")})
	if err != nil || plan.Ready || plan.Blocker != "El destino de respaldo no es válido." {
		t.Fatalf("nested root plan=%+v err=%v", plan, err)
	}
}

func TestProtectedReinstallPlanUsesProviderArtifactCountWithoutFixedDuplicate(t *testing.T) {
	launcherStatus := managedLauncherForRecoveryTest(t)
	layout := recoveryLayout(t.TempDir())
	layout.Artifacts = append([]integration.ManagedArtifact(nil), layout.Artifacts[:3]...)
	layout.AggregateSHA256 = recoveryLayoutDigest(layout.Artifacts)
	managed := &recoveryManaged{status: integration.Result{State: integration.StateInstalled, ArtifactCount: len(layout.Artifacts)}, layout: layout}
	service := recoveryService(launcherStatus, managed, &recoveryBackups{}, healthyRecoveryProber(), nil)
	plan, err := service.PlanProtectedReinstall(context.Background(), ProtectedReinstallRequest{Options: Options{Workspace: t.TempDir()}, Mode: opencodebackup.ModeManaged, BackupRoot: t.TempDir()})
	if err != nil || !plan.Ready || plan.ManagedArtifactCount != 3 {
		t.Fatalf("provider-sized inventory plan=%+v err=%v", plan, err)
	}
}

func TestProtectedReinstallPlanUsesManagedLayoutWhenStatusIncludesAdditionalArtifact(t *testing.T) {
	launcherStatus := managedLauncherForRecoveryTest(t)
	layout := recoveryLayout(t.TempDir())
	managed := &recoveryManaged{status: integration.Result{State: integration.StateInstalled, ArtifactCount: len(layout.Artifacts) + 1}, layout: layout}
	service := recoveryService(launcherStatus, managed, &recoveryBackups{}, healthyRecoveryProber(), nil)

	plan, err := service.PlanProtectedReinstall(context.Background(), ProtectedReinstallRequest{Options: Options{Workspace: t.TempDir()}, Mode: opencodebackup.ModeManaged, BackupRoot: t.TempDir()})
	if err != nil || !plan.Ready || plan.ManagedArtifactCount != len(layout.Artifacts) {
		t.Fatalf("PlanProtectedReinstall() = %+v, %v", plan, err)
	}
}

func TestProtectedReinstallPendingEvidenceBlocksBeforeBackupMutation(t *testing.T) {
	for name, pendingErr := range map[string]error{
		"valid marker":   nil,
		"invalid marker": errors.New("invalid pending evidence"),
	} {
		t.Run(name, func(t *testing.T) {
			launcherStatus := managedLauncherForRecoveryTest(t)
			layout := recoveryLayout(t.TempDir())
			managed := &recoveryManaged{
				status: integration.Result{State: integration.StateInstalled}, layout: layout,
				pending: pendingErr == nil, pendingErr: pendingErr,
			}
			backups := &recoveryBackups{snapshot: recoverySnapshot(layout.Root, opencodebackup.ModeManaged)}
			service := recoveryService(launcherStatus, managed, backups, healthyRecoveryProber(), nil)
			request := ProtectedReinstallRequest{Options: Options{Workspace: t.TempDir()}, Mode: opencodebackup.ModeManaged, BackupRoot: t.TempDir()}

			plan, err := service.PlanProtectedReinstall(context.Background(), request)
			if err != nil || plan.Ready || plan.Blocker == "" || (pendingErr == nil) != plan.RecoveryPending {
				t.Fatalf("PlanProtectedReinstall() = %+v, %v", plan, err)
			}
			result, err := service.ProtectedReinstall(context.Background(), request)
			if !errors.Is(err, ErrPrerequisite) || result.Plan.Blocker == "" || managed.reinstallCalls != 0 || backups.createMode != "" {
				t.Fatalf("ProtectedReinstall() = %+v, %v; reinstall=%d backupMode=%q", result, err, managed.reinstallCalls, backups.createMode)
			}
		})
	}
}

func TestPendingReinstallDoesNotBlockBackupListing(t *testing.T) {
	launcherStatus := managedLauncherForRecoveryTest(t)
	layout := recoveryLayout(t.TempDir())
	managed := &recoveryManaged{status: integration.Result{State: integration.StatePartial}, layout: layout, pending: true}
	backups := &recoveryBackups{list: []opencodebackup.Summary{{SnapshotID: "snapshot-1"}}}
	service := recoveryService(launcherStatus, managed, backups, &recoveryProber{}, nil)

	result, err := service.ListBackups(context.Background(), BackupListRequest{Options: Options{Workspace: t.TempDir()}, BackupRoot: t.TempDir()})
	if err != nil || len(result.Backups) != 1 {
		t.Fatalf("ListBackups() = %+v, %v", result, err)
	}
}

func TestSetupExposesBackupAndMergeRestoreOperations(t *testing.T) {
	launcherStatus := managedLauncherForRecoveryTest(t)
	layout := recoveryLayout(t.TempDir())
	managed := &recoveryManaged{status: integration.Result{State: integration.StateInstalled}, layout: layout}
	backups := &recoveryBackups{
		snapshot:      recoverySnapshot(layout.Root, opencodebackup.ModeManaged),
		list:          []opencodebackup.Summary{{SnapshotID: "snapshot-1"}},
		preview:       opencodebackup.RestorePreview{SnapshotID: "snapshot-1", SHA256: strings.Repeat("c", 64)},
		restoreResult: opencodebackup.RestoreResult{SnapshotID: "snapshot-1", Created: 1, Unresolved: []string{"agents/conflict.md"}},
	}
	service := recoveryService(launcherStatus, managed, backups, &recoveryProber{}, nil)
	options := Options{Workspace: t.TempDir()}
	list, err := service.ListBackups(context.Background(), BackupListRequest{Options: options, BackupRoot: t.TempDir()})
	if err != nil || len(list.Backups) != 1 {
		t.Fatalf("ListBackups()=%+v, %v", list, err)
	}
	created, err := service.CreateBackup(context.Background(), BackupRequest{Options: options, BackupRoot: t.TempDir(), Mode: opencodebackup.ModeManaged})
	if err != nil || created.Mode != opencodebackup.ModeManaged || created.Snapshot.Manifest.SnapshotID == "" {
		t.Fatalf("CreateBackup()=%+v, %v", created, err)
	}
	preview, err := service.PreviewRestore(context.Background(), RestorePreviewRequest{Options: options, BackupRoot: t.TempDir(), SnapshotID: "snapshot-1"})
	if err != nil || preview.Preview.SHA256 == "" {
		t.Fatalf("PreviewRestore()=%+v, %v", preview, err)
	}
	restored, err := service.Restore(context.Background(), RestoreRequest{Options: options, BackupRoot: t.TempDir(), SnapshotID: "snapshot-1", PreviewSHA256: preview.Preview.SHA256})
	if err != nil || restored.Result.Created != 1 || !reflect.DeepEqual(restored.Result.Unresolved, []string{"agents/conflict.md"}) || len(backups.restoreRequest.ReplaceConflicts) != 0 || backups.restoreRequest.PreviewSHA256 != preview.Preview.SHA256 {
		t.Fatalf("Restore()=%+v request=%+v err=%v", restored, backups.restoreRequest, err)
	}
}

type recoveryManaged struct {
	status         integration.Result
	layout         integration.ManagedLayout
	reinstallErr   error
	reinstallCalls int
	calls          *[]string
	onReinstall    func()
	pending        bool
	pendingErr     error
}

func (f *recoveryManaged) Preview(context.Context, integration.Options) (integration.Result, error) {
	return f.status, nil
}
func (f *recoveryManaged) Install(context.Context, integration.Options) (integration.Result, error) {
	return f.status, nil
}
func (f *recoveryManaged) Status(context.Context, integration.Options) (integration.Result, error) {
	if f.calls != nil {
		*f.calls = append(*f.calls, "integration-status")
	}
	return f.status, nil
}
func (f *recoveryManaged) Uninstall(context.Context, integration.Options) (integration.Result, error) {
	return f.status, nil
}
func (f *recoveryManaged) ManagedLayout(context.Context, integration.Options) (integration.ManagedLayout, error) {
	if f.calls != nil {
		*f.calls = append(*f.calls, "integration-layout")
	}
	return f.layout, nil
}
func (f *recoveryManaged) ReinstallPending(context.Context, integration.Options) (bool, error) {
	return f.pending, f.pendingErr
}
func (f *recoveryManaged) Reinstall(context.Context, integration.Options) (integration.Result, error) {
	f.reinstallCalls++
	if f.calls != nil {
		*f.calls = append(*f.calls, "integration-reinstall")
	}
	if f.onReinstall != nil {
		f.onReinstall()
	}
	return f.status, f.reinstallErr
}

type recoveryInstaller struct {
	status selfinstall.Result
	calls  *[]string
}

func (f *recoveryInstaller) Preview(context.Context, selfinstall.Options) (selfinstall.Result, error) {
	return f.status, nil
}
func (f *recoveryInstaller) Install(context.Context, selfinstall.Options) (selfinstall.Result, error) {
	return f.status, nil
}
func (f *recoveryInstaller) Status(context.Context, selfinstall.Options) (selfinstall.Result, error) {
	if f.calls != nil {
		*f.calls = append(*f.calls, "launcher-status")
	}
	return f.status, nil
}
func (f *recoveryInstaller) Rollback(context.Context, selfinstall.Options) (selfinstall.Result, error) {
	return f.status, nil
}
func (f *recoveryInstaller) GCPreview(context.Context, selfinstall.Options) (selfinstall.GCResult, error) {
	return selfinstall.GCResult{}, errors.New("unexpected self GC preview")
}
func (f *recoveryInstaller) GCApply(context.Context, selfinstall.Options, string) (selfinstall.GCResult, error) {
	return selfinstall.GCResult{}, errors.New("unexpected self GC apply")
}
func (f *recoveryInstaller) GCRecover(context.Context, selfinstall.Options) (selfinstall.GCResult, error) {
	return selfinstall.GCResult{}, errors.New("unexpected self GC recover")
}

type recoveryBackups struct {
	options        opencodebackup.Options
	createMode     opencodebackup.Mode
	snapshot       opencodebackup.Snapshot
	list           []opencodebackup.Summary
	preview        opencodebackup.RestorePreview
	restoreResult  opencodebackup.RestoreResult
	restoreRequest opencodebackup.RestoreRequest
	createErr      error
	restoreErr     error
	calls          *[]string
	previewCtxErr  error
	restoreCtxErr  error
}

func (f *recoveryBackups) List(context.Context) ([]opencodebackup.Summary, error) {
	return append([]opencodebackup.Summary(nil), f.list...), nil
}
func (f *recoveryBackups) Create(_ context.Context, mode opencodebackup.Mode) (opencodebackup.Snapshot, error) {
	f.createMode = mode
	if f.calls != nil {
		*f.calls = append(*f.calls, "backup-create:"+string(mode))
	}
	return f.snapshot, f.createErr
}
func (f *recoveryBackups) Verify(context.Context, string) (opencodebackup.Snapshot, error) {
	if f.calls != nil {
		*f.calls = append(*f.calls, "backup-verify")
	}
	return f.snapshot, nil
}
func (f *recoveryBackups) PreviewRestore(ctx context.Context, _ string) (opencodebackup.RestorePreview, error) {
	f.previewCtxErr = ctx.Err()
	return f.preview, nil
}
func (f *recoveryBackups) Restore(ctx context.Context, request opencodebackup.RestoreRequest) (opencodebackup.RestoreResult, error) {
	f.restoreCtxErr = ctx.Err()
	f.restoreRequest = request
	return f.restoreResult, f.restoreErr
}

type recoveryProber struct {
	responses []integration.Handshake
	index     int
}

func (f *recoveryProber) Probe(context.Context, string) (integration.Handshake, error) {
	if len(f.responses) == 0 {
		return integration.Handshake{}, nil
	}
	index := min(f.index, len(f.responses)-1)
	f.index++
	return f.responses[index], nil
}

func healthyRecoveryProber() *recoveryProber {
	return &recoveryProber{responses: []integration.Handshake{{OK: true, Status: integration.HandshakeHealthy}}}
}

func recoveryService(status selfinstall.Result, managed *recoveryManaged, backups *recoveryBackups, health *recoveryProber, calls *[]string) *Service {
	installer := &recoveryInstaller{status: status, calls: calls}
	return NewWithRecovery(installer, managed, func(string) (integration.ManagedRuntime, error) { return managed, nil }, func(options opencodebackup.Options) (BackupEngine, error) {
		backups.options = options
		return backups, nil
	}, health)
}

func recoveryLayout(root string) integration.ManagedLayout {
	artifacts := make([]integration.ManagedArtifact, 17)
	for index := range artifacts {
		artifacts[index] = integration.ManagedArtifact{RelativePath: "agents/artifact-" + string(rune('a'+index)), SHA256: strings.Repeat(string("0123456789abcdef"[index%16]), 64)}
	}
	return integration.ManagedLayout{Root: root, Artifacts: artifacts, AggregateSHA256: recoveryLayoutDigest(artifacts)}
}

func recoveryLayoutDigest(artifacts []integration.ManagedArtifact) string {
	hash := sha256.New()
	for _, artifact := range artifacts {
		_, _ = hash.Write([]byte(artifact.RelativePath))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(artifact.SHA256))
		_, _ = hash.Write([]byte{'\n'})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func managedLauncherForRecoveryTest(t *testing.T) selfinstall.Result {
	t.Helper()
	root := t.TempDir()
	launcherPath := filepath.Join(root, "bin", "vgxness")
	if err := os.MkdirAll(filepath.Dir(launcherPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(launcherPath, []byte("launcher"), 0o700); err != nil {
		t.Fatal(err)
	}
	launcherSHA, err := launcher.FileSHA256(launcherPath)
	if err != nil {
		t.Fatal(err)
	}
	activeBytes := []byte("active binary")
	activeDigest := sha256.Sum256(activeBytes)
	activeSHA := hex.EncodeToString(activeDigest[:])
	dataDir := filepath.Join(root, "data")
	activePath := launcher.VersionPath(dataDir, activeSHA)
	if err := os.MkdirAll(filepath.Dir(activePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(activePath, activeBytes, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := launcher.Manifest{SchemaVersion: launcher.SchemaVersion, ManagedBy: launcher.ManagedBy, LauncherPath: launcherPath, LauncherSHA256: launcherSHA, DataDir: dataDir, ActivePath: activePath, ActiveSHA256: activeSHA, UpdatedAt: "2026-07-29T12:00:00Z"}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := launcher.SidecarPath(launcherPath)
	if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return selfinstall.Result{State: selfinstall.StateInstalled, LauncherPath: launcherPath, ManifestPath: manifestPath, DataDir: dataDir, ActiveSHA256: activeSHA}
}

func recoverySnapshot(root string, mode opencodebackup.Mode) opencodebackup.Snapshot {
	manifest := opencodebackup.Manifest{SchemaVersion: opencodebackup.SchemaVersion, SnapshotID: "snapshot-1", Mode: mode, SourceRoot: root, Entries: []opencodebackup.Entry{}}
	return opencodebackup.Snapshot{Manifest: manifest, Summary: opencodebackup.Summary{SchemaVersion: opencodebackup.SchemaVersion, SnapshotID: manifest.SnapshotID, Mode: mode, SourceRoot: root}}
}

func indexOfRecoveryCall(values []string, target string) int {
	for index, value := range values {
		if value == target {
			return index
		}
	}
	return -1
}
