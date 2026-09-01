package testutil

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCodexRunnerIsolatesRootsAndReadsPluginVersion(t *testing.T) {
	runner := NewCodexRunner()
	first, second := t.TempDir(), t.TempDir()
	writeManifest := func(root, version string) {
		t.Helper()
		path := filepath.Join(root, "plugins", "vgxness", ".codex-plugin")
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "plugin.json"), []byte(`{"version":"`+version+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run := func(root string, args ...string) []byte {
		t.Helper()
		body, err := runner.Run(context.Background(), "fake", args, []string{"CODEX_HOME=" + root})
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	writeManifest(first, "1.2.3")
	run(first, "plugin", "marketplace", "add", first, "--json")
	run(first, "plugin", "add", "vgxness@vgxness", "--json")

	var plugins struct {
		Installed []struct {
			Version string `json:"version"`
		} `json:"installed"`
	}
	if err := json.Unmarshal(run(first, "plugin", "list", "--json"), &plugins); err != nil || len(plugins.Installed) != 1 || plugins.Installed[0].Version != "1.2.3" {
		t.Fatalf("first root plugins=%+v err=%v", plugins, err)
	}
	if err := json.Unmarshal(run(second, "plugin", "list", "--json"), &plugins); err != nil || len(plugins.Installed) != 0 {
		t.Fatalf("second root plugins=%+v err=%v", plugins, err)
	}
}
