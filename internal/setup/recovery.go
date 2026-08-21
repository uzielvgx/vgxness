package setup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/launcher"
	"github.com/vgxness/vgxness/internal/opencodebackup"
	"github.com/vgxness/vgxness/internal/providers/codex"
	"github.com/vgxness/vgxness/internal/selfinstall"
	"github.com/vgxness/vgxness/internal/skills"
)

type ManagedIntegrationFactory func(string) (integration.ManagedRuntime, error)

type BackupEngine interface {
	List(context.Context) ([]opencodebackup.Summary, error)
	Create(context.Context, opencodebackup.Mode) (opencodebackup.Snapshot, error)
	Verify(context.Context, string) (opencodebackup.Snapshot, error)
	PreviewRestore(context.Context, string) (opencodebackup.RestorePreview, error)
	Restore(context.Context, opencodebackup.RestoreRequest) (opencodebackup.RestoreResult, error)
}

type BackupEngineFactory func(opencodebackup.Options) (BackupEngine, error)

// NewWithRecovery preserves the ordinary setup surface while enabling the
// recovery operations used by later application and TUI wiring.
func NewWithRecovery(installer selfinstall.Runtime, preview integration.Runtime, integrations ManagedIntegrationFactory, backups BackupEngineFactory, prober Prober) *Service {
	var ordinary IntegrationFactory
	if integrations != nil {
		ordinary = func(path string) (integration.Runtime, error) { return integrations(path) }
	}
	service := New(installer, preview, ordinary, prober)
	service.skills = skills.New()
	service.managedIntegrations = integrations
	service.backups = backups
	return service
}

type BackupListRequest struct {
	Options    Options
	BackupRoot string
}

type BackupListResult struct {
	Backups []opencodebackup.Summary
}

type BackupRequest struct {
	Options    Options
	BackupRoot string
	Mode       opencodebackup.Mode
}

type BackupResult struct {
	Mode     opencodebackup.Mode
	Snapshot opencodebackup.Snapshot
	Summary  opencodebackup.Summary
}

type RestorePreviewRequest struct {
	Options    Options
	BackupRoot string
	SnapshotID string
}

type RestorePreviewResult struct {
	SnapshotID string
	Preview    opencodebackup.RestorePreview
}

type RestoreRequest struct {
	Options       Options
	BackupRoot    string
	SnapshotID    string
	PreviewSHA256 string
}

type RestoreResult struct {
	SnapshotID string
	Result     opencodebackup.RestoreResult
}

type ProtectedReinstallRequest struct {
	Options    Options
	BackupRoot string
	Mode       opencodebackup.Mode
}

type ProtectedReinstallPlan struct {
	Mode                 opencodebackup.Mode
	SourceRoot           string
	BackupRoot           string
	ManagedArtifactCount int
	Launcher             selfinstall.Result
	Integration          integration.Result
	Layout               integration.ManagedLayout
	Handshake            integration.Handshake
	RecoveryPending      bool
	Ready                bool
	Blocker              string
}

type RecoveryAttempt struct {
	Attempted bool
	Missing   []string
	Result    opencodebackup.RestoreResult
	Guidance  string
}

type ProtectedReinstallResult struct {
	Plan             ProtectedReinstallPlan
	Mode             opencodebackup.Mode
	Snapshot         opencodebackup.Snapshot
	SnapshotVerified bool
	Launcher         selfinstall.Result
	Integration      integration.Result
	Handshake        integration.Handshake
	Recovery         RecoveryAttempt
}

type recoveryPreflight struct {
	plan             ProtectedReinstallPlan
	runtime          integration.ManagedRuntime
	engine           BackupEngine
	launcherMetadata *opencodebackup.LauncherMetadata
}

func (service *Service) ListBackups(ctx context.Context, request BackupListRequest) (BackupListResult, error) {
	prepared, err := service.prepareRecovery(ctx, request.Options, request.BackupRoot, false)
	if err != nil {
		return BackupListResult{}, err
	}
	if prepared.plan.Blocker != "" {
		return BackupListResult{}, ErrPrerequisite
	}
	backups, err := prepared.engine.List(ctx)
	if err != nil {
		return BackupListResult{}, err
	}
	return BackupListResult{Backups: append([]opencodebackup.Summary(nil), backups...)}, nil
}

