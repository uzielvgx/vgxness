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

func TestRunCodexFailsClosed(t *testing.T) {
	_, err := runCodex(context.Background(), "/missing/codex", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "codex command failed") {
		t.Fatalf("runCodex() error = %v, want fail-closed command error", err)
	}
}

type fakeCodexCLI struct {
	market, plugin, enabled bool
	root, version           string
	fail                    map[string]error
	output                  map[string][]byte
	mutations               int
	after                   map[string]error
	afterMarket             error
	marketplaceAdd          error
	afterMutation           func(string)
	calls                   []string
}

func (f *fakeCodexCLI) Run(_ context.Context, _ string, args, _ []string) ([]byte, error) {
	key := fmt.Sprint(args)
	f.calls = append(f.calls, key)
	if err := f.fail[key]; err != nil {
		return nil, err
	}
	if strings.HasPrefix(key, "[plugin marketplace add ") && f.marketplaceAdd != nil {
		return nil, f.marketplaceAdd
	}
	if output, ok := f.output[key]; ok {
		return output, nil
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
		f.mutations++
		f.plugin, f.enabled = true, true
	case "[plugin remove vgxness@vgxness --json]":
		f.mutations++
		f.plugin = false
	case "[plugin marketplace remove vgxness --json]":
		f.mutations++
		f.market = false
	}
	if strings.HasPrefix(key, "[plugin marketplace add ") {
		f.mutations++
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

func fakeActivationIntegration(f *fakeCodexCLI) *Integration {
	s := NewIntegration()
	s.runner, s.codexBin = f, "fake"
	return s
}

func TestActivateInspectionFailuresDoNotMutate(t *testing.T) {
	marketList := "[plugin marketplace list --json]"
	pluginList := "[plugin list --json]"
	for name, fake := range map[string]*fakeCodexCLI{
		"marketplace command": {fail: map[string]error{marketList: errors.New("marketplace list")}},
		"plugin command":      {fail: map[string]error{pluginList: errors.New("plugin list")}},
		"marketplace JSON":    {output: map[string][]byte{marketList: []byte(`{`)}},
		"plugin JSON":         {output: map[string][]byte{pluginList: []byte(`{`)}},
	} {
		t.Run(name, func(t *testing.T) {
			root := openActivationRoot(t)
			_, _, _, err := fakeActivationIntegration(fake).activate(context.Background(), root)
			if err == nil || fake.mutations != 0 || fake.market || fake.plugin {
				t.Fatalf("activate() err=%v mutations=%d market=%t plugin=%t; want inspection failure without mutation", err, fake.mutations, fake.market, fake.plugin)
			}
		})
	}
}

func TestActivateRejectsIncompleteInspectionJSONWithoutMutation(t *testing.T) {
	marketList := "[plugin marketplace list --json]"
	pluginList := "[plugin list --json]"
	for name, output := range map[string]map[string][]byte{
		"empty object":              {marketList: []byte(`{}`), pluginList: []byte(`{}`)},
		"null arrays":               {marketList: []byte(`{"marketplaces":null}`), pluginList: []byte(`{"installed":null}`)},
		"omitted marketplace array": {marketList: []byte(`{}`), pluginList: []byte(`{"installed":[]}`)},
		"omitted plugin array":      {marketList: []byte(`{"marketplaces":[]}`), pluginList: []byte(`{}`)},
		"wrong array types":         {marketList: []byte(`{"marketplaces":{}}`), pluginList: []byte(`{"installed":{}}`)},
		"incomplete marketplace":    {marketList: []byte(`{"marketplaces":[{"name":"vgxness"}]}`), pluginList: []byte(`{"installed":[]}`)},
		"incomplete plugin":         {marketList: []byte(`{"marketplaces":[]}`), pluginList: []byte(`{"installed":[{"pluginId":"vgxness@vgxness"}]}`)},
	} {
		t.Run(name, func(t *testing.T) {
			fake := &fakeCodexCLI{output: output}
			root := openActivationRoot(t)
			_, _, _, err := fakeActivationIntegration(fake).activate(context.Background(), root)
			if err == nil || fake.mutations != 0 {
				t.Fatalf("activate() err=%v mutations=%d; want invalid inspection without mutation", err, fake.mutations)
			}
		})
	}
}

func openActivationRoot(t *testing.T) *Root {
	t.Helper()
	root, err := OpenRoot(context.Background(), integration.Options{ConfigDir: t.TempDir()}, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root
}

func TestCodexActivationRecoveryEvidenceSurvivesAbruptCLIMutations(t *testing.T) {
	for _, test := range []struct {
		name, mutation string
		uninstall      bool
	}{
		{"marketplace add", "[plugin marketplace add ", false},
		{"plugin add", "[plugin add vgxness@vgxness --json]", false},
		{"plugin remove", "[plugin remove vgxness@vgxness --json]", true},
		{"marketplace remove", "[plugin marketplace remove vgxness --json]", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "codex")
			options := integration.Options{ConfigDir: path}
			fake := &fakeCodexCLI{fail: map[string]error{}, after: map[string]error{}, root: path}
			s := fakeActivationIntegration(fake)
			if test.uninstall {
				if _, err := s.Install(context.Background(), options); err != nil {
					t.Fatal(err)
				}
			}
			fake.afterMutation = func(call string) {
				if strings.HasPrefix(call, test.mutation) {
					panic("interrupted")
				}
			}
			func() {
				defer func() {
					if recover() == nil {
						t.Fatal("expected interruption")
					}
				}()
				if test.uninstall {
					_, _ = s.Uninstall(context.Background(), options)
				} else {
					_, _ = s.Install(context.Background(), options)
				}
			}()
			fake.afterMutation = nil
			marker := filepath.Join(path, activationPendingName)
			body, err := os.ReadFile(marker)
			if err != nil || !strings.Contains(string(body), "sha256=") {
				t.Fatalf("marker=%q err=%v", body, err)
			}
			if _, err := s.Status(context.Background(), options); !errors.Is(err, integration.ErrRecovery) {
				t.Fatalf("Status=%v", err)
			}
			var result integration.Result
			if test.uninstall {
				result, err = s.Uninstall(context.Background(), options)
			} else {
				result, err = s.Reinstall(context.Background(), options)
			}
			want := integration.StateInstalled
			if test.uninstall {
				want = integration.StateAbsent
			}
			if err != nil || result.State != want {
				t.Fatalf("recovery=%+v err=%v", result, err)
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("marker retained: %v", err)
			}
		})
	}
}

func TestPendingJournalsBindExactPlanAndRejectTamperingBeforeMutation(t *testing.T) {
	for _, plan := range []sdd.Plan{sdd.PlanLow, sdd.PlanHigh, sdd.PlanUltra} {
		t.Run("exact-"+string(plan), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "codex")
			fake := &fakeCodexCLI{fail: map[string]error{}, after: map[string]error{}, root: path}
			s := fakeActivationIntegration(fake)
			s.checkpoint = func(point, _ string) error {
				if point == "pending" {
					panic("interrupted")
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
				t.Fatalf("artifact=%v", err)
			}
			s.checkpoint = nil
			if result, err := s.Reinstall(context.Background(), integration.Options{ConfigDir: path}); err != nil || result.ModelPlan != plan {
				t.Fatalf("recovery=%+v err=%v", result, err)
			}
		})
	}
	for _, body := range [][]byte{[]byte("codex-pending\n"), pendingEvidence(strings.Repeat("f", 64)), []byte("codex-pending-v2\nsha256=bad\n")} {
		path := filepath.Join(t.TempDir(), "codex")
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, pendingName), body, 0o600); err != nil {
			t.Fatal(err)
		}
		fake := &fakeCodexCLI{fail: map[string]error{}, after: map[string]error{}, root: path}
		if _, err := fakeActivationIntegration(fake).Reinstall(context.Background(), integration.Options{ConfigDir: path}); !errors.Is(err, integration.ErrRecovery) || len(fake.calls) != 0 {
			t.Fatalf("tampered recovery err=%v calls=%v", err, fake.calls)
		}
	}
	pkg, err := RenderPlan("v0.0.0", sdd.PlanHigh)
	if err != nil {
		t.Fatal(err)
	}
	for _, conflict := range []string{"AGENTS.md", "AGENTS.md.vgxness-stage"} {
		path := filepath.Join(t.TempDir(), "codex")
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, pendingName), pendingEvidence(pkg.SHA256), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, conflict), []byte("foreign\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		fake := &fakeCodexCLI{fail: map[string]error{}, after: map[string]error{}, root: path}
		if _, err := fakeActivationIntegration(fake).Reinstall(context.Background(), integration.Options{ConfigDir: path}); !errors.Is(err, integration.ErrRecovery) || len(fake.calls) != 0 {
			t.Fatalf("conflict %s err=%v calls=%v", conflict, err, fake.calls)
		}
	}
}

