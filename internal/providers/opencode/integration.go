package opencode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/vgxness/vgxness/internal/bridge"
	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/launcher"
)

const (
	managerAgentName     = "vgxness-manager.md"
	explorerAgentName    = "vgxness-explorer.md"
	implementerAgentName = "vgxness-implementer.md"
	reviewerAgentName    = "vgxness-reviewer.md"
	bridgePluginName     = "vgxness.ts"
	maxArtifactBytes     = 512 * 1024
	managerPrompt        = `---
description: VGXNESS manager — OpenCode interface to the VGXNESS control plane
mode: primary
color: primary
permission:
  "*": deny
  question: allow
  vgxness_status: allow
  vgxness_dispatch: allow
---

<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-manager; version: 7 -->

# Identity

You are VGXNESS Manager, the user-facing guide to the VGXNESS control plane inside OpenCode.

Your presence is calm, attentive, technically discerning, and collaborative. Speak like a thoughtful partner who understands what the user is trying to accomplish, has a point of view, and makes the system's evidence easy to understand. Do not sound like a command router or a status console.

Recommend the smallest sensible next move and briefly explain why it is the right move. Be confident when the evidence is clear and candid when it is incomplete. Avoid canned praise, theatrical enthusiasm, false familiarity, and needless verbosity.

# Language and voice

- Match the language and register of the user's direct conversation.
- Keep code, generated documentation, commit-style text, and other technical artifacts neutral and in English by default, unless the user explicitly requests another language or an established project policy requires it.
- Keep this conversational personality out of technical artifacts unless the user asks for that voice.
- Preserve the user's intent and terminology without merely echoing their words.

# Authority boundary

VGXNESS is the authority for intent routing, prompt identity, permissions, bounded coordination, execution evidence, and durable state. OpenCode is the interaction surface and provider runtime; it is not the control plane.

You may discuss the user's goal and explain VGXNESS behavior from this contract. For every claim about workspace state, every repository review, and every requested action, use only the installed vgxness_* control-plane tools. The managed VGXNESS plugin launches native OpenCode subagents behind those tools; never bypass it with direct file, shell, network, task, or delegation tools.

The available control-plane surface is exact:

- Use vgxness_status only to check bridge health and compatibility. It does not inspect project state.
- Use vgxness_dispatch with read-files for bounded workspace inspection.
- Native write-files is fail-closed until a ticket-authenticated edit broker is available.
- Use vgxness_dispatch with review-changes for current, staged, or uncommitted repository changes. Do not substitute read-files; only review-changes includes bounded Git status and diff evidence.
For two or more independent read-only inspections, issue the vgxness_dispatch calls together so OpenCode can run their native child sessions in parallel. VGXNESS admits at most four active one-shot native dispatches per workspace; never parallelize writes, review phases, or any dispatch that uses continuity. If capacity is exhausted, report the bounded blocker instead of retrying in a loop.
For one self-contained phase, omit continuity and use a normal isolated dispatch. For work that clearly needs more than one bounded phase, use continuity=start on the first dispatch, retain the exact runId and capsuleId returned by VGXNESS, use continuity=continue with that runId for intermediate phases, and use continuity=finish with the same runId for the final bounded phase. Never leave a successful multi-phase run active, invent an identity, reuse one across projects, or silently replace it. Continued phases receive only the validated prior capsule and curated VGXNESS memory context; they do not receive the full conversation or unrestricted history.

# Conversation rhythm

For an actionable request:

1. State in one short sentence what bounded action you are taking and why.
2. Call the exact VGXNESS tool required by the authority boundary.
3. Turn the returned envelope into a concise, human explanation covering the outcome, the evidence that supports it, any meaningful limitation, and the recommended next step.
4. Do not dump raw envelopes or internal protocol details unless the user asks for them.

Treat every VGXNESS receipt as bounded evidence. Never claim changes, verification, or completion beyond that receipt. If the result is blocked or needs follow-up, name the blocker plainly and propose the smallest safe way forward. Ask at most one blocking question at a time, and only when VGXNESS reports that a decision or approval is required.

# Degraded mode

If no vgxness_* tool is available, stop before acting. Explain that the OpenCode entrypoint is installed but the VGXNESS control-plane bridge is unavailable, and ask the user to run vgxness integrate opencode status.
	`
	explorerPrompt = `---
description: VGXNESS native explorer for bounded structural and workspace inspection
mode: subagent
hidden: true
permission:
  "*": deny
  glob: allow
  list: allow
  vgxness_native_read: allow
  task: deny
---

<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-explorer; version: 3 -->

You are the native VGXNESS explorer. Execute only the exact content-bound prompt supplied by the VGXNESS control plane. Use glob and list only to discover workspace-relative paths, and use vgxness_native_read for every file-content read. Never use an alternate content index, edit files, run shell commands, use the network, delegate, install packages, commit, or push. Return exactly one JSON object conforming to the agent.result contract in the supplied prompt, with no Markdown fence or commentary.
`
	implementerPrompt = `---
description: VGXNESS reserved read-only implementer profile
mode: subagent
hidden: true
permission:
  "*": deny
  glob: allow
  list: allow
  vgxness_native_read: allow
  task: deny
---

<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-implementer; version: 3 -->

You are a reserved read-only VGXNESS profile. Execute only the exact content-bound prompt supplied by the VGXNESS control plane. Never edit files. Return exactly one JSON object conforming to the agent.result contract in the supplied prompt.
`
	reviewerPrompt = `---
description: VGXNESS native reviewer for bounded pre-collected change evidence
mode: subagent
hidden: true
permission:
  "*": deny
  task: deny
---

<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-reviewer; version: 1 -->

You are the native VGXNESS reviewer. Review only the immutable Git status and diff evidence embedded in the exact content-bound prompt. Do not access the filesystem, shell, network, other tools, or subagents. Return exactly one JSON object conforming to the agent.result contract in the supplied prompt, with no Markdown fence or commentary.
`
)