func (service *Service) CreateBackup(ctx context.Context, request BackupRequest) (BackupResult, error) {
	mode := normalizedRecoveryMode(request.Mode)
	result := BackupResult{Mode: mode}
	if err := mode.Validate(); err != nil {
		return result, errors.Join(ErrInvalid, err)
	}
	prepared, err := service.prepareRecovery(ctx, request.Options, request.BackupRoot, false)
	if err != nil {
		return result, err
	}
	if prepared.plan.Blocker != "" {
		return result, ErrPrerequisite
	}
	snapshot, err := prepared.engine.Create(ctx, mode)
	result.Snapshot = snapshot
	result.Summary = snapshot.Summary
	if err != nil {
		return result, err
	}
	verified, err := prepared.engine.Verify(ctx, snapshot.Manifest.SnapshotID)
	if err != nil {
		return result, err
	}
	result.Snapshot = verified
	result.Summary = verified.Summary
	return result, nil
}

func (service *Service) PreviewRestore(ctx context.Context, request RestorePreviewRequest) (RestorePreviewResult, error) {
	result := RestorePreviewResult{SnapshotID: request.SnapshotID}
	prepared, err := service.prepareRecovery(ctx, request.Options, request.BackupRoot, false)
	if err != nil {
		return result, err
	}
	if prepared.plan.Blocker != "" {
		return result, ErrPrerequisite
	}
	preview, err := prepared.engine.PreviewRestore(ctx, request.SnapshotID)
	if err != nil {
		return result, err
	}
	result.Preview = preview
	return result, nil
}

func (service *Service) Restore(ctx context.Context, request RestoreRequest) (RestoreResult, error) {
	result := RestoreResult{SnapshotID: request.SnapshotID}
	prepared, err := service.prepareRecovery(ctx, request.Options, request.BackupRoot, false)
	if err != nil {
		return result, err
	}
	if prepared.plan.Blocker != "" {
		return result, ErrPrerequisite
	}
	restored, err := prepared.engine.Restore(ctx, opencodebackup.RestoreRequest{
		SnapshotID: request.SnapshotID, PreviewSHA256: request.PreviewSHA256,
	})
	result.Result = restored
	return result, err
}

func (service *Service) PlanProtectedReinstall(ctx context.Context, request ProtectedReinstallRequest) (ProtectedReinstallPlan, error) {
	mode := normalizedRecoveryMode(request.Mode)
	if err := mode.Validate(); err != nil {
		return ProtectedReinstallPlan{Mode: mode}, errors.Join(ErrInvalid, err)
	}
	prepared, err := service.prepareRecovery(ctx, request.Options, request.BackupRoot, true)
	if err != nil {
		return prepared.plan, err
	}
	prepared.plan.Mode = mode
	return prepared.plan, nil
}