func TestCodexActivationRollbackMutatesOnlyProvenOwnedState(t *testing.T) {
	for _, test := range []struct {
		name, mutation string
		foreign        bool
		want           []string
	}{
		{"marketplace", "[plugin marketplace add ", false, []string{"[plugin marketplace remove vgxness --json]"}},
		{"plugin", "[plugin add vgxness@vgxness --json]", false, []string{"[plugin remove vgxness@vgxness --json]", "[plugin marketplace remove vgxness --json]"}},
		{"foreign marketplace", "[plugin marketplace add ", true, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "codex")
			fake := &fakeCodexCLI{fail: map[string]error{}, after: map[string]error{}, root: path}
			if test.foreign {
				fake.root = path + "-foreign"
			}
			if strings.HasPrefix(test.mutation, "[plugin marketplace") {
				fake.afterMarket = errors.New("mutated")
			} else {
				fake.after[test.mutation] = errors.New("mutated")
			}
			_, err := fakeActivationIntegration(fake).Install(context.Background(), integration.Options{ConfigDir: path})
			if !errors.Is(err, integration.ErrRecovery) {
				t.Fatalf("Install=%v", err)
			}
			for _, want := range test.want {
				if !containsCall(fake.calls, want) {
					t.Fatalf("calls=%v missing %s", fake.calls, want)
				}
			}
			if test.foreign && (containsCall(fake.calls, "[plugin remove vgxness@vgxness --json]") || containsCall(fake.calls, "[plugin marketplace remove vgxness --json]")) {
				t.Fatalf("foreign state modified: %v", fake.calls)
			}
		})
	}
}