type Integration struct {
	now        func() time.Time
	executable string
}

type artifact struct {
	path    string
	content []byte
	backup  string
	present bool
	exact   bool
}

type inspection struct {
	result    integration.Result
	artifacts []artifact
}

type installedArtifact struct {
	path      string
	temporary string
}

type backedUpArtifact struct {
	target string
	backup string
}

func NewIntegration() *Integration {
	executable, _ := os.Executable()
	if executable != "" {
		executable, _ = filepath.Abs(executable)
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			executable = resolved
		}
	}
	if stable := trustedLauncher(executable, os.Getenv("VGXNESS_LAUNCHER")); stable != "" {
		executable = stable
	}
	return &Integration{now: time.Now, executable: executable}
}

func NewManagedIntegration(executable string) (*Integration, error) {
	managed, err := validateManagedLauncher(executable)
	if err != nil {
		return nil, fmt.Errorf("%w: managed VGXNESS launcher", integration.ErrInvalid)
	}
	return &Integration{now: time.Now, executable: managed}, nil
}

func validateManagedLauncher(candidate string) (string, error) {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" || !filepath.IsAbs(candidate) {
		return "", launcher.ErrInvalid
	}
	candidate = filepath.Clean(candidate)
	manifest, err := launcher.Load(candidate)
	if err != nil {
		return "", err
	}
	launcherDigest, err := launcher.FileSHA256(candidate)
	if err != nil || launcherDigest != manifest.LauncherSHA256 {
		return "", launcher.ErrInvalid
	}
	activeDigest, err := launcher.FileSHA256(manifest.ActivePath)
	if err != nil || activeDigest != manifest.ActiveSHA256 {
		return "", launcher.ErrInvalid
	}
	return candidate, nil
}

func trustedLauncher(activeExecutable, candidate string) string {
	candidate, err := validateManagedLauncher(candidate)
	if err != nil {
		return ""
	}
	manifest, _ := launcher.Load(candidate)
	activeInfo, activeErr := os.Stat(activeExecutable)
	managedInfo, managedErr := os.Stat(manifest.ActivePath)
	if activeErr != nil || managedErr != nil || !os.SameFile(activeInfo, managedInfo) {
		return ""
	}
	return candidate
}

func (service *Integration) Preview(ctx context.Context, options integration.Options) (integration.Result, error) {
	state, err := service.inspect(ctx, options, true)
	if err != nil {
		return integration.Result{}, err
	}
	state.result.Changed = state.result.State == integration.StateAbsent || state.result.State == integration.StatePartial
	return state.result, nil
}

func (service *Integration) Status(ctx context.Context, options integration.Options) (integration.Result, error) {
	state, err := service.inspect(ctx, options, false)
	return state.result, err
}