func (service *Service) ProtectedReinstall(ctx context.Context, request ProtectedReinstallRequest) (ProtectedReinstallResult, error) {
	mode := normalizedRecoveryMode(request.Mode)
	result := ProtectedReinstallResult{Mode: mode}
	if err := mode.Validate(); err != nil {
		return result, errors.Join(ErrInvalid, err)
	}
	prepared, err := service.prepareRecovery(ctx, request.Options, request.BackupRoot, true)
	result.Plan = prepared.plan
	result.Plan.Mode = mode
	if err != nil {
		return result, err
	}
	if !prepared.plan.Ready {
		return result, ErrPrerequisite
	}

	snapshot, err := prepared.engine.Create(ctx, mode)
	result.Snapshot = snapshot
	if err != nil {
		return result, err
	}
	verified, err := prepared.engine.Verify(ctx, snapshot.Manifest.SnapshotID)
	if err != nil {
		return result, err
	}
	if verified.Manifest.SnapshotID != snapshot.Manifest.SnapshotID || verified.Manifest.Mode != mode {
		return result, fmt.Errorf("%w: backup identity", ErrVerification)
	}
	result.Snapshot = verified
	result.SnapshotVerified = true

	launcherStatus, metadata, err := service.currentLauncher(ctx, request.Options)
	if err != nil || !sameLauncher(prepared.launcherMetadata, metadata) {
		return result, fmt.Errorf("%w: launcher changed before reinstall", ErrVerification)
	}
	result.Launcher = launcherStatus

	reinstalled, reinstallErr := prepared.runtime.Reinstall(ctx, request.Options.Integration)
	result.Integration = reinstalled
	if reinstallErr != nil {
		if errors.Is(reinstallErr, integration.ErrRecovery) {
			recoveryErr := service.recoverMissingManaged(ctx, prepared, verified, &result)
			return result, errors.Join(reinstallErr, recoveryErr)
		}
		return result, reinstallErr
	}

	status, err := prepared.runtime.Status(ctx, request.Options.Integration)
	result.Integration = status
	if err != nil || status.State != integration.StateInstalled {
		return result, fmt.Errorf("%w: integration state", ErrVerification)
	}
	layout, err := prepared.runtime.ManagedLayout(ctx, request.Options.Integration)
	if err != nil || validateManagedLayout(layout) != nil || layout.AggregateSHA256 != prepared.plan.Layout.AggregateSHA256 {
		return result, fmt.Errorf("%w: managed inventory", ErrVerification)
	}
	launcherStatus, metadata, err = service.currentLauncher(ctx, request.Options)
	result.Launcher = launcherStatus
	if err != nil || !sameLauncher(prepared.launcherMetadata, metadata) {
		return result, fmt.Errorf("%w: launcher identity", ErrVerification)
	}
	handshake, err := service.prober.Probe(ctx, request.Options.Workspace)
	result.Handshake = handshake
	if err != nil || !handshake.OK {
		result.Recovery.Guidance = "La integración está instalada y se conserva la instantánea verificada. Corrige la conexión con OpenCode antes de reintentar."
		return result, fmt.Errorf("%w: OpenCode handshake", ErrVerification)
	}
	return result, nil
}

func (service *Service) recoverMissingManaged(ctx context.Context, prepared recoveryPreflight, snapshot opencodebackup.Snapshot, result *ProtectedReinstallResult) error {
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	result.Recovery.Attempted = true
	preview, err := prepared.engine.PreviewRestore(recoveryCtx, snapshot.Manifest.SnapshotID)
	if err != nil {
		result.Recovery.Guidance = "Se conserva la instantánea verificada; inspecciona la integración antes de reintentar la recuperación."
		return err
	}
	managed := make(map[string]struct{}, len(prepared.plan.Layout.Artifacts))
	for _, artifact := range prepared.plan.Layout.Artifacts {
		managed[artifact.RelativePath] = struct{}{}
	}
	for _, relative := range preview.Missing {
		if _, exists := managed[relative]; exists {
			result.Recovery.Missing = append(result.Recovery.Missing, relative)
		}
	}
	sort.Strings(result.Recovery.Missing)
	if len(result.Recovery.Missing) == 0 {
		result.Recovery.Guidance = "Se conserva la instantánea verificada; no hay archivos administrados ausentes aptos para la recuperación automática por fusión."
		return nil
	}
	restored, err := prepared.engine.Restore(recoveryCtx, opencodebackup.RestoreRequest{
		SnapshotID: snapshot.Manifest.SnapshotID, PreviewSHA256: preview.SHA256,
		IncludePaths: append([]string(nil), result.Recovery.Missing...),
	})
	result.Recovery.Result = restored
	if err != nil {
		result.Recovery.Guidance = "Se conserva la instantánea verificada; inspecciona las rutas administradas sin resolver antes de reintentar."
		return err
	}
	result.Recovery.Guidance = "Los archivos administrados ausentes se restauraron por fusión desde la instantánea verificada conservada; no se sobrescribieron los conflictos existentes."
	return nil
}

