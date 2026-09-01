package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/sdd"
)

type fakeCodexCLI struct {
	market, plugin bool
	enabled        bool
	root           string
	version        string
	fail           map[string]error
	after          map[string]error
	afterMarket    error
	marketplaceAdd error
	afterMutation  func(string)
	calls          []string
}

func (f *fakeCodexCLI) run(_ context.Context, _ string, args, _ []string) ([]byte, error) {
	key := fmt.Sprint(args)
	f.calls = append(f.calls, key)
	if err := f.fail[key]; err != nil {
		return nil, err
	}
	if strings.HasPrefix(key, "[plugin marketplace add ") && f.marketplaceAdd != nil {
		return nil, f.marketplaceAdd
	}
	switch key {
	case "[plugin list --json]":
		installed := []any{}
		if f.plugin {
			version := f.version
			if version == "" {
				version = "0.0.0"
			}
			installed = append(installed, map[string]any{"pluginId": pluginID, "name": marketplaceName, "marketplaceName": marketplaceName, "version": version, "installed": true, "enabled": f.enabled})
		}
		return json.Marshal(map[string]any{"installed": installed})
	case "[plugin marketplace list --json]":
		markets := []any{}
		if f.market {
			markets = append(markets, map[string]any{"name": marketplaceName, "root": f.root})
		}
		return json.Marshal(map[string]any{"marketplaces": markets})
	case "[plugin add vgxness@vgxness --json]":
		f.plugin, f.enabled = true, true
	case "[plugin remove vgxness@vgxness --json]":
		f.plugin = false
	case "[plugin marketplace remove vgxness --json]":
		f.market = false
	}
	if strings.HasPrefix(key, "[plugin marketplace add ") {
		f.market = true
		if f.afterMutation != nil {
			f.afterMutation(key)
		}
		if f.afterMarket != nil {
			return nil, f.afterMarket
		}
	}
	if (key == "[plugin add vgxness@vgxness --json]" || key == "[plugin remove vgxness@vgxness --json]" || key == "[plugin marketplace remove vgxness --json]") && f.afterMutation != nil {
		f.afterMutation(key)
	}
	if err := f.after[key]; err != nil {
		return nil, err
	}
	return []byte(`{}`), nil
}

func TestCodexActivationRecoveryEvidenceSurvivesAbruptCLIMutations(t *testing.T) {
	for _, test := range []struct {
		name       string
		panicAfter string
		uninstall  bool
	}{
		{"marketplace add", "[plugin marketplace add ", false},
		{"plugin add before readback", "[plugin add vgxness@vgxness --json]", false},
		{"deactivation plugin remove", "[plugin remove vgxness@vgxness --json]", true},
		{"deactivation marketplace remove", "[plugin marketplace remove vgxness --json]", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "codex")
			fake := &fakeCodexCLI{fail: map[string]error{}, after: map[string]error{}, root: root}
			s := fakeActivationIntegration(fake)
			if test.uninstall {
				if _, err := s.Install(context.Background(), integration.Options{ConfigDir: root}); err != nil {
					t.Fatal(err)
				}
			}
			fake.afterMutation = func(call string) {
				if strings.HasPrefix(call, test.panicAfter) {
					panic("abrupt interruption")
				}
			}
			func() {
				defer func() {
					if recover() == nil {
						t.Fatal("expected interruption")
					}
				}()
				if test.uninstall {
					_, _ = s.Uninstall(context.Background(), integration.Options{ConfigDir: root})
				} else {
					_, _ = s.Install(context.Background(), integration.Options{ConfigDir: root})
				}
			}()
			fake.afterMutation = nil
			markerPath := filepath.Join(root, ".vgxness-activation-pending")
			marker, err := os.ReadFile(markerPath)
			if err != nil || !strings.Contains(string(marker), "sha256=") {
				t.Fatalf("durable activation marker=%q err=%v", marker, err)
			}
			if _, err := s.Status(context.Background(), integration.Options{ConfigDir: root}); !errors.Is(err, integration.ErrRecovery) {
				t.Fatalf("Status error=%v, want recovery pending", err)
			}
			var result integration.Result
			if test.uninstall {
				result, err = s.Uninstall(context.Background(), integration.Options{ConfigDir: root})
			} else {
				result, err = s.Reinstall(context.Background(), integration.Options{ConfigDir: root})
			}
			if err != nil || result.State != map[bool]integration.State{true: integration.StateAbsent, false: integration.StateInstalled}[test.uninstall] {
				t.Fatalf("recovery=%+v err=%v", result, err)
			}
			if _, err := os.Stat(markerPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("activation marker retained: %v", err)
			}
		})
	}
}