func (service *Integration) Install(ctx context.Context, options integration.Options) (integration.Result, error) {
	state, err := service.inspect(ctx, options, true)
	if err != nil {
		return integration.Result{}, err
	}
	if state.result.State == integration.StateInstalled {
		return state.result, nil
	}
	if state.result.State == integration.StateDrifted {
		return integration.Result{}, fmt.Errorf("%w: managed OpenCode artifacts", integration.ErrConflict)
	}
	if err := ctx.Err(); err != nil {
		return integration.Result{}, err
	}
	configDirectory := filepath.Dir(filepath.Dir(state.result.Path))
	if err := prepareDirectory(configDirectory); err != nil {
		return integration.Result{}, fmt.Errorf("prepare OpenCode config directory: %w", err)
	}
	for _, item := range state.artifacts {
		directory := filepath.Dir(item.path)
		if err := prepareDirectory(directory); err != nil {
			return integration.Result{}, fmt.Errorf("prepare OpenCode integration directory: %w", err)
		}
	}
	created := make([]installedArtifact, 0, len(state.artifacts))
	rollback := true
	defer func() {
		if rollback {
			for index := len(created) - 1; index >= 0; index-- {
				removeSameFileBestEffort(created[index].path, created[index].temporary)
			}
		}
		for _, item := range created {
			_ = os.Remove(item.temporary)
		}
	}()
	for _, item := range state.artifacts {
		if item.exact {
			continue
		}
		installed, installErr := installArtifact(ctx, item)
		if installErr != nil {
			return integration.Result{}, installErr
		}
		created = append(created, installed)
	}
	verified, err := service.inspect(ctx, options, true)
	if err != nil || verified.result.State != integration.StateInstalled {
		return integration.Result{}, fmt.Errorf("read back OpenCode integration artifacts: %w", integration.ErrDrift)
	}
	rollback = false
	verified.result.Changed = len(created) != 0
	return verified.result, nil
}

func (service *Integration) Uninstall(ctx context.Context, options integration.Options) (integration.Result, error) {
	state, err := service.inspect(ctx, options, false)
	if err != nil {
		return integration.Result{}, err
	}
	if state.result.State == integration.StateAbsent {
		return state.result, nil
	}
	if state.result.State == integration.StateDrifted {
		return integration.Result{}, fmt.Errorf("%w: managed OpenCode artifacts", integration.ErrDrift)
	}
	if err := ctx.Err(); err != nil {
		return integration.Result{}, err
	}
	backupDirectory := filepath.Join(filepath.Dir(filepath.Dir(state.result.Path)), ".vgxness-backups")
	if err := prepareDirectory(backupDirectory); err != nil {
		return integration.Result{}, fmt.Errorf("prepare OpenCode integration backup: %w", err)
	}
	now := time.Now().UTC()
	if service != nil && service.now != nil {
		now = service.now().UTC()
	}
	stamp := fmt.Sprintf("%s.%09d", now.Format("20060102T150405"), now.Nanosecond())
	backupPaths := make(map[string]string, len(state.artifacts))
	for _, item := range state.artifacts {
		backupPaths[item.path] = filepath.Join(backupDirectory, item.backup+"."+stamp+filepath.Ext(item.path))
	}
	for _, item := range state.artifacts {
		if !item.exact {
			continue
		}
		if _, statErr := os.Lstat(backupPaths[item.path]); statErr == nil {
			return integration.Result{}, fmt.Errorf("%w: backup already exists", integration.ErrConflict)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return integration.Result{}, fmt.Errorf("inspect OpenCode integration backup: %w", statErr)
		}
	}
	backups := make([]backedUpArtifact, 0, len(state.artifacts))
	rollback := true
	defer func() {
		if rollback {
			for index := len(backups) - 1; index >= 0; index-- {
				restoreWithoutOverwrite(backups[index].backup, backups[index].target)
			}
		}
	}()
	for _, item := range state.artifacts {
		if !item.exact {
			continue
		}
		backupPath := backupPaths[item.path]
		if err := os.Link(item.path, backupPath); err != nil {
			return integration.Result{}, fmt.Errorf("backup OpenCode integration artifact: %w", err)
		}
		if err := syncDirectory(filepath.Dir(backupPath)); err != nil {
			removeSameFileBestEffort(backupPath, item.path)
			return integration.Result{}, fmt.Errorf("sync OpenCode integration backup: %w", err)
		}
		backup, readErr := readRegularFile(backupPath)
		if readErr != nil || !bytes.Equal(backup, item.content) {
			removeSameFileBestEffort(backupPath, item.path)
			return integration.Result{}, fmt.Errorf("read back OpenCode integration backup: %w", integration.ErrDrift)
		}
		backups = append(backups, backedUpArtifact{target: item.path, backup: backupPath})
		if err := removeSameFileDurably(item.path, backupPath); err != nil {
			return integration.Result{}, fmt.Errorf("sync OpenCode integration removal: %w", err)
		}
		if _, statErr := os.Lstat(item.path); !errors.Is(statErr, os.ErrNotExist) {
			return integration.Result{}, fmt.Errorf("%w: integration artifact changed during uninstall", integration.ErrConflict)
		}
		if err := ctx.Err(); err != nil {
			return integration.Result{}, err
		}
	}
	rollback = false
	state.result.State = integration.StateAbsent
	state.result.Bridge = integration.BridgeUnavailable
	state.result.Changed = len(backups) != 0
	for _, item := range backups {
		if item.target == state.result.Path {
			state.result.BackupPath = item.backup
		}
		if item.target == state.result.ToolPath {
			state.result.ToolBackupPath = item.backup
		}
	}
	return state.result, nil
}

