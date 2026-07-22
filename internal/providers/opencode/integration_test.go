package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vgxness/vgxness/internal/bridge"
	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/launcher"
	"github.com/vgxness/vgxness/internal/testutil"
)

const integrationTestModel = "openai/gpt-5.6-sol"

func TestIntegration_PreviewIsNonMutatingAndUsesGlobalOpenCodePath(t *testing.T) {
	home := t.TempDir()
	service := NewIntegration()
	result, err := service.Preview(context.Background(), integration.Options{HomeDir: home, Model: integrationTestModel})
	testutil.NoError(t, err)
	expected := filepath.Join(home, ".config", "opencode", "agents", managerAgentName)
	expectedTool := filepath.Join(home, ".config", "opencode", "plugins", bridgePluginName)
	_, statErr := os.Stat(filepath.Join(home, ".config"))
	testutil.Require(t, result.Provider == "opencode" && result.State == integration.StateAbsent && result.Bridge == integration.BridgeUnavailable && result.Path == expected && result.ToolPath == expectedTool && result.Changed && len(result.ArtifactSHA256) == 64 && len(result.ToolSHA256) == 64, "unexpected preview: %#v", result)
	testutil.Require(t, os.IsNotExist(statErr), "preview mutated filesystem: %v", statErr)
}

func TestIntegration_InstallReadbackStatusAndIdempotence(t *testing.T) {
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	service := NewIntegration()
	options := integration.Options{ConfigDir: configDirectory, Model: integrationTestModel}
	installed, err := service.Install(context.Background(), options)
	testutil.NoError(t, err)
	data, err := os.ReadFile(installed.Path)
	testutil.NoError(t, err)
	toolData, err := os.ReadFile(installed.ToolPath)
	testutil.NoError(t, err)
	expectedTool, err := bridgeToolContent(service.executable, integrationTestModel)
	testutil.NoError(t, err)
	info, err := os.Stat(installed.Path)
	testutil.NoError(t, err)
	toolInfo, err := os.Stat(installed.ToolPath)
	testutil.NoError(t, err)
	for name, expected := range map[string]string{
		navigatorAgentName: navigatorPrompt, explorerAgentName: explorerPrompt, implementerAgentName: implementerPrompt, reviewerAgentName: reviewerPrompt,
	} {
		content, readErr := os.ReadFile(filepath.Join(configDirectory, "agents", name))
		testutil.NoError(t, readErr)
		testutil.Require(t, string(content) == expected, "unexpected managed subagent %s", name)
	}
	testutil.Require(t, installed.State == integration.StateInstalled && installed.Bridge == integration.BridgeConfigured && installed.Changed && string(data) == managerPrompt && string(toolData) == string(expectedTool), "unexpected install: %#v", installed)
	if runtime.GOOS != "windows" {
		testutil.Require(t, info.Mode().Perm() == 0o600 && toolInfo.Mode().Perm() == 0o600, "artifact modes=%o/%o", info.Mode().Perm(), toolInfo.Mode().Perm())
	}
	status, err := service.Status(context.Background(), integration.Options{ConfigDir: configDirectory})
	testutil.NoError(t, err)
	testutil.Require(t, status.State == integration.StateInstalled && status.Bridge == integration.BridgeConfigured && status.Model == integrationTestModel && !status.Changed && status.ArtifactSHA256 == managerPromptSHA256() && status.ToolSHA256 == artifactSHA256(expectedTool), "unexpected status: %#v", status)
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
	testutil.Require(t, status.State == integration.StatePartial && status.Bridge == integration.BridgeUnavailable, "unexpected partial status: %#v", status)
	installed, err := service.Install(context.Background(), integration.Options{ConfigDir: configDirectory, Model: integrationTestModel})
	testutil.NoError(t, err)
	after, err := os.Stat(managerPath)
	testutil.NoError(t, err)
	_, toolErr := os.Stat(installed.ToolPath)
	testutil.Require(t, installed.State == integration.StateInstalled && installed.Changed && os.SameFile(before, after) && toolErr == nil, "partial repair replaced existing artifact: %#v tool=%v", installed, toolErr)
}