func TestUninstallCompletesActivateEvidenceWithoutReinstalling(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*fakeCodexCLI, *Root, Package) error
		want  error
	}{
		{"marketplace-only interruption", func(f *fakeCodexCLI, _ *Root, _ Package) error {
			f.market, f.plugin = true, false
			return nil
		}, nil},
		{"plugin-only", func(f *fakeCodexCLI, _ *Root, _ Package) error {
			f.market, f.plugin = false, true
			return nil
		}, nil},
		{"foreign identity", func(f *fakeCodexCLI, _ *Root, _ Package) error {
			f.root, f.market, f.plugin = f.root+"-foreign", true, false
			return nil
		}, integration.ErrRecovery},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "codex")
			options := integration.Options{ConfigDir: path}
			fake := &fakeCodexCLI{fail: map[string]error{}, after: map[string]error{}, root: path}
			s := fakeActivationIntegration(fake)
			if _, err := s.Install(context.Background(), options); err != nil {
				t.Fatal(err)
			}
			root, err := OpenRoot(context.Background(), options, false)
			if err != nil {
				t.Fatal(err)
			}
			pkg, err := RenderPlan("v0.0.0", sdd.PlanMedium)
			if err == nil {
				err = test.setup(fake, root, pkg)
			}
			if err == nil {
				err = root.MarkActivationPending(activationEvidence(pkg, "activate").body)
			}
			if closeErr := root.Close(); err == nil {
				err = closeErr
			}
			if err != nil {
				t.Fatal(err)
			}
			fake.calls = nil
			result, err := s.Uninstall(context.Background(), options)
			if !errors.Is(err, test.want) || (test.want == nil && result.State != integration.StateAbsent) {
				t.Fatalf("Uninstall=%+v err=%v", result, err)
			}
			marker := filepath.Join(path, activationPendingName)
			if test.want == nil {
				if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) || fake.market || fake.plugin {
					t.Fatalf("partial uninstall marker=%v market=%v plugin=%v", err, fake.market, fake.plugin)
				}
			} else if _, err := os.Stat(marker); err != nil {
				t.Fatalf("foreign marker not retained: %v", err)
			} else {
				for _, call := range fake.calls {
					if strings.Contains(call, " remove ") {
						t.Fatalf("foreign state was modified: %v", fake.calls)
					}
				}
			}
		})
	}
}

func TestUninstallAfterMarketplaceAddInterruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codex")
	options := integration.Options{ConfigDir: path}
	fake := &fakeCodexCLI{fail: map[string]error{}, after: map[string]error{}, root: path}
	s := fakeActivationIntegration(fake)
	fake.afterMutation = func(call string) {
		if strings.HasPrefix(call, "[plugin marketplace add ") {
			panic("abrupt interruption")
		}
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected interruption")
			}
		}()
		_, _ = s.Install(context.Background(), options)
	}()
	fake.afterMutation = nil
	result, err := s.Uninstall(context.Background(), options)
	if err != nil || result.State != integration.StateAbsent || fake.market || fake.plugin {
		t.Fatalf("Uninstall=%+v err=%v market=%v plugin=%v", result, err, fake.market, fake.plugin)
	}
}