func (service *Integration) inspect(ctx context.Context, options integration.Options, requireModel bool) (inspection, error) {
	if err := ctx.Err(); err != nil {
		return inspection{}, err
	}
	configDirectory, err := integrationConfigDirectory(options)
	if err != nil {
		return inspection{}, err
	}
	managerPath := filepath.Join(configDirectory, "agents", managerAgentName)
	explorerPath := filepath.Join(configDirectory, "agents", explorerAgentName)
	implementerPath := filepath.Join(configDirectory, "agents", implementerAgentName)
	reviewerPath := filepath.Join(configDirectory, "agents", reviewerAgentName)
	toolPath := filepath.Join(configDirectory, "plugins", bridgePluginName)
	model := strings.TrimSpace(options.Model)
	if model != "" && !validModel(model) {
		return inspection{}, fmt.Errorf("%w: OpenCode execution model", integration.ErrInvalid)
	}
	if model == "" {
		if current, readErr := readRegularFile(toolPath); readErr == nil {
			model = managedToolModel(current)
		}
	}
	if requireModel && model == "" {
		return inspection{}, fmt.Errorf("%w: OpenCode execution model is required as provider/model", integration.ErrInvalid)
	}
	var toolContent []byte
	if model != "" {
		toolContent, err = bridgeToolContent(service.executable, model)
		if err != nil {
			return inspection{}, err
		}
	}
	result := integration.Result{
		Provider: "opencode", State: integration.StateAbsent, Path: managerPath, ArtifactSHA256: artifactSHA256([]byte(managerPrompt)),
		ToolPath: toolPath, Model: model, Bridge: integration.BridgeUnavailable,
	}
	if len(toolContent) != 0 {
		result.ToolSHA256 = artifactSHA256(toolContent)
	}
	exists, drifted, containerErr := inspectDirectory(configDirectory)
	if containerErr != nil {
		return inspection{}, fmt.Errorf("inspect OpenCode integration directory: %w", containerErr)
	}
	state := inspection{result: result, artifacts: []artifact{
		{path: managerPath, content: []byte(managerPrompt), backup: "vgxness-manager"},
		{path: explorerPath, content: []byte(explorerPrompt), backup: "vgxness-explorer"},
		{path: implementerPath, content: []byte(implementerPrompt), backup: "vgxness-implementer"},
		{path: reviewerPath, content: []byte(reviewerPrompt), backup: "vgxness-reviewer"},
		{path: toolPath, content: toolContent, backup: "vgxness-plugin"},
	}}
	if drifted {
		state.result.State = integration.StateDrifted
		return state, nil
	}
	if !exists {
		return state, nil
	}
	exact := 0
	for index := range state.artifacts {
		item := &state.artifacts[index]
		directoryExists, directoryDrifted, directoryErr := inspectDirectory(filepath.Dir(item.path))
		if directoryErr != nil {
			return inspection{}, fmt.Errorf("inspect OpenCode integration directory: %w", directoryErr)
		}
		if directoryDrifted {
			state.result.State = integration.StateDrifted
			return state, nil
		}
		if !directoryExists {
			continue
		}
		current, readErr := readRegularFile(item.path)
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			if errors.Is(readErr, integration.ErrDrift) {
				state.result.State = integration.StateDrifted
				return state, nil
			}
			return inspection{}, fmt.Errorf("inspect OpenCode integration artifact: %w", readErr)
		}
		item.present = true
		item.exact = bytes.Equal(current, item.content)
		if !item.exact {
			state.result.State = integration.StateDrifted
			return state, nil
		}
		exact++
		if item.path == toolPath {
			state.result.Bridge = integration.BridgeConfigured
		}
	}
	switch exact {
	case 0:
		state.result.State = integration.StateAbsent
	case len(state.artifacts):
		state.result.State = integration.StateInstalled
	default:
		state.result.State = integration.StatePartial
	}
	return state, nil
}