func TestIntegration_RefusesForeignBridgeToolWithoutTouchingManager(t *testing.T) {
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	toolPath := filepath.Join(configDirectory, "plugins", bridgePluginName)
	testutil.NoError(t, os.MkdirAll(filepath.Dir(toolPath), 0o700))
	testutil.NoError(t, os.WriteFile(toolPath, []byte("user-owned tool\n"), 0o600))
	service := NewIntegration()
	_, err := service.Install(context.Background(), integration.Options{ConfigDir: configDirectory, Model: integrationTestModel})
	data, readErr := os.ReadFile(toolPath)
	_, managerErr := os.Stat(filepath.Join(configDirectory, "agents", managerAgentName))
	testutil.NoError(t, readErr)
	testutil.Require(t, errors.Is(err, integration.ErrConflict) && string(data) == "user-owned tool\n" && os.IsNotExist(managerErr), "foreign tool was touched: err=%v data=%q manager=%v", err, data, managerErr)
}

func TestIntegration_BridgeToolUsesPortableArgumentVectorAndTrustedExecutable(t *testing.T) {
	service := NewIntegration()
	content, err := bridgeToolContent(service.executable, integrationTestModel)
	testutil.NoError(t, err)
	tool := string(content)
	for _, required := range []string{
		`import { spawn } from "node:child_process"`, "spawn(VGXNESS_EXECUTABLE, [...args", `const VGXNESS_EXECUTABLE = "`,
		`const VGXNESS_MODEL = "openai/gpt-5.6-sol"`, fmt.Sprintf("const MAX_OUTPUT_BYTES = %d", bridge.MaxBridgeOutputBytes),
		`client.session.create`, `client.session.prompt`, `["bridge", "prepare", "--stdin"]`, `["bridge", "read", "--stdin"]`,
		`["bridge", "complete", "--stdin"]`, `value.protocolVersion !== "1"`, `"review-changes"`,
		`tool.schema.enum(["start", "continue", "finish"]).optional()`, `runId: tool.schema.string().optional()`,
		`"native-subagent-deadline"`, `envelope.status === "recovered"`, `return JSON.stringify(envelope)`, "artifact: opencode-plugin/vgxness; version: 11",
		"shell: false", `child?.kill("SIGKILL")`, "invokeBounded", "invokeTerminal", "nativeTickets.delete(childSessionId)",
		"MAX_NATIVE_DISPATCHES = 4", "acquireNativeCapacity", "VGXNESS native dispatch capacity exhausted", "releaseCapacity()",
		"vgxness_orchestrate", "vgxness-navigator", `["bridge", "orchestrate-plan", "--stdin"]`, `["bridge", "orchestrate-wave", "--stdin"]`,
		`["bridge", "orchestrate-terminal", "--stdin"]`, `["bridge", "orchestrate-join", "--stdin"]`, "Promise.allSettled(bindings.map",
	} {
		testutil.Require(t, strings.Contains(tool, required), "bridge plugin missing %q: %s", required, content)
	}
	for _, forbidden := range []string{`"bridge", "dispatch"`, "Bun.spawn", "run-command", "shell: true"} {
		testutil.Require(t, !strings.Contains(tool, forbidden), "bridge plugin contains unsafe %q: %s", forbidden, content)
	}
	testutil.Require(t, strings.Contains(managerPrompt, "vgxness_dispatch with review-changes") && strings.Contains(managerPrompt, "Do not substitute read-files"), "manager does not route Git review explicitly: %s", managerPrompt)
}

