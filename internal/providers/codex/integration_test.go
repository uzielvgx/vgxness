package codex

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/modelplan"
	"github.com/vgxness/vgxness/internal/testutil"
)

const (
	windowsErrorAccessDenied     syscall.Errno = 5
	windowsErrorSharingViolation syscall.Errno = 32
)

func init() { defaultRunner = testutil.NewCodexRunner() }

// windowsRootRenameBlocked recognizes only Windows errors caused by renaming
// an opened root; unrelated rename failures must propagate and fail the test.
func windowsRootRenameBlocked(err error) bool {
	return runtime.GOOS == "windows" &&
		(errors.Is(err, windowsErrorAccessDenied) || errors.Is(err, windowsErrorSharingViolation))
}

func TestLogicalArtifactDirUsesSlashSeparators(t *testing.T) {
	if got := logicalArtifactDir(".agents/plugins/marketplace.json"); got != ".agents/plugins" {
		t.Fatalf("logicalArtifactDir() = %q, want .agents/plugins", got)
	}
}

func planIndex(plan modelplan.Plan) int {
	for index, candidate := range []modelplan.Plan{modelplan.PlanLow, modelplan.PlanMedium, modelplan.PlanHigh, modelplan.PlanUltra} {
		if plan == candidate {
			return index
		}
	}
	panic("unknown plan")
}

func TestIntegrationReinstallSwitchesAndPersistsModelPlan(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	service := NewIntegration()
	medium := integration.Options{ConfigDir: root, ModelPlan: modelplan.PlanMedium}
	installed, err := service.Install(context.Background(), medium)
	require(t, err == nil && installed.ModelPlan == modelplan.PlanMedium)

	ultra := integration.Options{ConfigDir: root, ModelPlan: modelplan.PlanUltra}
	switched, err := service.Reinstall(context.Background(), ultra)
	require(t, err == nil && switched.State == integration.StateInstalled && switched.Changed && switched.ModelPlan == modelplan.PlanUltra)
	general, err := os.ReadFile(filepath.Join(root, "agents", "general.toml"))
	require(t, err == nil && strings.Contains(string(general), `model = "gpt-5.6-sol"`) && strings.Contains(string(general), `model_reasoning_effort = "high"`))
	retained, err := service.Reinstall(context.Background(), integration.Options{ConfigDir: root})
	require(t, err == nil && retained.State == integration.StateInstalled && !retained.Changed && retained.ModelPlan == modelplan.PlanUltra)

	status, err := service.Status(context.Background(), integration.Options{ConfigDir: root})
	require(t, err == nil && status.State == integration.StateInstalled && status.ModelPlan == modelplan.PlanUltra)
	removed, err := service.Uninstall(context.Background(), integration.Options{ConfigDir: root})
	require(t, err == nil && removed.State == integration.StateAbsent && removed.Changed)
}

func TestIntegrationPreviewReportsRequestedUltraPlan(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	service := NewIntegration()
	medium := integration.Options{ConfigDir: root, ModelPlan: modelplan.PlanMedium}
	mustInstall(t, service, medium)

	preview, err := service.Preview(context.Background(), integration.Options{ConfigDir: root, ModelPlan: modelplan.PlanUltra})
	require(t, err == nil && preview.State == integration.StatePartial && preview.Changed && preview.RestartRequired && preview.ModelPlan == modelplan.PlanUltra)
}

func TestIntegrationPreviewPartialExplicitPlanUsesDesiredPackageIdentity(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	service := NewIntegration()
	mustInstall(t, service, integration.Options{ConfigDir: root, ModelPlan: modelplan.PlanMedium})
	require(t, os.Remove(filepath.Join(root, "agents", "general.toml")) == nil)
	want, err := RenderPlan("v0.0.0", modelplan.PlanUltra)
	require(t, err == nil)

	preview, err := service.Preview(context.Background(), integration.Options{ConfigDir: root, ModelPlan: modelplan.PlanUltra})
	if err != nil || preview.State != integration.StatePartial || !preview.Changed || !preview.RestartRequired || preview.ArtifactSHA256 != want.SHA256 || preview.ArtifactCount != len(want.Artifacts) || preview.ModelPlan != modelplan.PlanUltra || preview.ModelProvider == "" {
		t.Fatalf("Preview(partial requested ultra) = %+v, %v", preview, err)
	}
}

func TestIntegrationRejectsModelSlotCustomizationBeforeWriting(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	for _, options := range []integration.Options{
		{ConfigDir: root, ModelEfficient: "openai/custom-fast", ModelBalanced: "openai/custom-balanced", ModelFrontier: "openai/custom-frontier"},
		{ConfigDir: root, ModelEfficient: "openai/custom-fast", ModelBalanced: "anthropic/custom-balanced", ModelFrontier: "acme/custom-frontier"},
		{ConfigDir: root, ModelEfficientEffort: modelplan.EffortHigh, ModelBalancedEffort: modelplan.EffortHigh, ModelFrontierEffort: modelplan.EffortHigh},
		{ConfigDir: root, ModelEfficientEffort: modelplan.EffortLow, ModelBalancedEffort: modelplan.EffortHigh, ModelFrontierEffort: modelplan.EffortUltra},
	} {
		for name, call := range map[string]func(context.Context, integration.Options) (integration.Result, error){
			"preview": NewIntegration().Preview, "status": NewIntegration().Status, "install": NewIntegration().Install, "reinstall": NewIntegration().Reinstall, "uninstall": NewIntegration().Uninstall,
		} {
			_, err := call(context.Background(), options)
			if !errors.Is(err, integration.ErrInvalid) || !strings.Contains(err.Error(), "model-slot customization") {
				t.Fatalf("%s(%+v) error=%v", name, options, err)
			}
		}
		if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("customization created root: %v", err)
		}
	}
}

func TestIntegrationStatusReportsRequestedUltraMismatch(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	service := NewIntegration()
	mustInstall(t, service, integration.Options{ConfigDir: root, ModelPlan: modelplan.PlanLow})

	status, err := service.Status(context.Background(), integration.Options{ConfigDir: root, ModelPlan: modelplan.PlanUltra})
	if err != nil || status.State != integration.StatePartial || !status.Changed || !status.RestartRequired || status.ModelPlan != modelplan.PlanUltra {
		t.Fatalf("Status(requested ultra) = %+v, %v; want changed partial ultra", status, err)
	}
}