func installArtifact(ctx context.Context, item artifact) (installedArtifact, error) {
	if err := ctx.Err(); err != nil {
		return installedArtifact{}, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(item.path), ".vgxness-*.tmp")
	if err != nil {
		return installedArtifact{}, fmt.Errorf("create OpenCode integration artifact: %w", err)
	}
	temporaryPath := temporary.Name()
	closeWithError := func(cause error) (installedArtifact, error) {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
		return installedArtifact{}, cause
	}
	if err := temporary.Chmod(0o600); err != nil {
		return closeWithError(fmt.Errorf("secure OpenCode integration artifact: %w", err))
	}
	if _, err := io.Copy(temporary, bytes.NewReader(item.content)); err != nil {
		return closeWithError(fmt.Errorf("write OpenCode integration artifact: %w", err))
	}
	if err := temporary.Sync(); err != nil {
		return closeWithError(fmt.Errorf("sync OpenCode integration artifact: %w", err))
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return installedArtifact{}, fmt.Errorf("close OpenCode integration artifact: %w", err)
	}
	if err := ctx.Err(); err != nil {
		_ = os.Remove(temporaryPath)
		return installedArtifact{}, err
	}
	if err := os.Link(temporaryPath, item.path); err != nil {
		_ = os.Remove(temporaryPath)
		if errors.Is(err, os.ErrExist) {
			return installedArtifact{}, fmt.Errorf("%w: %s", integration.ErrConflict, item.path)
		}
		return installedArtifact{}, fmt.Errorf("install OpenCode integration artifact: %w", err)
	}
	if err := syncDirectory(filepath.Dir(item.path)); err != nil {
		removeSameFileBestEffort(item.path, temporaryPath)
		return installedArtifact{}, fmt.Errorf("sync OpenCode integration directory: %w", err)
	}
	installed := installedArtifact{path: item.path, temporary: temporaryPath}
	readback, readErr := readRegularFile(item.path)
	if readErr != nil || !bytes.Equal(readback, item.content) {
		removeSameFileBestEffort(item.path, temporaryPath)
		_ = os.Remove(temporaryPath)
		return installedArtifact{}, fmt.Errorf("read back OpenCode integration artifact: %w", integration.ErrDrift)
	}
	return installed, nil
}

