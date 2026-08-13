package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/sdd"
)

func TestKnownPackagesRecognizeActiveV6ForEveryPlan(t *testing.T) {
	known, err := knownPackages()
	if err != nil {
		t.Fatal(err)
	}
	for _, plan := range []sdd.Plan{sdd.PlanLow, sdd.PlanMedium, sdd.PlanHigh, sdd.PlanUltra} {
		v6, err := renderActiveV6("v0.0.0", plan)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, pkg := range known {
			found = found || pkg.SHA256 == v6.SHA256
		}
		if !found {
			t.Errorf("known packages omit active v6 %s", plan)
		}
	}
}

func TestIntegrationReinstallSwitchesAndPersistsModelPlan(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	service := NewIntegration()
	medium := integration.Options{ConfigDir: root, ModelPlan: sdd.PlanMedium}
	installed, err := service.Install(context.Background(), medium)
	require(t, err == nil && installed.ModelPlan == sdd.PlanMedium)

	ultra := integration.Options{ConfigDir: root, ModelPlan: sdd.PlanUltra}
	switched, err := service.Reinstall(context.Background(), ultra)
	require(t, err == nil && switched.State == integration.StateInstalled && switched.Changed && switched.ModelPlan == sdd.PlanUltra)
	general, err := os.ReadFile(filepath.Join(root, "agents", "general.toml"))
	require(t, err == nil && strings.Contains(string(general), `model = "gpt-5.6-sol"`) && strings.Contains(string(general), `model_reasoning_effort = "high"`))
	retained, err := service.Reinstall(context.Background(), integration.Options{ConfigDir: root})
	require(t, err == nil && retained.State == integration.StateInstalled && !retained.Changed && retained.ModelPlan == sdd.PlanUltra)

	status, err := service.Status(context.Background(), integration.Options{ConfigDir: root})
	require(t, err == nil && status.State == integration.StateInstalled && status.ModelPlan == sdd.PlanUltra)
	removed, err := service.Uninstall(context.Background(), integration.Options{ConfigDir: root})
	require(t, err == nil && removed.State == integration.StateAbsent && removed.Changed)
}

func TestIntegrationPreviewReportsRequestedUltraPlan(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	service := NewIntegration()
	medium := integration.Options{ConfigDir: root, ModelPlan: sdd.PlanMedium}
	mustInstall(t, service, medium)

	preview, err := service.Preview(context.Background(), integration.Options{ConfigDir: root, ModelPlan: sdd.PlanUltra})
	require(t, err == nil && preview.State == integration.StatePartial && preview.Changed && preview.RestartRequired && preview.ModelPlan == sdd.PlanUltra)
}

func TestIntegrationPreviewPartialExplicitPlanUsesDesiredPackageIdentity(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	service := NewIntegration()
	mustInstall(t, service, integration.Options{ConfigDir: root, ModelPlan: sdd.PlanMedium})
	require(t, os.Remove(filepath.Join(root, "agents", "general.toml")) == nil)
	want, err := RenderPlan("v0.0.0", sdd.PlanUltra)
	require(t, err == nil)

	preview, err := service.Preview(context.Background(), integration.Options{ConfigDir: root, ModelPlan: sdd.PlanUltra})
	if err != nil || preview.State != integration.StatePartial || !preview.Changed || !preview.RestartRequired || preview.ArtifactSHA256 != want.SHA256 || preview.ArtifactCount != len(want.Artifacts) || preview.ModelPlan != sdd.PlanUltra || preview.ModelProvider == "" {
		t.Fatalf("Preview(partial requested ultra) = %+v, %v", preview, err)
	}
}