func TestIntegrationNoOptionStatusAndReinstallPreserveExactPartialPlan(t *testing.T) {
	for _, plan := range []modelplan.Plan{modelplan.PlanLow, modelplan.PlanHigh, modelplan.PlanUltra} {
		t.Run(string(plan), func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "codex")
			service := NewIntegration()
			withPlan := integration.Options{ConfigDir: root, ModelPlan: plan}
			mustInstall(t, service, withPlan)
			require(t, os.Remove(filepath.Join(root, "agents", "general.toml")) == nil)

			status, err := service.Status(context.Background(), integration.Options{ConfigDir: root})
			if err != nil || status.State != integration.StatePartial || status.ModelPlan != plan {
				t.Fatalf("Status() = %+v, %v; want partial %s", status, err, plan)
			}
			reinstalled, err := service.Reinstall(context.Background(), integration.Options{ConfigDir: root})
			if err != nil || reinstalled.State != integration.StateInstalled || !reinstalled.Changed || reinstalled.ModelPlan != plan {
				t.Fatalf("Reinstall() = %+v, %v; want changed installed %s", reinstalled, err, plan)
			}
		})
	}
}

func TestIntegrationRecoversSharedRemoveSidecarWithExplicitPlan(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	options := integration.Options{ConfigDir: root, ModelPlan: modelplan.PlanUltra}
	pkg, err := RenderPlan("v0.0.0", modelplan.PlanUltra)
	require(t, err == nil)
	require(t, os.MkdirAll(root, 0o700) == nil)
	require(t, os.WriteFile(filepath.Join(root, ".vgxness-pending"), []byte("codex-pending\n"), 0o600) == nil)
	require(t, os.WriteFile(filepath.Join(root, "AGENTS.md.vgxness-remove"), artifact(t, pkg, "AGENTS.md").Bytes, 0o600) == nil)

	result, err := NewIntegration().Reinstall(context.Background(), options)
	if err != nil || result.State != integration.StateInstalled || !result.Changed || result.ModelPlan != modelplan.PlanUltra {
		t.Fatalf("Reinstall(shared remove sidecar) = %+v, %v; want changed installed ultra", result, err)
	}
	assertNoEvidence(t, root)
}

func TestIntegrationReinstallAbsentInstallsExplicitRequestedPlan(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	service := NewIntegration()
	mustInstall(t, service, integration.Options{ConfigDir: root, ModelPlan: modelplan.PlanLow})
	_, err := service.Uninstall(context.Background(), integration.Options{ConfigDir: root, ModelPlan: modelplan.PlanLow})
	require(t, err == nil)

	result, err := service.Reinstall(context.Background(), integration.Options{ConfigDir: root, ModelPlan: modelplan.PlanUltra})
	if err != nil || result.State != integration.StateInstalled || !result.Changed || result.ModelPlan != modelplan.PlanUltra {
		t.Fatalf("Reinstall(absent requested ultra) = %+v, %v; want changed installed ultra", result, err)
	}
}

func TestIntegrationPlanSwitchBlocksUnknownDrift(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	service := NewIntegration()
	mustInstall(t, service, integration.Options{ConfigDir: root, ModelPlan: modelplan.PlanLow})
	require(t, os.WriteFile(filepath.Join(root, "agents", "general.toml"), []byte("user change\n"), 0o600) == nil)

	_, err := service.Reinstall(context.Background(), integration.Options{ConfigDir: root, ModelPlan: modelplan.PlanUltra})
	require(t, errors.Is(err, integration.ErrDrift))
	body, readErr := os.ReadFile(filepath.Join(root, "agents", "general.toml"))
	require(t, readErr == nil && string(body) == "user change\n")
}

func TestIntegrationRecoveryUsesInstalledPlanForSharedSidecar(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	service := NewIntegration()
	low := integration.Options{ConfigDir: root, ModelPlan: modelplan.PlanLow}
	mustInstall(t, service, low)
	require(t, os.WriteFile(filepath.Join(root, ".vgxness-pending"), []byte("codex-pending\n"), 0o600) == nil)
	require(t, os.Link(filepath.Join(root, "AGENTS.md"), filepath.Join(root, "AGENTS.md.vgxness-stage")) == nil)

	result, err := service.Reinstall(context.Background(), integration.Options{ConfigDir: root, ModelPlan: modelplan.PlanUltra})
	if err != nil || result.State != integration.StateInstalled || result.ModelPlan != modelplan.PlanLow {
		t.Fatalf("recovery result = %+v, err = %v", result, err)
	}
	assertNoEvidence(t, root)
}

func TestIntegrationInstallAndIdempotence(t *testing.T) {
	options := integration.Options{ConfigDir: filepath.Join(t.TempDir(), "codex")}
	service := NewIntegration()
	before, err := service.Status(context.Background(), options)
	require(t, err == nil && before.State == integration.StateAbsent && before.ArtifactCount == 10)
	installed, err := service.Install(context.Background(), options)
	if err != nil || installed.State != integration.StateInstalled || !installed.Changed || !installed.RestartRequired {
		t.Fatalf("Install() = %+v, %v", installed, err)
	}
	again, err := service.Install(context.Background(), options)
	require(t, err == nil && !again.Changed && !again.RestartRequired && again.State == integration.StateInstalled)
}

func TestReinstallMigratesExactHistoricalCodexHooks(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	pkg, err := Render("v0.0.0")
	if err != nil {
		t.Fatal(err)
	}
	writePackage(t, root, pkg)
	hook := filepath.Join(root, "plugins", "vgxness", "hooks.json")
	require(t, os.WriteFile(hook, historicalCodexHooksGolden(t), 0o600) == nil)
	service := NewIntegration()
	options := integration.Options{ConfigDir: root}
	status, err := service.Status(context.Background(), options)
	require(t, err == nil && status.State == integration.StatePartial)
	result, err := service.Reinstall(context.Background(), options)
	require(t, err == nil && result.State == integration.StateInstalled && result.Changed)
	_, err = os.Stat(hook)
	require(t, errors.Is(err, os.ErrNotExist))
	again, err := service.Reinstall(context.Background(), options)
	require(t, err == nil && !again.Changed)
}