func TestReinstallJournalsExactArtifactsBeforeEveryActivationMutation(t *testing.T) {
	for _, test := range []struct {
		plan     sdd.Plan
		mutation string
	}{
		{sdd.PlanLow, "[plugin marketplace add "}, {sdd.PlanLow, "[plugin add vgxness@vgxness --json]"},
		{sdd.PlanMedium, "[plugin marketplace add "}, {sdd.PlanMedium, "[plugin add vgxness@vgxness --json]"},
		{sdd.PlanHigh, "[plugin marketplace add "}, {sdd.PlanHigh, "[plugin add vgxness@vgxness --json]"},
		{sdd.PlanUltra, "[plugin marketplace add "}, {sdd.PlanUltra, "[plugin add vgxness@vgxness --json]"},
	} {
		t.Run(string(test.plan)+test.mutation, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "codex")
			fake := &fakeCodexCLI{fail: map[string]error{}, after: map[string]error{}, root: path}
			s := fakeActivationIntegration(fake)
			if _, err := s.Install(context.Background(), integration.Options{ConfigDir: path, ModelPlan: test.plan}); err != nil {
				t.Fatal(err)
			}
			fake.market, fake.plugin = false, false
			fake.afterMutation = func(call string) {
				if !strings.HasPrefix(call, test.mutation) {
					return
				}
				if _, err := os.Stat(filepath.Join(path, activationPendingName)); err != nil {
					t.Fatalf("mutation without durable evidence: %v", err)
				}
				panic("abrupt interruption")
			}
			func() {
				defer func() { _ = recover() }()
				_, _ = s.Reinstall(context.Background(), integration.Options{ConfigDir: path})
			}()
			fake.afterMutation = nil
			if result, err := s.Reinstall(context.Background(), integration.Options{ConfigDir: path}); err != nil || result.State != integration.StateInstalled {
				t.Fatalf("no-option retry=%+v err=%v", result, err)
			}
		})
	}
}

func TestPendingJournalRecoversExactPlanBeforeAnyArtifact(t *testing.T) {
	for _, plan := range []sdd.Plan{sdd.PlanLow, sdd.PlanHigh, sdd.PlanUltra} {
		t.Run(string(plan), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "codex")
			s := fakeActivationIntegration(&fakeCodexCLI{fail: map[string]error{}, after: map[string]error{}, root: path})
			s.checkpoint = func(point, _ string) error {
				if point == "pending" {
					panic("interrupt")
				}
				return nil
			}
			func() {
				defer func() { _ = recover() }()
				_, _ = s.Install(context.Background(), integration.Options{ConfigDir: path, ModelPlan: plan})
			}()
			pkg, _ := RenderPlan("v0.0.0", plan)
			body, err := os.ReadFile(filepath.Join(path, pendingName))
			if err != nil || string(body) != string(pendingEvidence(pkg.SHA256)) {
				t.Fatalf("marker=%q err=%v", body, err)
			}
			if _, err := os.Stat(filepath.Join(path, "AGENTS.md")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("artifact exists: %v", err)
			}
			s.checkpoint = nil
			result, err := s.Reinstall(context.Background(), integration.Options{ConfigDir: path})
			if err != nil || result.ModelPlan != plan {
				t.Fatalf("recovered=%+v err=%v", result, err)
			}
		})
	}
}

func TestPendingJournalRejectsAmbiguousLegacyAndTamperedEvidenceBeforeMutation(t *testing.T) {
	t.Run("legacy empty requires an explicit plan", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "codex")
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, pendingName), []byte("codex-pending\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		fake := &fakeCodexCLI{fail: map[string]error{}, after: map[string]error{}, root: path}
		s := fakeActivationIntegration(fake)
		before := rootEntryNames(t, path)
		if _, err := s.Reinstall(context.Background(), integration.Options{ConfigDir: path}); !errors.Is(err, integration.ErrRecovery) {
			t.Fatalf("no-option legacy recovery error=%v", err)
		}
		if after := rootEntryNames(t, path); after != before || len(fake.calls) != 0 {
			t.Fatalf("ambiguous legacy recovery mutated root before=%q after=%q calls=%v", before, after, fake.calls)
		}
		result, err := s.Reinstall(context.Background(), integration.Options{ConfigDir: path, ModelPlan: sdd.PlanHigh})
		if err != nil || result.State != integration.StateInstalled || result.ModelPlan != sdd.PlanHigh {
			t.Fatalf("explicit legacy recovery=%+v err=%v", result, err)
		}
	})

	for _, test := range []struct {
		name string
		body []byte
	}{
		{"unknown sha", pendingEvidence(strings.Repeat("f", 64))},
		{"malformed", []byte("codex-pending-v2\nsha256=not-a-sha\n")},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "codex")
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(path, pendingName), test.body, 0o600); err != nil {
				t.Fatal(err)
			}
			fake := &fakeCodexCLI{fail: map[string]error{}, after: map[string]error{}, root: path}
			s := fakeActivationIntegration(fake)
			before := rootEntryNames(t, path)
			if _, err := s.Reinstall(context.Background(), integration.Options{ConfigDir: path}); !errors.Is(err, integration.ErrRecovery) {
				t.Fatalf("Reinstall error=%v", err)
			}
			if after := rootEntryNames(t, path); after != before || len(fake.calls) != 0 {
				t.Fatalf("tampered recovery mutated root before=%q after=%q calls=%v", before, after, fake.calls)
			}
		})
	}
}