func (service *Service) prepareRecovery(ctx context.Context, options Options, backupOverride string, requireHandshake bool) (recoveryPreflight, error) {
	prepared := recoveryPreflight{plan: ProtectedReinstallPlan{Mode: opencodebackup.ModeManaged}}
	if service == nil || service.installer == nil || service.managedIntegrations == nil || service.backups == nil || service.prober == nil || options.Workspace == "" {
		return prepared, ErrInvalid
	}
	launcherStatus, metadata, err := service.currentLauncher(ctx, options)
	prepared.plan.Launcher = launcherStatus
	if err != nil {
		prepared.plan.Blocker = "El launcher administrado no está instalado y verificado."
		return prepared, nil
	}
	managed, err := service.managedIntegrations(launcherStatus.LauncherPath)
	if err != nil || managed == nil {
		prepared.plan.Blocker = "La integración administrada de OpenCode no está disponible."
		return prepared, nil
	}
	prepared.runtime = managed
	if requireHandshake {
		pending, pendingErr := managed.ReinstallPending(ctx, options.Integration)
		if pendingErr != nil {
			prepared.plan.Blocker = "El marcador de reinstalación interrumpida de OpenCode no es válido; se conservaron las evidencias para su inspección."
			return prepared, nil
		}
		if pending {
			prepared.plan.RecoveryPending = true
			prepared.plan.Blocker = "Se detectó una reinstalación interrumpida de OpenCode; se conservaron las evidencias y se bloqueó la modificación automática."
			return prepared, nil
		}
	}
	status, err := managed.Status(ctx, options.Integration)
	prepared.plan.Integration = status
	if err != nil {
		return prepared, err
	}
	layout, err := managed.ManagedLayout(ctx, options.Integration)
	prepared.plan.Layout = layout
	if err != nil {
		return prepared, err
	}
	if err := validateManagedLayout(layout); err != nil {
		prepared.plan.Blocker = "El inventario de artefactos administrados de OpenCode no es válido."
		return prepared, nil
	}
	prepared.plan.SourceRoot = layout.Root
	prepared.plan.ManagedArtifactCount = len(layout.Artifacts)
	if requireHandshake {
		if status.State == integration.StateDrifted {
			prepared.plan.Blocker = "Los artefactos administrados de OpenCode presentan drift."
			return prepared, nil
		}
		if status.State != integration.StateInstalled {
			prepared.plan.Blocker = "La integración administrada de OpenCode está incompleta."
			return prepared, nil
		}
		handshake, handshakeErr := service.prober.Probe(ctx, options.Workspace)
		prepared.plan.Handshake = handshake
		if handshakeErr != nil || !handshake.OK {
			prepared.plan.Blocker = "La conexión con OpenCode no está saludable."
			return prepared, nil
		}
	}
	backupRoot, err := resolveBackupRoot(options, backupOverride)
	if err != nil || pathsContainEachOther(layout.Root, backupRoot) {
		prepared.plan.Blocker = "El destino de respaldo no es válido."
		return prepared, nil
	}
	prepared.plan.BackupRoot = backupRoot
	managedPaths := make([]string, len(layout.Artifacts))
	for index, artifact := range layout.Artifacts {
		managedPaths[index] = artifact.RelativePath
	}
	engine, err := service.backups(opencodebackup.Options{
		SourceRoot: layout.Root, BackupRoot: backupRoot, ManagedPaths: managedPaths, Launcher: metadata,
	})
	if err != nil || engine == nil {
		prepared.plan.Blocker = "El destino de respaldo no es válido."
		return prepared, nil
	}
	prepared.engine = engine
	prepared.launcherMetadata = metadata
	prepared.plan.Ready = true
	return prepared, nil
}

func (service *Service) currentLauncher(ctx context.Context, options Options) (selfinstall.Result, *opencodebackup.LauncherMetadata, error) {
	status, err := service.installer.Status(ctx, options.SelfInstall)
	if err != nil || status.State != selfinstall.StateInstalled || !filepath.IsAbs(status.LauncherPath) || !filepath.IsAbs(status.ManifestPath) || filepath.Clean(status.ManifestPath) != launcher.SidecarPath(filepath.Clean(status.LauncherPath)) {
		return status, nil, ErrPrerequisite
	}
	manifest, err := launcher.Load(filepath.Clean(status.LauncherPath))
	if err != nil || manifest.ActiveSHA256 != status.ActiveSHA256 {
		return status, nil, ErrPrerequisite
	}
	launcherSHA, launcherErr := launcher.FileSHA256(manifest.LauncherPath)
	activeSHA, activeErr := launcher.FileSHA256(manifest.ActivePath)
	if launcherErr != nil || activeErr != nil || launcherSHA != manifest.LauncherSHA256 || activeSHA != manifest.ActiveSHA256 {
		return status, nil, ErrPrerequisite
	}
	metadata := &opencodebackup.LauncherMetadata{
		Version: launcher.SchemaVersion, ManagedBy: launcher.ManagedBy, SHA256: manifest.ActiveSHA256,
		LauncherPath: manifest.LauncherPath, ManifestPath: status.ManifestPath,
		LauncherSHA256: manifest.LauncherSHA256, ActiveSHA256: manifest.ActiveSHA256,
	}
	return status, metadata, nil
}