func TestIntegrationRefreshesPluginVersionWithExactRemoveAddAndReadback(t *testing.T) {
	ctx, path := context.Background(), filepath.Join(t.TempDir(), "codex")
	root, err := OpenRoot(ctx, integration.Options{ConfigDir: path}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	fake := &fakeCodexCLI{fail: map[string]error{}, after: map[string]error{}, root: path, version: "1.0.0"}
	s := fakeActivationIntegration(fake)
	pkgA, _ := RenderPlan("v1.0.0", sdd.PlanMedium)
	stateA, _ := inspectRoot(ctx, root, pkgA)
	if _, err := s.installAndActivate(ctx, root, pkgA, stateA); err != nil {
		t.Fatal(err)
	}
	stateA, _ = inspectRoot(ctx, root, pkgA)
	if _, err := s.uninstall(ctx, root, pkgA, stateA); err != nil {
		t.Fatal(err)
	}
	pkgB, _ := RenderPlan("v2.0.0", sdd.PlanMedium)
	fake.version = "2.0.0"
	stateB, _ := inspectRoot(ctx, root, pkgB)
	if result, err := s.installAndActivate(ctx, root, pkgB, stateB); err != nil || result.State != integration.StateInstalled {
		t.Fatalf("install B=%+v err=%v", result, err)
	}
	if !fake.plugin || !fake.enabled || fake.version != "2.0.0" {
		t.Fatalf("version B readback plugin=%t enabled=%t version=%s", fake.plugin, fake.enabled, fake.version)
	}
	var mutations []string
	for _, call := range fake.calls {
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
	if activation, err := s.activation(ctx, root); err != nil || activation != activationActive {
		t.Fatalf("activation=%v err=%v", activation, err)
	}
}

func containsCall(calls []string, want string) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}
	return false
}
