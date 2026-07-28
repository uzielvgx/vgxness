package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/launcher"
	"github.com/vgxness/vgxness/internal/testutil"
)

func TestIntegration_PreviewIsNonMutatingAndModelIndependent(t *testing.T) {
	home := t.TempDir()
	service := NewIntegration()
	result, err := service.Preview(context.Background(), integration.Options{
		HomeDir: home,
		Model:   "legacy/value-is-ignored",
	})
	testutil.NoError(t, err)

	expected := filepath.Join(home, ".config", "opencode", "agents", managerAgentName)
	expectedTool := filepath.Join(home, ".config", "opencode", "plugins", memoryPluginName)
	_, statErr := os.Stat(filepath.Join(home, ".config"))
	testutil.Require(t,
		result.Provider == "opencode" &&
			result.State == integration.StateAbsent &&
			result.Bridge == integration.BridgeNotRequired &&
			result.Path == expected &&
			result.ToolPath == expectedTool &&
			len(result.ToolSHA256) == 64 &&
			result.Model == "" &&
			result.Changed &&
			len(result.ArtifactSHA256) == 64,
		"unexpected preview: %#v", result,
	)
	testutil.Require(t, os.IsNotExist(statErr), "preview mutated filesystem: %v", statErr)
}

func TestIntegration_InstallReadbackStatusAndIdempotence(t *testing.T) {
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	service := NewIntegration()
	options := integration.Options{ConfigDir: configDirectory}

	installed, err := service.Install(context.Background(), options)
	testutil.NoError(t, err)
	data, err := os.ReadFile(installed.Path)
	testutil.NoError(t, err)
	info, err := os.Stat(installed.Path)
	testutil.NoError(t, err)
	toolData, err := os.ReadFile(installed.ToolPath)
	testutil.NoError(t, err)
	toolInfo, err := os.Stat(installed.ToolPath)
	testutil.NoError(t, err)
	expectedTool, err := memoryPluginContent(service.executable)
	testutil.NoError(t, err)

	expectedAgents := map[string]string{
		managerAgentName:      managerPrompt,
		reviewRiskName:        reviewRiskPrompt,
		reviewReadabilityName: reviewReadabilityPrompt,
		reviewReliabilityName: reviewReliabilityPrompt,
		reviewResilienceName:  reviewResiliencePrompt,
		reviewRefuterName:     reviewRefuterPrompt,
	}
	entries, err := os.ReadDir(filepath.Join(configDirectory, "agents"))
	testutil.NoError(t, err)
	testutil.Require(t, len(entries) == len(expectedAgents), "unexpected managed agent count: %d", len(entries))
	for name, expected := range expectedAgents {
		content, readErr := os.ReadFile(filepath.Join(configDirectory, "agents", name))
		testutil.NoError(t, readErr)
		testutil.Require(t, string(content) == expected, "unexpected managed agent %s", name)
	}
	testutil.Require(t,
		installed.State == integration.StateInstalled &&
			installed.Bridge == integration.BridgeNotRequired &&
			installed.Changed &&
			installed.ToolPath == filepath.Join(configDirectory, "plugins", memoryPluginName) &&
			installed.ToolSHA256 == artifactSHA256(expectedTool) &&
			installed.Model == "" &&
			string(data) == managerPrompt &&
			string(toolData) == string(expectedTool),
		"unexpected install: %#v", installed,
	)
	if runtime.GOOS != "windows" {
		testutil.Require(t, info.Mode().Perm() == 0o600 && toolInfo.Mode().Perm() == 0o600, "artifact modes=%o/%o", info.Mode().Perm(), toolInfo.Mode().Perm())
	}

	status, err := service.Status(context.Background(), options)
	testutil.NoError(t, err)
	testutil.Require(t,
		status.State == integration.StateInstalled &&
			status.Bridge == integration.BridgeNotRequired &&
			!status.Changed &&
			status.ArtifactSHA256 == managerPromptSHA256() &&
			status.ToolSHA256 == artifactSHA256(expectedTool),
		"unexpected status: %#v", status,
	)
	second, err := service.Install(context.Background(), options)
	testutil.NoError(t, err)
	testutil.Require(t, second.State == integration.StateInstalled && !second.Changed, "install was not idempotent: %#v", second)
}