func TestManagerPromptDefinesPersonalityLanguageAndAuthorityContracts(t *testing.T) {
	required := []string{
		"artifact: opencode-agent/vgxness-manager; version: 8",
		"managed VGXNESS plugin launches native OpenCode subagents",
		"calm, attentive, technically discerning, and collaborative",
		"has a point of view",
		"Match the language and register of the user's direct conversation",
		"technical artifacts neutral and in English by default",
		"Use vgxness_status only to check bridge health and compatibility",
		"Use vgxness_orchestrate for a goal that benefits from adaptive decomposition",
		"vgxness_dispatch with read-files",
		"Native write-files is fail-closed",
		"vgxness_dispatch with review-changes",
		"at most four active one-shot native dispatches per workspace",
		"never parallelize writes, review phases, or any dispatch that uses continuity",
		"use continuity=start on the first dispatch",
		"use continuity=continue with that runId",
		"use continuity=finish with the same runId",
		"validated prior capsule and curated VGXNESS memory context",
		"outcome, the evidence that supports it, any meaningful limitation, and the recommended next step",
		"Ask at most one blocking question at a time",
	}
	for _, contract := range required {
		if !strings.Contains(managerPrompt, contract) {
			t.Errorf("manager prompt is missing contract %q", contract)
		}
	}

	forbidden := []string{"gentle-orchestrator", "Judgment Day", "sdd-apply", "sdd-verify"}
	for _, importedMechanic := range forbidden {
		if strings.Contains(managerPrompt, importedMechanic) {
			t.Errorf("manager prompt imports incompatible mechanic %q", importedMechanic)
		}
	}
}

func TestManagedNativeSubagentsHaveRoleSpecificFailClosedPermissions(t *testing.T) {
	for name, prompt := range map[string]string{
		"navigator": navigatorPrompt, "explorer": explorerPrompt, "implementer": implementerPrompt, "reviewer": reviewerPrompt,
	} {
		for _, required := range []string{"mode: subagent", "hidden: true", `"*": deny`, "task: deny", "exact content-bound prompt"} {
			if !strings.Contains(prompt, required) {
				t.Errorf("%s profile missing %q", name, required)
			}
		}
	}
	if strings.Contains(explorerPrompt, "\n  read: allow\n") || strings.Contains(explorerPrompt, "edit: allow") || strings.Contains(explorerPrompt, "grep: allow") || strings.Contains(explorerPrompt, "lsp: allow") || strings.Contains(explorerPrompt, "codegraph_*: allow") || !strings.Contains(explorerPrompt, "vgxness_native_read: allow") {
		t.Fatal("explorer exposes an alternate content-access path")
	}
	if strings.Contains(implementerPrompt, "\n  read: allow\n") || strings.Contains(implementerPrompt, "edit: allow") || strings.Contains(implementerPrompt, "write: allow") || strings.Contains(implementerPrompt, "grep: allow") || strings.Contains(implementerPrompt, "lsp: allow") || strings.Contains(implementerPrompt, "codegraph_*: allow") || !strings.Contains(implementerPrompt, "vgxness_native_read: allow") {
		t.Fatal("reserved implementer profile can edit without a ticket-authenticated broker")
	}
	if strings.Contains(reviewerPrompt, "read: allow") || strings.Contains(reviewerPrompt, "codegraph_*: allow") {
		t.Fatal("reviewer can escape immutable Git evidence")
	}
}

func TestIntegration_RequiresExplicitSafeModelForPreviewAndInstall(t *testing.T) {
	service := NewIntegration()
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	for _, options := range []integration.Options{
		{ConfigDir: configDirectory},
		{ConfigDir: configDirectory, Model: "invalid"},
		{ConfigDir: configDirectory, Model: "--auto/model"},
		{ConfigDir: configDirectory, Model: "openai/model/extra"},
	} {
		if _, err := service.Preview(context.Background(), options); !errors.Is(err, integration.ErrInvalid) {
			t.Fatalf("preview options=%#v error=%v", options, err)
		}
		if _, err := service.Install(context.Background(), options); !errors.Is(err, integration.ErrInvalid) {
			t.Fatalf("install options=%#v error=%v", options, err)
		}
	}
	if _, err := os.Stat(configDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid model mutated config directory: %v", err)
	}
}