func TestUninstallMigratesExactHistoricalCodexHooks(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	pkg, err := Render("v0.0.0")
	if err != nil {
		t.Fatal(err)
	}
	writePackage(t, root, pkg)
	hook := filepath.Join(root, "plugins", "vgxness", "hooks.json")
	require(t, os.WriteFile(hook, historicalCodexHooksGolden(t), 0o600) == nil)
	result, err := NewIntegration().Uninstall(context.Background(), integration.Options{ConfigDir: root})
	require(t, err == nil && result.State == integration.StateAbsent && result.Changed)
	_, err = os.Stat(hook)
	require(t, errors.Is(err, os.ErrNotExist))
}

func TestModifiedHistoricalCodexHooksAreRetainedAndReported(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	pkg, err := Render("v0.0.0")
	if err != nil {
		t.Fatal(err)
	}
	writePackage(t, root, pkg)
	hook := filepath.Join(root, "plugins", "vgxness", "hooks.json")
	modified := append(historicalCodexHooksGolden(t), "modified\n"...)
	require(t, os.WriteFile(hook, modified, 0o600) == nil)
	service := NewIntegration()
	status, err := service.Status(context.Background(), integration.Options{ConfigDir: root})
	require(t, err == nil && status.State == integration.StateDrifted)
	_, err = service.Uninstall(context.Background(), integration.Options{ConfigDir: root})
	require(t, errors.Is(err, integration.ErrDrift))
	retained, readErr := os.ReadFile(hook)
	require(t, readErr == nil && bytes.Equal(retained, modified))
}

func TestStatusReportsHistoricalCodexHookRemovalSidecarAsRecovery(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	pkg, err := Render("v0.0.0")
	if err != nil {
		t.Fatal(err)
	}
	writePackage(t, root, pkg)
	sidecar := filepath.Join(root, "plugins", "vgxness", "hooks.json.vgxness-remove")
	require(t, os.WriteFile(sidecar, historicalCodexHooksGolden(t), 0o600) == nil)
	status, err := NewIntegration().Status(context.Background(), integration.Options{ConfigDir: root})
	require(t, errors.Is(err, integration.ErrRecovery) && status.State != integration.StateInstalled)
}

func TestHistoricalCodexHooksGoldenIdentity(t *testing.T) {
	golden := historicalCodexHooksGolden(t)
	const wantSHA256 = "bad09752e8c719c6826705bd4d31dc46c0da7f943c35f665d11dcc23a8f15a94"
	got := fmt.Sprintf("%x", sha256.Sum256(golden))
	require(t, got == wantSHA256)
	require(t, bytes.Equal(golden, []byte(historicalCodexHooksJSON)))
}

func TestHistoricalCodexHookRemovalFailuresRemainRecoverable(t *testing.T) {
	pluginDir := filepath.Join("plugins", "vgxness")
	tests := []struct {
		name  string
		match func(root, name string) bool
	}{
		{name: "pending durability", match: func(root, name string) bool {
			_, err := os.Lstat(filepath.Join(root, ".vgxness-pending"))
			return name == "." && err == nil
		}},
		{name: "backup durability", match: func(root, name string) bool {
			_, hookErr := os.Lstat(filepath.Join(root, "plugins", "vgxness", "hooks.json"))
			_, sidecarErr := os.Lstat(filepath.Join(root, "plugins", "vgxness", "hooks.json.vgxness-remove"))
			return name == pluginDir && hookErr == nil && sidecarErr == nil
		}},
		{name: "target removal durability", match: func(root, name string) bool {
			_, hookErr := os.Lstat(filepath.Join(root, "plugins", "vgxness", "hooks.json"))
			_, sidecarErr := os.Lstat(filepath.Join(root, "plugins", "vgxness", "hooks.json.vgxness-remove"))
			return name == pluginDir && errors.Is(hookErr, os.ErrNotExist) && sidecarErr == nil
		}},
		{name: "backup cleanup durability", match: func(root, name string) bool {
			_, hookErr := os.Lstat(filepath.Join(root, "plugins", "vgxness", "hooks.json"))
			_, sidecarErr := os.Lstat(filepath.Join(root, "plugins", "vgxness", "hooks.json.vgxness-remove"))
			return name == pluginDir && errors.Is(hookErr, os.ErrNotExist) && errors.Is(sidecarErr, os.ErrNotExist)
		}},
		{name: "activation evidence durability", match: func(root, name string) bool {
			_, err := os.Lstat(filepath.Join(root, ".vgxness-activation-pending"))
			return name == "." && err == nil
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "codex")
			pkg, err := Render("v0.0.0")
			if err != nil {
				t.Fatal(err)
			}
			writePackage(t, root, pkg)
			hook := filepath.Join(root, "plugins", "vgxness", "hooks.json")
			require(t, os.WriteFile(hook, historicalCodexHooksGolden(t), 0o600) == nil)

			fired := false
			failing := NewIntegration()
			failing.open = func(ctx context.Context, options integration.Options, create bool) (*Root, error) {
				r, openErr := OpenRoot(ctx, options, create)
				if openErr == nil {
					r.syncHook = func(name string) error {
						if test.match(root, name) {
							fired = true
							return errors.New("injected retired-hook durability failure")
						}
						return nil
					}
				}
				return r, openErr
			}
			options := integration.Options{ConfigDir: root}
			_, err = failing.Reinstall(context.Background(), options)
			require(t, fired)
			require(t, errors.Is(err, integration.ErrRecovery))

			status, statusErr := NewIntegration().Status(context.Background(), options)
			require(t, errors.Is(statusErr, integration.ErrRecovery))
			require(t, status.State != integration.StateInstalled)

			recovered, recoverErr := NewIntegration().Reinstall(context.Background(), options)
			require(t, recoverErr == nil && recovered.State == integration.StateInstalled)
			_, hookErr := os.Lstat(hook)
			_, sidecarErr := os.Lstat(hook + ".vgxness-remove")
			require(t, errors.Is(hookErr, os.ErrNotExist) && errors.Is(sidecarErr, os.ErrNotExist))
			assertNoEvidence(t, root)
		})
	}
}