func TestIntegration_RepairsOnlyMissingManagedArtifact(t *testing.T) {
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	managerPath := filepath.Join(configDirectory, "agents", managerAgentName)
	testutil.NoError(t, os.MkdirAll(filepath.Dir(managerPath), 0o700))
	testutil.NoError(t, os.WriteFile(managerPath, []byte(managerPrompt), 0o600))
	before, err := os.Stat(managerPath)
	testutil.NoError(t, err)

	service := NewIntegration()
	status, err := service.Status(context.Background(), integration.Options{ConfigDir: configDirectory})
	testutil.NoError(t, err)
	testutil.Require(t, status.State == integration.StatePartial && status.Bridge == integration.BridgeNotRequired, "unexpected partial status: %#v", status)
	installed, err := service.Install(context.Background(), integration.Options{ConfigDir: configDirectory})
	testutil.NoError(t, err)
	after, err := os.Stat(managerPath)
	testutil.NoError(t, err)
	testutil.Require(t, installed.State == integration.StateInstalled && installed.Changed && os.SameFile(before, after), "partial repair replaced existing artifact: %#v", installed)
}

func TestIntegration_RefusesForeignMemoryPluginAndDoesNotInspectLegacyAgents(t *testing.T) {
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	pluginPath := filepath.Join(configDirectory, "plugins", "vgxness.ts")
	legacyAgentPath := filepath.Join(configDirectory, "agents", "vgxness-explorer.md")
	testutil.NoError(t, os.MkdirAll(filepath.Dir(pluginPath), 0o700))
	testutil.NoError(t, os.MkdirAll(filepath.Dir(legacyAgentPath), 0o700))
	testutil.NoError(t, os.WriteFile(pluginPath, []byte("user-owned plugin\n"), 0o600))
	testutil.NoError(t, os.WriteFile(legacyAgentPath, []byte("user-owned agent\n"), 0o600))

	service := NewIntegration()
	_, installErr := service.Install(context.Background(), integration.Options{ConfigDir: configDirectory})
	plugin, err := os.ReadFile(pluginPath)
	testutil.NoError(t, err)
	legacyAgent, err := os.ReadFile(legacyAgentPath)
	testutil.NoError(t, err)
	testutil.Require(t,
		errors.Is(installErr, integration.ErrConflict) &&
			string(plugin) == "user-owned plugin\n" &&
			string(legacyAgent) == "user-owned agent\n",
		"legacy or user-owned projection was touched: %v", installErr,
	)
}

func TestManagerPromptDefinesNativeSkillsCodeGraphAndAuthority(t *testing.T) {
	required := []string{
		"artifact: opencode-agent/vgxness-manager; version: 22",
		"user's OpenCode-native engineering partner",
		"OpenCode's native tools, skills, memory, Task subagents",
		"Direct inline",
		"Delegated direct",
		"Optional SDD",
		"OpenCode is the execution authority for normal work",
		"built-in explore and general subagents",
		"relevant native skill names",
		"load every clearly applicable skill through the skill tool",
		"Pass exact skill names, never filesystem paths",
		"use one bounded codegraph_explore query",
		"Exact source, Git diff, and test output remain authoritative",
		"VGXNESS-owned memory is the only persistent memory authority",
		"vgxness_memory_search",
		"vgxness_memory_get",
		"vgxness_memory_save",
		"vgxness_memory_forget",
		"Do not use any external memory system",
		"Never ask the user to run terminal, Git, filesystem, test, or diagnostic commands",
		"one correction transaction and one scoped validation",
		"Do not commit or push unless the user explicitly asks",
	}
	for _, contract := range required {
		if !strings.Contains(managerPrompt, contract) {
			t.Errorf("manager prompt is missing contract %q", contract)
		}
	}
	for _, forbidden := range []string{
		"vgxness_run", "vgxness_dispatch", "vgxness_orchestrate", "vgxness_native_", "vgxness_codegraph",
		"vgxness-explorer", "vgxness-implementer", "vgxness-maintainer",
		"vgxness-navigator", "skill paths", "managed plugin", "ticket system: allow",
		"guide to the VGXNESS control plane", "gentle-orchestrator",
	} {
		if strings.Contains(managerPrompt, forbidden) {
			t.Errorf("manager prompt retains deprecated mechanic %q", forbidden)
		}
	}
}