func bridgeToolContent(executable, model string) ([]byte, error) {
	if strings.TrimSpace(executable) == "" || !filepath.IsAbs(executable) {
		return nil, fmt.Errorf("%w: VGXNESS executable path", integration.ErrInvalid)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(executable))
	if err != nil {
		return nil, fmt.Errorf("%w: VGXNESS executable unavailable", integration.ErrInvalid)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: VGXNESS executable is not a regular file", integration.ErrInvalid)
	}
	model = strings.TrimSpace(model)
	if model == "" || !validModel(model) {
		return nil, fmt.Errorf("%w: OpenCode execution model", integration.ErrInvalid)
	}
	quoted, err := json.Marshal(resolved)
	if err != nil {
		return nil, fmt.Errorf("%w: VGXNESS executable path", integration.ErrInvalid)
	}
	quotedModel, err := json.Marshal(model)
	if err != nil {
		return nil, fmt.Errorf("%w: OpenCode execution model", integration.ErrInvalid)
	}
	content := `import { spawn } from "node:child_process"
	import { randomUUID } from "node:crypto"
	import { tool } from "@opencode-ai/plugin"

		// managed-by: vgxness; artifact: opencode-plugin/vgxness; version: 10
	const VGXNESS_EXECUTABLE = ` + string(quoted) + `
	const VGXNESS_MODEL = ` + string(quotedModel) + `
	const MAX_OUTPUT_BYTES = __MAX_OUTPUT_BYTES__
	const TERMINAL_TIMEOUT_MS = 30_000
	const MAX_NATIVE_DISPATCHES = 4
	const nativeTickets = new Map()
	const nativeCapacity = new Map()

async function readBounded(stream) {
  const chunks = []
  let size = 0
  for await (const chunk of stream) {
    const value = typeof chunk === "string" ? new TextEncoder().encode(chunk) : new Uint8Array(chunk)
    size += value.byteLength
    if (size > MAX_OUTPUT_BYTES) throw new Error("VGXNESS bridge output exceeded its bound")
    chunks.push(value)
  }
  const joined = new Uint8Array(size)
  let offset = 0
  for (const chunk of chunks) {
    joined.set(chunk, offset)
    offset += chunk.byteLength
  }
  return new TextDecoder().decode(joined)
}

	function exactEnvelope(output) {
	  const value = JSON.parse(output)
  if (
    value === null ||
    Array.isArray(value) ||
    typeof value !== "object" ||
    value.protocolVersion !== "1" ||
    typeof value.ok !== "boolean" ||
    typeof value.bridge !== "string" ||
    value.provider !== "opencode" ||
    typeof value.status !== "string"
	  ) {
	    throw new Error("VGXNESS bridge returned an invalid envelope")
	  }
	  return value
	}

	async function invoke(args, payload, workspace, signal) {
	  if (!workspace) throw new Error("OpenCode did not provide a workspace")
  let child
  let exited
  try {
    child = spawn(VGXNESS_EXECUTABLE, [...args, "--workspace", workspace], {
	      cwd: workspace,
	      shell: false,
	      stdio: [payload === undefined ? "ignore" : "pipe", "pipe", "pipe"],
	      signal,
      windowsHide: true,
    })
    if (!child.stdout || !child.stderr) throw new Error("VGXNESS bridge subprocess pipes are unavailable")
    exited = new Promise((resolve, reject) => {
      child.once("error", reject)
      child.once("close", (code) => {
        if (code === 0) resolve(undefined)
        else reject(new Error("VGXNESS bridge subprocess exited unsuccessfully"))
      })
    })
    const input = payload === undefined
      ? Promise.resolve()
      : new Promise((resolve, reject) => {
          if (!child.stdin) {
            reject(new Error("VGXNESS bridge subprocess input is unavailable"))
            return
          }
          child.stdin.once("error", reject)
          child.stdin.end(JSON.stringify(payload), resolve)
        })
    const [stdout] = await Promise.all([readBounded(child.stdout), readBounded(child.stderr), input, exited])
    return exactEnvelope(stdout)
  } catch {
    child?.kill("SIGKILL")
    await exited?.catch(() => undefined)
    throw new Error("VGXNESS bridge invocation failed")
  }
	}

	async function invokeBounded(args, payload, workspace, timeoutMs = TERMINAL_TIMEOUT_MS, outerSignal) {
	  const controller = new AbortController()
	  const forwardAbort = () => controller.abort()
	  if (outerSignal?.aborted) controller.abort()
	  else outerSignal?.addEventListener("abort", forwardAbort, { once: true })
	  const timer = setTimeout(() => controller.abort(), timeoutMs)
	  try {
	    return await invoke(args, payload, workspace, controller.signal)
	  } finally {
	    clearTimeout(timer)
	    outerSignal?.removeEventListener("abort", forwardAbort)
	  }
	}

	async function invokeTerminal(args, payload, workspace) {
	  try {
	    return await invokeBounded(args, payload, workspace)
	  } catch {
	    return await invokeBounded(args, payload, workspace)
	  }
	}

	function responseData(response, label) {
	  if (!response || response.error || !response.data) throw new Error(label)
	  return response.data
	}

	function exactPrepared(envelope) {
	  const value = envelope?.prepared
	  if (
	    !envelope?.ok || !value || typeof value.ticketId !== "string" ||
	    typeof value.agent !== "string" || typeof value.model !== "string" ||
	    typeof value.prompt !== "string" || typeof value.promptSha256 !== "string"
	  ) throw new Error("VGXNESS did not return a native dispatch ticket")
	  return value
	}

	function exactAgentResult(parts) {
	  const output = parts
	    .filter((part) => part?.type === "text" && typeof part.text === "string")
	    .map((part) => part.text)
	    .join("")
	    .trim()
	  const value = JSON.parse(output)
	  if (!value || Array.isArray(value) || typeof value !== "object") {
	    throw new Error("Native VGXNESS subagent returned an invalid result")
	  }
	  return value
	}

	function acquireNativeCapacity(workspace, sharedRead) {
	  const state = nativeCapacity.get(workspace) || { sharedReads: 0, exclusive: false }
	  if (sharedRead) {
	    if (state.exclusive || state.sharedReads >= MAX_NATIVE_DISPATCHES) {
	      throw new Error("VGXNESS native dispatch capacity exhausted")
	    }
	    state.sharedReads++
	  } else {
	    if (state.exclusive || state.sharedReads > 0) {
	      throw new Error("VGXNESS exclusive native dispatch is busy")
	    }
	    state.exclusive = true
	  }
	  nativeCapacity.set(workspace, state)
	  let released = false
	  return () => {
	    if (released) return
	    released = true
	    const current = nativeCapacity.get(workspace)
	    if (!current) return
	    if (sharedRead) current.sharedReads = Math.max(0, current.sharedReads - 1)
	    else current.exclusive = false
	    if (!current.exclusive && current.sharedReads === 0) nativeCapacity.delete(workspace)
	    else nativeCapacity.set(workspace, current)
	  }
	}

	export default async function VGXNESSPlugin({ client }) {
	  return {
	    tool: {
	      vgxness_native_read: tool({
	        description: "Read one bounded workspace-relative text file through the ticket-authenticated VGXNESS broker. Pass nextOffset as a decimal cursor to continue a truncated file.",
	        args: {
	          path: tool.schema.string(),
	          cursor: tool.schema.string().optional(),
	        },
	        async execute(args, context) {
	          const workspace = context.worktree || context.directory
	          const active = nativeTickets.get(context.sessionID)
	          if (!active) throw new Error("No active VGXNESS native read ticket for this child session")
	          const offset = args.cursor === undefined ? 0 : Number(args.cursor)
	          if (!Number.isSafeInteger(offset) || offset < 0 || String(offset) !== (args.cursor ?? "0")) throw new Error("VGXNESS native read cursor is invalid")
	          const envelope = await invokeBounded(
	            ["bridge", "read", "--stdin"],
	            { protocolVersion: "1", ticketId: active.ticketId, childSessionId: context.sessionID, path: args.path, offset, limit: 65536 },
	            workspace,
	            TERMINAL_TIMEOUT_MS,
	            context.abort,
	          )
	          if (!envelope.ok || !envelope.read) throw new Error("VGXNESS native read was denied")
	          return JSON.stringify(envelope.read)
	        },
	      }),
	      vgxness_status: tool({
	        description: "Check the bounded VGXNESS control-plane bridge for the current OpenCode workspace.",
	        args: {},
	        async execute(_args, context) {
	          const workspace = context.worktree || context.directory
	          return JSON.stringify(await invoke(["bridge", "status"], undefined, workspace, context.abort))
	        },
	      }),
	      vgxness_dispatch: tool({
	        description: "Prepare one bounded operation in VGXNESS, execute it in a native OpenCode child session, then durably accept its result. Use review-changes for current, staged, or uncommitted changes. Use continuity start, continue, and finish for multi-phase work.",
	        args: {
	          operation: tool.schema.enum(["read-files", "write-files", "review-changes"]),
	          goal: tool.schema.string(),
	          acceptanceCriteria: tool.schema.array(tool.schema.string()).optional(),
	          continuity: tool.schema.enum(["start", "continue", "finish"]).optional(),
	          runId: tool.schema.string().optional(),
	        },
	        async execute(args, context) {
	          const workspace = context.worktree || context.directory
	          const releaseCapacity = acquireNativeCapacity(workspace, args.operation === "read-files" && args.continuity === undefined && args.runId === undefined)
	          let prepared
	          let ticketId = ""
	          let childSessionId = ""
	          let deadlineExceeded = false
	          try {
	            const created = responseData(await client.session.create({
	              query: { directory: workspace },
	              body: { parentID: context.sessionID, title: "VGXNESS native subagent" },
	            }), "OpenCode could not create a native VGXNESS subagent")
	            childSessionId = created.id
	            ticketId = "ticket-" + randomUUID()
	            const envelope = await invoke(
	              ["bridge", "prepare", "--stdin"],
	              { ...args, protocolVersion: "1", ticketId, model: VGXNESS_MODEL, parentSessionId: context.sessionID, parentMessageId: context.messageID, childSessionId },
	              workspace,
	              context.abort,
	            )
	            if (envelope.ok && envelope.status === "recovered" && typeof envelope.runId === "string" && envelope.runId) {
	              await client.session.abort({ path: { id: childSessionId }, query: { directory: workspace } }).catch(() => undefined)
	              ticketId = ""
	              return JSON.stringify(envelope)
	            }
	            prepared = exactPrepared(envelope)
	            if (prepared.ticketId !== ticketId) throw new Error("VGXNESS returned a mismatched native dispatch ticket")
	            nativeTickets.set(childSessionId, { ticketId: prepared.ticketId })
	            context.metadata({ title: "VGXNESS native subagent", metadata: { sessionId: childSessionId, ticketId: prepared.ticketId, agent: prepared.agent } })
	            const separator = prepared.model.indexOf("/")
	            if (separator <= 0 || separator === prepared.model.length - 1) throw new Error("VGXNESS returned an invalid native model")
	            const abortChild = () => client.session.abort({ path: { id: childSessionId }, query: { directory: workspace } }).catch(() => undefined)
	            const abortChildOnContext = () => { void abortChild() }
	            context.abort.addEventListener("abort", abortChildOnContext, { once: true })
	            let message
	            let timer
	            try {
	              const timeoutMs = Math.max(1, Date.parse(prepared.deadline) - Date.now())
	              const deadline = new Promise((_, reject) => {
	                timer = setTimeout(() => {
	                  deadlineExceeded = true
	                  void abortChild()
	                  reject(new Error("Native VGXNESS subagent deadline exceeded"))
	                }, timeoutMs)
	              })
	              message = responseData(await Promise.race([client.session.prompt({
	                path: { id: childSessionId },
	                query: { directory: workspace },
	                body: {
	                  agent: prepared.agent,
	                  model: { providerID: prepared.model.slice(0, separator), modelID: prepared.model.slice(separator + 1) },
	                  parts: [{ type: "text", text: prepared.prompt }],
	                },
	              }), deadline]), "Native VGXNESS subagent execution failed")
	            } finally {
	              if (timer) clearTimeout(timer)
	              context.abort.removeEventListener("abort", abortChildOnContext)
	            }
	            const result = exactAgentResult(message.parts || [])
	            const completed = await invokeTerminal(
	              ["bridge", "complete", "--stdin"],
	              {
	                protocolVersion: "1", ticketId: prepared.ticketId, parentSessionId: context.sessionID,
	                childSessionId, messageId: message.info.id, result,
	              },
	              workspace,
	            )
	            return JSON.stringify(completed)
	          } catch (cause) {
	            let cleanupFailure
	            if (childSessionId) await client.session.abort({ path: { id: childSessionId }, query: { directory: workspace } }).catch(() => undefined)
	            if (ticketId) {
	              const category = context.abort.aborted
	                ? "native-subagent-cancelled"
	                : deadlineExceeded ? "native-subagent-deadline" : "native-subagent-failed"
	              try {
	                await invokeTerminal(
	                  ["bridge", "fail", "--stdin"],
	                  { protocolVersion: "1", ticketId, parentSessionId: context.sessionID, childSessionId, category },
	                  workspace,
	                )
	              } catch (failure) {
	                cleanupFailure = failure
	              }
	            }
	            if (cleanupFailure) {
	              const detail = cleanupFailure instanceof Error ? cleanupFailure.message : "unknown bridge cleanup error"
	              throw new Error("Native VGXNESS dispatch failed and durable cleanup failed: " + detail)
	            }
	            throw cause instanceof Error ? cause : new Error("Native VGXNESS dispatch failed")
	          } finally {
	            if (childSessionId) nativeTickets.delete(childSessionId)
	            releaseCapacity()
	          }
	        },
	      }),
	    },
	  }
	}
	`
	content = strings.ReplaceAll(content, "\n\t", "\n")
	content = strings.ReplaceAll(content, "__MAX_OUTPUT_BYTES__", strconv.Itoa(bridge.MaxBridgeOutputBytes))
	return []byte(content), nil
}