func validateManagedLayout(layout integration.ManagedLayout) error {
	if !filepath.IsAbs(layout.Root) || filepath.Clean(layout.Root) != layout.Root || len(layout.Artifacts) == 0 || !validSHA256(layout.AggregateSHA256) {
		return ErrInvalid
	}
	hash := sha256.New()
	previous := ""
	for _, artifact := range layout.Artifacts {
		if artifact.RelativePath == "" || !fs.ValidPath(artifact.RelativePath) || path.Clean(artifact.RelativePath) != artifact.RelativePath || strings.ContainsAny(artifact.RelativePath, "\\:\x00") || artifact.RelativePath <= previous || !validSHA256(artifact.SHA256) {
			return ErrInvalid
		}
		_, _ = io.WriteString(hash, artifact.RelativePath)
		_, _ = hash.Write([]byte{0})
		_, _ = io.WriteString(hash, artifact.SHA256)
		_, _ = hash.Write([]byte{'\n'})
		previous = artifact.RelativePath
	}
	if hex.EncodeToString(hash.Sum(nil)) != layout.AggregateSHA256 {
		return ErrInvalid
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}

func resolveBackupRoot(options Options, override string) (string, error) {
	value := override
	if value == "" {
		home := options.Integration.HomeDir
		if home == "" {
			home = options.SelfInstall.HomeDir
		}
		if home == "" {
			var err error
			home, err = os.UserHomeDir()
			if err != nil {
				return "", err
			}
		}
		value = filepath.Join(home, ".local", "share", "vgxness", "backups", "opencode")
	}
	if strings.TrimSpace(value) == "" {
		return "", ErrInvalid
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}

func pathsContainEachOther(left, right string) bool {
	return containsSetupPath(left, right) || containsSetupPath(right, left)
}

func containsSetupPath(parent, child string) bool {
	relative, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	return err == nil && (relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

type managedProtection struct {
	options  integration.Options
	home     string
	recovery *codex.Recovery
}

func newManagedProtection(provider Provider, options integration.Options, home string) ManagedProtection {
	if provider != ProviderCodex {
		return nil
	}
	return &managedProtection{options: options, home: home}
}

func (p *managedProtection) Protect(ctx context.Context) (ManagedSnapshot, error) {
	if p.recovery == nil {
		recovery, err := codex.NewRecovery(ctx, codex.RecoveryOptions{Integration: p.options, HomeDir: p.home})
		if err != nil {
			return ManagedSnapshot{}, err
		}
		p.recovery = recovery
	}
	found, err := p.recovery.HasManagedFiles(ctx)
	if err != nil || !found {
		if err != nil {
			return ManagedSnapshot{}, err
		}
		return ManagedSnapshot{Skipped: true, Source: p.recovery.Source()}, nil
	}
	snapshot, err := p.recovery.Create(ctx, opencodebackup.ModeManaged)
	if err != nil {
		return ManagedSnapshot{}, err
	}
	verified, err := p.recovery.Verify(ctx, snapshot.ID)
	if err != nil || verified.ID != snapshot.ID {
		return ManagedSnapshot{}, fmt.Errorf("%w: codex snapshot verification", ErrVerification)
	}
	if verified.Source() == nil {
		return ManagedSnapshot{}, fmt.Errorf("%w: codex snapshot source", ErrVerification)
	}
	return ManagedSnapshot{ID: snapshot.ID, Verified: true, Source: verified.Source()}, nil
}

func sameLauncher(left, right *opencodebackup.LauncherMetadata) bool {
	return left != nil && right != nil && *left == *right
}

func normalizedRecoveryMode(mode opencodebackup.Mode) opencodebackup.Mode {
	if mode == "" {
		return opencodebackup.ModeManaged
	}
	return mode
}
