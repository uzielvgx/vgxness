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
	"strconv"
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
		`client.session.create`, `client.session.prompt`, `["bridge", "prepare", "--stdin"]`, `["bridge", "read", "--stdin"]`, `["bridge", "edit", "--stdin"]`, `["bridge", "codegraph", "--stdin"]`,
		`["bridge", "complete", "--stdin"]`, `value.protocolVersion !== "1"`, `"review-changes"`,
		`tool.schema.enum(["start", "continue", "finish"]).optional()`, `runId: tool.schema.string().optional()`,
		`"native-subagent-deadline"`, `envelope.status === "recovered"`, `output: JSON.stringify(envelope)`, "artifact: opencode-plugin/vgxness; version: 29",
		"shell: false", `child?.kill("SIGKILL")`, "invokeBounded", "invokeTerminal", "nativeTickets.delete(childSessionId)",
		"MAX_NATIVE_DISPATCHES = 4", "acquireNativeCapacity", "VGXNESS native dispatch capacity exhausted", "releaseCapacity()",
		"vgxness_run", "startVisibleOrchestration", `tool.schema.enum(["fast", "auto", "deep"]).optional()`,
		"boundedRunStrings", "VGXNESS desired outcome exceeded its bound",
		"vgxness_orchestrate", "vgxness-navigator", `["bridge", "orchestrate-plan", "--stdin"]`, `["bridge", "orchestrate-wave", "--stdin"]`,
		`["bridge", "orchestrate-terminal", "--stdin"]`, `["bridge", "orchestrate-join", "--stdin"]`,
		"vgxness_task_claim", "vgxness_task_complete", "visibleWaveClaims", "prepareVisibleWave",
		`arguments: {`, `subagent_type: agent`, `status: "delegation-required"`, `client.session.get`, `"tool.execute.before"`,
		`kind: "vgxness.direct-dispatch.directive"`,
		`action: tool.schema.enum(["start", "advance"]).optional()`, "parentSessionId: context.sessionID",
		`action: tool.schema.enum(["start", "join"]).optional()`, `"task-dispatch-"`, "advanceVisibleOrchestration",
		`depth: tool.schema.number().optional()`, `maxFiles: tool.schema.number().optional()`, "withNativeTicketLane", "bridgeFailure",
		`active.operation !== "analyze-structure"`, `authorizedOperation: active.operation`, `operation: task.operation`,
	} {
		testutil.Require(t, strings.Contains(tool, required), "bridge plugin missing %q: %s", required, content)
	}
	for _, forbidden := range []string{`"bridge", "dispatch"`, "Bun.spawn", "run-command", "shell: true"} {
		testutil.Require(t, !strings.Contains(tool, forbidden), "bridge plugin contains unsafe %q: %s", forbidden, content)
	}
	testutil.Require(t, strings.Contains(managerPrompt, "vgxness_dispatch action=start with analyze-structure") && strings.Contains(managerPrompt, "vgxness_dispatch action=start with review-changes") && strings.Contains(managerPrompt, "Do not substitute read-files"), "manager does not route structural and Git review explicitly: %s", managerPrompt)
}