func TestMemoryPluginExposesOnlyBoundedOwnedMemoryTools(t *testing.T) {
	service := NewIntegration()
	content, err := memoryPluginContent(service.executable)
	testutil.NoError(t, err)
	plugin := string(content)
	for _, required := range []string{
		"artifact: opencode-plugin/vgxness-memory; version: 1",
		"vgxness_memory_search", "vgxness_memory_get", "vgxness_memory_save", "vgxness_memory_forget",
		`["memory", operation, "--stdin", "--json", "--workspace", workspace]`,
		"shell: false", "MAX_INPUT_BYTES", "MAX_OUTPUT_BYTES", "TIMEOUT_MS",
		`env: {`, `HOME: process.env.HOME`, `context?.abort?.addEventListener`,
		"VGXNESS-owned durable project memory",
	} {
		if !strings.Contains(plugin, required) {
			t.Errorf("memory plugin missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"vgxness_run", "vgxness_status", "vgxness_dispatch", "vgxness_orchestrate",
		"vgxness_native_", "vgxness_codegraph", "client.session", "--model", "ENGRAM",
	} {
		if strings.Contains(plugin, forbidden) {
			t.Errorf("memory plugin retains non-memory capability %q", forbidden)
		}
	}
}

func TestReviewAgentsAreReadOnlyWithNativeSkillAndCodeGraphAccess(t *testing.T) {
	profiles := map[string]struct {
		prompt string
		role   string
		prefix string
	}{
		"risk":        {prompt: reviewRiskPrompt, role: "security boundaries", prefix: "RISK-"},
		"readability": {prompt: reviewReadabilityPrompt, role: "intention is clear", prefix: "READ-"},
		"reliability": {prompt: reviewReliabilityPrompt, role: "behavioral contracts", prefix: "REL-"},
		"resilience":  {prompt: reviewResiliencePrompt, role: "failure paths", prefix: "RES-"},
	}
	for name, profile := range profiles {
		for _, required := range []string{
			"mode: subagent", "hidden: true", `"*": deny`, "read: allow", "grep: allow",
			"glob: allow", "list: allow", "skill: allow", "codegraph_explore: allow",
			"vgxness_memory_search: allow", "vgxness_memory_get: allow",
			"task: deny", "relevant native skill names", "Load every supplied skill name",
			"use at most one bounded codegraph_explore query", "exact source and supplied diff evidence remain authoritative",
			"candidateIdentity", "changedPaths", "acceptanceCriteria", profile.role, profile.prefix,
		} {
			if !strings.Contains(profile.prompt, required) {
				t.Errorf("%s reviewer missing %q", name, required)
			}
		}
		for _, forbidden := range []string{"bash: allow", "edit: allow", "write: allow", "task: allow", "codegraph_*: allow", "vgxness_memory_save: allow", "vgxness_memory_forget: allow"} {
			if strings.Contains(profile.prompt, forbidden) {
				t.Errorf("%s reviewer enables %q", name, forbidden)
			}
		}
	}
	for _, required := range []string{
		"severe-finding refuter", "skill: allow", "codegraph_explore: allow", "vgxness_memory_search: allow", "vgxness_memory_get: allow",
		"Load every supplied native skill name", "Never add a new finding",
		`"outcome":"corroborated|refuted|inconclusive"`,
	} {
		if !strings.Contains(reviewRefuterPrompt, required) {
			t.Errorf("refuter missing %q", required)
		}
	}
}

func TestTrustedLauncherRequiresManagedLauncherForCurrentActiveBinary(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	launcherPath := filepath.Join(root, "bin", "vgxness")
	activeSource, err := os.Executable()
	testutil.NoError(t, err)
	activeDigest, err := launcher.FileSHA256(activeSource)
	testutil.NoError(t, err)
	activePath := launcher.VersionPath(dataDir, activeDigest)
	testutil.NoError(t, os.MkdirAll(filepath.Dir(activePath), 0o700))
	activeData, err := os.ReadFile(activeSource)
	testutil.NoError(t, err)
	testutil.NoError(t, os.WriteFile(activePath, activeData, 0o555))
	testutil.NoError(t, os.MkdirAll(filepath.Dir(launcherPath), 0o700))
	testutil.NoError(t, os.WriteFile(launcherPath, activeData, 0o755))
	launcherDigest, err := launcher.FileSHA256(launcherPath)
	testutil.NoError(t, err)
	manifest := launcher.Manifest{
		SchemaVersion:  launcher.SchemaVersion,
		ManagedBy:      launcher.ManagedBy,
		LauncherPath:   launcherPath,
		LauncherSHA256: launcherDigest,
		DataDir:        dataDir,
		ActivePath:     activePath,
		ActiveSHA256:   activeDigest,
		UpdatedAt:      time.Now().UTC().Format(time.RFC3339Nano),
	}
	manifestData, err := json.Marshal(manifest)
	testutil.NoError(t, err)
	testutil.NoError(t, os.WriteFile(launcher.SidecarPath(launcherPath), append(manifestData, '\n'), 0o600))

	testutil.Require(t, trustedLauncher(activePath, launcherPath) == launcherPath, "valid managed launcher was rejected")
	testutil.Require(t, trustedLauncher(activeSource, launcherPath) == "", "different active inode was trusted")
	testutil.Require(t, trustedLauncher(activePath, filepath.Join(root, "missing")) == "", "missing launcher was trusted")
	managed, err := NewManagedIntegration(launcherPath)
	testutil.NoError(t, err)
	testutil.Require(t, managed.executable == launcherPath, "managed integration executable=%q", managed.executable)
	testutil.NoError(t, os.WriteFile(launcherPath, []byte("tampered"), 0o755))
	if _, err := NewManagedIntegration(launcherPath); !errors.Is(err, integration.ErrInvalid) {
		t.Fatalf("tampered managed launcher error=%v", err)
	}
}

func TestIntegration_InstallNeverOverwritesForeignOrDriftedContent(t *testing.T) {
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	target := filepath.Join(configDirectory, "agents", managerAgentName)
	testutil.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
	foreign := []byte("user-owned prompt\n")
	testutil.NoError(t, os.WriteFile(target, foreign, 0o600))
	service := NewIntegration()
	status, err := service.Status(context.Background(), integration.Options{ConfigDir: configDirectory})
	testutil.NoError(t, err)
	_, installErr := service.Install(context.Background(), integration.Options{ConfigDir: configDirectory})
	after, err := os.ReadFile(target)
	testutil.NoError(t, err)
	testutil.Require(t, status.State == integration.StateDrifted && errors.Is(installErr, integration.ErrConflict) && string(after) == string(foreign), "status=%#v install=%v after=%q", status, installErr, after)
}

func TestIntegration_RefusesSymlinkArtifactAndConfigDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on Windows")
	}
	t.Run("artifact", func(t *testing.T) {
		root := t.TempDir()
		configDirectory := filepath.Join(root, "opencode")
		target := filepath.Join(configDirectory, "agents", managerAgentName)
		foreign := filepath.Join(root, "foreign.md")
		testutil.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
		testutil.NoError(t, os.WriteFile(foreign, []byte("foreign"), 0o600))
		testutil.NoError(t, os.Symlink(foreign, target))
		service := NewIntegration()
		status, err := service.Status(context.Background(), integration.Options{ConfigDir: configDirectory})
		testutil.NoError(t, err)
		_, installErr := service.Install(context.Background(), integration.Options{ConfigDir: configDirectory})
		data, err := os.ReadFile(foreign)
		testutil.NoError(t, err)
		testutil.Require(t, status.State == integration.StateDrifted && errors.Is(installErr, integration.ErrConflict) && string(data) == "foreign", "status=%#v install=%v", status, installErr)
	})
	t.Run("config", func(t *testing.T) {
		root := t.TempDir()
		foreign := filepath.Join(root, "foreign-config")
		configDirectory := filepath.Join(root, "opencode")
		testutil.NoError(t, os.MkdirAll(foreign, 0o700))
		testutil.NoError(t, os.Symlink(foreign, configDirectory))
		service := NewIntegration()
		status, err := service.Status(context.Background(), integration.Options{ConfigDir: configDirectory})
		testutil.NoError(t, err)
		_, installErr := service.Install(context.Background(), integration.Options{ConfigDir: configDirectory})
		_, foreignErr := os.Stat(filepath.Join(foreign, "agents"))
		testutil.Require(t, status.State == integration.StateDrifted && errors.Is(installErr, integration.ErrConflict) && os.IsNotExist(foreignErr), "status=%#v install=%v", status, installErr)
	})
}