func TestIntegration_BridgeToolRunsWithNodeAndBunWhenAvailable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("portable runtime smoke helper uses a POSIX executable")
	}
	engines := make([]string, 0, 2)
	for _, name := range []string{"node", "bun"} {
		if executable, err := exec.LookPath(name); err == nil {
			engines = append(engines, executable)
		}
	}
	if len(engines) == 0 {
		t.Skip("Node and Bun are not installed")
	}

	root := t.TempDir()
	helper := filepath.Join(root, "vgxness-helper")
	expected := `{"protocolVersion":"1","ok":true,"bridge":"healthy","provider":"opencode","status":"healthy"}`
	prepared := `{"protocolVersion":"1","ok":true,"bridge":"healthy","provider":"opencode","status":"running","prepared":{"ticketId":"ticket-1","executionId":"execution-1","agent":"vgxness-explorer","model":"openai/gpt-5.6-sol","prompt":"return json","promptSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","deadline":"` + time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano) + `","promptRef":{"id":"prompt","version":"1","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}`
	read := `{"protocolVersion":"1","ok":true,"bridge":"healthy","provider":"opencode","status":"reading","read":{"path":"go.mod","content":"module example","truncated":false}}`
	helperSource := "#!/bin/sh\n" +
		"if [ \"$2\" = \"prepare\" ]; then\n" +
		"  payload=$(cat)\n" +
		"  case \"$payload\" in\n" +
		"    *'\"model\":\"openai/gpt-5.6-sol\"'*) ;;\n" +
		"    *) exit 9 ;;\n" +
		"  esac\n" +
		"  printf '%s' '" + prepared + "'\n" +
		"elif [ \"$2\" = \"complete\" ]; then\n" +
		"  cat >/dev/null\n" +
		"  printf '%s' '" + expected + "'\n" +
		"elif [ \"$2\" = \"read\" ]; then\n" +
		"  payload=$(cat)\n" +
		"  case \"$payload\" in\n" +
		"    *'\"ticketId\":\"ticket-1\"'*'\"childSessionId\":\"ses_child\"'*'\"path\":\"go.mod\"'*) ;;\n" +
		"    *) exit 10 ;;\n" +
		"  esac\n" +
		"  printf '%s' '" + read + "'\n" +
		"elif [ \"$2\" = \"fail\" ]; then\n" +
		"  cat >/dev/null\n" +
		"  printf '%s' '" + expected + "'\n" +
		"else\n" +
		"  printf '%s' '" + expected + "'\n" +
		"fi\n" +
		"exit 0\n"
	testutil.NoError(t, os.WriteFile(helper, []byte(helperSource), 0o700))
	content, err := bridgeToolContent(helper, integrationTestModel)
	testutil.NoError(t, err)
	stub := `const optionalSchema = () => ({ optional() { return this } })
const tool = Object.assign((definition) => definition, {
  schema: { enum: optionalSchema, string: optionalSchema, array: optionalSchema },
})`
	source := strings.Replace(string(content), `import { tool } from "@opencode-ai/plugin"`, stub, 1)
	source = strings.Replace(source, `import { randomUUID } from "node:crypto"`, `const randomUUID = () => "1"`, 1)
	workspace, err := json.Marshal(root)
	testutil.NoError(t, err)
	source += "\nlet plugin\n"
	source += "\nconst client = { session: {\n"
	source += "  create: async () => ({ data: { id: \"ses_child\" } }),\n"
	source += "  prompt: async () => { const value = JSON.parse(await plugin.tool.vgxness_native_read.execute({ path: \"go.mod\" }, { directory: " + string(workspace) + ", worktree: \"\", sessionID: \"ses_child\", abort: new AbortController().signal })); if (value.content !== \"module example\") throw new Error(\"native read broker failed\"); return { data: { info: { id: \"msg_child\" }, parts: [{ type: \"text\", text: JSON.stringify({ kind: \"agent.result\", schemaVersion: \"1\", resultId: \"result-1\", taskId: \"task-1\", agentId: \"vgxness-bounded-worker-v1\", status: \"success\", summary: \"ok\", findings: [], changes: [], validations: [], artifactRefs: [], memoryCandidates: [], nextRecommended: \"done\", confidence: 1 }) }] } } },\n"
	source += "  abort: async () => ({ data: true }),\n"
	source += "} }\n"
	source += "plugin = await VGXNESSPlugin({ client })\n"
	source += "const context = { directory: " + string(workspace) + ", worktree: \"\", sessionID: \"ses_parent\", messageID: \"msg_parent\", abort: new AbortController().signal, metadata() {} }\n"
	source += "const statusOutput = await plugin.tool.vgxness_status.execute({}, context)\n"
	source += "const dispatchOutput = await plugin.tool.vgxness_dispatch.execute({ operation: \"read-files\", goal: \"inspect\" }, context)\n"
	source += "process.stdout.write(statusOutput + \"\\n\" + dispatchOutput)\n"
	script := filepath.Join(root, "bridge.mjs")
	testutil.NoError(t, os.WriteFile(script, []byte(source), 0o600))

	for _, engine := range engines {
		t.Run(filepath.Base(engine), func(t *testing.T) {
			command := exec.Command(engine, script)
			output, runErr := command.CombinedOutput()
			if runErr != nil {
				t.Fatalf("generated plugin runtime failed: %v\n%s", runErr, output)
			}
			testutil.Require(t, string(output) == expected+"\n"+expected, "%s bridge output=%q", engine, output)
		})
	}
}

