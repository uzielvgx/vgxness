package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	appruntime "github.com/vgxness/vgxness/internal/app/runtime"
	"github.com/vgxness/vgxness/internal/cli"
	"github.com/vgxness/vgxness/internal/config"
	"github.com/vgxness/vgxness/internal/hooks"
	"github.com/vgxness/vgxness/internal/inspection"
	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/memory"
	"github.com/vgxness/vgxness/internal/modelcatalog"
	"github.com/vgxness/vgxness/internal/opencodebackup"
	"github.com/vgxness/vgxness/internal/providers/codex"
	"github.com/vgxness/vgxness/internal/providers/opencode"
	"github.com/vgxness/vgxness/internal/sdd"
	"github.com/vgxness/vgxness/internal/selfinstall"
	setupflow "github.com/vgxness/vgxness/internal/setup"
	"github.com/vgxness/vgxness/internal/skills"
	"github.com/vgxness/vgxness/internal/tui"
)

var _ [integration.ModelAssignmentCount - tui.SetupModelAssignmentCount]struct{}
var _ [tui.SetupModelAssignmentCount - integration.ModelAssignmentCount]struct{}

func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runWithMCP(ctx, args, stdin, stdout, stderr, tui.Run, cli.RunMCP)
}

type tuiLauncher func(context.Context, io.Reader, io.Writer, io.Writer, tui.Backend, tui.Options) int
type mcpLauncher func(context.Context, []string, io.Reader, io.Writer, io.Writer, string) int

type appRuntimes struct {
	opencode   integration.Runtime
	codex      integration.Runtime
	dispatcher *hooks.Dispatcher
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, launchTUI tuiLauncher) int {
	return runWithMCP(ctx, args, stdin, stdout, stderr, launchTUI, cli.RunMCP)
}

func runWithMCP(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, launchTUI tuiLauncher, launchMCP mcpLauncher) int {
	return runWithMCPAndRuntimes(ctx, args, stdin, stdout, stderr, launchTUI, launchMCP, appRuntimes{opencode: opencode.NewIntegration(), codex: codex.NewIntegration()})
}

