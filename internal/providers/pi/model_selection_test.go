package pi

import (
	"context"
	"encoding/json"
	"github.com/vgxness/vgxness/internal/agentmodels"
	"os"
	"path/filepath"
	"testing"
)

func TestModelOnlyUpdatePreservesSettingsAndBindsPreview(t *testing.T) {
	root := t.TempDir()
	o := Options{ReleaseDir: tinyRelease(t, filepath.Join(root, "release"), "a"), AgentDir: filepath.Join(root, "agent"), InstallRoot: filepath.Join(root, "managed")}
	first, err := Install(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(o.AgentDir, "settings.json")
	data, _ := os.ReadFile(settings)
	var value map[string]json.RawMessage
	json.Unmarshal(data, &value)
	value["foreign"] = json.RawMessage(`{"keep":true}`)
	data, _ = json.Marshal(value)
	os.WriteFile(settings, data, 0600)
	c, err := agentmodels.Single("vendor/family/model", "off")
	if err != nil {
		t.Fatal(err)
	}
	o.Models = &c
	digest, _, changed, err := modelSettings(o.AgentDir, o.Models)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	o.expectedSettingsSHA = digest
	second, err := Install(context.Background(), o)
	if err != nil || second.PackagePath != first.PackagePath {
		t.Fatalf("%+v %v", second, err)
	}
	after, _ := os.ReadFile(settings)
	json.Unmarshal(after, &value)
	var foreign map[string]bool
	json.Unmarshal(value["foreign"], &foreign)
	if !foreign["keep"] || string(value["defaultProvider"]) != `"vendor"` || string(value["defaultModel"]) != `"family/model"` {
		t.Fatalf("unexpected settings: %s", after)
	}
	if _, _, changed, err := modelSettings(o.AgentDir, &c); err != nil || changed {
		t.Fatalf("idempotence %v %v", changed, err)
	}
	c.Assignments["manager"] = agentmodels.Assignment{Model: "other/model", Effort: "off"}
	c.Mode = "per-agent"
	if _, err = Install(context.Background(), o); err == nil {
		t.Fatal("stale settings preview accepted")
	}
	current, _ := os.ReadFile(settings)
	if string(current) != string(after) {
		t.Fatal("stale update changed settings")
	}
}