func TestManagerPromptDefinesPersonalityLanguageAndAuthorityContracts(t *testing.T) {
	expectedFrontmatter := `---
description: VGXNESS manager — OpenCode interface to the VGXNESS control plane
mode: primary
color: primary
permission:
  "*": deny
  question: allow
  task:
    "*": deny
    vgxness-explorer: allow
    vgxness-implementer: allow
    vgxness-reviewer: allow
  vgxness_status: allow
  vgxness_run: allow
  vgxness_dispatch: allow
  vgxness_orchestrate: allow
---

<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-manager; version: 17 -->`
	if !strings.HasPrefix(managerPrompt, expectedFrontmatter) {
		t.Fatalf("manager prompt has invalid OpenCode frontmatter:\n%s", managerPrompt)
	}

	required := []string{
		"artifact: opencode-agent/vgxness-manager; version: 17",
		"exact native Task directives returned by vgxness_run, vgxness_dispatch, or vgxness_orchestrate",
		"senior engineer with more than two decades of experience",
		"calm, attentive, technically discerning, pragmatic, and collaborative",
		"has a point of view",
		"challenge unnecessary complexity respectfully",
		"Match the language and register of the user's direct conversation",
		"technical artifacts neutral and in English by default",
		"Optimize for the user's outcome and time, not for visible orchestration activity",
		"Answer directly when the user is chatting, asking a conceptual question",
		"prefer the goal-first vgxness_run entrypoint",
		"Select mode fast when the user explicitly prioritizes speed",
		"Keep vgxness_dispatch and vgxness_orchestrate for explicit low-level control",
		"Do not run a health check by habit",
		"Scale verification to risk and uncertainty",
		"Flexibility changes route selection, not authority",
		"Use vgxness_status only to check bridge health and compatibility",
		"Use vgxness_run as the normal goal-first entrypoint",
		"call vgxness_run with action=advance and only the exact orchestrationId",
		"Use vgxness_orchestrate for a goal that benefits from adaptive decomposition",
		"Start an orchestration with action=start",
		"one exact arguments object for the built-in Task tool",
		"issue all calls together in one response so OpenCode displays and runs the subagents in parallel",
		"call vgxness_orchestrate with action=advance",
		"Never retry vgxness_orchestrate automatically after a tool failure",
		"When vgxness_orchestrate returns a completed join, use that join as the final result",
		"Do not launch a second vgxness_dispatch to re-synthesize completed orchestration evidence",
		"vgxness_dispatch action=start with read-files",
		"vgxness_dispatch action=start with analyze-structure",
		"then call vgxness_dispatch action=join",
		"vgxness_dispatch action=start with write-files",
		"ticket-authenticated edit broker in an isolated sibling worktree",
		"never silently merges it into the source checkout",
		"vgxness_dispatch action=start with review-changes",
		"issue all returned native Task calls together",
		"Join each dispatch only after its Task terminates",
		"use vgxness_orchestrate so every phase remains visible",
		"only for backward compatibility with older callers",
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
	if strings.Contains(explorerPrompt, "\n  read: allow\n") || strings.Contains(explorerPrompt, "edit: allow") || strings.Contains(explorerPrompt, "grep: allow") || strings.Contains(explorerPrompt, "lsp: allow") || strings.Contains(explorerPrompt, "codegraph_*: allow") || !strings.Contains(explorerPrompt, "vgxness_native_read: allow") || !strings.Contains(explorerPrompt, "vgxness_codegraph: allow") || !strings.Contains(explorerPrompt, "Never invoke CodeGraph CLI/MCP directly") {
		t.Fatal("explorer exposes an alternate content-access path")
	}
	for _, prompt := range []string{explorerPrompt, implementerPrompt, reviewerPrompt} {
		for _, required := range []string{"exactly two top-level input envelopes", "vgxness.visible-task.directive", "vgxness.direct-dispatch.directive", "without calling vgxness_task_claim or vgxness_task_complete", "Reject every other top-level input shape"} {
			if !strings.Contains(prompt, required) {
				t.Errorf("managed native profile is missing dual-protocol contract %q", required)
			}
		}
	}
	if strings.Contains(implementerPrompt, "\n  read: allow\n") || strings.Contains(implementerPrompt, "\n  edit: allow\n") || strings.Contains(implementerPrompt, "\n  write: allow\n") || strings.Contains(implementerPrompt, "grep: allow") || strings.Contains(implementerPrompt, "lsp: allow") || strings.Contains(implementerPrompt, "codegraph_*: allow") || !strings.Contains(implementerPrompt, "vgxness_native_read: allow") || !strings.Contains(implementerPrompt, "vgxness_native_edit: allow") {
		t.Fatal("implementer exposes an alternate write path")
	}
	if !strings.Contains(implementerPrompt, "vgxness_task_claim: allow") || !strings.Contains(implementerPrompt, "vgxness_task_complete: allow") || !strings.Contains(implementerPrompt, "SHA-256 returned by its latest read or edit receipt") {
		t.Fatal("implementer is missing the ticket-authenticated edit protocol")
	}
	if strings.Contains(reviewerPrompt, "read: allow") || strings.Contains(reviewerPrompt, "codegraph_*: allow") {
		t.Fatal("reviewer can escape immutable Git evidence")
	}
}

func TestNavigatorRoutesReadOnlySynthesisWithoutGitReview(t *testing.T) {
	for _, required := range []string{
		"artifact: opencode-agent/vgxness-navigator; version: 5",
		"implement with write-files",
		"Every implementation task is exclusive",
		"smallest sufficient set of bounded work units",
		"Optimize for reliable elapsed time, not task count or visible activity",
		"Use explore/analyze-structure for architecture, symbol, dependency, call-path, blast-radius, or affected-test questions",
		"Use explore/read-files for exact file-content inspection and for a final synthesis",
		"A synthesis task must depend on every evidence task and use continuity linked",
		"Reserve review/review-changes exclusively for goals that explicitly review current, staged, or uncommitted Git changes",
		"clean-repository audit, architecture assessment, health check, or improvement analysis must not use review-changes",
		"Honor the supplied operatingMode and numeric constraints",
		"In fast mode return exactly one smallest-sufficient task",
		"In auto mode use proportional verification and at most four tasks",
		"In deep mode inspect all material requested concerns",
		"Default to one task",
		"Parallelize independent tasks only when doing so reduces elapsed time",
	} {
		if !strings.Contains(navigatorPrompt, required) {
			t.Errorf("navigator prompt is missing routing contract %q", required)
		}
	}
	for _, required := range []string{
		"artifact: opencode-agent/vgxness-explorer; version: 10",
		"use supplied memory and dependency evidence before gathering more context",
		"For read-files, never call vgxness_codegraph",
		"Stop when the acceptance criteria are satisfied",
		"Propose memoryCandidates only for durable reusable project knowledge",
		"VGXNESS, not you, decides whether a proposal is saved",
	} {
		if !strings.Contains(explorerPrompt, required) {
			t.Errorf("explorer prompt is missing efficiency contract %q", required)
		}
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
	edit := `{"protocolVersion":"1","ok":true,"bridge":"healthy","provider":"opencode","status":"editing","edit":{"path":"go.mod","sha256":"sha256-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","previousSha256":"sha256-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","bytes":15,"created":false}}`
	codegraph := `{"protocolVersion":"1","ok":true,"bridge":"healthy","provider":"opencode","status":"completed","codegraph":{"available":true,"operation":"explore"}}`
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
		"elif [ \"$2\" = \"edit\" ]; then\n" +
		"  payload=$(cat)\n" +
		"  case \"$payload\" in\n" +
		"    *'\"ticketId\":\"ticket-1\"'*'\"childSessionId\":\"ses_child\"'*'\"path\":\"go.mod\"'*'\"expectedSha256\":\"sha256-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"'*) ;;\n" +
		"    *) exit 12 ;;\n" +
		"  esac\n" +
		"  printf '%s' '" + edit + "'\n" +
		"elif [ \"$2\" = \"codegraph\" ]; then\n" +
		"  payload=$(cat)\n" +
		"  case \"$payload\" in\n" +
		"    *'\"ticketId\":\"ticket-1\"'*'\"childSessionId\":\"ses_child\"'*'\"depth\":5'*'\"maxFiles\":12'*) ;;\n" +
		"    *) exit 11 ;;\n" +
		"  esac\n" +
		"  printf '%s' '" + codegraph + "'\n" +
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
  schema: { enum: optionalSchema, string: optionalSchema, number: optionalSchema, boolean: optionalSchema, array: optionalSchema },
})`
	source := strings.Replace(string(content), `import { tool } from "@opencode-ai/plugin"`, stub, 1)
	source = strings.Replace(source, `import { randomUUID } from "node:crypto"`, `const randomUUID = () => "1"`, 1)
	workspace, err := json.Marshal(root)
	testutil.NoError(t, err)
	source += "\nlet plugin, createRequest\n"
	source += "const metadataEvents = []\n"
	source += "\nconst client = { session: {\n"
	source += "  create: async (request) => { createRequest = request; return { data: { id: \"ses_child\" } } },\n"
	source += "  prompt: async () => { const childContext = { directory: " + string(workspace) + ", worktree: \"\", sessionID: \"ses_child\", abort: new AbortController().signal }; const value = JSON.parse(await plugin.tool.vgxness_native_read.execute({ path: \"go.mod\" }, childContext)); if (value.content !== \"module example\") throw new Error(\"native read broker failed\"); const changed = JSON.parse(await plugin.tool.vgxness_native_edit.execute({ path: \"go.mod\", content: \"module changed\\n\", expectedSha256: \"sha256-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\" }, childContext)); if (changed.bytes !== 15) throw new Error(\"native edit broker failed\"); const graph = JSON.parse(await plugin.tool.vgxness_codegraph.execute({ operation: \"explore\", query: \"architecture\", depth: 8, maxFiles: 40 }, childContext)); if (!graph.available || graph.operation !== \"explore\") throw new Error(\"codegraph numeric bounds failed\"); return { data: { info: { id: \"msg_child\" }, parts: [{ type: \"text\", text: JSON.stringify({ kind: \"agent.result\", schemaVersion: \"1\", resultId: \"result-1\", taskId: \"task-1\", agentId: \"vgxness-bounded-worker-v1\", status: \"success\", summary: \"ok\", findings: [], changes: [], validations: [], artifactRefs: [], memoryCandidates: [], nextRecommended: \"done\", confidence: 1 }) }] } } },\n"
	source += "  abort: async () => ({ data: true }),\n"
	source += "} }\n"
	source += "plugin = await VGXNESSPlugin({ client })\n"
	source += "const context = { directory: " + string(workspace) + ", worktree: \"\", sessionID: \"ses_parent\", messageID: \"msg_parent\", abort: new AbortController().signal, metadata(input) { metadataEvents.push(JSON.parse(JSON.stringify(input))) } }\n"
	source += "let runBoundsRejected = false\n"
	source += "try { await plugin.tool.vgxness_run.execute({ goal: 'inspect', constraints: Array(17).fill('bounded') }, context) } catch { runBoundsRejected = true }\n"
	source += "if (!runBoundsRejected) throw new Error('unbounded goal-first constraints were accepted')\n"
	source += "const statusOutput = await plugin.tool.vgxness_status.execute({}, context)\n"
	source += "const dispatchOutput = await plugin.tool.vgxness_dispatch.execute({ operation: \"analyze-structure\", goal: \"inspect\", continuity: \"start\" }, context)\n"
	source += "if (createRequest.body.parentID !== 'ses_parent' || !createRequest.body.title.includes('@vgxness-explorer subagent')) throw new Error(JSON.stringify(createRequest))\n"
	source += "if (!metadataEvents.some((event) => event.metadata?.sessionId === 'ses_child' && event.metadata?.parentSessionId === 'ses_parent' && event.metadata?.phase === 'running' && event.metadata?.subagents?.[0]?.status === 'running')) throw new Error(JSON.stringify(metadataEvents))\n"
	source += "if (metadataEvents.at(-1)?.metadata?.phase !== 'completed' || metadataEvents.at(-1)?.metadata?.subagents?.[0]?.status !== 'completed') throw new Error(JSON.stringify(metadataEvents))\n"
	source += "if (dispatchOutput.metadata?.phase !== 'completed' || dispatchOutput.metadata?.sessionId !== 'ses_child' || !dispatchOutput.title?.includes('1/1 completed')) throw new Error(JSON.stringify(dispatchOutput))\n"
	source += "client.session.create = async () => ({ data: { id: 'ses_nav_failed' } })\n"
	source += "client.session.prompt = async () => { throw new Error('navigator failed') }\n"
	source += "let navigatorFailed = false\n"
	source += "try { await plugin.tool.vgxness_orchestrate.execute({ goal: 'inspect' }, context) } catch { navigatorFailed = true }\n"
	source += "if (!navigatorFailed) throw new Error('navigator failure was not propagated')\n"
	source += "process.stdout.write(statusOutput + \"\\n\" + dispatchOutput.output)\n"
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

func TestIntegration_DispatchReturnsOneNativeTaskAndJoinsDurably(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("portable runtime smoke helper uses a POSIX executable")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is not installed")
	}

	root := t.TempDir()
	helper := filepath.Join(root, "vgxness-helper")
	planBody := `"orchestrationId":"orchestration-dispatch","scheduleId":"schedule-dispatch","ownerId":"owner-dispatch","parentSessionId":"ses_parent","currentWave":0,"nextWave":0,"plan":{"kind":"delegation.plan","schemaVersion":"1","planId":"plan-dispatch","requestDigest":"sha256-dispatch","decision":"sequential","rationale":"one direct task","policyVersion":"bridge-balanced-v1","maxParallel":1,"tasks":[{"taskId":"task-dispatch-dispatchid","capability":"explore","operation":"analyze-structure","goal":"inspect architecture","acceptanceCriteria":[],"dependsOn":[],"continuity":"isolated"}],"waves":[{"waveId":"wave-dispatch","index":0,"mode":"sequential","taskIds":["task-dispatch-dispatchid"]}]}`
	planned := `{"protocolVersion":"1","ok":true,"bridge":"healthy","provider":"opencode","status":"pending","orchestration":{` + planBody + `,"status":"pending"}}`
	completed := `{"protocolVersion":"1","ok":true,"bridge":"healthy","provider":"opencode","status":"completed","orchestration":{` + planBody + `,"status":"completed"}}`
	joined := `{"protocolVersion":"1","ok":true,"bridge":"healthy","provider":"opencode","status":"completed","orchestration":{` + planBody + `,"status":"completed","join":{"kind":"delegation.join","status":"completed"}}}`
	helperSource := "#!/bin/sh\n" +
		"cat >/dev/null\n" +
		"case \"$2\" in\n" +
		"  orchestrate-plan) printf '%s' '" + planned + "' ;;\n" +
		"  orchestrate-status) printf '%s' '" + completed + "' ;;\n" +
		"  orchestrate-join) printf '%s' '" + joined + "' ;;\n" +
		"  *) exit 9 ;;\n" +
		"esac\n"
	testutil.NoError(t, os.WriteFile(helper, []byte(helperSource), 0o700))
	content, err := bridgeToolContent(helper, integrationTestModel)
	testutil.NoError(t, err)
	stub := `const optionalSchema = () => ({ optional() { return this } })
const tool = Object.assign((definition) => definition, {
  schema: { enum: optionalSchema, string: optionalSchema, number: optionalSchema, boolean: optionalSchema, array: optionalSchema },
})`
	source := strings.Replace(string(content), `import { tool } from "@opencode-ai/plugin"`, stub, 1)
	source = strings.Replace(source, `import { randomUUID } from "node:crypto"`, `const randomUUID = () => "dispatchid"`, 1)
	workspace, err := json.Marshal(root)
	testutil.NoError(t, err)
	source += "\nconst metadataEvents = []\n"
	source += "const client = { session: { create: async () => { throw new Error('dispatch created a hidden child') } } }\n"
	source += "const plugin = await VGXNESSPlugin({ client })\n"
	source += "const context = { directory: " + string(workspace) + ", worktree: '', sessionID: 'ses_parent', messageID: 'msg_parent', abort: new AbortController().signal, metadata(input) { metadataEvents.push(input) } }\n"
	source += "const started = await plugin.tool.vgxness_dispatch.execute({ action: 'start', operation: 'analyze-structure', goal: 'inspect architecture' }, context)\n"
	source += "const delegation = JSON.parse(started.output)\n"
	source += "const task = delegation.delegation?.tasks?.[0]\n"
	source += "const directive = JSON.parse(task?.arguments?.prompt || '{}')\n"
	source += "if (delegation.status !== 'delegation-required' || delegation.delegation.tasks.length !== 1 || task.taskId !== 'task-dispatch-dispatchid') throw new Error(JSON.stringify(delegation))\n"
	source += "if (task.arguments.subagent_type !== 'vgxness-explorer' || Object.keys(task.arguments).sort().join(',') !== 'description,prompt,subagent_type' || directive.kind !== 'vgxness.visible-task.directive') throw new Error(JSON.stringify(task))\n"
	source += "if (started.metadata?.visibleTaskCount !== 1 || started.metadata?.orchestrationId !== 'orchestration-dispatch') throw new Error(JSON.stringify(started))\n"
	source += "const finished = await plugin.tool.vgxness_dispatch.execute({ action: 'join', orchestrationId: 'orchestration-dispatch' }, context)\n"
	source += "const result = JSON.parse(finished.output)\n"
	source += "if (result.status !== 'completed' || result.orchestration?.join?.status !== 'completed') throw new Error(JSON.stringify(result))\n"
	source += "process.stdout.write('task=' + task.taskId + ' status=' + result.status)\n"
	script := filepath.Join(root, "dispatch-visible.mjs")
	testutil.NoError(t, os.WriteFile(script, []byte(source), 0o600))

	command := exec.Command(node, script)
	output, runErr := command.CombinedOutput()
	if runErr != nil {
		t.Fatalf("generated visible dispatch runtime failed: %v\n%s", runErr, output)
	}
	testutil.Require(t, string(output) == "task=task-dispatch-dispatchid status=completed", "visible dispatch output=%q", output)
}

func TestIntegration_BridgeToolPreservesStructuredFailureEnvelope(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("portable runtime smoke helper uses a POSIX executable")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is not installed")
	}

	root := t.TempDir()
	helper := filepath.Join(root, "vgxness-helper")
	failure := `{"protocolVersion":"1","ok":false,"bridge":"healthy","provider":"opencode","status":"error","error":{"code":"denied","message":"bridge request was denied by policy","recoverable":false}}`
	helperSource := "#!/bin/sh\nprintf '%s' '" + failure + "'\nexit 7\n"
	testutil.NoError(t, os.WriteFile(helper, []byte(helperSource), 0o700))
	content, err := bridgeToolContent(helper, integrationTestModel)
	testutil.NoError(t, err)
	stub := `const optionalSchema = () => ({ optional() { return this } })
const tool = Object.assign((definition) => definition, {
  schema: { enum: optionalSchema, string: optionalSchema, number: optionalSchema, boolean: optionalSchema, array: optionalSchema },
})`
	source := strings.Replace(string(content), `import { tool } from "@opencode-ai/plugin"`, stub, 1)
	source = strings.Replace(source, `import { randomUUID } from "node:crypto"`, `const randomUUID = () => "1"`, 1)
	workspace, err := json.Marshal(root)
	testutil.NoError(t, err)
	source += "\nconst plugin = await VGXNESSPlugin({ client: { session: {} } })\n"
	source += "const context = { directory: " + string(workspace) + ", worktree: '', sessionID: 'ses_parent', messageID: 'msg_parent', abort: new AbortController().signal, metadata() {} }\n"
	source += "const output = await plugin.tool.vgxness_status.execute({}, context)\n"
	source += "if (output !== " + strconv.Quote(failure) + ") throw new Error(output)\n"
	source += "let orchestrationFailure = ''\n"
	source += "try { await plugin.tool.vgxness_orchestrate.execute({ action: 'advance', orchestrationId: 'orchestration-failed' }, context) } catch (cause) { orchestrationFailure = String(cause?.message || cause) }\n"
	source += "if (!orchestrationFailure.includes('denied') || orchestrationFailure.includes('invalid visible orchestration')) throw new Error(orchestrationFailure)\n"
	source += "process.stdout.write(output)\n"
	script := filepath.Join(root, "bridge-failure.mjs")
	testutil.NoError(t, os.WriteFile(script, []byte(source), 0o600))

	command := exec.Command(node, script)
	output, runErr := command.CombinedOutput()
	if runErr != nil {
		t.Fatalf("generated bridge failure handling failed: %v\n%s", runErr, output)
	}
	testutil.Require(t, string(output) == failure, "structured failure output=%q", output)
}

func TestIntegration_BridgeToolNormalizesCommonAgentResultShapes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("portable runtime smoke helper uses a POSIX executable")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is not installed")
	}

	root := t.TempDir()
	helper := filepath.Join(root, "vgxness-helper")
	testutil.NoError(t, os.WriteFile(helper, []byte("#!/bin/sh\nexit 0\n"), 0o700))
	content, err := bridgeToolContent(helper, integrationTestModel)
	testutil.NoError(t, err)
	stub := `const optionalSchema = () => ({ optional() { return this } })
const tool = Object.assign((definition) => definition, {
  schema: { enum: optionalSchema, string: optionalSchema, number: optionalSchema, boolean: optionalSchema, array: optionalSchema },
})`
	source := strings.Replace(string(content), `import { tool } from "@opencode-ai/plugin"`, stub, 1)
	source = strings.Replace(source, `import { randomUUID } from "node:crypto"`, `const randomUUID = () => "1"`, 1)
	source += `
const result = exactAgentResult([{ type: "text", text: JSON.stringify({
  kind: "agent.result",
  artifacts: ["Inline finding", { type: "code-review", findings: ["Detailed finding"] }, {
    kind: "artifact.reference", uri: "README.md", title: "Incomplete reference",
  }, {
    kind: "artifact.reference", schemaVersion: "1", provider: "opencode", id: "artifact-1",
    artifactType: "report", provenance: { producer: "vgxness-worker", createdAt: "2026-07-23T00:00:00Z" },
  }],
  risks: ["Existing risk"],
  errors: ["The bounded broker denied one optional read."],
}) }])
const expected = { code: "native-subagent-observation", message: "The bounded broker denied one optional read.", recoverable: true }
if (JSON.stringify(result.errors) !== JSON.stringify([expected])) throw new Error(JSON.stringify(result))
if (result.artifacts.length !== 1 || result.artifacts[0].id !== "artifact-1") throw new Error(JSON.stringify(result))
if (result.risks.length !== 4 || !result.risks[1].includes("Inline finding") || !result.risks[2].includes("Detailed finding") || !result.risks[3].includes("Incomplete reference")) throw new Error(JSON.stringify(result))
process.stdout.write(JSON.stringify({ error: result.errors[0], artifact: result.artifacts[0].id, risks: result.risks.length }))
`
	script := filepath.Join(root, "normalize-agent-errors.mjs")
	testutil.NoError(t, os.WriteFile(script, []byte(source), 0o600))

	command := exec.Command(node, script)
	output, runErr := command.CombinedOutput()
	if runErr != nil {
		t.Fatalf("generated agent result normalization failed: %v\n%s", runErr, output)
	}
	testutil.Require(t, string(output) == `{"error":{"code":"native-subagent-observation","message":"The bounded broker denied one optional read.","recoverable":true},"artifact":"artifact-1","risks":4}`, "normalized result=%q", output)
}

func TestIntegration_OrchestrateProducesParallelVisibleTaskDirectives(t *testing.T) {
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
	planBody := `"orchestrationId":"orchestration-1","scheduleId":"schedule-1","ownerId":"owner-1","parentSessionId":"ses_parent","currentWave":0,"nextWave":0,"plan":{"kind":"delegation.plan","schemaVersion":"1","planId":"plan-1","requestDigest":"sha256-1","decision":"parallel","rationale":"two independent reads","policyVersion":"bridge-balanced-v1","maxParallel":4,"tasks":[{"taskId":"task-a","capability":"explore","operation":"read-files","goal":"inspect a","acceptanceCriteria":[],"dependsOn":[],"continuity":"isolated"},{"taskId":"task-b","capability":"explore","operation":"read-files","goal":"inspect b","acceptanceCriteria":[],"dependsOn":[],"continuity":"isolated"}],"waves":[{"waveId":"wave-1","index":0,"mode":"parallel","taskIds":["task-a","task-b"]}]}`
	plan := `{"protocolVersion":"1","ok":true,"bridge":"healthy","provider":"opencode","status":"pending","orchestration":{` + planBody + `,"status":"pending"}}`
	waveBody := planBody
	preparedA := `{"taskId":"task-a","childSessionId":"ses_task_a","prepared":{"ticketId":"ticket-3","executionId":"execution-1","agent":"vgxness-explorer","model":"openai/gpt-5.6-sol","prompt":"inspect a","promptSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","deadline":"2099-01-01T00:00:00Z","promptRef":{"id":"prompt-a","version":"1","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}`
	preparedB := `{"taskId":"task-b","childSessionId":"ses_task_b","prepared":{"ticketId":"ticket-4","executionId":"execution-2","agent":"vgxness-explorer","model":"openai/gpt-5.6-sol","prompt":"inspect b","promptSha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","deadline":"2099-01-01T00:00:00Z","promptRef":{"id":"prompt-b","version":"1","sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}}`
	wavePrefix := `{"protocolVersion":"1","ok":true,"bridge":"healthy","provider":"opencode","status":"running","orchestration":{` + waveBody + `,"status":"running","prepared":[`
	wave := wavePrefix + preparedA + `,` + preparedB + `]}}`
	partialWave := wavePrefix + preparedA + `]}}`
	terminal := `{"protocolVersion":"1","ok":true,"bridge":"healthy","provider":"opencode","status":"running","orchestration":{` + waveBody + `,"status":"running"}}`
	completed := `{"protocolVersion":"1","ok":true,"bridge":"healthy","provider":"opencode","status":"completed","orchestration":{` + waveBody + `,"status":"completed"}}`
	joined := `{"protocolVersion":"1","ok":true,"bridge":"healthy","provider":"opencode","status":"completed","orchestration":{` + waveBody + `,"status":"completed","join":{"kind":"delegation.join","status":"completed"}}}`
	doneMarker := filepath.Join(root, "done")
	helperSource := "#!/bin/sh\n" +
		"payload=$(cat)\n" +
		"case \"$2\" in\n" +
		"  orchestrate-plan) case \"$payload\" in *'\"taskId\":\"task-a\"'*'\"acceptanceCriteria\":[\"Constraint: read only\",\"Desired outcome: prioritized evidence\"]'*) ;; *) exit 12 ;; esac; printf '%s' '" + plan + "' ;;\n" +
		"  orchestrate-wave) if [ \"$VGXNESS_PARTIAL_WAVE\" = 1 ]; then printf '%s' '" + partialWave + "'; else printf '%s' '" + wave + "'; fi ;;\n" +
		"  orchestrate-status) if [ -f '" + doneMarker + "' ]; then printf '%s' '" + completed + "'; else printf '%s' '" + plan + "'; fi ;;\n" +
		"  orchestrate-terminal) : > '" + doneMarker + "'; printf '%s' '" + terminal + "' ;;\n" +
		"  orchestrate-join) printf '%s' '" + joined + "' ;;\n" +
		"  complete|fail|orchestrate-cancel) printf '%s' '{\"protocolVersion\":\"1\",\"ok\":true,\"bridge\":\"healthy\",\"provider\":\"opencode\",\"status\":\"completed\"}' ;;\n" +
		"  *) exit 9 ;;\n" +
		"esac\n"
	testutil.NoError(t, os.WriteFile(helper, []byte(helperSource), 0o700))
	content, err := bridgeToolContent(helper, integrationTestModel)
	testutil.NoError(t, err)
	stub := `const optionalSchema = () => ({ optional() { return this } })
const tool = Object.assign((definition) => definition, {
  schema: { enum: optionalSchema, string: optionalSchema, number: optionalSchema, boolean: optionalSchema, array: optionalSchema },
})`
	source := strings.Replace(string(content), `import { tool } from "@opencode-ai/plugin"`, stub, 1)
	source = strings.Replace(source, `import { randomUUID } from "node:crypto"`, `let uuidCounter = 0; const randomUUID = () => String(++uuidCounter)`, 1)
	workspace, err := json.Marshal(root)
	testutil.NoError(t, err)
	source += "\nlet plugin, created = 0\n"
	source += "const createBodies = [], metadataEvents = []\n"
	source += "const client = { session: {\n"
	source += "  create: async (request) => { created++; createBodies.push(request.body); return { data: { id: 'ses_nav' } } },\n"
	source += "  get: async (request) => ({ data: { id: request.path.id, parentID: 'ses_parent', agent: request.path.id === 'ses_other' ? 'build' : request.path.id === 'ses_unverified' ? undefined : 'vgxness-explorer' } }),\n"
	source += "  prompt: async () => ({ data: { info: { id: 'msg_nav' }, parts: [{ type: 'text', text: JSON.stringify({ tasks: [{ taskId: 'task-a', capability: 'explore', operation: 'read-files', goal: 'inspect a', acceptanceCriteria: [], dependsOn: [], continuity: 'isolated' }, { taskId: 'task-b', capability: 'explore', operation: 'read-files', goal: 'inspect b', acceptanceCriteria: [], dependsOn: [], continuity: 'isolated' }] }) }] } }),\n"
	source += "  abort: async () => ({ data: true }),\n"
	source += "} }\n"
	source += "plugin = await VGXNESSPlugin({ client, directory: " + string(workspace) + " })\n"
	source += "const partialWave = process.env.VGXNESS_PARTIAL_WAVE === '1'\n"
	source += "let blockedDiscovery = false\n"
	source += "try { await plugin['tool.execute.before']({ tool: 'glob', sessionID: 'ses_unclaimed', callID: 'call-1' }, { args: {} }) } catch { blockedDiscovery = true }\n"
	source += "if (!blockedDiscovery) throw new Error('unclaimed explorer discovery was not blocked')\n"
	source += "blockedDiscovery = false\n"
	source += "try { await plugin['tool.execute.before']({ tool: 'list', sessionID: 'ses_unverified', callID: 'call-unknown' }, { args: {} }) } catch { blockedDiscovery = true }\n"
	source += "if (!blockedDiscovery) throw new Error('unverified child discovery was not blocked')\n"
	source += "await plugin['tool.execute.before']({ tool: 'glob', sessionID: 'ses_other', callID: 'call-2' }, { args: {} })\n"
	source += "const context = { directory: " + string(workspace) + ", worktree: '', sessionID: 'ses_parent', messageID: 'msg_parent', abort: new AbortController().signal, metadata(input) { metadataEvents.push(JSON.parse(JSON.stringify(input))) } }\n"
	source += "const started = await plugin.tool.vgxness_run.execute({ action: 'start', goal: 'inspect both', mode: 'auto', constraints: ['read only'], desiredOutcome: 'prioritized evidence' }, context)\n"
	source += "const planned = JSON.parse(started.output)\n"
	source += "if (planned.status !== 'delegation-required' || planned.delegation?.mode !== 'parallel' || planned.delegation?.tasks?.length !== 2 || created !== 1) throw new Error(JSON.stringify({ planned, created }))\n"
	source += "if (!planned.delegation.tasks.every((task) => task.arguments?.subagent_type === 'vgxness-explorer' && JSON.parse(task.arguments?.prompt).kind === 'vgxness.visible-task.directive' && Object.keys(task.arguments).sort().join(',') === 'description,prompt,subagent_type')) throw new Error(JSON.stringify(planned.delegation))\n"
	source += "const directiveA = JSON.parse(planned.delegation.tasks.find((task) => task.taskId === 'task-a').arguments.prompt)\n"
	source += "const directiveB = JSON.parse(planned.delegation.tasks.find((task) => task.taskId === 'task-b').arguments.prompt)\n"
	source += "const childContext = (id, messageID) => ({ directory: " + string(workspace) + ", worktree: '', sessionID: id, messageID, agent: 'vgxness-explorer', abort: new AbortController().signal, metadata() {} })\n"
	source += "let invalidClaimRejected = false\n"
	source += "try { await plugin.tool.vgxness_task_claim.execute({ orchestrationId: 'orchestration-1', ownerId: 'owner-1', taskId: 'task-a', claimToken: 'claim-forged' }, childContext('ses_task_a', 'msg_forged')) } catch { invalidClaimRejected = true }\n"
	source += "if (!invalidClaimRejected) throw new Error('forged visible claim capability was accepted')\n"
	source += "const claimAPromise = plugin.tool.vgxness_task_claim.execute({ orchestrationId: 'orchestration-1', ownerId: 'owner-1', taskId: 'task-a', claimToken: directiveA.claimToken }, childContext('ses_task_a', 'msg_a'))\n"
	source += "await new Promise((resolve) => setTimeout(resolve, 50))\n"
	source += "const claimBPromise = plugin.tool.vgxness_task_claim.execute({ orchestrationId: 'orchestration-1', ownerId: 'owner-1', taskId: 'task-b', claimToken: directiveB.claimToken }, childContext('ses_task_b', 'msg_b'))\n"
	source += "const claims = await Promise.allSettled([claimAPromise, claimBPromise])\n"
	source += "if (claims[0].status !== 'fulfilled' || JSON.parse(claims[0].value).prompt !== 'inspect a') throw new Error(JSON.stringify(claims))\n"
	source += "if (partialWave ? claims[1].status !== 'rejected' : claims[1].status !== 'fulfilled' || JSON.parse(claims[1].value).prompt !== 'inspect b') throw new Error(JSON.stringify(claims))\n"
	source += "await plugin['tool.execute.before']({ tool: 'glob', sessionID: 'ses_task_a', callID: 'call-3' }, { args: {} })\n"
	source += "const result = JSON.stringify({ kind: 'agent.result', schemaVersion: '1', status: 'success', summary: 'ok', artifacts: [], risks: [], errors: [] })\n"
	source += "await plugin.tool.vgxness_task_complete.execute({ result }, childContext('ses_task_a', 'msg_a_complete'))\n"
	source += "if (partialWave) { process.stdout.write('partial=isolated'); process.exit(0) }\n"
	source += "await plugin.tool.vgxness_task_complete.execute({ result }, childContext('ses_task_b', 'msg_b_complete'))\n"
	source += "const advanced = await plugin.tool.vgxness_run.execute({ action: 'advance', orchestrationId: 'orchestration-1' }, context)\n"
	source += "const output = JSON.parse(advanced.output)\n"
	source += "if (output.status !== 'completed' || output.orchestration?.join?.status !== 'completed') throw new Error(JSON.stringify(output))\n"
	source += "process.stdout.write('directives=' + planned.delegation.tasks.length + ' status=' + output.status)\n"
	script := filepath.Join(root, "orchestrate.mjs")
	testutil.NoError(t, os.WriteFile(script, []byte(source), 0o600))

	for _, engine := range engines {
		t.Run(filepath.Base(engine), func(t *testing.T) {
			for _, testCase := range []struct {
				name    string
				partial bool
				want    string
			}{
				{name: "complete-wave", want: "directives=2 status=completed"},
				{name: "partial-wave", partial: true, want: "partial=isolated"},
			} {
				t.Run(testCase.name, func(t *testing.T) {
					_ = os.Remove(doneMarker)
					command := exec.Command(engine, script)
					if testCase.partial {
						command.Env = append(os.Environ(), "VGXNESS_PARTIAL_WAVE=1")
					}
					output, runErr := command.CombinedOutput()
					if runErr != nil {
						t.Fatalf("generated orchestration runtime failed: %v\n%s", runErr, output)
					}
					testutil.Require(t, string(output) == testCase.want, "%s orchestration output=%q", engine, output)
				})
			}
		})
	}
}

func TestIntegration_OrchestrateAdvanceRecoversNextVisibleWave(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("portable runtime smoke helper uses a POSIX executable")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is not installed")
	}

	root := t.TempDir()
	helper := filepath.Join(root, "vgxness-helper")
	plan := `{"protocolVersion":"1","ok":true,"bridge":"healthy","provider":"opencode","status":"pending","orchestration":{"orchestrationId":"orchestration-1","scheduleId":"schedule-1","ownerId":"owner-1","parentSessionId":"ses_parent","status":"pending","currentWave":0,"nextWave":0,"plan":{"kind":"delegation.plan","schemaVersion":"1","planId":"plan-1","requestDigest":"sha256-1","decision":"sequential","rationale":"dependent reads","policyVersion":"bridge-balanced-v1","maxParallel":1,"tasks":[{"taskId":"task-a","capability":"explore","operation":"read-files","goal":"inspect a","acceptanceCriteria":[],"dependsOn":[],"continuity":"isolated"},{"taskId":"task-b","capability":"explore","operation":"read-files","goal":"inspect b","acceptanceCriteria":[],"dependsOn":["task-a"],"continuity":"isolated"}],"waves":[{"waveId":"wave-1","index":0,"mode":"sequential","taskIds":["task-a"]},{"waveId":"wave-2","index":1,"mode":"sequential","taskIds":["task-b"]}]}}}`
	wave := `{"protocolVersion":"1","ok":true,"bridge":"healthy","provider":"opencode","status":"running","orchestration":{"orchestrationId":"orchestration-1","scheduleId":"schedule-1","ownerId":"owner-1","status":"running","currentWave":0,"plan":{"decision":"sequential","tasks":[],"waves":[]},"prepared":[{"taskId":"task-a","prepared":{"ticketId":"ticket-2","executionId":"execution-1","agent":"vgxness-explorer","model":"openai/gpt-5.6-sol","prompt":"inspect a","promptSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","deadline":"2099-01-01T00:00:00Z","promptRef":{"id":"prompt-a","version":"1","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}]}}`
	pending := strings.Replace(plan, `"nextWave":0`, `"nextWave":1`, 1)
	helperSource := "#!/bin/sh\n" +
		"cat >/dev/null\n" +
		"case \"$2\" in\n" +
		"  orchestrate-plan) printf '%s' '" + plan + "' ;;\n" +
		"  orchestrate-wave) printf '%s' '" + wave + "' ;;\n" +
		"  orchestrate-terminal|orchestrate-status) printf '%s' '" + pending + "' ;;\n" +
		"  complete|fail) printf '%s' '{\"protocolVersion\":\"1\",\"ok\":true,\"bridge\":\"healthy\",\"provider\":\"opencode\",\"status\":\"completed\"}' ;;\n" +
		"  orchestrate-cancel) exit 19 ;;\n" +
		"  *) exit 9 ;;\n" +
		"esac\n"
	testutil.NoError(t, os.WriteFile(helper, []byte(helperSource), 0o700))
	content, err := bridgeToolContent(helper, integrationTestModel)
	testutil.NoError(t, err)
	stub := `const optionalSchema = () => ({ optional() { return this } })
const tool = Object.assign((definition) => definition, {
  schema: { enum: optionalSchema, string: optionalSchema, number: optionalSchema, boolean: optionalSchema, array: optionalSchema },
})`
	source := strings.Replace(string(content), `import { tool } from "@opencode-ai/plugin"`, stub, 1)
	source = strings.Replace(source, `import { randomUUID } from "node:crypto"`, `let uuidCounter = 0; const randomUUID = () => String(++uuidCounter)`, 1)
	workspace, err := json.Marshal(root)
	testutil.NoError(t, err)
	source += "\nlet plugin, created = 0\n"
	source += "const client = { session: {\n"
	source += "  create: async () => { created++; return { data: { id: 'unused' } } },\n"
	source += "  prompt: async () => { throw new Error('unused') },\n"
	source += "  abort: async () => ({ data: true }),\n"
	source += "} }\n"
	source += "plugin = await VGXNESSPlugin({ client })\n"
	source += "const context = { directory: " + string(workspace) + ", worktree: '', sessionID: 'ses_parent', messageID: 'msg_parent', abort: new AbortController().signal, metadata() {} }\n"
	source += "const orchestrationToolResult = await plugin.tool.vgxness_orchestrate.execute({ action: 'advance', orchestrationId: 'orchestration-1' }, context)\n"
	source += "const output = JSON.parse(orchestrationToolResult.output)\n"
	source += "if (!output.ok || output.status !== 'delegation-required' || output.delegation?.waveIndex !== 1 || output.delegation?.tasks?.[0]?.taskId !== 'task-b' || created !== 0) throw new Error(JSON.stringify({ output, created }))\n"
	source += "process.stdout.write('status=' + output.status + ' wave=' + output.delegation.waveIndex)\n"
	script := filepath.Join(root, "orchestrate-recovery.mjs")
	testutil.NoError(t, os.WriteFile(script, []byte(source), 0o600))

	command := exec.Command(node, script)
	output, runErr := command.CombinedOutput()
	if runErr != nil {
		t.Fatalf("generated orchestration recovery failed: %v\n%s", runErr, output)
	}
	testutil.Require(t, string(output) == "status=delegation-required wave=1", "orchestration recovery output=%q", output)
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