func runWithMCPAndRuntimes(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, launchTUI tuiLauncher, launchMCP mcpLauncher, runtimes appRuntimes) int {
	dispatcher := runtimes.dispatcher
	if dispatcher == nil {
		dispatcher = hooks.New()
	}
	defer dispatcher.Close()
	if len(args) > 0 && args[0] == "version" {
		return cli.RunVersion(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "skills" {
		return cli.RunSkills(ctx, args[1:], stdout, stderr, skills.New())
	}
	if len(args) > 0 && args[0] == "mcp" {
		if launchMCP == nil {
			fmt.Fprintln(stderr, "operational: MCP launcher is unavailable")
			return 1
		}
		return launchMCP(ctx, args[1:], stdin, stdout, stderr, mustWorkspace())
	}
	if len(args) > 0 && args[0] == "tui" && len(args) != 1 {
		fmt.Fprintln(stderr, "usage: vgxness tui")
		return 2
	}
	installer := selfinstall.New(selfinstall.Config{})
	integrationRuntime := runtimes.opencode
	if integrationRuntime == nil {
		integrationRuntime = opencode.NewIntegration()
	}
	integrationRuntime = integration.Observe(integrationRuntime, dispatcher)
	codexIntegrationRuntime := runtimes.codex
	if codexIntegrationRuntime == nil {
		codexIntegrationRuntime = codex.NewIntegration()
	}
	codexIntegrationRuntime = integration.Observe(codexIntegrationRuntime, dispatcher)
	cliMemory := appruntime.NewMemoryWithHooks("cli", false, dispatcher)
	setupRuntime := setupflow.NewWithRecovery(
		installer,
		integrationRuntime,
		func(executable string) (integration.ManagedRuntime, error) {
			runtime, err := opencode.NewManagedIntegration(executable)
			if err != nil {
				return nil, err
			}
			managed, ok := integration.Observe(runtime, dispatcher).(integration.ManagedRuntime)
			if !ok {
				return nil, errors.New("operational: managed integration observation unavailable")
			}
			return managed, nil
		},
		func(options opencodebackup.Options) (setupflow.BackupEngine, error) {
			return opencodebackup.New(options)
		},
		opencode.NewProber(""),
	)
	workspace := mustWorkspace()
	if len(args) == 1 && args[0] == "tui" {
		if launchTUI == nil {
			fmt.Fprintln(stderr, "operational: TUI launcher is unavailable")
			return 1
		}
		backend := tuiBackend{
			inspection: inspection.Service{Health: memory.HealthFile},
			setup:      setupRuntime,
			opencode:   integrationRuntime,
			codex:      codexIntegrationRuntime,
			recovery:   setupRuntime,
			memory:     appruntime.NewMemoryWithHooks("cli", true, dispatcher),
			catalog:    modelcatalog.NewOpenCode("", nil, modelcatalog.Options{}),
			hooks:      dispatcher,
		}
		return launchTUI(ctx, stdin, stdout, stderr, backend, tui.Options{Workspace: workspace})
	}
	return cli.RunProductSDDRuntime(ctx, args, stdin, stdout, stderr, inspection.Service{Health: memory.HealthFile}, cliMemory, integrationRuntime, codexIntegrationRuntime, installer, setupRuntime, appruntime.NewSDDWithHooks(dispatcher))
}

func mustWorkspace() string {
	workspace, err := os.Getwd()
	if err != nil {
		return "."
	}
	return workspace
}

type tuiInspectionRuntime interface {
	Status(context.Context, config.Options) (inspection.Result, error)
}

type tuiSetupRuntime interface {
	Plan(context.Context, setupflow.Options) (setupflow.Plan, error)
	Apply(context.Context, setupflow.Options) (setupflow.Result, error)
	Status(context.Context, setupflow.Options) (setupflow.Plan, error)
}

type tuiSharedSetupRuntime interface {
	Shared(setupflow.Options) setupflow.SharedRuntime
}

type tuiOpenCodeProviderRuntime interface {
	OpenCodeProvider(setupflow.Options, setupflow.PreviewIntegrationFactory) setupflow.ProviderRuntime
}

type tuiRecoveryRuntime interface {
	PlanProtectedReinstall(context.Context, setupflow.ProtectedReinstallRequest) (setupflow.ProtectedReinstallPlan, error)
	ListBackups(context.Context, setupflow.BackupListRequest) (setupflow.BackupListResult, error)
	CreateBackup(context.Context, setupflow.BackupRequest) (setupflow.BackupResult, error)
	PreviewRestore(context.Context, setupflow.RestorePreviewRequest) (setupflow.RestorePreviewResult, error)
	Restore(context.Context, setupflow.RestoreRequest) (setupflow.RestoreResult, error)
	ProtectedReinstall(context.Context, setupflow.ProtectedReinstallRequest) (setupflow.ProtectedReinstallResult, error)
}

type tuiMemoryRuntime interface {
	ResolveProject(context.Context, config.Options, string) (string, error)
	Recall(context.Context, config.Options, memory.Recall) ([]memory.Entry, error)
	Recent(context.Context, config.Options, memory.Recent) ([]memory.Entry, error)
	Get(context.Context, config.Options, memory.Lookup) (memory.Entry, error)
}

type tuiModelCatalog interface {
	Discover(context.Context) (modelcatalog.Snapshot, error)
	Refresh(context.Context) (modelcatalog.Snapshot, error)
}

type tuiBackend struct {
	inspection tuiInspectionRuntime
	setup      tuiSetupRuntime
	opencode   integration.Runtime
	codex      integration.Runtime
	recovery   tuiRecoveryRuntime
	memory     tuiMemoryRuntime
	catalog    tuiModelCatalog
	hooks      hooks.Emitter
}

func (backend tuiBackend) PlanMultiSetup(ctx context.Context, request tui.MultiSetupRequest) (setupflow.MultiPlan, error) {
	multi, options, err := backend.multiSetup(request)
	if err != nil {
		return setupflow.MultiPlan{}, err
	}
	return multi.Plan(ctx, options)
}

func (backend tuiBackend) ApplyMultiSetup(ctx context.Context, request tui.MultiSetupRequest) (setupflow.MultiResult, error) {
	multi, options, err := backend.multiSetup(request)
	if err != nil {
		return setupflow.MultiResult{}, err
	}
	return multi.Apply(ctx, options)
}

func (backend tuiBackend) multiSetup(request tui.MultiSetupRequest) (*setupflow.Multi, setupflow.MultiOptions, error) {
	shared, ok := backend.setup.(tuiSharedSetupRuntime)
	if !ok {
		return nil, setupflow.MultiOptions{}, fmt.Errorf("multi-provider setup unavailable")
	}
	openCode, ok := backend.setup.(tuiOpenCodeProviderRuntime)
	if !ok {
		return nil, setupflow.MultiOptions{}, fmt.Errorf("multi-provider OpenCode setup unavailable")
	}
	for _, provider := range request.Providers {
		if provider == setupflow.ProviderCodex && backend.codex == nil {
			return nil, setupflow.MultiOptions{}, fmt.Errorf("selected provider is unavailable")
		}
	}
	options, err := tuiSetupOptions(request.Setup)
	if err != nil {
		return nil, setupflow.MultiOptions{}, err
	}
	runtimes := make([]setupflow.ProviderRuntime, 0, len(request.Providers))
	for _, provider := range request.Providers {
		if provider == setupflow.ProviderOpenCode {
			runtimes = append(runtimes, openCode.OpenCodeProvider(options, func(path string) (integration.Runtime, error) {
				runtime, err := opencode.NewPreviewIntegration(path)
				if err != nil {
					return nil, err
				}
				return integration.Observe(runtime, backend.hooks), nil
			}))
			continue
		}
		codexOptions, err := codexSetupOptions(options.Integration)
		if err != nil {
			return nil, setupflow.MultiOptions{}, err
		}
		runtimes = append(runtimes, setupflow.NewIntegrationProvider(provider, backend.codex, codexOptions))
	}
	multi := setupflow.NewMultiWithShared(shared.Shared(options), runtimes...)
	return multi, setupflow.MultiOptions{Providers: request.Providers, ExpectedPlanDigest: request.ExpectedPlanDigest, Verified: request.Verified}, nil
}

func codexSetupOptions(options integration.Options) (integration.Options, error) {
	if options.ConfigDir != "" {
		return integration.Options{ConfigDir: options.ConfigDir, ModelPlan: options.ModelPlan}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return integration.Options{}, fmt.Errorf("resolve Codex home directory")
	}
	return integration.Options{HomeDir: home, ModelPlan: options.ModelPlan}, nil
}

func (backend tuiBackend) ModelCatalog(ctx context.Context, refresh bool) ([]tui.SetupCatalogModel, error) {
	if backend.catalog == nil {
		return nil, fmt.Errorf("model catalog unavailable")
	}
	var snapshot modelcatalog.Snapshot
	var err error
	if refresh {
		snapshot, err = backend.catalog.Refresh(ctx)
	} else {
		snapshot, err = backend.catalog.Discover(ctx)
	}
	if err != nil {
		return nil, err
	}
	rows := make([]tui.SetupCatalogModel, len(snapshot.Models))
	for index, reference := range snapshot.Models {
		provider, _, _ := strings.Cut(reference, "/")
		rows[index] = tui.SetupCatalogModel{Provider: provider, Reference: reference, Variants: append([]string{}, snapshot.Variants[reference]...), Source: "custom", Availability: "unknown"}
	}
	return rows, nil
}

func (backend tuiBackend) Inspect(ctx context.Context, request tui.Request) (tui.Inspection, error) {
	result, err := backend.inspection.Status(ctx, config.Options{ProjectDir: request.Workspace})
	if err != nil {
		return tui.Inspection{}, err
	}
	return tui.Inspection{Root: result.Root, Database: result.Database, Migration: result.Migration}, nil
}

func (backend tuiBackend) SetupStatus(ctx context.Context, request tui.Request) (tui.SetupStatus, error) {
	workspace, err := cleanWorkspace(request.Workspace)
	if err != nil {
		return tui.SetupStatus{}, err
	}
	plan, err := backend.setup.Status(ctx, setupflow.Options{Workspace: workspace})
	if err != nil {
		return tui.SetupStatus{}, err
	}
	preview := tuiSetupPlan(plan)
	return tui.SetupStatus{
		Provider: preview.Provider, Ready: preview.Ready, Blocker: preview.Blocker,
		SelfInstallState: preview.SelfInstallState, SelfInstallPath: preview.SelfInstallPath,
		IntegrationState: preview.IntegrationState, IntegrationPath: preview.IntegrationPath,
		SkillsState: preview.SkillsState, SkillsPath: preview.SkillsPath, SkillsFileCount: preview.SkillsFileCount,
		ArtifactCount: preview.ArtifactCount,
		HandshakeOK:   preview.HandshakeOK, HandshakeStatus: preview.HandshakeStatus, ModelPlan: preview.ModelPlan,
		ModelSchemaVersion: preview.ModelSchemaVersion, ModelAssignments: preview.ModelAssignments,
	}, nil
}

func (backend tuiBackend) PlanSetup(ctx context.Context, request tui.SetupRequest) (tui.SetupPlan, error) {
	options, err := tuiSetupOptions(request)
	if err != nil {
		return tui.SetupPlan{}, err
	}
	plan, err := backend.setup.Plan(ctx, options)
	return tuiSetupPlan(plan), err
}

func (backend tuiBackend) ApplySetup(ctx context.Context, request tui.SetupRequest) (tui.SetupResult, error) {
	options, err := tuiSetupOptions(request)
	if err != nil {
		return tui.SetupResult{}, err
	}
	result, err := backend.setup.Apply(ctx, options)
	return tui.SetupResult{
		Plan:             tuiSetupPlan(result.Plan),
		SelfInstallState: fmt.Sprint(result.SelfInstall.State), SelfInstallPath: result.SelfInstall.LauncherPath,
		SelfInstallUpdateAvailable: result.SelfInstall.UpdateAvailable, SelfInstallRollbackAvailable: result.SelfInstall.RollbackAvailable,
		SelfInstallActiveSHA256: result.SelfInstall.ActiveSHA256, SelfInstallPreviousSHA256: result.SelfInstall.PreviousSHA256,
		IntegrationState: fmt.Sprint(result.Integration.State), IntegrationPath: result.Integration.Path,
		SkillsState: fmt.Sprint(result.Plan.Skills.State), SkillsPath: result.Plan.Skills.Path, SkillsFileCount: result.Plan.Skills.FileCount,
		ArtifactCount: result.Integration.ArtifactCount,
		HandshakeOK:   result.Handshake.OK, HandshakeStatus: result.Handshake.Status.String(),
		Recovery: result.Recovery, Changed: result.Changed, RestartRequired: result.Integration.RestartRequired,
	}, err
}

func (backend tuiBackend) PlanRecovery(ctx context.Context, request tui.RecoveryPlanRequest) (tui.RecoveryPlan, error) {
	options, backupRoot, mode, err := tuiRecoveryOptions(request.Workspace, request.BackupRoot, request.Mode)
	if err != nil || backend.recovery == nil {
		return tui.RecoveryPlan{}, fmt.Errorf("invalid recovery request")
	}
	plan, domainErr := backend.recovery.PlanProtectedReinstall(ctx, setupflow.ProtectedReinstallRequest{Options: options, BackupRoot: backupRoot, Mode: mode})
	result := tui.RecoveryPlan{
		Mode: string(plan.Mode), SourceRoot: plan.SourceRoot, BackupRoot: plan.BackupRoot,
		ArtifactCount: plan.ManagedArtifactCount, LauncherState: fmt.Sprint(plan.Launcher.State),
		IntegrationState: fmt.Sprint(plan.Integration.State), HandshakeOK: plan.Handshake.OK,
		HandshakeStatus: plan.Handshake.Status.String(), Ready: plan.Ready, Blocker: plan.Blocker,
	}
	return result, safeRecoveryError(ctx, "recovery plan", domainErr)
}

func (backend tuiBackend) ListBackups(ctx context.Context, request tui.BackupListRequest) (tui.BackupListResult, error) {
	options, backupRoot, err := tuiRecoveryBase(request.Workspace, request.BackupRoot)
	if err != nil || backend.recovery == nil {
		return tui.BackupListResult{}, fmt.Errorf("invalid backup list request")
	}
	listed, domainErr := backend.recovery.ListBackups(ctx, setupflow.BackupListRequest{Options: options, BackupRoot: backupRoot})
	result := tui.BackupListResult{Snapshots: make([]tui.BackupSummary, len(listed.Backups))}
	for index, summary := range listed.Backups {
		result.Snapshots[index] = tuiBackupSummary(summary)
	}
	return result, safeRecoveryError(ctx, "backup list", domainErr)
}

func (backend tuiBackend) CreateBackup(ctx context.Context, request tui.CreateBackupRequest) (tui.BackupResult, error) {
	options, backupRoot, mode, err := tuiRecoveryOptions(request.Workspace, request.BackupRoot, request.Mode)
	if err != nil || backend.recovery == nil {
		return tui.BackupResult{}, fmt.Errorf("invalid backup request")
	}
	created, domainErr := backend.recovery.CreateBackup(ctx, setupflow.BackupRequest{Options: options, BackupRoot: backupRoot, Mode: mode})
	summary := created.Summary
	if summary.SnapshotID == "" {
		summary = created.Snapshot.Summary
	}
	return tui.BackupResult{Mode: string(created.Mode), Snapshot: tuiBackupSummary(summary)}, safeRecoveryError(ctx, "backup creation", domainErr)
}

func (backend tuiBackend) PreviewRestore(ctx context.Context, request tui.RestorePreviewRequest) (tui.RestorePreview, error) {
	options, backupRoot, err := tuiRecoveryBase(request.Workspace, request.BackupRoot)
	if err != nil || opencodebackup.ValidateSnapshotID(request.SnapshotID) != nil || backend.recovery == nil {
		return tui.RestorePreview{}, fmt.Errorf("invalid restore preview request")
	}
	previewed, domainErr := backend.recovery.PreviewRestore(ctx, setupflow.RestorePreviewRequest{Options: options, BackupRoot: backupRoot, SnapshotID: request.SnapshotID})
	preview := previewed.Preview
	return tui.RestorePreview{
		SnapshotID: preview.SnapshotID, PreviewSHA256: preview.SHA256,
		Missing: append([]string(nil), preview.Missing...), Identical: append([]string(nil), preview.Identical...), Conflicts: append([]string(nil), preview.Conflicts...),
	}, safeRecoveryError(ctx, "restore preview", domainErr)
}

func (backend tuiBackend) RestoreBackup(ctx context.Context, request tui.RestoreRequest) (tui.RestoreResult, error) {
	options, backupRoot, err := tuiRecoveryBase(request.Workspace, request.BackupRoot)
	if err != nil || opencodebackup.ValidateSnapshotID(request.SnapshotID) != nil || opencodebackup.ValidatePreviewSHA256(request.PreviewSHA256) != nil || backend.recovery == nil {
		return tui.RestoreResult{}, fmt.Errorf("invalid restore request")
	}
	restored, domainErr := backend.recovery.Restore(ctx, setupflow.RestoreRequest{
		Options: options, BackupRoot: backupRoot, SnapshotID: request.SnapshotID,
		PreviewSHA256: request.PreviewSHA256,
	})
	value := restored.Result
	return tui.RestoreResult{
		SnapshotID: value.SnapshotID, Created: value.Created, Identical: value.Identical,
		Replaced: value.Replaced, Unresolved: append([]string(nil), value.Unresolved...),
	}, safeRecoveryError(ctx, "restore", domainErr)
}

func (backend tuiBackend) ProtectedReinstall(ctx context.Context, request tui.ProtectedReinstallRequest) (tui.ProtectedReinstallResult, error) {
	options, backupRoot, mode, err := tuiRecoveryOptions(request.Workspace, request.BackupRoot, request.Mode)
	if err != nil || backend.recovery == nil {
		return tui.ProtectedReinstallResult{}, fmt.Errorf("invalid protected reinstall request")
	}
	reinstalled, domainErr := backend.recovery.ProtectedReinstall(ctx, setupflow.ProtectedReinstallRequest{Options: options, BackupRoot: backupRoot, Mode: mode})
	snapshotID := reinstalled.Snapshot.Summary.SnapshotID
	if snapshotID == "" {
		snapshotID = reinstalled.Snapshot.Manifest.SnapshotID
	}
	return tui.ProtectedReinstallResult{
		Mode: string(reinstalled.Mode), SnapshotID: snapshotID,
		SnapshotVerified: reinstalled.SnapshotVerified, LauncherState: fmt.Sprint(reinstalled.Launcher.State),
		IntegrationState: fmt.Sprint(reinstalled.Integration.State), HandshakeOK: reinstalled.Handshake.OK,
		HandshakeStatus: reinstalled.Handshake.Status.String(), RecoveryAttempted: reinstalled.Recovery.Attempted,
		RecoveryMissing: append([]string(nil), reinstalled.Recovery.Missing...), RecoveryGuidance: reinstalled.Recovery.Guidance,
	}, safeRecoveryError(ctx, "protected reinstall", domainErr)
}

func tuiRecoveryOptions(workspace, backupRoot, selectedMode string) (setupflow.Options, string, opencodebackup.Mode, error) {
	options, cleanBackupRoot, err := tuiRecoveryBase(workspace, backupRoot)
	if err != nil {
		return setupflow.Options{}, "", "", err
	}
	mode := opencodebackup.Mode(selectedMode)
	if err := mode.Validate(); err != nil {
		return setupflow.Options{}, "", "", err
	}
	return options, cleanBackupRoot, mode, nil
}

func tuiRecoveryBase(workspace, backupRoot string) (setupflow.Options, string, error) {
	cleanedWorkspace, err := cleanWorkspace(workspace)
	if err != nil || strings.TrimSpace(workspace) == "" {
		return setupflow.Options{}, "", fmt.Errorf("invalid workspace")
	}
	cleanedBackup := ""
	if backupRoot != "" {
		cleanedBackup, err = filepath.Abs(backupRoot)
		if err != nil || strings.IndexByte(backupRoot, 0) >= 0 {
			return setupflow.Options{}, "", fmt.Errorf("invalid backup root")
		}
		cleanedBackup = filepath.Clean(cleanedBackup)
	}
	return setupflow.Options{Workspace: cleanedWorkspace}, cleanedBackup, nil
}

func tuiBackupSummary(summary opencodebackup.Summary) tui.BackupSummary {
	createdAt := ""
	if !summary.CreatedAt.IsZero() {
		createdAt = summary.CreatedAt.UTC().Format(time.RFC3339)
	}
	return tui.BackupSummary{
		SnapshotID: summary.SnapshotID, CreatedAt: createdAt, Mode: string(summary.Mode),
		FileCount: summary.EntryCount, TotalBytes: summary.TotalBytes,
	}
}

func safeRecoveryError(ctx context.Context, operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || ctx.Err() != nil {
		return context.Canceled
	}
	return fmt.Errorf("%s unavailable", operation)
}

func tuiSetupOptions(request tui.SetupRequest) (setupflow.Options, error) {
	workspace, err := cleanWorkspace(request.Workspace)
	if err != nil {
		return setupflow.Options{}, err
	}
	integrationOptions := integration.Options{}
	if request.ModelAssignments != nil {
		assignments := make(map[string]sdd.ManagedAgentModelConfig, tui.SetupModelAssignmentCount)
		providerSummary := ""
		for _, row := range request.ModelAssignments {
			provider, validReference := modelcatalog.ValidReference(row.Reference)
			effort := sdd.Effort(row.RequestedEffort)
			source, availability := sdd.ModelSlotSource(row.Source), sdd.ModelSlotAvailability(row.Availability)
			validProvenance := source == sdd.ModelSlotCatalog && availability == sdd.ModelSlotCatalogKnown || source == sdd.ModelSlotCustom && availability == sdd.ModelSlotUnknown
			if row.ArtifactKey == "" || !validReference || row.Provider != provider || !effort.Valid() || !validProvenance {
				return setupflow.Options{}, fmt.Errorf("invalid TUI setup model assignments")
			}
			if _, duplicate := assignments[row.ArtifactKey]; duplicate {
				return setupflow.Options{}, fmt.Errorf("invalid TUI setup model assignments")
			}
			assignments[row.ArtifactKey] = sdd.ManagedAgentModelConfig{
				Provider: row.Provider, Reference: row.Reference, RequestedEffort: effort,
				Variant: sdd.OpenCodeVariant(row.Variant), VariantSpecified: row.VariantSpecified,
				Source: source, Availability: availability,
			}
			if providerSummary == "" {
				providerSummary = provider
			} else if providerSummary != provider {
				providerSummary = "mixed"
			}
		}
		if len(assignments) != integration.ModelAssignmentCount {
			return setupflow.Options{}, fmt.Errorf("invalid TUI setup model assignments")
		}
		if _, err := sdd.ResolveOpenCodePlanV3(sdd.ModelPlanConfigV3{SchemaVersion: 3, Provider: providerSummary, Assignments: assignments, Provenance: sdd.ModelPlanCLI}, opencode.ModelAgentInventoryV3()); err != nil {
			return setupflow.Options{}, fmt.Errorf("invalid TUI setup model assignments")
		}
		integrationOptions.ModelAssignments = &assignments
	} else {
		plan := sdd.Plan(request.Plan)
		if !plan.Valid() {
			return setupflow.Options{}, fmt.Errorf("invalid TUI setup plan")
		}
		integrationOptions = integration.Options{ModelPlan: plan,
			ModelEfficient: request.ModelEfficient, ModelBalanced: request.ModelBalanced, ModelFrontier: request.ModelFrontier,
			ModelEfficientEffort: sdd.Effort(request.ModelEfficientEffort), ModelBalancedEffort: sdd.Effort(request.ModelBalancedEffort), ModelFrontierEffort: sdd.Effort(request.ModelFrontierEffort),
			ModelEfficientVariant: sdd.OpenCodeVariant(request.ModelEfficientVariant), ModelBalancedVariant: sdd.OpenCodeVariant(request.ModelBalancedVariant), ModelFrontierVariant: sdd.OpenCodeVariant(request.ModelFrontierVariant), ModelVariantsSpecified: request.ModelVariantsSpecified,
		}
	}
	return setupflow.Options{Workspace: workspace, ExpectedPlanDigest: request.ExpectedPlanDigest, Integration: integrationOptions}, nil
}

func cleanWorkspace(workspace string) (string, error) {
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}

func tuiSetupPlan(plan setupflow.Plan) tui.SetupPlan {
	steps := make([]tui.SetupStep, len(plan.Steps))
	for index, step := range plan.Steps {
		steps[index] = tui.SetupStep{
			Number: step.Number, Title: step.Title, Explanation: step.Explanation, Mutates: step.Mutates,
		}
	}
	var modelAssignments *[tui.SetupModelAssignmentCount]tui.SetupModelAssignment
	if plan.Integration.ModelAssignments != nil {
		modelAssignments = new([tui.SetupModelAssignmentCount]tui.SetupModelAssignment)
		limit := min(len(plan.Integration.ModelAssignments), len(modelAssignments))
		for index := range limit {
			row := plan.Integration.ModelAssignments[index]
			modelAssignments[index] = tui.SetupModelAssignment{
				ArtifactKey: row.ArtifactKey, Role: string(row.Role), Class: string(row.Class), Provider: row.Provider, Model: row.Model,
				RequestedEffort: string(row.RequestedEffort), Effort: string(row.Effort), Variant: string(row.Variant), VariantSpecified: row.VariantSpecified,
				Degraded: row.Degradation.Degraded, DegradationReason: row.Degradation.Reason,
				Source: string(row.Source), Availability: string(row.Availability),
			}
		}
	}
	return tui.SetupPlan{
		Digest: plan.Digest, Provider: plan.Provider, Steps: steps,
		SelfInstallState: fmt.Sprint(plan.SelfInstall.State), SelfInstallPath: plan.SelfInstall.LauncherPath,
		SelfInstallUpdateAvailable: plan.SelfInstall.UpdateAvailable, SelfInstallRollbackAvailable: plan.SelfInstall.RollbackAvailable,
		SelfInstallActiveSHA256: plan.SelfInstall.ActiveSHA256, SelfInstallPreviousSHA256: plan.SelfInstall.PreviousSHA256,
		SelfInstallChanged: plan.SelfInstall.Changed,
		IntegrationState:   fmt.Sprint(plan.Integration.State), IntegrationPath: plan.Integration.Path,
		IntegrationChanged: plan.Integration.Changed, IntegrationRestartRequired: plan.Integration.RestartRequired,
		SkillsState: fmt.Sprint(plan.Skills.State), SkillsPath: plan.Skills.Path, SkillsFileCount: plan.Skills.FileCount,
		SkillsChanged: plan.Skills.Changed, SkillsUpdateNeeded: plan.Skills.UpdateNeeded,
		ArtifactCount:      plan.Integration.ArtifactCount,
		ModelSchemaVersion: plan.Integration.ModelSchemaVersion, ModelAssignments: modelAssignments,
		ModelPlan: fmt.Sprint(plan.Integration.ModelPlan), ModelProvider: plan.Integration.ModelProvider,
		ModelEfficient: plan.Integration.ModelEfficient, ModelBalanced: plan.Integration.ModelBalanced,
		ModelFrontier:        plan.Integration.ModelFrontier,
		ModelEfficientEffort: string(plan.Integration.ModelEfficientEffort), ModelBalancedEffort: string(plan.Integration.ModelBalancedEffort), ModelFrontierEffort: string(plan.Integration.ModelFrontierEffort),
		ModelEfficientVariant: string(plan.Integration.ModelEfficientVariant), ModelBalancedVariant: string(plan.Integration.ModelBalancedVariant), ModelFrontierVariant: string(plan.Integration.ModelFrontierVariant), ModelVariantsSpecified: plan.Integration.ModelVariantsSpecified,
		ModelEfficientSource: string(plan.Integration.ModelEfficientSource), ModelBalancedSource: string(plan.Integration.ModelBalancedSource), ModelFrontierSource: string(plan.Integration.ModelFrontierSource),
		ModelEfficientAvailability: string(plan.Integration.ModelEfficientAvailability), ModelBalancedAvailability: string(plan.Integration.ModelBalancedAvailability), ModelFrontierAvailability: string(plan.Integration.ModelFrontierAvailability),
		HandshakeOK: plan.Handshake.OK, HandshakeStatus: plan.Handshake.Status.String(),
		Ready: plan.Ready, Blocker: plan.Blocker,
	}
}

func (backend tuiBackend) Recent(ctx context.Context, request tui.Request) ([]tui.MemorySummary, error) {
	opts := config.Options{ProjectDir: request.Workspace}
	project, err := backend.memory.ResolveProject(ctx, opts, request.Workspace)
	if err != nil {
		return nil, err
	}
	entries, err := backend.memory.Recent(ctx, opts, memory.Recent{Project: project, Scope: memory.ScopeProject, Limit: 5})
	if err != nil {
		return nil, err
	}
	return tuiMemorySummaries(entries), nil
}

func (backend tuiBackend) Search(ctx context.Context, request tui.MemorySearch) ([]tui.MemorySummary, error) {
	opts := config.Options{ProjectDir: request.Workspace}
	project, err := backend.memory.ResolveProject(ctx, opts, request.Workspace)
	if err != nil {
		return nil, err
	}
	entries, err := backend.memory.Recall(ctx, opts, memory.Recall{
		Query: request.Query, Project: project, Scope: memory.ScopeProject,
		Limit: request.Limit, MatchAny: true,
	})
	if err != nil {
		return nil, err
	}
	return tuiMemorySummaries(entries), nil
}

func (backend tuiBackend) GetMemory(ctx context.Context, request tui.MemoryLookup) (tui.MemoryDetail, error) {
	opts := config.Options{ProjectDir: request.Workspace}
	project, err := backend.memory.ResolveProject(ctx, opts, request.Workspace)
	if err != nil {
		return tui.MemoryDetail{}, err
	}
	entry, err := backend.memory.Get(ctx, opts, memory.Lookup{ID: request.ID, Project: project, Scope: memory.ScopeProject})
	if err != nil {
		return tui.MemoryDetail{}, err
	}
	return tui.MemoryDetail{
		ID: entry.ID, Title: entry.Title, Content: entry.Content,
		Project: entry.Project, Scope: string(entry.Scope), Type: entry.Type,
		TopicKey: entry.TopicKey, Session: entry.Session, Producer: entry.Producer,
		SourceProvider: entry.SourceProvider, SourceID: entry.SourceID,
		State: string(entry.State), CreatedAt: entry.CreatedAt, UpdatedAt: entry.UpdatedAt,
		References: append([]string(nil), entry.References...),
	}, nil
}

func tuiMemorySummaries(entries []memory.Entry) []tui.MemorySummary {
	result := make([]tui.MemorySummary, len(entries))
	for index, entry := range entries {
		result[index] = tui.MemorySummary{
			ID: entry.ID, Title: entry.Title, Preview: entry.Preview, Type: entry.Type,
			State: string(entry.State), UpdatedAt: entry.UpdatedAt,
		}
	}
	return result
}