func TestHistoricalCodexHookUninstallEvidenceFailureRemainsRecoverable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	pkg, err := Render("v0.0.0")
	if err != nil {
		t.Fatal(err)
	}
	writePackage(t, root, pkg)
	hook := filepath.Join(root, "plugins", "vgxness", "hooks.json")
	require(t, os.WriteFile(hook, historicalCodexHooksGolden(t), 0o600) == nil)

	fired := false
	failing := NewIntegration()
	failing.open = func(ctx context.Context, options integration.Options, create bool) (*Root, error) {
		r, openErr := OpenRoot(ctx, options, create)
		if openErr == nil {
			r.syncHook = func(name string) error {
				_, pendingErr := os.Lstat(filepath.Join(root, ".vgxness-activation-pending"))
				if name == "." && pendingErr == nil {
					fired = true
					return errors.New("injected deactivation evidence durability failure")
				}
				return nil
			}
		}
		return r, openErr
	}
	options := integration.Options{ConfigDir: root}
	_, err = failing.Uninstall(context.Background(), options)
	require(t, fired)
	require(t, errors.Is(err, integration.ErrRecovery))
	status, statusErr := NewIntegration().Status(context.Background(), options)
	require(t, errors.Is(statusErr, integration.ErrRecovery) && status.State != integration.StateInstalled)

	recovered, recoverErr := NewIntegration().Uninstall(context.Background(), options)
	require(t, recoverErr == nil && recovered.State == integration.StateAbsent)
	_, hookErr := os.Lstat(hook)
	_, sidecarErr := os.Lstat(hook + ".vgxness-remove")
	require(t, errors.Is(hookErr, os.ErrNotExist) && errors.Is(sidecarErr, os.ErrNotExist))
	assertNoEvidence(t, root)
}

func TestActivationRecoveryReconcilesHistoricalCodexHookSidecarWithoutPendingMarker(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	pkg, err := Render("v0.0.0")
	if err != nil {
		t.Fatal(err)
	}
	writePackage(t, root, pkg)
	sidecar := filepath.Join(root, "plugins", "vgxness", "hooks.json.vgxness-remove")
	require(t, os.WriteFile(sidecar, historicalCodexHooksGolden(t), 0o600) == nil)
	writeActivationPending(t, root, pkg, "activate")
	_, err = os.Lstat(filepath.Join(root, pendingName))
	require(t, errors.Is(err, os.ErrNotExist))

	result, err := NewIntegration().Reinstall(context.Background(), integration.Options{ConfigDir: root})
	require(t, err == nil && result.State == integration.StateInstalled && result.Changed && result.RestartRequired)
	_, err = os.Lstat(sidecar)
	require(t, errors.Is(err, os.ErrNotExist))
	status, statusErr := NewIntegration().Status(context.Background(), integration.Options{ConfigDir: root})
	require(t, statusErr == nil && status.State == integration.StateInstalled)
	assertNoEvidence(t, root)
}

func TestActivationRecoveryRejectsPendingPackageMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codex")
	a, err := RenderPlan("v0.0.0", modelplan.PlanMedium)
	require(t, err == nil)
	b, err := RenderPlan("v0.0.0", modelplan.PlanUltra)
	require(t, err == nil && a.SHA256 != b.SHA256)
	writePackage(t, path, a)
	sidecar := filepath.Join(path, "plugins", "vgxness", "hooks.json.vgxness-remove")
	sidecarBody := historicalCodexHooksGolden(t)
	require(t, os.WriteFile(sidecar, sidecarBody, 0o600) == nil)
	writeActivationPending(t, path, a, "activate")
	root, err := OpenRoot(context.Background(), integration.Options{ConfigDir: path}, false)
	require(t, err == nil)
	pendingBody := pendingEvidence(b.SHA256)
	require(t, root.MarkPending(pendingBody) == nil)
	require(t, root.Close() == nil)

	result, err := NewIntegration().Reinstall(context.Background(), integration.Options{ConfigDir: path})
	require(t, errors.Is(err, integration.ErrRecovery) && errors.Is(err, integration.ErrConflict))
	require(t, !result.Changed)
	activationBody, activationErr := os.ReadFile(filepath.Join(path, activationPendingName))
	require(t, activationErr == nil && bytes.Equal(activationBody, activationEvidence(a, "activate").body))
	pendingActual, pendingErr := os.ReadFile(filepath.Join(path, pendingName))
	require(t, pendingErr == nil && bytes.Equal(pendingActual, pendingBody))
	retained, sidecarErr := os.ReadFile(sidecar)
	require(t, sidecarErr == nil && bytes.Equal(retained, sidecarBody))
	_, targetErr := os.Lstat(filepath.Join(path, "plugins", "vgxness", "hooks.json"))
	require(t, errors.Is(targetErr, os.ErrNotExist))
}

func TestActivationRecoveryRejectsLegacyPendingPackageMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codex")
	a, err := RenderPlan("v0.0.0", modelplan.PlanMedium)
	require(t, err == nil)
	b, sidecar := uniqueLegacyRecoverySidecar(t, a)
	writePackage(t, path, a)
	activationBody := activationEvidence(a, "activate").body
	writeActivationPending(t, path, a, "activate")
	pendingBody := []byte("codex-pending\n")
	require(t, os.WriteFile(filepath.Join(path, pendingName), pendingBody, 0o600) == nil)
	sidecarPath := filepath.Join(path, filepath.FromSlash(sidecar.Path+".vgxness-stage"))
	require(t, os.MkdirAll(filepath.Dir(sidecarPath), 0o700) == nil)
	require(t, os.WriteFile(sidecarPath, sidecar.Bytes, 0o600) == nil)

	fake := &fakeCodexCLI{fail: map[string]error{}, after: map[string]error{}, root: path}
	result, err := fakeActivationIntegration(fake).Reinstall(context.Background(), integration.Options{ConfigDir: path})
	if !errors.Is(err, integration.ErrRecovery) || !errors.Is(err, integration.ErrConflict) {
		t.Fatalf("Reinstall() = %+v, %v; want recovery conflict", result, err)
	}
	if !strings.Contains(err.Error(), "pending package does not match activation evidence") {
		t.Fatalf("Reinstall() error = %v; want pending package mismatch", err)
	}
	require(t, !result.Changed && !result.RestartRequired && fake.mutations == 0)
	actualActivation, activationErr := os.ReadFile(filepath.Join(path, activationPendingName))
	actualPending, pendingErr := os.ReadFile(filepath.Join(path, pendingName))
	actualSidecar, sidecarErr := os.ReadFile(sidecarPath)
	require(t, activationErr == nil && bytes.Equal(actualActivation, activationBody))
	require(t, pendingErr == nil && bytes.Equal(actualPending, pendingBody))
	require(t, sidecarErr == nil && bytes.Equal(actualSidecar, sidecar.Bytes))
	require(t, b.SHA256 != a.SHA256)
}