func TestIntegration_UninstallIsRecoverableAndRefusesDrift(t *testing.T) {
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	service := NewIntegration()
	service.now = func() time.Time { return time.Date(2026, 7, 21, 12, 34, 56, 7, time.UTC) }
	options := integration.Options{ConfigDir: configDirectory}
	installed, err := service.Install(context.Background(), options)
	testutil.NoError(t, err)
	removed, err := service.Uninstall(context.Background(), options)
	testutil.NoError(t, err)
	backup, err := os.ReadFile(removed.BackupPath)
	testutil.NoError(t, err)
	toolBackup, err := os.ReadFile(removed.ToolBackupPath)
	testutil.NoError(t, err)
	expectedTool, err := memoryPluginContent(service.executable)
	testutil.NoError(t, err)
	_, targetErr := os.Stat(installed.Path)
	_, toolErr := os.Stat(installed.ToolPath)
	for _, name := range []string{reviewRiskName, reviewReadabilityName, reviewReliabilityName, reviewResilienceName, reviewRefuterName} {
		if _, statErr := os.Stat(filepath.Join(configDirectory, "agents", name)); !os.IsNotExist(statErr) {
			t.Errorf("managed reviewer %s was not removed: %v", name, statErr)
		}
	}
	testutil.Require(t,
		removed.State == integration.StateAbsent &&
			removed.Bridge == integration.BridgeNotRequired &&
			removed.Changed &&
			strings.Contains(removed.BackupPath, "20260721T123456") &&
			string(backup) == managerPrompt &&
			string(toolBackup) == string(expectedTool) &&
			os.IsNotExist(targetErr) &&
			os.IsNotExist(toolErr),
		"unexpected uninstall: %#v target=%v tool=%v", removed, targetErr, toolErr,
	)
	second, err := service.Uninstall(context.Background(), options)
	testutil.NoError(t, err)
	testutil.Require(t, second.State == integration.StateAbsent && !second.Changed, "uninstall was not idempotent: %#v", second)

	_, err = service.Install(context.Background(), options)
	testutil.NoError(t, err)
	testutil.NoError(t, os.WriteFile(installed.Path, []byte("changed"), 0o600))
	_, err = service.Uninstall(context.Background(), options)
	testutil.Require(t, errors.Is(err, integration.ErrDrift), "drifted uninstall error=%v", err)
}