func managedToolModel(content []byte) string {
	const prefix = "const VGXNESS_MODEL = "
	var model string
	found := false
	for _, line := range strings.Split(string(content), "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		if found || json.Unmarshal([]byte(strings.TrimPrefix(line, prefix)), &model) != nil {
			return ""
		}
		found = true
	}
	model = strings.TrimSpace(model)
	if !found || model == "" || !validModel(model) {
		return ""
	}
	return model
}

func inspectDirectory(path string) (exists, drifted bool, err error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return true, true, nil
	}
	return true, false, nil
}

func removeSameFileDurably(target, expected string) error {
	targetInfo, targetErr := os.Lstat(target)
	expectedInfo, expectedErr := os.Lstat(expected)
	if targetErr == nil && expectedErr == nil && os.SameFile(targetInfo, expectedInfo) {
		if err := os.Remove(target); err != nil {
			return err
		}
		return syncDirectory(filepath.Dir(target))
	}
	return nil
}

func removeSameFileBestEffort(target, expected string) { _ = removeSameFileDurably(target, expected) }

func restoreWithoutOverwrite(backup, target string) {
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		return
	}
	if err := os.Link(backup, target); err == nil {
		if syncDirectory(filepath.Dir(target)) == nil {
			removeSameFileBestEffort(backup, target)
		}
	}
}

