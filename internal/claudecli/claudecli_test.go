package claudecli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type fakeRunner struct {
	outputs map[string]string
	errs    map[string]error
	calls   []string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	command := strings.Join(append([]string{name}, args...), " ")
	f.calls = append(f.calls, command)
	return []byte(f.outputs[command]), f.errs[command]
}

func TestVersionParsesClaudeOutput(t *testing.T) {
	runner := &fakeRunner{outputs: map[string]string{"claude --version": "2.1.287 (Claude Code)\n"}}
	version, err := New(runner).Version(context.Background())
	if err != nil || version != "2.1.287" {
		t.Fatalf("version=%q err=%v", version, err)
	}
	runner.outputs["claude --version"] = "garbage"
	if _, err := New(runner).Version(context.Background()); err == nil {
		t.Fatal("garbage output accepted")
	}
	runner.errs = map[string]error{"claude --version": ErrUnavailable}
	if _, err := New(runner).Version(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing claude err=%v", err)
	}
}

func TestPluginsAndMarketplacesDecodeJSONAndFindVgxness(t *testing.T) {
	runner := &fakeRunner{outputs: map[string]string{
		"claude plugin list --json":             `[{"id":"design@synced","version":"1.2.0","enabled":true},{"id":"vgxness@vgxness","version":"0.1.0","scope":"user","enabled":true,"installPath":"/cache/vgxness"}]`,
		"claude plugin marketplace list --json": `[{"name":"claude-plugins-official","source":"github"},{"name":"vgxness","source":"github","repo":"uzielvgx/vgxness","installLocation":"/mk"}]`,
	}}
	client := New(runner)
	plugins, err := client.Plugins(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	plugin, found := FindPlugin(plugins)
	if !found || plugin.Version != "0.1.0" || plugin.InstallPath != "/cache/vgxness" || !plugin.Enabled {
		t.Fatalf("plugin=%+v found=%v", plugin, found)
	}
	marketplaces, err := client.Marketplaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	marketplace, found := FindMarketplace(marketplaces)
	if !found || marketplace.Repo != "uzielvgx/vgxness" || marketplace.InstallLocation != "/mk" {
		t.Fatalf("marketplace=%+v found=%v", marketplace, found)
	}
	if _, found := FindPlugin(plugins[:1]); found {
		t.Fatal("found vgxness in a list without it")
	}
	runner.outputs["claude plugin list --json"] = "not json"
	if _, err := client.Plugins(context.Background()); err == nil {
		t.Fatal("malformed JSON accepted")
	}
}

func TestSetupCommandsRunTheDocumentedCLI(t *testing.T) {
	runner := &fakeRunner{}
	client := New(runner)
	_, _ = client.AddMarketplace(context.Background())
	_, _ = client.InstallPlugin(context.Background())
	_, _ = client.UpdatePlugin(context.Background())
	_, _ = client.ListMCP(context.Background())
	want := []string{
		"claude plugin marketplace add uzielvgx/vgxness",
		"claude plugin install vgxness@vgxness",
		"claude plugin update vgxness@vgxness",
		"claude mcp list",
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls=%v", runner.calls)
	}
}

func TestMarketplacePluginVersionReadsTheCheckout(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "plugins", "vgxness", ".claude-plugin")
	if err := os.MkdirAll(manifest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manifest, "plugin.json"), []byte(`{"name":"vgxness","version":"0.2.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	version, err := MarketplacePluginVersion(Marketplace{InstallLocation: root})
	if err != nil || version != "0.2.0" {
		t.Fatalf("version=%q err=%v", version, err)
	}
	if _, err := MarketplacePluginVersion(Marketplace{}); err == nil {
		t.Fatal("empty location accepted")
	}
}

func TestAtLeastComparesNumerically(t *testing.T) {
	for _, tc := range []struct {
		version, minimum string
		want             bool
	}{
		{"2.1.287", "2.1.284", true},
		{"2.1.284", "2.1.284", true},
		{"2.1.283", "2.1.284", false},
		{"2.10.0", "2.9.9", true},
		{"v3.0.0", "2.1.284", true},
		{"0.2.0-probe", "0.1.0", true},
		{"garbage", "2.1.284", false},
		{"2.1", "2.1.0", false},
	} {
		if got := AtLeast(tc.version, tc.minimum); got != tc.want {
			t.Errorf("AtLeast(%q, %q) = %v", tc.version, tc.minimum, got)
		}
	}
}

func TestExecRunnerReportsExitCodeAndStderr(t *testing.T) {
	_, err := ExecRunner{}.Run(context.Background(), "sh", "-c", "echo boom >&2; exit 3")
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 3 || exitErr.Stderr != "boom" {
		t.Fatalf("err=%#v", err)
	}
	if _, err := (ExecRunner{}).Run(context.Background(), "vgxness-definitely-missing-binary"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing binary err=%v", err)
	}
}