func uniqueLegacyRecoverySidecar(t *testing.T, fallback Package) (Package, Artifact) {
	t.Helper()
	packages, err := knownPackages()
	require(t, err == nil)
	for _, candidate := range packages {
		if candidate.SHA256 == fallback.SHA256 {
			continue
		}
		for _, artifact := range candidate.Artifacts {
			matches := 0
			for _, known := range packages {
				for _, knownArtifact := range known.Artifacts {
					if knownArtifact.Path == artifact.Path && bytes.Equal(knownArtifact.Bytes, artifact.Bytes) {
						matches++
					}
				}
			}
			if matches == 1 {
				return candidate, artifact
			}
		}
	}
	t.Fatal("no uniquely identifiable legacy recovery package")
	return Package{}, Artifact{}
}

func TestActivationRecoveryReportsSuccessfulCLIMutations(t *testing.T) {
	for _, test := range []struct {
		name string
		fake func(string) *fakeCodexCLI
	}{
		{"marketplace-only", func(path string) *fakeCodexCLI {
			return &fakeCodexCLI{fail: map[string]error{}, after: map[string]error{}, root: path, market: true}
		}},
		{"plugin-only", func(path string) *fakeCodexCLI {
			return &fakeCodexCLI{fail: map[string]error{}, after: map[string]error{}, root: path, plugin: true, enabled: true}
		}},
		{"activation-absent fallback", func(path string) *fakeCodexCLI {
			return &fakeCodexCLI{fail: map[string]error{}, after: map[string]error{}, root: path}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "codex")
			pkg, err := RenderPlan("v0.0.0", modelplan.PlanMedium)
			require(t, err == nil)
			writePackage(t, path, pkg)
			writeActivationPending(t, path, pkg, "activate")
			fake := test.fake(path)

			result, err := fakeActivationIntegration(fake).Reinstall(context.Background(), integration.Options{ConfigDir: path})
			if err != nil || result.State != integration.StateInstalled || !result.Changed || !result.RestartRequired {
				t.Fatalf("Reinstall() = %+v, %v; want changed installed recovery", result, err)
			}
			require(t, fake.mutations > 0)
		})
	}
}

func TestActivationRecoveryFailureDoesNotReportReconciliation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codex")
	pkg, err := RenderPlan("v0.0.0", modelplan.PlanMedium)
	require(t, err == nil)
	writePackage(t, path, pkg)
	writeActivationPending(t, path, pkg, "activate")
	require(t, os.WriteFile(filepath.Join(path, pendingName), pendingEvidence(pkg.SHA256), 0o600) == nil)
	sidecar := filepath.Join(path, "AGENTS.md.vgxness-stage")
	require(t, os.WriteFile(sidecar, artifact(t, pkg, "AGENTS.md").Bytes, 0o600) == nil)
	fake := &fakeCodexCLI{
		fail:  map[string]error{"[plugin marketplace add " + path + " --json]": errors.New("activation failed")},
		after: map[string]error{}, root: path,
	}

	result, err := fakeActivationIntegration(fake).Reinstall(context.Background(), integration.Options{ConfigDir: path})
	require(t, errors.Is(err, integration.ErrRecovery))
	require(t, !result.Changed && !result.RestartRequired)
}

func TestActivationRecoveryRetainsModifiedHistoricalCodexHookSidecar(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	pkg, err := Render("v0.0.0")
	if err != nil {
		t.Fatal(err)
	}
	writePackage(t, root, pkg)
	sidecar := filepath.Join(root, "plugins", "vgxness", "hooks.json.vgxness-remove")
	modified := append(historicalCodexHooksGolden(t), "modified\n"...)
	require(t, os.WriteFile(sidecar, modified, 0o600) == nil)
	writeActivationPending(t, root, pkg, "activate")

	_, err = NewIntegration().Reinstall(context.Background(), integration.Options{ConfigDir: root})
	require(t, errors.Is(err, integration.ErrRecovery) && errors.Is(err, integration.ErrDrift))
	retained, readErr := os.ReadFile(sidecar)
	require(t, readErr == nil && bytes.Equal(retained, modified))
	_, activationErr := os.Lstat(filepath.Join(root, activationPendingName))
	require(t, activationErr == nil)
}

func writeActivationPending(t *testing.T, path string, pkg Package, phase string) {
	t.Helper()
	root, err := OpenRoot(context.Background(), integration.Options{ConfigDir: path}, false)
	if err != nil {
		t.Fatal(err)
	}
	evidence := activationEvidence(pkg, phase)
	require(t, root.MarkActivationPending(evidence.body) == nil)
	require(t, root.Close() == nil)
}

