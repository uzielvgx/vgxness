package opencode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/launcher"
)

func TestRepairedMCPConfigPreservesOtherServersAndMode(t *testing.T) {
	state, err := json.Marshal(defaultAgentState{SchemaVersion: 1, MCPOwned: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		entry func(string) json.RawMessage
		mode  string
	}{
		{"full", func(path string) json.RawMessage { entry, _ := managedMCPConfig(path); return entry }, "full"},
		{"readonly", managedReadOnlyMCPConfig, "readonly"},
	} {
		t.Run(test.name, func(t *testing.T) {
			old := "/opt/vgxness-old"
			entry := test.entry(old)
			config := []byte(`{"mcp":{"other":{"type":"remote","url":"https://example.invalid"},"vgxness":` + string(entry) + `}}`)
			digest, err := canonicalMCPEntrySHA256(entry)
			if err != nil {
				t.Fatal(err)
			}
			service := &Integration{executable: "/opt/vgxness-stable"}
			repaired, err := service.repairedMCPConfig(config, state, integration.MCPRepairProof{OldExecutable: old, ExpectedEntrySHA256: digest})
			if err != nil {
				t.Fatal(err)
			}
			values, _, err := readOpenCodeConfigFromBytes(repaired)
			if err != nil {
				t.Fatal(err)
			}
			got, present, err := openCodeMCP(values)
			if err != nil || !present {
				t.Fatalf("mcp=%s present=%t err=%v", got, present, err)
			}
			want := managedReadOnlyMCPConfig(service.executable)
			if test.mode == "full" {
				want, _ = managedMCPConfig(service.executable)
			}
			if !sameJSONValue(got, want) || string(values["mcp"]) == "" {
				t.Fatalf("entry=%s want=%s mcp=%s", got, want, values["mcp"])
			}
		})
	}
}

func TestRepairMCPAppliesAndRepeatsWithSameProof(t *testing.T) {
	configDirectory := t.TempDir()
	service := &Integration{executable: repairLauncherFixture(t)}
	old := filepath.Join(t.TempDir(), "vgxness-old")
	entry, _ := managedMCPConfig(old)
	digest, _ := canonicalMCPEntrySHA256(entry)
	config := []byte(`{"mcp":{"other":{"type":"remote","url":"https://example.invalid"},"vgxness":` + string(entry) + `}}`)
	if err := os.WriteFile(filepath.Join(configDirectory, defaultAgentConfigName), config, 0o600); err != nil {
		t.Fatal(err)
	}
	state, _ := json.Marshal(defaultAgentState{SchemaVersion: 1, MCPOwned: true})
	if err := os.MkdirAll(filepath.Join(configDirectory, "vgxness"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDirectory, "vgxness", defaultAgentStateName), state, 0o600); err != nil {
		t.Fatal(err)
	}
	proof := integration.MCPRepairProof{OldExecutable: old, ExpectedEntrySHA256: digest}
	preview, err := service.PreviewMCPRepair(context.Background(), integration.Options{ConfigDir: configDirectory}, integration.MCPRepairProof{})
	if err != nil || preview.MCPRepairOldExecutable != old || preview.MCPRepairEntrySHA256 != digest || preview.Changed {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	result, err := service.RepairMCP(context.Background(), integration.Options{ConfigDir: configDirectory}, proof)
	if err != nil || !result.Changed {
		t.Fatalf("repair=%+v err=%v", result, err)
	}
	repeated, err := service.RepairMCP(context.Background(), integration.Options{ConfigDir: configDirectory}, proof)
	if err != nil || repeated.Changed {
		t.Fatalf("repeat=%+v err=%v", repeated, err)
	}
	// A post-link failure must restore only the exact predecessor and leave the
	// ownership state untouched for retry.
	if err := os.WriteFile(filepath.Join(configDirectory, defaultAgentConfigName), config, 0o600); err != nil {
		t.Fatal(err)
	}
	service.afterMCPRepairRoot = func(root *rootTransaction) {
		root.afterPublishLink = func(string) error { return errors.New("publish failure") }
	}
	if _, err := service.RepairMCP(context.Background(), integration.Options{ConfigDir: configDirectory}, proof); err == nil {
		t.Fatal("repair succeeded after injected publication failure")
	}
	after, readErr := os.ReadFile(filepath.Join(configDirectory, defaultAgentConfigName))
	if readErr != nil || string(after) != string(config) {
		t.Fatalf("rollback config=%q err=%v", after, readErr)
	}
	if err := os.WriteFile(filepath.Join(configDirectory, "vgxness", defaultAgentStateName), state, 0o600); err != nil {
		t.Fatal(err)
	}
	service.afterMCPRepairRoot = func(root *rootTransaction) {
		root.afterPublishLink = func(string) error {
			changed, _ := json.Marshal(defaultAgentState{SchemaVersion: 1, MCPOwned: false})
			return os.WriteFile(filepath.Join(configDirectory, "vgxness", defaultAgentStateName), changed, 0o600)
		}
	}
	if _, err := service.RepairMCP(context.Background(), integration.Options{ConfigDir: configDirectory}, proof); err == nil {
		t.Fatalf("state replacement repair=%v", err)
	}
	after, readErr = os.ReadFile(filepath.Join(configDirectory, defaultAgentConfigName))
	if readErr != nil || string(after) != string(config) {
		t.Fatalf("state rollback config=%q err=%v", after, readErr)
	}
	changed, readErr := os.ReadFile(filepath.Join(configDirectory, "vgxness", defaultAgentStateName))
	if readErr != nil || bytes.Contains(changed, []byte(`"mcp_owned":true`)) {
		t.Fatalf("state was not preserved=%s err=%v", changed, readErr)
	}
	if err := os.WriteFile(filepath.Join(configDirectory, defaultAgentConfigName), config, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDirectory, "vgxness", defaultAgentStateName), state, 0o600); err != nil {
		t.Fatal(err)
	}
	competitor := []byte(`{"mcp":{"vgxness":{"type":"local","command":["competitor","mcp"],"enabled":true}}}`)
	service.afterMCPRepairRoot = func(_ *rootTransaction) {
		_ = os.Remove(filepath.Join(configDirectory, defaultAgentConfigName))
		_ = os.WriteFile(filepath.Join(configDirectory, defaultAgentConfigName), competitor, 0o600)
	}
	if _, err := service.RepairMCP(context.Background(), integration.Options{ConfigDir: configDirectory}, proof); err == nil {
		t.Fatalf("competitor repair=%v", err)
	}
	after, readErr = os.ReadFile(filepath.Join(configDirectory, defaultAgentConfigName))
	if readErr != nil || !bytes.Equal(after, competitor) {
		t.Fatalf("competitor config=%q err=%v", after, readErr)
	}
}

func TestRepairMCPRejectsSymlinkedConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symlink creation requires privileges unavailable to ordinary test runs")
	}
	configDirectory := t.TempDir()
	service := &Integration{executable: repairLauncherFixture(t)}
	old := filepath.Join(t.TempDir(), "vgxness-old")
	entry, _ := managedMCPConfig(old)
	digest, _ := canonicalMCPEntrySHA256(entry)
	external := filepath.Join(t.TempDir(), "opencode.json")
	if err := os.WriteFile(external, []byte(`{"mcp":{"vgxness":`+string(entry)+`}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(configDirectory, defaultAgentConfigName)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(configDirectory, "vgxness"), 0o700); err != nil {
		t.Fatal(err)
	}
	state, _ := json.Marshal(defaultAgentState{SchemaVersion: 1, MCPOwned: true})
	if err := os.WriteFile(filepath.Join(configDirectory, "vgxness", defaultAgentStateName), state, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RepairMCP(context.Background(), integration.Options{ConfigDir: configDirectory}, integration.MCPRepairProof{OldExecutable: old, ExpectedEntrySHA256: digest}); !errors.Is(err, integration.ErrDrift) {
		t.Fatalf("symlink repair=%v", err)
	}
	info, err := os.Lstat(filepath.Join(configDirectory, defaultAgentConfigName))
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("config symlink info=%v err=%v", info, err)
	}
}

func repairLauncherFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	launcherPath := filepath.Join(root, "vgxness")
	active := []byte("active")
	activeHash := sha256.Sum256(active)
	dataDir := filepath.Join(root, "data")
	activePath := launcher.VersionPath(dataDir, hex.EncodeToString(activeHash[:]))
	if err := os.MkdirAll(filepath.Dir(activePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(activePath, active, 0o700); err != nil {
		t.Fatal(err)
	}
	launcherBytes := []byte("launcher")
	if err := os.WriteFile(launcherPath, launcherBytes, 0o700); err != nil {
		t.Fatal(err)
	}
	launcherHash := sha256.Sum256(launcherBytes)
	manifest, _ := json.Marshal(launcher.Manifest{SchemaVersion: launcher.SchemaVersion, ManagedBy: launcher.ManagedBy, LauncherPath: launcherPath, LauncherSHA256: hex.EncodeToString(launcherHash[:]), DataDir: dataDir, ActivePath: activePath, ActiveSHA256: hex.EncodeToString(activeHash[:]), UpdatedAt: "2026-09-09T00:00:00Z"})
	if err := os.WriteFile(launcher.SidecarPath(launcherPath), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	return launcherPath
}

func TestRepairedMCPConfigRejectsWrongProofAndUnownedState(t *testing.T) {
	entry, err := managedMCPConfig("/opt/vgxness-old")
	if err != nil {
		t.Fatal(err)
	}
	config := []byte(`{"mcp":{"vgxness":` + string(entry) + `}}`)
	state, _ := json.Marshal(defaultAgentState{SchemaVersion: 1, MCPOwned: true})
	service := &Integration{executable: "/opt/vgxness-stable"}
	if _, err := service.repairedMCPConfig(config, state, integration.MCPRepairProof{OldExecutable: "/opt/vgxness-old", ExpectedEntrySHA256: "0000000000000000000000000000000000000000000000000000000000000000"}); !errors.Is(err, integration.ErrDrift) {
		t.Fatalf("wrong proof error=%v", err)
	}
	unowned, _ := json.Marshal(defaultAgentState{SchemaVersion: 1, MCPExisted: true, MCP: entry})
	if _, err := service.repairedMCPConfig(config, unowned, integration.MCPRepairProof{}); !errors.Is(err, integration.ErrDrift) {
		t.Fatalf("unowned state error=%v", err)
	}
}
func TestPreviewMCPRepairRejectsPartialProofBeforeConfigRead(t *testing.T) {
	service := &Integration{executable: repairLauncherFixture(t)}
	_, err := service.PreviewMCPRepair(context.Background(), integration.Options{ConfigDir: filepath.Join(t.TempDir(), "missing")}, integration.MCPRepairProof{OldExecutable: "/tmp/old"})
	if !errors.Is(err, integration.ErrInvalid) {
		t.Fatalf("err=%v", err)
	}
}
func TestRepairMCPRetainsVerifiedBackupAgainstPostPublishCompetitor(t *testing.T) {
	d := t.TempDir()
	service := &Integration{executable: repairLauncherFixture(t)}
	old := filepath.Join(t.TempDir(), "old")
	entry, _ := managedMCPConfig(old)
	digest, _ := canonicalMCPEntrySHA256(entry)
	original := []byte(`{"mcp":{"vgxness":` + string(entry) + `}}`)
	path := filepath.Join(d, defaultAgentConfigName)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	state, _ := json.Marshal(defaultAgentState{SchemaVersion: 1, MCPOwned: true})
	if err := os.MkdirAll(filepath.Join(d, "vgxness"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "vgxness", defaultAgentStateName), state, 0o600); err != nil {
		t.Fatal(err)
	}
	competitor := []byte(`{"mcp":{"competitor":{}}}`)
	service.afterMCPRepairRoot = func(root *rootTransaction) {
		root.afterPublishLink = func(string) error {
			if err := os.Remove(path); err != nil {
				return err
			}
			if err := os.WriteFile(path, competitor, 0o600); err != nil {
				return err
			}
			return errors.New("postpublish competitor")
		}
	}
	result, err := service.RepairMCP(context.Background(), integration.Options{ConfigDir: d}, integration.MCPRepairProof{OldExecutable: old, ExpectedEntrySHA256: digest})
	if !errors.Is(err, integration.ErrRecovery) || result.BackupPath == "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	backup, readErr := os.ReadFile(result.BackupPath)
	if readErr != nil || !bytes.Equal(backup, original) {
		t.Fatalf("backup=%q err=%v", backup, readErr)
	}
	live, readErr := os.ReadFile(path)
	if readErr != nil || !bytes.Equal(live, competitor) {
		t.Fatalf("live=%q err=%v", live, readErr)
	}
}