func TestIntegrationRejectsModelSlotCustomizationBeforeWriting(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	for _, options := range []integration.Options{
		{ConfigDir: root, ModelEfficient: "openai/custom-fast", ModelBalanced: "openai/custom-balanced", ModelFrontier: "openai/custom-frontier"},
		{ConfigDir: root, ModelEfficient: "openai/custom-fast", ModelBalanced: "anthropic/custom-balanced", ModelFrontier: "acme/custom-frontier"},
		{ConfigDir: root, ModelEfficientEffort: sdd.EffortHigh, ModelBalancedEffort: sdd.EffortHigh, ModelFrontierEffort: sdd.EffortHigh},
		{ConfigDir: root, ModelEfficientEffort: sdd.EffortLow, ModelBalancedEffort: sdd.EffortHigh, ModelFrontierEffort: sdd.EffortUltra},
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
	mustInstall(t, service, integration.Options{ConfigDir: root, ModelPlan: sdd.PlanLow})

	status, err := service.Status(context.Background(), integration.Options{ConfigDir: root, ModelPlan: sdd.PlanUltra})
	if err != nil || status.State != integration.StatePartial || !status.Changed || !status.RestartRequired || status.ModelPlan != sdd.PlanUltra {
		t.Fatalf("Status(requested ultra) = %+v, %v; want changed partial ultra", status, err)
	}
}

func TestIntegrationNoOptionStatusAndReinstallPreserveExactPartialPlan(t *testing.T) {
	for _, plan := range []sdd.Plan{sdd.PlanLow, sdd.PlanHigh, sdd.PlanUltra} {
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
	options := integration.Options{ConfigDir: root, ModelPlan: sdd.PlanUltra}
	pkg, err := RenderPlan("v0.0.0", sdd.PlanUltra)
	require(t, err == nil)
	require(t, os.MkdirAll(root, 0o700) == nil)
	require(t, os.WriteFile(filepath.Join(root, ".vgxness-pending"), []byte("codex-pending\n"), 0o600) == nil)
	require(t, os.WriteFile(filepath.Join(root, "AGENTS.md.vgxness-remove"), artifact(t, pkg, "AGENTS.md").Bytes, 0o600) == nil)

	result, err := NewIntegration().Reinstall(context.Background(), options)
	if err != nil || result.State != integration.StateInstalled || !result.Changed || result.ModelPlan != sdd.PlanUltra {
		t.Fatalf("Reinstall(shared remove sidecar) = %+v, %v; want changed installed ultra", result, err)
	}
	assertNoEvidence(t, root)
}

func TestIntegrationReinstallAbsentInstallsExplicitRequestedPlan(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	service := NewIntegration()
	mustInstall(t, service, integration.Options{ConfigDir: root, ModelPlan: sdd.PlanLow})
	_, err := service.Uninstall(context.Background(), integration.Options{ConfigDir: root, ModelPlan: sdd.PlanLow})
	require(t, err == nil)

	result, err := service.Reinstall(context.Background(), integration.Options{ConfigDir: root, ModelPlan: sdd.PlanUltra})
	if err != nil || result.State != integration.StateInstalled || !result.Changed || result.ModelPlan != sdd.PlanUltra {
		t.Fatalf("Reinstall(absent requested ultra) = %+v, %v; want changed installed ultra", result, err)
	}
}

func TestIntegrationReinstallMigratesLegacyStaticPackage(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	legacy, err := renderLegacy("v0.0.0")
	require(t, err == nil)
	writePackage(t, root, legacy)

	high := integration.Options{ConfigDir: root, ModelPlan: sdd.PlanHigh}
	result, err := NewIntegration().Reinstall(context.Background(), high)
	require(t, err == nil && result.State == integration.StateInstalled && result.Changed && result.ModelPlan == sdd.PlanHigh)
	general, err := os.ReadFile(filepath.Join(root, "agents", "general.toml"))
	require(t, err == nil && strings.Contains(string(general), `model = "gpt-5.6-sol"`) && strings.Contains(string(general), `model_reasoning_effort = "high"`))
}

func TestIntegrationReinstallsOnlyCompletePreConsolidationV4Package(t *testing.T) {
	for name, mutate := range map[string]func(*Package){
		"exact":   func(pkg *Package) {},
		"mutated": func(pkg *Package) { pkg.Artifacts[1].Bytes[0] ^= 1 },
		"mixed": func(pkg *Package) {
			current, err := RenderPlan("v0.0.0", sdd.PlanMedium)
			require(t, err == nil)
			pkg.Artifacts[0] = current.Artifacts[0]
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "codex")
			pkg, err := renderPreConsolidationV4("v0.0.0", sdd.PlanMedium)
			require(t, err == nil)
			mutate(&pkg)
			before := append([]byte(nil), pkg.Artifacts[1].Bytes...)
			writePackage(t, root, pkg)
			result, reinstallErr := NewIntegration().Reinstall(context.Background(), integration.Options{ConfigDir: root, ModelPlan: sdd.PlanMedium})
			if name == "exact" {
				require(t, reinstallErr == nil && result.State == integration.StateInstalled && result.Changed)
				return
			}
			after, readErr := os.ReadFile(filepath.Join(root, pkg.Artifacts[1].Path))
			require(t, errors.Is(reinstallErr, integration.ErrDrift) && readErr == nil && string(after) == string(before))
		})
	}
}