func historicalCodexHooksGolden(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "historical-hooks-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestIntegrationProtectedInstallBindsSnapshotSourceToHeldRoot(t *testing.T) {
	for _, test := range []struct {
		name string
		open func(*testing.T, string, *Integration, *bool)
		want error
	}{
		{
			name: "replacement before open fails closed",
			open: func(t *testing.T, root string, service *Integration, replacementBlocked *bool) {
				t.Helper()
				replaced := root + "-protected"
				require(t, os.Rename(root, replaced) == nil)
				require(t, os.Mkdir(root, 0o700) == nil)
			},
			want: integration.ErrConflict,
		},
		{
			name: "replacement after open cannot redirect writes",
			open: func(t *testing.T, root string, service *Integration, replacementBlocked *bool) {
				t.Helper()
				service.open = func(ctx context.Context, options integration.Options, create bool) (*Root, error) {
					held, err := OpenRoot(ctx, options, create)
					if err != nil {
						return nil, err
					}
					prior := root + "-protected"
					if err := os.Rename(root, prior); err != nil {
						if windowsRootRenameBlocked(err) {
							heldInfo, heldErr := held.fs.Lstat(".")
							rootInfo, rootErr := os.Lstat(root)
							_, priorErr := os.Lstat(prior)
							if heldErr != nil || rootErr != nil || !os.SameFile(heldInfo, rootInfo) || !errors.Is(priorErr, os.ErrNotExist) {
								_ = held.Close()
								t.Fatalf("blocked replacement did not retain held root: held=%v root=%v prior=%v", heldErr, rootErr, priorErr)
							}
							*replacementBlocked = true
							return held, nil
						}
						_ = held.Close()
						return nil, err
					}
					if err := os.Mkdir(root, 0o700); err != nil {
						_ = held.Close()
						return nil, err
					}
					if err := os.Mkdir(filepath.Join(root, "agents"), 0o700); err != nil {
						_ = held.Close()
						return nil, err
					}
					return held, nil
				}
			},
			want: integration.ErrConflict,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "codex")
			require(t, os.Mkdir(root, 0o700) == nil)
			require(t, os.Mkdir(filepath.Join(root, "agents"), 0o700) == nil)
			info, err := sourceRootIdentity(root)
			require(t, err == nil)
			service := NewIntegration()
			replacementBlocked := false
			test.open(t, root, service, &replacementBlocked)

			result, err := service.InstallProtected(context.Background(), integration.Options{ConfigDir: root}, sourceIdentity{info: info})
			if replacementBlocked {
				if err != nil || result.State != integration.StateInstalled {
					t.Fatalf("InstallProtected() = %+v, %v", result, err)
				}
				_, originalErr := os.Stat(filepath.Join(root, "AGENTS.md"))
				require(t, originalErr == nil)
				_, priorErr := os.Stat(filepath.Join(root+"-protected", "AGENTS.md"))
				require(t, errors.Is(priorErr, os.ErrNotExist))
				return
			}
			if test.want != nil {
				require(t, errors.Is(err, test.want) && !result.Changed)
				_, replacementErr := os.Stat(filepath.Join(root, "AGENTS.md"))
				require(t, errors.Is(replacementErr, os.ErrNotExist))
				_, protectedErr := os.Stat(filepath.Join(root+"-protected", "AGENTS.md"))
				require(t, errors.Is(protectedErr, os.ErrNotExist))
				return
			}
		})
	}
}

func TestProtectedInstallDoesNotCreateMissingRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	require(t, os.Mkdir(root, 0o700) == nil)
	info, err := sourceRootIdentity(root)
	require(t, err == nil)
	require(t, os.Remove(root) == nil)

	_, err = NewIntegration().InstallProtected(context.Background(), integration.Options{ConfigDir: root}, sourceIdentity{info: info})
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("InstallProtected() error = %v", err)
	}
	_, statErr := os.Stat(root)
	require(t, errors.Is(statErr, os.ErrNotExist))
}

func TestReinstallProtectedBindsOrPreservesMissingRoot(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, string)
		want   error
	}{
		{"replacement", func(t *testing.T, root string) {
			require(t, os.Rename(root, root+"-prior") == nil)
			require(t, os.Mkdir(root, 0o700) == nil)
		}, integration.ErrConflict},
		{"missing", func(t *testing.T, root string) { require(t, os.Remove(root) == nil) }, integration.ErrInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "codex")
			require(t, os.Mkdir(root, 0o700) == nil)
			info, err := sourceRootIdentity(root)
			require(t, err == nil)
			test.mutate(t, root)
			_, err = NewIntegration().ReinstallProtected(context.Background(), integration.Options{ConfigDir: root, ModelPlan: modelplan.PlanMedium}, sourceIdentity{info: info})
			require(t, errors.Is(err, test.want))
			if test.name == "missing" {
				_, err = os.Stat(root)
				require(t, errors.Is(err, os.ErrNotExist))
			}
		})
	}
}

func writePackage(t *testing.T, root string, pkg Package) {
	t.Helper()
	for _, item := range pkg.Artifacts {
		path := filepath.Join(root, filepath.FromSlash(item.Path))
		require(t, os.MkdirAll(filepath.Dir(path), 0o700) == nil)
		require(t, os.WriteFile(path, item.Bytes, 0o600) == nil)
	}
}
func TestIntegrationReinstallsPartialAndPreservesUnrelatedFiles(t *testing.T) {
	options := integration.Options{ConfigDir: filepath.Join(t.TempDir(), "codex")}
	service := NewIntegration()
	mustInstall(t, service, options)
	config := []byte("model = \"safe\"\n[mcp_servers.user]\ncommand = \"user-tool\"\n")
	require(t, os.WriteFile(filepath.Join(options.ConfigDir, "config.toml"), config, 0o600) == nil)
	require(t, os.Remove(filepath.Join(options.ConfigDir, "agents", "explore.toml")) == nil)
	partial, err := service.Status(context.Background(), options)
	require(t, err == nil && partial.State == integration.StatePartial)
	result, err := service.Reinstall(context.Background(), options)
	if err != nil || result.State != integration.StateInstalled || !result.Changed {
		t.Fatalf("repair=%+v err=%v", result, err)
	}
	body, err := os.ReadFile(filepath.Join(options.ConfigDir, "config.toml"))
	require(t, err == nil && string(body) == string(config))
}
func TestIntegrationBlocksDriftAndPendingEvidence(t *testing.T) {
	options := integration.Options{ConfigDir: filepath.Join(t.TempDir(), "codex")}
	service := NewIntegration()
	mustInstall(t, service, options)
	require(t, os.WriteFile(filepath.Join(options.ConfigDir, ".vgxness-pending"), []byte("codex-pending\n"), 0o600) == nil)
	pending, err := service.ReinstallPending(context.Background(), options)
	require(t, err == nil && pending)
	_, err = service.Uninstall(context.Background(), options)
	require(t, errors.Is(err, integration.ErrRecovery))
	require(t, os.Remove(filepath.Join(options.ConfigDir, ".vgxness-pending")) == nil)
	require(t, os.WriteFile(filepath.Join(options.ConfigDir, "agents", "explore.toml"), []byte("changed"), 0o600) == nil)
	status, err := service.Status(context.Background(), options)
	require(t, err == nil && status.State == integration.StateDrifted)
	_, err = service.Uninstall(context.Background(), options)
	require(t, errors.Is(err, integration.ErrDrift))
}

