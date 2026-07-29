//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/bridge"
	"github.com/vgxness/vgxness/internal/sdd"
)

func TestCleanCheckoutSetupAndDispatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native Windows runtime smoke is tracked separately")
	}
	repository := repositoryRoot(t)
	root := t.TempDir()
	buildDirectory := filepath.Join(root, "build")
	fakeBinDirectory := filepath.Join(root, "fake-bin")
	homeDirectory := filepath.Join(root, "home")
	temporaryDirectory := filepath.Join(root, "tmp")
	workspace := filepath.Join(root, "workspace")
	launcherDirectory := filepath.Join(root, "installed", "bin")
	dataDirectory := filepath.Join(root, "installed", "data")
	configDirectory := filepath.Join(root, "opencode")
	for _, directory := range []string{buildDirectory, fakeBinDirectory, homeDirectory, temporaryDirectory, workspace} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("# Hermetic workspace\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	goExecutable, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	sourceExecutable := filepath.Join(buildDirectory, executableName("vgxness"))
	fakeOpenCode := filepath.Join(fakeBinDirectory, executableName("opencode"))
	build(t, goExecutable, repository, sourceExecutable, "./cmd/vgxness")
	build(t, goExecutable, repository, fakeOpenCode, "./internal/e2e/testdata/fakeopencode")

	environment := isolatedEnvironment(homeDirectory, temporaryDirectory, fakeBinDirectory)
	setupOutput := run(t, environment, workspace, sourceExecutable,
		"setup", "opencode", "--yes", "--workspace", workspace,
		"--bin-dir", launcherDirectory, "--data-dir", dataDirectory, "--config-dir", configDirectory,
	)
	for _, expected := range []string{"Paso 1 de 6", "Paso 6 de 6", "configuración completa", "handshake OpenCode=healthy"} {
		if !strings.Contains(setupOutput, expected) {
			t.Fatalf("setup output is missing %q:\n%s", expected, setupOutput)
		}
	}

	launcher := filepath.Join(launcherDirectory, executableName("vgxness"))
	manager := filepath.Join(configDirectory, "agents", "vgxness-manager.md")
	memoryPlugin := filepath.Join(configDirectory, "plugins", "vgxness.ts")
	reviewers := []string{
		"vgxness-review-risk.md",
		"vgxness-review-readability.md",
		"vgxness-review-reliability.md",
		"vgxness-review-resilience.md",
		"vgxness-review-refuter.md",
	}
	sddProfiles := []string{
		"vgxness-sdd-research.md",
		"vgxness-sdd-proposal.md",
		"vgxness-sdd-spec.md",
		"vgxness-sdd-design.md",
		"vgxness-sdd-tasks.md",
		"vgxness-sdd-apply.md",
	}
	managedProfiles := append(reviewerPaths(configDirectory, reviewers), reviewerPaths(configDirectory, sddProfiles)...)
	for _, path := range append([]string{launcher, manager, memoryPlugin}, managedProfiles...) {
		info, statErr := os.Stat(path)
		if statErr != nil || !info.Mode().IsRegular() {
			t.Fatalf("expected installed regular file %s: %v", path, statErr)
		}
	}
	pluginData, err := os.ReadFile(memoryPlugin)
	if err != nil ||
		!bytes.Contains(pluginData, []byte("opencode-plugin/vgxness-memory")) ||
		!bytes.Contains(pluginData, []byte("vgxness_memory_search")) ||
		!bytes.Contains(pluginData, []byte("vgxness_sdd_set_interaction_mode")) ||
		bytes.Contains(pluginData, []byte("vgxness_orchestrate")) ||
		bytes.Contains(pluginData, []byte("vgxness_native_edit")) {
		t.Fatalf("setup did not install the bounded storage-only plugin: %v", err)
	}
	managerData, err := os.ReadFile(manager)
	if err != nil || !bytes.Contains(managerData, []byte("artifact: opencode-agent/vgxness-manager; version: 29")) || !bytes.Contains(managerData, []byte("At the start of every accepted SDD change")) {
		t.Fatalf("setup did not install the executable SDD manager contract: %v", err)
	}
	if err := os.Rename(sourceExecutable, sourceExecutable+".offline"); err != nil {
		t.Fatalf("retire source executable: %v", err)
	}

	statusOutput := run(t, environment, workspace, launcher,
		"setup", "opencode", "--status", "--workspace", workspace,
		"--bin-dir", launcherDirectory, "--data-dir", dataDirectory, "--config-dir", configDirectory,
	)
	if !strings.Contains(statusOutput, "Launcher: state=installed") || !strings.Contains(statusOutput, "Handshake: ok=true status=healthy") {
		t.Fatalf("installed setup is not healthy:\n%s", statusOutput)
	}

	var createdEnvelope struct {
		SchemaVersion int        `json:"schemaVersion"`
		Result        sdd.Change `json:"result"`
	}
	decodeJSON(t, runWithInput(t, environment, workspace, `{"schemaVersion":1,"idempotencyKey":"hermetic-lifecycle-1","title":"Hermetic lifecycle","backend":"memory","interactionMode":"automatic","plan":"low"}`, launcher, "sdd", "create", "--stdin", "--json", "--workspace", workspace), &createdEnvelope)
	change := createdEnvelope.Result
	if createdEnvelope.SchemaVersion != 1 || change.ID == "" || change.StateVersion != 1 || change.Phase != sdd.PhaseExplore {
		t.Fatalf("unexpected SDD change: %+v", createdEnvelope)
	}
	var modeEnvelope struct {
		Result sdd.Change `json:"result"`
	}
	decodeJSON(t, runWithInput(t, environment, workspace, fmt.Sprintf(`{"schemaVersion":1,"changeId":%q,"interactionMode":"interactive","expectedStateVersion":1}`, change.ID), launcher, "sdd", "set-interaction-mode", "--stdin", "--json", "--workspace", workspace), &modeEnvelope)
	if modeEnvelope.Result.InteractionMode != sdd.InteractionInteractive || modeEnvelope.Result.StateVersion != 2 {
		t.Fatalf("unexpected SDD mode update: %+v", modeEnvelope.Result)
	}
	var savedEnvelope struct {
		Result sdd.Revision `json:"result"`
	}
	decodeJSON(t, runWithInput(t, environment, workspace, fmt.Sprintf(`{"schemaVersion":1,"changeId":%q,"artifact":"explore","content":"bounded research","expectedStateVersion":2}`, change.ID), launcher, "sdd", "save-revision", "--stdin", "--json", "--workspace", workspace), &savedEnvelope)
	var acceptedEnvelope struct {
		Result sdd.Revision `json:"result"`
	}
	decodeJSON(t, runWithInput(t, environment, workspace, fmt.Sprintf(`{"schemaVersion":1,"changeId":%q,"revisionId":%q,"expectedStateVersion":%d}`, change.ID, savedEnvelope.Result.ID, savedEnvelope.Result.StateVersion), launcher, "sdd", "accept-revision", "--stdin", "--json", "--workspace", workspace), &acceptedEnvelope)
	var transitionEnvelope struct {
		Result sdd.Change `json:"result"`
	}
	decodeJSON(t, runWithInput(t, environment, workspace, fmt.Sprintf(`{"schemaVersion":1,"changeId":%q,"targetPhase":"proposal","expectedStateVersion":%d}`, change.ID, acceptedEnvelope.Result.StateVersion), launcher, "sdd", "transition", "--stdin", "--json", "--workspace", workspace), &transitionEnvelope)
	if transitionEnvelope.Result.Phase != sdd.PhaseProposal {
		t.Fatalf("SDD lifecycle did not advance: %+v", transitionEnvelope.Result)
	}

	var health bridge.Response
	decodeJSON(t, run(t, environment, workspace, launcher, "bridge", "status", "--workspace", workspace), &health)
	if !health.OK || health.Status != "healthy" || health.Workspace != canonicalPath(t, workspace) {
		t.Fatalf("unexpected bridge health: %#v", health)
	}

	response := nativeDispatch(t, environment, workspace, launcher, "isolated", bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Inspect the hermetic workspace", AcceptanceCriteria: []string{"Return a valid bounded result"},
	})
	if !response.OK || response.Status != "completed" || response.RunID == "" || response.TaskID == "" || response.Receipt == nil {
		t.Fatalf("unexpected dispatch response: %#v", response)
	}
	if response.Receipt.Decision != "allow" || response.Receipt.EventCount != 3 || response.Receipt.Provider != "opencode" {
		t.Fatalf("unexpected bounded receipt: %#v", response.Receipt)
	}
	var result struct {
		TaskID  string `json:"taskId"`
		AgentID string `json:"agentId"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal(response.Result, &result); err != nil || result.TaskID != response.TaskID || result.AgentID != "vgxness-worker" || result.Status != "success" {
		t.Fatalf("unexpected provider result %s: %#v err=%v", response.Result, result, err)
	}

	logs, err := filepath.Glob(filepath.Join(homeDirectory, ".vgxness", "projects", "*", "logs", "*.jsonl"))
	if err != nil || len(logs) != 1 {
		t.Fatalf("expected one Chronicle log, got %v: %v", logs, err)
	}
	logData, err := os.ReadFile(logs[0])
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(logData), []byte{'\n'})
	if len(lines) != 3 || !bytes.Contains(logData, []byte(`"type":"task.started"`)) || !bytes.Contains(logData, []byte(`"type":"task.completed"`)) || !bytes.Contains(logData, []byte(`"type":"result.accepted"`)) {
		t.Fatalf("unexpected Chronicle evidence:\n%s", logData)
	}

	started := nativeDispatch(t, environment, workspace, launcher, "start", bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Inspect continuity before implementation", Continuity: bridge.ContinuityStart,
	})
	if !started.OK || started.RunID == "" || started.CapsuleID == "" || started.StateVersion != 1 || len(started.MemoryRefs) != 2 {
		t.Fatalf("unexpected continuity start: %#v", started)
	}
	continued := nativeDispatch(t, environment, workspace, launcher, "continue", bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Inspect continuity before implementation", Continuity: bridge.ContinuityContinue, RunID: started.RunID,
	})
	if !continued.OK || continued.RunID != started.RunID || continued.CapsuleID == started.CapsuleID || continued.StateVersion != 2 || len(continued.MemoryRefs) != 3 {
		t.Fatalf("unexpected continuity continuation: %#v", continued)
	}
	projectRoots, err := filepath.Glob(filepath.Join(homeDirectory, ".vgxness", "projects", "*"))
	if err != nil || len(projectRoots) == 0 {
		t.Fatalf("expected project storage roots, got %v: %v", projectRoots, err)
	}
	continuityRoot := ""
	for _, root := range projectRoots {
		if info, statErr := os.Stat(filepath.Join(root, "current-run.json")); statErr == nil && info.Mode().IsRegular() {
			if continuityRoot != "" {
				t.Fatalf("multiple active continuity roots: %s and %s", continuityRoot, root)
			}
			continuityRoot = root
		}
	}
	if continuityRoot == "" {
		t.Fatalf("active continuity root is missing from %v", projectRoots)
	}
	currentData, err := os.ReadFile(filepath.Join(continuityRoot, "current-run.json"))
	if err != nil || !bytes.Contains(currentData, []byte(started.RunID)) || !bytes.Contains(currentData, []byte(continued.CapsuleID)) || !bytes.Contains(currentData, []byte(`"status": "paused"`)) {
		t.Fatalf("continuity pointer was not persisted: err=%v\n%s", err, currentData)
	}
	if info, err := os.Stat(filepath.Join(homeDirectory, ".vgxness", "memory.db")); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("global project-isolated memory store is missing: info=%v err=%v", info, err)
	}
	if _, err := os.Stat(filepath.Join(continuityRoot, "memory.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy per-project memory store was recreated: %v", err)
	}
	finished := nativeDispatch(t, environment, workspace, launcher, "finish", bridge.DispatchRequest{
		ProtocolVersion: bridge.ProtocolVersion, Model: "openai/gpt-5.6-sol", Operation: bridge.ReadFiles,
		Goal: "Inspect continuity before implementation", Continuity: bridge.ContinuityFinish, RunID: started.RunID,
	})
	if !finished.OK || finished.RunID != started.RunID || finished.CapsuleID == continued.CapsuleID || finished.StateVersion != 3 || len(finished.MemoryRefs) != 4 {
		t.Fatalf("unexpected continuity finish: %#v", finished)
	}
	if _, err := os.Stat(filepath.Join(continuityRoot, "current-run.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("terminal continuity pointer still exists: %v", err)
	}
	if info, err := os.Stat(filepath.Join(continuityRoot, "runs", started.RunID+".json")); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("terminal continuity snapshot is missing: info=%v err=%v", info, err)
	}
	logs, err = filepath.Glob(filepath.Join(continuityRoot, "logs", "*.jsonl"))
	if err != nil || len(logs) != 2 {
		t.Fatalf("expected isolated and continuity logs, got %v: %v", logs, err)
	}
	var continuityLog []byte
	for _, path := range logs {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if bytes.Contains(data, []byte(started.RunID)) {
			continuityLog = data
		}
	}
	if bytes.Count(continuityLog, []byte(`"type":"task.completed"`)) != 3 || bytes.Count(continuityLog, []byte(`"type":"memory.written"`)) != 3 || bytes.Count(continuityLog, []byte(`"type":"capsule.written"`)) != 3 || !bytes.Contains(continuityLog, []byte(`"type":"run.completed"`)) {
		t.Fatalf("continuity evidence is incomplete:\n%s", continuityLog)
	}
}

func reviewerPaths(configDirectory string, names []string) []string {
	paths := make([]string, 0, len(names))
	for _, name := range names {
		paths = append(paths, filepath.Join(configDirectory, "agents", name))
	}
	return paths
}

func nativeDispatch(t *testing.T, environment []string, workspace, launcher, suffix string, request bridge.DispatchRequest) bridge.Response {
	t.Helper()
	request.ParentSessionID = "ses_parent_" + suffix
	request.ParentMessageID = "msg_parent_" + suffix
	request.ChildSessionID = "ses_child_" + suffix
	request.TicketID = "ticket_" + suffix
	requestData, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var prepared bridge.Response
	decodeJSON(t, runWithInput(t, environment, workspace, string(requestData), launcher, "bridge", "prepare", "--workspace", workspace, "--stdin"), &prepared)
	if !prepared.OK || prepared.Prepared == nil || prepared.Prepared.TicketID == "" {
		t.Fatalf("unexpected native preparation: %#v", prepared)
	}
	result, err := json.Marshal(map[string]any{
		"kind": "agent.result", "schemaVersion": "1", "resultId": "result-" + suffix, "taskId": prepared.TaskID,
		"agentId": "vgxness-worker", "status": "success", "summary": "hermetic native dispatch completed", "artifacts": []any{},
		"nextRecommended": "inspect Chronicle evidence", "risks": []any{}, "errors": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	completionData, err := json.Marshal(bridge.NativeCompletionRequest{
		ProtocolVersion: bridge.ProtocolVersion, TicketID: prepared.Prepared.TicketID, ParentSessionID: request.ParentSessionID,
		ChildSessionID: request.ChildSessionID, MessageID: "msg_child_" + suffix, Result: result,
	})
	if err != nil {
		t.Fatal(err)
	}
	var completed bridge.Response
	decodeJSON(t, runWithInput(t, environment, workspace, string(completionData), launcher, "bridge", "complete", "--workspace", workspace, "--stdin"), &completed)
	return completed
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	return canonicalPath(t, filepath.Join(filepath.Dir(file), "..", ".."))
}

func canonicalPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(resolved)
}

func executableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func build(t *testing.T, goExecutable, repository, output, packagePath string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, goExecutable, "build", "-trimpath", "-o", output, packagePath)
	command.Dir = repository
	command.Env = replaceEnvironment(os.Environ(), map[string]string{"GOPROXY": "off", "GOTOOLCHAIN": "local"}, "GOPROXY", "GOTOOLCHAIN")
	combined, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("build %s: %v\n%s", packagePath, err, combined)
	}
}

func isolatedEnvironment(home, temporary, fakeBin string) []string {
	path := fakeBin + string(os.PathListSeparator) + os.Getenv("PATH")
	return replaceEnvironment(os.Environ(), map[string]string{
		"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config"), "TMPDIR": temporary,
		"TMP": temporary, "TEMP": temporary, "PATH": path,
	}, "HOME", "XDG_CONFIG_HOME", "TMPDIR", "TMP", "TEMP", "PATH", "OPENCODE_", "VGXNESS_")
}

func replaceEnvironment(current []string, additions map[string]string, denied ...string) []string {
	filtered := make([]string, 0, len(current)+len(additions))
	for _, entry := range current {
		name, _, found := strings.Cut(entry, "=")
		if !found || deniedEnvironment(name, denied) {
			continue
		}
		filtered = append(filtered, entry)
	}
	for name, value := range additions {
		filtered = append(filtered, name+"="+value)
	}
	return filtered
}

func deniedEnvironment(name string, denied []string) bool {
	for _, item := range denied {
		if strings.HasSuffix(item, "_") && strings.HasPrefix(name, item) || name == item {
			return true
		}
	}
	return false
}

func run(t *testing.T, environment []string, directory, executable string, args ...string) string {
	t.Helper()
	return runWithInput(t, environment, directory, "", executable, args...)
}

func runWithInput(t *testing.T, environment []string, directory, input, executable string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, args...)
	command.Dir = directory
	command.Env = environment
	command.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("run %s %q: %v\nstdout:\n%s\nstderr:\n%s", executable, args, err, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr from %s %q:\n%s", executable, args, stderr.String())
	}
	return stdout.String()
}

func decodeJSON(t *testing.T, data string, target any) {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		t.Fatalf("decode JSON response: %v\n%s", err, data)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		t.Fatalf("invalid trailing JSON in response: %v\n%s", err, data)
	}
}
