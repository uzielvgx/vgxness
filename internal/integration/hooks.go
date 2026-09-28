package integration

import (
	"context"

	"github.com/vgxness/vgxness/internal/hooks"
)

// Observe adds best-effort lifecycle observation without changing runtime results.
// Every optional capability of runtime remains reachable through the wrapper.
func Observe(runtime Runtime, emitter hooks.Emitter) Runtime {
	if emitter == nil || runtime == nil {
		return runtime
	}
	repair, repairable := runtime.(MCPRepairRuntime)
	if managed, ok := runtime.(ManagedRuntime); ok {
		if protected, ok := managed.(ProtectedRuntime); ok {
			observed := observedProtected{ProtectedRuntime: protected, emitter: emitter}
			if repairable {
				return observedProtectedRepair{observed, mcpRepairForwarder{repair}}
			}
			return observed
		}
		observed := observedManaged{ManagedRuntime: managed, emitter: emitter}
		if repairable {
			return observedManagedRepair{observed, mcpRepairForwarder{repair}}
		}
		return observed
	}
	observed := observedRuntime{Runtime: runtime, emitter: emitter}
	if repairable {
		return observedRuntimeRepair{observed, mcpRepairForwarder{repair}}
	}
	return observed
}

// mcpRepairForwarder keeps the explicit MCP repair route reachable when a
// runtime is wrapped for observation. Repair emits no lifecycle hook.
type mcpRepairForwarder struct{ repair MCPRepairRuntime }

func (f mcpRepairForwarder) PreviewMCPRepair(ctx context.Context, o Options, proof MCPRepairProof) (Result, error) {
	return f.repair.PreviewMCPRepair(ctx, o, proof)
}
func (f mcpRepairForwarder) RepairMCP(ctx context.Context, o Options, proof MCPRepairProof) (Result, error) {
	return f.repair.RepairMCP(ctx, o, proof)
}

type observedProtectedRepair struct {
	observedProtected
	mcpRepairForwarder
}

type observedManagedRepair struct {
	observedManaged
	mcpRepairForwarder
}

type observedRuntimeRepair struct {
	observedRuntime
	mcpRepairForwarder
}

type observedProtected struct {
	ProtectedRuntime
	emitter hooks.Emitter
}

func (r observedProtected) Preview(ctx context.Context, o Options) (Result, error) {
	return observedManaged{r.ProtectedRuntime, r.emitter}.Preview(ctx, o)
}
func (r observedProtected) Install(ctx context.Context, o Options) (Result, error) {
	return observedManaged{r.ProtectedRuntime, r.emitter}.Install(ctx, o)
}
func (r observedProtected) Status(ctx context.Context, o Options) (Result, error) {
	return observedManaged{r.ProtectedRuntime, r.emitter}.Status(ctx, o)
}
func (r observedProtected) Uninstall(ctx context.Context, o Options) (Result, error) {
	return observedManaged{r.ProtectedRuntime, r.emitter}.Uninstall(ctx, o)
}
func (r observedProtected) InstallProtected(ctx context.Context, o Options, source SourceIdentity) (Result, error) {
	result, err := r.ProtectedRuntime.InstallProtected(ctx, o, source)
	emitIntegration(ctx, r.emitter, hooks.NewIntegrationInstallCompleted, result, err)
	return result, err
}
func (r observedProtected) ReinstallProtected(ctx context.Context, o Options, source SourceIdentity) (Result, error) {
	result, err := r.ProtectedRuntime.ReinstallProtected(ctx, o, source)
	emitIntegration(ctx, r.emitter, hooks.NewIntegrationInstallCompleted, result, err)
	return result, err
}

type observedRuntime struct {
	Runtime
	emitter hooks.Emitter
}

func (r observedRuntime) Preview(ctx context.Context, options Options) (Result, error) {
	result, err := r.Runtime.Preview(ctx, options)
	emitIntegration(ctx, r.emitter, hooks.NewIntegrationPreviewCompleted, result, err)
	return result, err
}
func (r observedRuntime) Install(ctx context.Context, options Options) (Result, error) {
	result, err := r.Runtime.Install(ctx, options)
	emitIntegration(ctx, r.emitter, hooks.NewIntegrationInstallCompleted, result, err)
	return result, err
}
func (r observedRuntime) Status(ctx context.Context, options Options) (Result, error) {
	result, err := r.Runtime.Status(ctx, options)
	emitIntegration(ctx, r.emitter, hooks.NewIntegrationStatusCompleted, result, err)
	return result, err
}
func (r observedRuntime) Uninstall(ctx context.Context, options Options) (Result, error) {
	result, err := r.Runtime.Uninstall(ctx, options)
	emitIntegration(ctx, r.emitter, hooks.NewIntegrationUninstallCompleted, result, err)
	return result, err
}

type observedManaged struct {
	ManagedRuntime
	emitter hooks.Emitter
}

func (r observedManaged) Preview(ctx context.Context, o Options) (Result, error) {
	result, err := r.ManagedRuntime.Preview(ctx, o)
	emitIntegration(ctx, r.emitter, hooks.NewIntegrationPreviewCompleted, result, err)
	return result, err
}
func (r observedManaged) Install(ctx context.Context, o Options) (Result, error) {
	result, err := r.ManagedRuntime.Install(ctx, o)
	emitIntegration(ctx, r.emitter, hooks.NewIntegrationInstallCompleted, result, err)
	return result, err
}
func (r observedManaged) Status(ctx context.Context, o Options) (Result, error) {
	result, err := r.ManagedRuntime.Status(ctx, o)
	emitIntegration(ctx, r.emitter, hooks.NewIntegrationStatusCompleted, result, err)
	return result, err
}
func (r observedManaged) Uninstall(ctx context.Context, o Options) (Result, error) {
	result, err := r.ManagedRuntime.Uninstall(ctx, o)
	emitIntegration(ctx, r.emitter, hooks.NewIntegrationUninstallCompleted, result, err)
	return result, err
}
func emitIntegration(ctx context.Context, emitter hooks.Emitter, build func(string, string, bool, string, int64, bool) (hooks.Draft, error), result Result, err error) {
	if err != nil {
		return
	}
	defer func() { recover() }()
	draft, err := build(result.Provider, string(result.State), result.Changed, result.ArtifactSHA256, int64(result.ArtifactCount), result.RestartRequired)
	if err == nil {
		emitter.Emit(ctx, draft)
	}
}