func TestStatusReportsRecoveryWhenClearPendingFails(t *testing.T) {
	options := integration.Options{ConfigDir: filepath.Join(t.TempDir(), "codex")}
	s := NewIntegration()
	syncs := 0
	s.open = func(ctx context.Context, o integration.Options, create bool) (*Root, error) {
		r, err := OpenRoot(ctx, o, create)
		if err == nil {
			r.syncHook = func(name string) error {
				if name == "." {
					syncs++
					// The native plugin projection adds two root-level lifecycle writes.
					if syncs == 7 {
						return errors.New("clear pending")
					}
				}
				return nil
			}
		}
		return r, err
	}
	_, err := s.Install(context.Background(), options)
	require(t, errors.Is(err, integration.ErrRecovery))
	assertPending(t, options.ConfigDir)

	status, err := s.Status(context.Background(), options)
	require(t, status.State == integration.StatePartial && errors.Is(err, integration.ErrRecovery))
	assertPending(t, options.ConfigDir)
}
func TestManagedLayoutExcludesPluginArtifacts(t *testing.T) {
	layout, err := NewIntegration().ManagedLayout(context.Background(), integration.Options{ConfigDir: filepath.Join(t.TempDir(), "codex")})
	require(t, err == nil && len(layout.Artifacts) == 10)
	for _, item := range layout.Artifacts {
		require(t, item.RelativePath != "config.toml" && item.RelativePath != ".mcp.json" && filepath.Ext(item.RelativePath) != ".plugin")
	}
}
func TestUninstallLeavesAbsentAndUnrelated(t *testing.T) {
	options := integration.Options{ConfigDir: filepath.Join(t.TempDir(), "codex")}
	s := NewIntegration()
	mustInstall(t, s, options)
	config := []byte("model = \"safe\"\n[mcp_servers.user]\ncommand = \"user-tool\"\n")
	require(t, os.WriteFile(filepath.Join(options.ConfigDir, "config.toml"), config, 0o600) == nil)
	got, err := s.Uninstall(context.Background(), options)
	require(t, err == nil && got.State == integration.StateAbsent && got.Changed && got.RestartRequired)
	b, err := os.ReadFile(filepath.Join(options.ConfigDir, "config.toml"))
	require(t, err == nil && string(b) == string(config))
	assertNoEvidence(t, options.ConfigDir)
}
func TestPendingSidecarsBlockOnlyKnownArtifacts(t *testing.T) {
	options := integration.Options{ConfigDir: filepath.Join(t.TempDir(), "codex")}
	s := NewIntegration()
	mustInstall(t, s, options)
	for _, suffix := range []string{".vgxness-stage", ".vgxness-remove"} {
		name := filepath.Join(options.ConfigDir, "AGENTS.md"+suffix)
		require(t, os.WriteFile(name, []byte("evidence"), 0o600) == nil)
		pending, err := s.ReinstallPending(context.Background(), options)
		require(t, err == nil && pending)
		for _, call := range []func(context.Context, integration.Options) (integration.Result, error){s.Install, s.Reinstall, s.Uninstall} {
			_, err := call(context.Background(), options)
			require(t, errors.Is(err, integration.ErrRecovery))
		}
		require(t, os.Remove(name) == nil)
	}
	require(t, os.WriteFile(filepath.Join(options.ConfigDir, ".vgxness-custom"), []byte("keep"), 0o600) == nil)
	pending, err := s.ReinstallPending(context.Background(), options)
	require(t, err == nil && !pending)
}
func TestInstallCancellationAndPostLinkFailureRetainRecoveryEvidence(t *testing.T) {
	t.Run("cancellation", func(t *testing.T) {
		options := integration.Options{ConfigDir: filepath.Join(t.TempDir(), "codex")}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		require(t, os.Mkdir(options.ConfigDir, 0o700) == nil)
		config := []byte("model = \"safe\"\n[mcp_servers.user]\ncommand = \"user-tool\"\n")
		require(t, os.WriteFile(filepath.Join(options.ConfigDir, "config.toml"), config, 0o600) == nil)
		s := NewIntegration()
		s.checkpoint = func(point, _ string) error {
			if point == "published" {
				cancel()
			}
			return nil
		}
		_, err := s.Install(ctx, options)
		require(t, errors.Is(err, context.Canceled) && errors.Is(err, integration.ErrRecovery))
		_, err = os.Stat(filepath.Join(options.ConfigDir, "AGENTS.md"))
		require(t, errors.Is(err, os.ErrNotExist))
		b, err := os.ReadFile(filepath.Join(options.ConfigDir, "config.toml"))
		require(t, err == nil && string(b) == string(config))
		assertPending(t, options.ConfigDir)
		recovered, err := s.Reinstall(context.Background(), options)
		require(t, err == nil && recovered.State == integration.StateInstalled)
		assertNoEvidence(t, options.ConfigDir)
	})
	t.Run("post-link", func(t *testing.T) {
		options := integration.Options{ConfigDir: filepath.Join(t.TempDir(), "codex")}
		calls := 0
		s := NewIntegration()
		s.open = func(ctx context.Context, o integration.Options, create bool) (*Root, error) {
			r, err := OpenRoot(ctx, o, create)
			if err == nil {
				r.syncHook = func(name string) error {
					if name == "agents" {
						calls++
						if calls == 2 {
							return errors.New("post-link")
						}
					}
					return nil
				}
			}
			return r, err
		}
		_, err := s.Install(context.Background(), options)
		require(t, errors.Is(err, integration.ErrRecovery))
		assertPending(t, options.ConfigDir)
	})
}
func TestUninstallCleanupFailureReinstalls(t *testing.T) {
	options := integration.Options{ConfigDir: filepath.Join(t.TempDir(), "codex")}
	s := NewIntegration()
	mustInstall(t, s, options)
	s.checkpoint = func(point, _ string) error {
		if point == "cleanup" {
			return errors.New("cleanup")
		}
		return nil
	}
	_, err := s.Uninstall(context.Background(), options)
	require(t, errors.Is(err, integration.ErrRecovery))
	_, targetErr := os.Stat(filepath.Join(options.ConfigDir, "AGENTS.md"))
	_, sidecarErr := os.Stat(filepath.Join(options.ConfigDir, "AGENTS.md.vgxness-remove"))
	require(t, os.IsNotExist(targetErr) && sidecarErr == nil)
	recovered, err := s.Reinstall(context.Background(), options)
	require(t, err == nil && recovered.State == integration.StateInstalled && recovered.Changed && recovered.RestartRequired)
	assertNoEvidence(t, options.ConfigDir)
}
func TestReactivationKeepsDeactivateEvidenceUntilVerified(t *testing.T) {
	options := integration.Options{ConfigDir: filepath.Join(t.TempDir(), "codex")}
	pkg, err := RenderPlan("v0.0.0", modelplan.PlanMedium)
	require(t, err == nil)
	writePackage(t, options.ConfigDir, pkg)
	root, err := OpenRoot(context.Background(), options, false)
	require(t, err == nil)
	evidence := activationEvidence(pkg, "deactivate")
	require(t, root.MarkActivationPending(evidence.body) == nil)
	require(t, root.Close() == nil)

	fake := &fakeCodexCLI{fail: map[string]error{"[plugin marketplace list --json]": errors.New("inspection")}}
	_, err = fakeActivationIntegration(fake).Reinstall(context.Background(), options)
	require(t, errors.Is(err, integration.ErrRecovery))
	root, err = OpenRoot(context.Background(), options, false)
	require(t, err == nil)
	body, present, readErr := root.ActivationPending()
	require(t, root.Close() == nil)
	if readErr != nil || !present || !bytes.Equal(body, evidence.body) {
		t.Fatalf("deactivate evidence after failed reactivation = %q, present=%t, err=%v", body, present, readErr)
	}
}
func TestUninstallRejectsReplacedIdentityAndRetainsBackup(t *testing.T) {
	options := integration.Options{ConfigDir: filepath.Join(t.TempDir(), "codex")}
	s := NewIntegration()
	mustInstall(t, s, options)
	pkg, err := Render("v0.0.0")
	require(t, err == nil)
	body := artifact(t, pkg, "AGENTS.md").Bytes
	s.checkpoint = func(point, name string) error {
		if point == "before-backup" && name == "AGENTS.md" {
			replacement := filepath.Join(options.ConfigDir, "replacement")
			if err := os.WriteFile(replacement, body, 0o600); err != nil {
				return err
			}
			return os.Rename(replacement, filepath.Join(options.ConfigDir, name))
		}
		return nil
	}
	_, err = s.Uninstall(context.Background(), options)
	require(t, errors.Is(err, integration.ErrRecovery))
	b, err := os.ReadFile(filepath.Join(options.ConfigDir, "AGENTS.md"))
	require(t, err == nil && string(b) == string(body))
	assertPending(t, options.ConfigDir)
}
func TestUninstallFailedRestoreAndSymlinksRemainRecoveryOrDrift(t *testing.T) {
	t.Run("restore", func(t *testing.T) {
		options := integration.Options{ConfigDir: filepath.Join(t.TempDir(), "codex")}
		s := NewIntegration()
		mustInstall(t, s, options)
		s.checkpoint = func(point, name string) error {
			if point == "removed" && name == "AGENTS.md" {
				_ = os.WriteFile(filepath.Join(options.ConfigDir, name), []byte("racer"), 0o600)
				return errors.New("stop")
			}
			return nil
		}
		_, err := s.Uninstall(context.Background(), options)
		require(t, errors.Is(err, integration.ErrRecovery))
		_, err = os.Stat(filepath.Join(options.ConfigDir, "AGENTS.md.vgxness-remove"))
		require(t, err == nil)
		assertPending(t, options.ConfigDir)
	})
	t.Run("symlink", func(t *testing.T) {
		options := integration.Options{ConfigDir: filepath.Join(t.TempDir(), "codex")}
		s := NewIntegration()
		mustInstall(t, s, options)
		require(t, os.Remove(filepath.Join(options.ConfigDir, "agents", "explore.toml")) == nil)
		if err := os.Symlink("missing", filepath.Join(options.ConfigDir, "agents", "explore.toml")); err != nil {
			if errors.Is(err, os.ErrPermission) {
				t.Skip("symlink privilege unavailable")
			}
			t.Fatal(err)
		}
		got, err := s.Status(context.Background(), options)
		require(t, err == nil && got.State == integration.StateDrifted)
	})
	t.Run("agents-dir", func(t *testing.T) {
		options := integration.Options{ConfigDir: filepath.Join(t.TempDir(), "codex")}
		s := NewIntegration()
		mustInstall(t, s, options)
		pkg, _ := Render("v0.0.0")
		for _, item := range pkg.Artifacts {
			if filepath.Dir(item.Path) == "agents" {
				require(t, os.Remove(filepath.Join(options.ConfigDir, item.Path)) == nil)
			}
		}
		require(t, os.Remove(filepath.Join(options.ConfigDir, "agents")) == nil)
		if err := os.Symlink("missing", filepath.Join(options.ConfigDir, "agents")); err != nil {
			if errors.Is(err, os.ErrPermission) {
				t.Skip("symlink privilege unavailable")
			}
			t.Fatal(err)
		}
		got, err := s.Status(context.Background(), options)
		require(t, err == nil && got.State == integration.StateDrifted)
	})
}
func TestReinstallAbsentIsInvalid(t *testing.T) {
	_, err := NewIntegration().Reinstall(context.Background(), integration.Options{ConfigDir: filepath.Join(t.TempDir(), "codex")})
	require(t, errors.Is(err, integration.ErrInvalid))
}
func assertPending(t *testing.T, root string) {
	_, err := os.Stat(filepath.Join(root, ".vgxness-pending"))
	require(t, err == nil)
}
func assertNoEvidence(t *testing.T, root string) {
	assertAbsent := func(name string) {
		_, err := os.Stat(filepath.Join(root, name))
		require(t, errors.Is(err, os.ErrNotExist))
	}
	assertAbsent(".vgxness-pending")
	pkg, _ := Render("v0.0.0")
	for _, item := range pkg.Artifacts {
		assertAbsent(item.Path + ".vgxness-stage")
		assertAbsent(item.Path + ".vgxness-remove")
	}
}
func mustInstall(t *testing.T, s *Integration, options integration.Options) {
	_, err := s.Install(context.Background(), options)
	require(t, err == nil)
}
func require(t *testing.T, ok bool) {
	t.Helper()
	if !ok {
		t.Fatal("requirement failed")
	}
}