func TestIntegration_InvalidAndCancelledRequestsDoNotMutate(t *testing.T) {
	service := NewIntegration()
	_, err := service.Preview(context.Background(), integration.Options{ConfigDir: "relative"})
	testutil.Require(t, errors.Is(err, integration.ErrInvalid), "relative config error=%v", err)
	root := filepath.Join(t.TempDir(), "opencode")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = service.Install(ctx, integration.Options{ConfigDir: root})
	_, statErr := os.Stat(root)
	testutil.Require(t, errors.Is(err, context.Canceled) && os.IsNotExist(statErr), "cancel error=%v stat=%v", err, statErr)
}

func TestIntegration_RollbackNeverRemovesOrOverwritesConcurrentReplacement(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	expected := filepath.Join(root, "expected")
	backup := filepath.Join(root, "backup")
	testutil.NoError(t, os.WriteFile(target, []byte("foreign"), 0o600))
	testutil.NoError(t, os.WriteFile(expected, []byte("managed"), 0o600))
	removeSameFileBestEffort(target, expected)
	data, err := os.ReadFile(target)
	testutil.NoError(t, err)
	testutil.Require(t, string(data) == "foreign", "install rollback removed replacement: %q", data)
	testutil.NoError(t, os.WriteFile(backup, []byte("managed"), 0o600))
	restoreWithoutOverwrite(backup, target)
	data, err = os.ReadFile(target)
	testutil.NoError(t, err)
	_, backupErr := os.Stat(backup)
	testutil.Require(t, string(data) == "foreign" && backupErr == nil, "uninstall rollback overwrote replacement: target=%q backup=%v", data, backupErr)
}
