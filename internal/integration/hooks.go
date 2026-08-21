package integration

import (
	"context"

	"github.com/vgxness/vgxness/internal/hooks"
)

// Observe adds best-effort lifecycle observation without changing runtime results.
func Observe(runtime Runtime, emitter hooks.Emitter) Runtime {
	if emitter == nil || runtime == nil {
		return runtime
	}
	if managed, ok := runtime.(ManagedRuntime); ok {
		if protected, ok := managed.(ProtectedRuntime); ok {
			return observedProtected{ProtectedRuntime: protected, emitter: emitter}
		}
		return observedManaged{ManagedRuntime: managed, emitter: emitter}
	}
	return observedRuntime{Runtime: runtime, emitter: emitter}
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