func TestIntegration_OrchestrateCreatesNativeNavigatorAndParallelChildren(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("portable runtime smoke helper uses a POSIX executable")
	}
	engines := make([]string, 0, 2)
	for _, name := range []string{"node", "bun"} {
		if executable, err := exec.LookPath(name); err == nil {
			engines = append(engines, executable)
		}
	}
	if len(engines) == 0 {
		t.Skip("Node and Bun are not installed")
	}

	root := t.TempDir()
	helper := filepath.Join(root, "vgxness-helper")
	plan := `{"protocolVersion":"1","ok":true,"bridge":"healthy","provider":"opencode","status":"pending","orchestration":{"orchestrationId":"orchestration-1","scheduleId":"schedule-1","ownerId":"owner-1","status":"pending","currentWave":0,"plan":{"kind":"delegation.plan","schemaVersion":"1","planId":"plan-1","requestDigest":"sha256-1","decision":"parallel","rationale":"two independent reads","policyVersion":"bridge-balanced-v1","maxParallel":4,"tasks":[{"taskId":"task-a","capability":"explore","operation":"read-files","goal":"inspect a","acceptanceCriteria":[],"dependsOn":[],"continuity":"isolated"},{"taskId":"task-b","capability":"explore","operation":"read-files","goal":"inspect b","acceptanceCriteria":[],"dependsOn":[],"continuity":"isolated"}],"waves":[{"waveId":"wave-1","index":0,"mode":"parallel","taskIds":["task-a","task-b"]}]}}}`
	wave := `{"protocolVersion":"1","ok":true,"bridge":"healthy","provider":"opencode","status":"running","orchestration":{"orchestrationId":"orchestration-1","scheduleId":"schedule-1","ownerId":"owner-1","status":"running","currentWave":0,"plan":{"kind":"delegation.plan","schemaVersion":"1","planId":"plan-1","requestDigest":"sha256-1","decision":"parallel","rationale":"two independent reads","policyVersion":"bridge-balanced-v1","maxParallel":4,"tasks":[{"taskId":"task-a","capability":"explore","operation":"read-files","goal":"inspect a","acceptanceCriteria":[],"dependsOn":[],"continuity":"isolated"},{"taskId":"task-b","capability":"explore","operation":"read-files","goal":"inspect b","acceptanceCriteria":[],"dependsOn":[],"continuity":"isolated"}],"waves":[{"waveId":"wave-1","index":0,"mode":"parallel","taskIds":["task-a","task-b"]}]},"prepared":[{"taskId":"task-a","prepared":{"ticketId":"ticket-1","executionId":"execution-1","agent":"vgxness-explorer","model":"openai/gpt-5.6-sol","prompt":"inspect a","promptSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","deadline":"2099-01-01T00:00:00Z","promptRef":{"id":"prompt-a","version":"1","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}},{"taskId":"task-b","prepared":{"ticketId":"ticket-2","executionId":"execution-2","agent":"vgxness-explorer","model":"openai/gpt-5.6-sol","prompt":"inspect b","promptSha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","deadline":"2099-01-01T00:00:00Z","promptRef":{"id":"prompt-b","version":"1","sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}}]}}`
	terminal := `{"protocolVersion":"1","ok":true,"bridge":"healthy","provider":"opencode","status":"running","orchestration":{"orchestrationId":"orchestration-1","scheduleId":"schedule-1","ownerId":"owner-1","status":"running","currentWave":0,"plan":{"decision":"parallel","tasks":[],"waves":[]}}}`
	joined := `{"protocolVersion":"1","ok":true,"bridge":"healthy","provider":"opencode","status":"completed","orchestration":{"orchestrationId":"orchestration-1","scheduleId":"schedule-1","ownerId":"owner-1","status":"completed","currentWave":0,"plan":{"decision":"parallel","tasks":[],"waves":[]},"join":{"kind":"delegation.join","status":"completed"}}}`
	helperSource := "#!/bin/sh\n" +
		"cat >/dev/null\n" +
		"case \"$2\" in\n" +
		"  orchestrate-plan) printf '%s' '" + plan + "' ;;\n" +
		"  orchestrate-wave) printf '%s' '" + wave + "' ;;\n" +
		"  orchestrate-terminal|orchestrate-status) printf '%s' '" + terminal + "' ;;\n" +
		"  orchestrate-join) printf '%s' '" + joined + "' ;;\n" +
		"  complete|fail|orchestrate-cancel) printf '%s' '{\"protocolVersion\":\"1\",\"ok\":true,\"bridge\":\"healthy\",\"provider\":\"opencode\",\"status\":\"completed\"}' ;;\n" +
		"  *) exit 9 ;;\n" +
		"esac\n"
	testutil.NoError(t, os.WriteFile(helper, []byte(helperSource), 0o700))
	content, err := bridgeToolContent(helper, integrationTestModel)
	testutil.NoError(t, err)
	stub := `const optionalSchema = () => ({ optional() { return this } })
const tool = Object.assign((definition) => definition, {
  schema: { enum: optionalSchema, string: optionalSchema, array: optionalSchema },
})`
	source := strings.Replace(string(content), `import { tool } from "@opencode-ai/plugin"`, stub, 1)
	source = strings.Replace(source, `import { randomUUID } from "node:crypto"`, `let uuidCounter = 0; const randomUUID = () => String(++uuidCounter)`, 1)
	workspace, err := json.Marshal(root)
	testutil.NoError(t, err)
	source += "\nlet plugin, created = 0, active = 0, peak = 0\n"
	source += "const parentIDs = []\n"
	source += "const client = { session: {\n"
	source += "  create: async (request) => { created++; parentIDs.push(request.body.parentID); return { data: { id: created === 1 ? 'ses_nav' : 'ses_task_' + (created - 1) } } },\n"
	source += "  prompt: async (request) => { if (request.path.id === 'ses_nav') return { data: { info: { id: 'msg_nav' }, parts: [{ type: 'text', text: JSON.stringify({ tasks: [{ taskId: 'task-a', capability: 'explore', operation: 'read-files', goal: 'inspect a', acceptanceCriteria: [], dependsOn: [], continuity: 'isolated' }, { taskId: 'task-b', capability: 'explore', operation: 'read-files', goal: 'inspect b', acceptanceCriteria: [], dependsOn: [], continuity: 'isolated' }] }) }] } }; active++; peak = Math.max(peak, active); await new Promise((resolve) => setTimeout(resolve, 40)); active--; return { data: { info: { id: 'msg_' + request.path.id }, parts: [{ type: 'text', text: JSON.stringify({ kind: 'agent.result', status: 'success' }) }] } } },\n"
	source += "  abort: async () => ({ data: true }),\n"
	source += "} }\n"
	source += "plugin = await VGXNESSPlugin({ client })\n"
	source += "const context = { directory: " + string(workspace) + ", worktree: '', sessionID: 'ses_parent', messageID: 'msg_parent', abort: new AbortController().signal, metadata() {} }\n"
	source += "const output = JSON.parse(await plugin.tool.vgxness_orchestrate.execute({ goal: 'inspect both' }, context))\n"
	source += "if (!output.ok || output.status !== 'completed' || created !== 3 || peak !== 2 || parentIDs.some((id) => id !== 'ses_parent')) throw new Error(JSON.stringify({ output, created, peak, parentIDs }))\n"
	source += "process.stdout.write('created=' + created + ' peak=' + peak)\n"
	script := filepath.Join(root, "orchestrate.mjs")
	testutil.NoError(t, os.WriteFile(script, []byte(source), 0o600))

	for _, engine := range engines {
		t.Run(filepath.Base(engine), func(t *testing.T) {
			command := exec.Command(engine, script)
			output, runErr := command.CombinedOutput()
			if runErr != nil {
				t.Fatalf("generated orchestration runtime failed: %v\n%s", runErr, output)
			}
			testutil.Require(t, string(output) == "created=3 peak=2", "%s orchestration output=%q", engine, output)
		})
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
		SchemaVersion: launcher.SchemaVersion, ManagedBy: launcher.ManagedBy,
		LauncherPath: launcherPath, LauncherSHA256: launcherDigest, DataDir: dataDir,
		ActivePath: activePath, ActiveSHA256: activeDigest, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
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

func TestIntegration_BridgeToolParsesWithBunWhenAvailable(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("Bun is not installed")
	}
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	service := NewIntegration()
	installed, err := service.Install(context.Background(), integration.Options{ConfigDir: configDirectory, Model: integrationTestModel})
	testutil.NoError(t, err)
	output := filepath.Join(t.TempDir(), "vgxness.js")
	command := exec.Command(bun, "build", installed.ToolPath, "--target=bun", "--external", "@opencode-ai/plugin", "--outfile", output)
	if data, runErr := command.CombinedOutput(); runErr != nil {
		t.Fatalf("generated bridge tool did not parse: %v\n%s", runErr, data)
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
	_, installErr := service.Install(context.Background(), integration.Options{ConfigDir: configDirectory, Model: integrationTestModel})
	after, err := os.ReadFile(target)
	testutil.NoError(t, err)
	testutil.Require(t, status.State == integration.StateDrifted && errors.Is(installErr, integration.ErrConflict) && string(after) == string(foreign), "status=%#v install=%v after=%q", status, installErr, after)
}

func TestIntegration_RefusesSymlinkArtifact(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on Windows")
	}
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
	_, installErr := service.Install(context.Background(), integration.Options{ConfigDir: configDirectory, Model: integrationTestModel})
	data, err := os.ReadFile(foreign)
	testutil.NoError(t, err)
	testutil.Require(t, status.State == integration.StateDrifted && errors.Is(installErr, integration.ErrConflict) && string(data) == "foreign", "status=%#v install=%v foreign=%q", status, installErr, data)
}

func TestIntegration_RefusesSymlinkConfigDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on Windows")
	}
	root := t.TempDir()
	foreign := filepath.Join(root, "foreign-config")
	configDirectory := filepath.Join(root, "opencode")
	testutil.NoError(t, os.MkdirAll(foreign, 0o700))
	testutil.NoError(t, os.Symlink(foreign, configDirectory))
	service := NewIntegration()
	status, err := service.Status(context.Background(), integration.Options{ConfigDir: configDirectory})
	testutil.NoError(t, err)
	_, installErr := service.Install(context.Background(), integration.Options{ConfigDir: configDirectory, Model: integrationTestModel})
	_, foreignErr := os.Stat(filepath.Join(foreign, "agents"))
	testutil.Require(t, status.State == integration.StateDrifted && errors.Is(installErr, integration.ErrConflict) && os.IsNotExist(foreignErr), "status=%#v install=%v foreign=%v", status, installErr, foreignErr)
}

func TestIntegration_UninstallIsRecoverableAndRefusesDrift(t *testing.T) {
	configDirectory := filepath.Join(t.TempDir(), "opencode")
	service := NewIntegration()
	service.now = func() time.Time { return time.Date(2026, 7, 21, 12, 34, 56, 7, time.UTC) }
	options := integration.Options{ConfigDir: configDirectory, Model: integrationTestModel}
	installed, err := service.Install(context.Background(), options)
	testutil.NoError(t, err)
	removed, err := service.Uninstall(context.Background(), options)
	testutil.NoError(t, err)
	backup, err := os.ReadFile(removed.BackupPath)
	testutil.NoError(t, err)
	toolBackup, err := os.ReadFile(removed.ToolBackupPath)
	testutil.NoError(t, err)
	expectedTool, err := bridgeToolContent(service.executable, integrationTestModel)
	testutil.NoError(t, err)
	_, targetErr := os.Stat(installed.Path)
	_, toolTargetErr := os.Stat(installed.ToolPath)
	subagentsRemoved := true
	for _, name := range []string{navigatorAgentName, explorerAgentName, implementerAgentName, reviewerAgentName} {
		if _, statErr := os.Stat(filepath.Join(configDirectory, "agents", name)); !os.IsNotExist(statErr) {
			subagentsRemoved = false
		}
	}
	testutil.Require(t, removed.State == integration.StateAbsent && removed.Bridge == integration.BridgeUnavailable && removed.Changed && strings.Contains(removed.BackupPath, "20260721T123456") && string(backup) == managerPrompt && string(toolBackup) == string(expectedTool) && os.IsNotExist(targetErr) && os.IsNotExist(toolTargetErr) && subagentsRemoved, "unexpected uninstall: %#v target=%v tool=%v", removed, targetErr, toolTargetErr)
	second, err := service.Uninstall(context.Background(), options)
	testutil.NoError(t, err)
	testutil.Require(t, second.State == integration.StateAbsent && !second.Changed, "uninstall was not idempotent: %#v", second)

	testutil.NoError(t, serviceInstallForDrift(service, options))
	testutil.NoError(t, os.WriteFile(installed.Path, []byte("changed"), 0o600))
	_, err = service.Uninstall(context.Background(), options)
	testutil.Require(t, errors.Is(err, integration.ErrDrift), "drifted uninstall error=%v", err)
}

func TestIntegration_InvalidAndCancelledRequestsDoNotMutate(t *testing.T) {
	service := NewIntegration()
	_, err := service.Preview(context.Background(), integration.Options{ConfigDir: "relative", Model: integrationTestModel})
	testutil.Require(t, errors.Is(err, integration.ErrInvalid), "relative config error=%v", err)
	root := filepath.Join(t.TempDir(), "opencode")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = service.Install(ctx, integration.Options{ConfigDir: root, Model: integrationTestModel})
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

func serviceInstallForDrift(service *Integration, options integration.Options) error {
	_, err := service.Install(context.Background(), options)
	return err
}