func TestPendingJournalRejectsBoundArtifactAndSidecarConflictsBeforeMutation(t *testing.T) {
	pkg, err := RenderPlan("v0.0.0", sdd.PlanHigh)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		path string
	}{
		{"foreign artifact", "AGENTS.md"},
		{"mismatched sidecar", "AGENTS.md.vgxness-stage"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "codex")
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(path, pendingName), pendingEvidence(pkg.SHA256), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(path, test.path), []byte("foreign\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			fake := &fakeCodexCLI{fail: map[string]error{}, after: map[string]error{}, root: path}
			s := fakeActivationIntegration(fake)
			before := rootEntryNames(t, path)
			if _, err := s.Reinstall(context.Background(), integration.Options{ConfigDir: path}); !errors.Is(err, integration.ErrRecovery) {
				t.Fatalf("Reinstall error=%v", err)
			}
			if after := rootEntryNames(t, path); after != before || len(fake.calls) != 0 {
				t.Fatalf("conflicting recovery mutated root before=%q after=%q calls=%v", before, after, fake.calls)
			}
		})
	}
}

func rootEntryNames(t *testing.T, path string) string {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return strings.Join(names, "\n")
}

func TestCodexActivationEvidenceRejectsTamperingAndResumesPluginOnly(t *testing.T) {
	rootPath := filepath.Join(t.TempDir(), "codex")
	options := integration.Options{ConfigDir: rootPath}
	fake := &fakeCodexCLI{fail: map[string]error{}, after: map[string]error{}, root: rootPath}
	s := fakeActivationIntegration(fake)
	if _, err := s.Install(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	root, err := OpenRoot(context.Background(), options, false)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := RenderPlan("v0.0.0", sdd.PlanMedium)
	if err != nil {
		t.Fatal(err)
	}
	fake.market = false // exact plugin-only interruption state.
	evidence := activationEvidence(pkg, "activate")
	if err := root.MarkActivationPending(evidence.body); err != nil {
		t.Fatal(err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	if recovered, err := s.Reinstall(context.Background(), options); err != nil || recovered.State != integration.StateInstalled || !fake.market || !fake.plugin {
		t.Fatalf("plugin-only recovery=%+v err=%v market=%v plugin=%v", recovered, err, fake.market, fake.plugin)
	}
	if err := os.WriteFile(filepath.Join(rootPath, activationPendingName), []byte("tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake.calls = nil
	if _, err := s.Reinstall(context.Background(), options); !errors.Is(err, integration.ErrRecovery) {
		t.Fatalf("tampered Reinstall error=%v", err)
	}
	for _, call := range fake.calls {
		if strings.Contains(call, " remove ") {
			t.Fatalf("tampering caused destructive call: %v", fake.calls)
		}
	}
}

func fakeActivationIntegration(f *fakeCodexCLI) *Integration {
	s := NewIntegration()
	s.runner, s.codexBin = f.run, "fake"
	return s
}

func TestCommandPassesExactlyOneManagedCodexHome(t *testing.T) {
	var got []string
	s := NewIntegration()
	s.runner = func(_ context.Context, _ string, _ []string, env []string) ([]byte, error) {
		got = append([]string(nil), env...)
		return nil, nil
	}
	_, err := s.command(context.Background(), &Root{Path: "/managed/codex"}, "version")
	if err != nil {
		t.Fatal(err)
	}
	homes := 0
	for _, value := range got {
		if value == "CODEX_HOME=/managed/codex" {
			homes++
		} else if strings.HasPrefix(value, "CODEX_HOME=") {
			t.Fatalf("unexpected CODEX_HOME %q", value)
		}
	}
	if homes != 1 {
		t.Fatalf("CODEX_HOME values=%v", got)
	}
}

func TestCodexActivationLifecycleUsesExactCLITransaction(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex")
	options := integration.Options{ConfigDir: root}
	fake := &fakeCodexCLI{fail: map[string]error{}, after: map[string]error{}, root: root}
	s := fakeActivationIntegration(fake)
	installed, err := s.Install(context.Background(), options)
	if err != nil || installed.State != integration.StateInstalled || !fake.market || !fake.plugin || !fake.enabled {
		t.Fatalf("install=%+v err=%v", installed, err)
	}
	again, err := s.Reinstall(context.Background(), options)
	if err != nil || again.Changed {
		t.Fatalf("idempotent reinstall=%+v err=%v", again, err)
	}
	fake.market, fake.plugin = false, false
	status, err := s.Status(context.Background(), options)
	if err != nil || status.State != integration.StatePartial {
		t.Fatalf("artifacts-only status=%+v err=%v", status, err)
	}
	repaired, err := s.Reinstall(context.Background(), options)
	if err != nil || repaired.State != integration.StateInstalled || !fake.market || !fake.plugin {
		t.Fatalf("repair=%+v err=%v", repaired, err)
	}
	if _, err := s.Uninstall(context.Background(), options); err != nil || fake.market || fake.plugin {
		t.Fatalf("uninstall err=%v market=%v plugin=%v", err, fake.market, fake.plugin)
	}
	for i, call := range fake.calls {
		if call == "[plugin remove vgxness@vgxness --json]" && (i+1 >= len(fake.calls) || fake.calls[i+1] != "[plugin marketplace remove vgxness --json]") {
			t.Fatal("plugin removal was not followed by marketplace removal")
		}
	}
}

func TestCodexActivationFailureDriftAndRecoveryAreRetained(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*fakeCodexCLI)
		want  error
	}{
		{"marketplace-add-does-not-rollback-foreign", func(f *fakeCodexCLI) { f.marketplaceAdd = errors.New("market") }, integration.ErrRecovery},
		{"plugin-add-rolls-back", func(f *fakeCodexCLI) { f.fail["[plugin add vgxness@vgxness --json]"] = errors.New("add") }, integration.ErrRecovery},
		{"rollback-failure-is-recovery", func(f *fakeCodexCLI) {
			f.fail["[plugin add vgxness@vgxness --json]"] = errors.New("add")
			f.fail["[plugin marketplace remove vgxness --json]"] = errors.New("remove")
		}, integration.ErrRecovery},
		{"foreign-state-blocks", func(f *fakeCodexCLI) { f.market, f.plugin, f.enabled = true, true, false }, integration.ErrConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "codex")
			f := &fakeCodexCLI{fail: map[string]error{}, after: map[string]error{}, root: root}
			test.setup(f)
			s := fakeActivationIntegration(f)
			_, err := s.Install(context.Background(), integration.Options{ConfigDir: root})
			if !errors.Is(err, test.want) {
				t.Fatalf("err=%v want=%v", err, test.want)
			}
			if test.name == "marketplace-add-does-not-rollback-foreign" {
				for _, call := range f.calls {
					if call == "[plugin marketplace remove vgxness --json]" {
						t.Fatal("removed marketplace after failed add")
					}
				}
			}
		})
	}
}

func TestCodexActivationMutationThenErrorRollsBackOnlyProvenCreatedState(t *testing.T) {
	for _, test := range []struct {
		name      string
		mutation  string
		ambiguous bool
		rollback  bool
		wantCalls []string
	}{
		{"marketplace exact", "[plugin marketplace add ", false, false, []string{"[plugin marketplace remove vgxness --json]"}},
		{"plugin exact", "[plugin add vgxness@vgxness --json]", false, false, []string{"[plugin remove vgxness@vgxness --json]", "[plugin marketplace remove vgxness --json]"}},
		{"marketplace ambiguous", "[plugin marketplace add ", true, false, nil},
		{"plugin rollback failure", "[plugin add vgxness@vgxness --json]", false, true, []string{"[plugin remove vgxness@vgxness --json]"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "codex")
			f := &fakeCodexCLI{fail: map[string]error{}, after: map[string]error{}, root: root}
			if strings.HasPrefix(test.mutation, "[plugin marketplace add ") {
				f.afterMarket = errors.New("market mutated")
			} else {
				f.after[test.mutation] = errors.New("plugin mutated")
			}
			if test.ambiguous {
				f.root = root + "-foreign"
			}
			if test.rollback {
				f.fail["[plugin remove vgxness@vgxness --json]"] = errors.New("remove")
			}
			s := fakeActivationIntegration(f)
			_, err := s.Install(context.Background(), integration.Options{ConfigDir: root})
			if !errors.Is(err, integration.ErrRecovery) {
				t.Fatalf("Install error=%v, want recovery", err)
			}
			for _, want := range test.wantCalls {
				found := false
				for _, got := range f.calls {
					found = found || got == want
				}
				if !found {
					t.Fatalf("calls=%v missing %s", f.calls, want)
				}
			}
			if test.ambiguous {
				for _, got := range f.calls {
					if got == "[plugin marketplace remove vgxness --json]" || got == "[plugin remove vgxness@vgxness --json]" {
						t.Fatalf("destructive cleanup after ambiguous mutation: %v", f.calls)
					}
				}
				return
			}
			if test.rollback {
				return
			}
			f.after, f.afterMarket = map[string]error{}, nil
			installed, retryErr := s.Install(context.Background(), integration.Options{ConfigDir: root})
			if retryErr != nil || installed.State != integration.StateInstalled || !f.market || !f.plugin || !f.enabled {
				t.Fatalf("safe retry=%+v err=%v market=%v plugin=%v enabled=%v", installed, retryErr, f.market, f.plugin, f.enabled)
			}
		})
	}
}