func integrationConfigDirectory(options integration.Options) (string, error) {
	if options.ConfigDir != "" {
		if !filepath.IsAbs(options.ConfigDir) {
			return "", fmt.Errorf("%w: OpenCode config directory must be absolute", integration.ErrInvalid)
		}
		return filepath.Clean(options.ConfigDir), nil
	}
	home := options.HomeDir
	var err error
	if home == "" {
		home, err = os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
	}
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("%w: home directory must be absolute", integration.ErrInvalid)
	}
	return filepath.Join(filepath.Clean(home), ".config", "opencode"), nil
}

func prepareDirectory(path string) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return integration.ErrDrift
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(path)
	if parent != path {
		if err := prepareDirectory(parent); err != nil {
			return err
		}
	}
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return integration.ErrDrift
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return err
	}
	if err := syncDirectory(path); err != nil {
		return err
	}
	return syncDirectory(parent)
}

func readRegularFile(path string) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || before.Size() > maxArtifactBytes {
		return nil, integration.ErrDrift
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(before, after) || !after.Mode().IsRegular() || after.Size() > maxArtifactBytes {
		return nil, integration.ErrDrift
	}
	data, err := io.ReadAll(io.LimitReader(file, maxArtifactBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxArtifactBytes {
		return nil, integration.ErrDrift
	}
	return data, nil
}

func artifactSHA256(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

func managerPromptSHA256() string { return artifactSHA256([]byte(managerPrompt)) }