func TestIntegrationPlanSwitchBlocksUnknownDrift(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	service := NewIntegration()
	mustInstall(t, service, integration.Options{ConfigDir: root, ModelPlan: sdd.PlanLow})
	require(t, os.WriteFile(filepath.Join(root, "agents", "general.toml"), []byte("user change\n"), 0o600) == nil)

	_, err := service.Reinstall(context.Background(), integration.Options{ConfigDir: root, ModelPlan: sdd.PlanUltra})
	require(t, errors.Is(err, integration.ErrDrift))
	body, readErr := os.ReadFile(filepath.Join(root, "agents", "general.toml"))
	require(t, readErr == nil && string(body) == "user change\n")
}

func TestIntegrationRecoveryUsesInstalledPlanForSharedSidecar(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	service := NewIntegration()
	low := integration.Options{ConfigDir: root, ModelPlan: sdd.PlanLow}
	mustInstall(t, service, low)
	require(t, os.WriteFile(filepath.Join(root, ".vgxness-pending"), []byte("codex-pending\n"), 0o600) == nil)
	require(t, os.Link(filepath.Join(root, "AGENTS.md"), filepath.Join(root, "AGENTS.md.vgxness-stage")) == nil)

	result, err := service.Reinstall(context.Background(), integration.Options{ConfigDir: root, ModelPlan: sdd.PlanUltra})
	if err != nil || result.State != integration.StateInstalled || result.ModelPlan != sdd.PlanLow {
		t.Fatalf("recovery result = %+v, err = %v", result, err)
	}
	assertNoEvidence(t, root)
}

func TestIntegrationInstallAndIdempotence(t *testing.T) {
	options := integration.Options{ConfigDir: filepath.Join(t.TempDir(), "codex")}
	service := NewIntegration()
	before, err := service.Status(context.Background(), options)
	require(t, err == nil && before.State == integration.StateAbsent && before.ArtifactCount == 15)
	installed, err := service.Install(context.Background(), options)
	require(t, err == nil && installed.State == integration.StateInstalled && installed.Changed && installed.RestartRequired)
	again, err := service.Install(context.Background(), options)
	require(t, err == nil && !again.Changed && !again.RestartRequired && again.State == integration.StateInstalled)
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
	require(t, err == nil && result.State == integration.StateInstalled && result.Changed)
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
					if syncs == 5 {
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
	require(t, status.State == integration.StateInstalled && errors.Is(err, integration.ErrRecovery))
	assertPending(t, options.ConfigDir)
}
func TestManagedLayoutExcludesPluginArtifacts(t *testing.T) {
	layout, err := NewIntegration().ManagedLayout(context.Background(), integration.Options{ConfigDir: filepath.Join(t.TempDir(), "codex")})
	require(t, err == nil && len(layout.Artifacts) == 15)
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
	if !ok {
		t.Fatal("requirement failed")
	}
}