func TestIntegrationRefreshesPluginVersionWithExactRemoveAddAndVersionBReadback(t *testing.T) {
	ctx := context.Background()
	rootPath := filepath.Join(t.TempDir(), "codex")
	root, err := OpenRoot(ctx, integration.Options{ConfigDir: rootPath}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	f := &fakeCodexCLI{fail: map[string]error{}, after: map[string]error{}, root: rootPath, version: "1.0.0"}
	s := fakeActivationIntegration(f)
	pkgA, err := RenderPlan("v1.0.0", sdd.PlanMedium)
	if err != nil {
		t.Fatal(err)
	}
	stateA, err := inspectRoot(ctx, root, pkgA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.installAndActivate(ctx, root, pkgA, stateA); err != nil {
		t.Fatalf("install A: %v", err)
	}
	stateA, err = inspectRoot(ctx, root, pkgA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.uninstall(ctx, root, pkgA, stateA); err != nil {
		t.Fatalf("remove A: %v", err)
	}
	pkgB, err := RenderPlan("v2.0.0", sdd.PlanMedium)
	if err != nil {
		t.Fatal(err)
	}
	f.version = "2.0.0"
	stateB, err := inspectRoot(ctx, root, pkgB)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := s.installAndActivate(ctx, root, pkgB, stateB)
	if err != nil || installed.State != integration.StateInstalled || !f.plugin || !f.enabled || f.version != "2.0.0" {
		t.Fatalf("install B=%+v err=%v plugin=%v enabled=%v version=%s", installed, err, f.plugin, f.enabled, f.version)
	}
	var mutations []string
	for _, call := range f.calls {
		if strings.Contains(call, " marketplace add ") || call == "[plugin add vgxness@vgxness --json]" || call == "[plugin remove vgxness@vgxness --json]" || call == "[plugin marketplace remove vgxness --json]" {
			mutations = append(mutations, call)
		}
	}
	want := []string{
		"[plugin marketplace add " + root.Path + " --json]", "[plugin add vgxness@vgxness --json]",
		"[plugin remove vgxness@vgxness --json]", "[plugin marketplace remove vgxness --json]",
		"[plugin marketplace add " + root.Path + " --json]", "[plugin add vgxness@vgxness --json]",
	}
	if strings.Join(mutations, "\n") != strings.Join(want, "\n") {
		t.Fatalf("mutation order=%v want=%v", mutations, want)
	}
	stateB, err = inspectRoot(ctx, root, pkgB)
	if err != nil || stateB.result.State != integration.StateInstalled {
		t.Fatalf("version B artifact readback=%+v err=%v", stateB.result, err)
	}
	activation, err := s.activation(ctx, root)
	if err != nil || activation != activationActive {
		t.Fatalf("version B activation readback=%v err=%v", activation, err)
	}
}
