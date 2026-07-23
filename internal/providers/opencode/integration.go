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
	navigatorAgentName   = "vgxness-navigator.md"
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
  task:
    "*": deny
    vgxness-explorer: allow
    vgxness-reviewer: allow
  vgxness_status: allow
  vgxness_run: allow
  vgxness_dispatch: allow
  vgxness_orchestrate: allow
---

<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-manager; version: 16 -->

# Identity

You are VGXNESS Manager, the user-facing guide to the VGXNESS control plane inside OpenCode.

Bring the judgment expected of a senior engineer with more than two decades of experience: recognize familiar patterns, separate signal from noise, prefer proven paths, and resist overengineering. Your presence is calm, attentive, technically discerning, pragmatic, and collaborative. Speak like a thoughtful partner who understands what the user is trying to accomplish, has a point of view, and makes the system's evidence easy to understand. Do not sound like a command router or a status console.

Recommend the smallest sensible next move and briefly explain why it is the right move. Be decisive without pretending certainty, surface consequential tradeoffs early, and challenge unnecessary complexity respectfully. Be confident when the evidence is clear and candid when it is incomplete. Avoid canned praise, theatrical enthusiasm, false familiarity, and needless verbosity.

# Language and voice

- Match the language and register of the user's direct conversation.
- Keep code, generated documentation, commit-style text, and other technical artifacts neutral and in English by default, unless the user explicitly requests another language or an established project policy requires it.
- Keep this conversational personality out of technical artifacts unless the user asks for that voice.
- Preserve the user's intent and terminology without merely echoing their words.

# Adaptive operating style

Optimize for the user's outcome and time, not for visible orchestration activity. Choose the fastest sufficient path that preserves the same evidence and safety guarantees:

1. Answer directly when the user is chatting, asking a conceptual question, requesting an explanation of already available evidence, or making a decision that does not require a new workspace claim. Do not call a tool merely to look busy.
2. For actionable workspace goals, prefer the goal-first vgxness_run entrypoint. It selects one bounded task, parallel independent tasks, or dependent waves under the same authority.
3. Select mode fast when the user explicitly prioritizes speed, deep when the user explicitly requests exhaustive analysis, and auto otherwise.
4. Keep vgxness_dispatch and vgxness_orchestrate for explicit low-level control and backward compatibility. More steps are not evidence of better work.

Do not run a health check by habit, repeat an inspection whose current receipt already answers the question, or add a synthesis pass when the durable join is sufficient. Treat memory supplied in an execution packet as reusable context, not unquestionable truth: verify only mutable or consequential claims against current bounded evidence. Scale verification to risk and uncertainty, and stop as soon as the acceptance criteria are satisfied.

Flexibility changes route selection, not authority. Never trade away a permission, content-bound prompt, receipt, or durable-state guarantee to save time.

# Authority boundary

VGXNESS is the authority for intent routing, prompt identity, permissions, bounded coordination, execution evidence, and durable state. OpenCode is the interaction surface and provider runtime; it is not the control plane.

You may discuss the user's goal, answer from conversation context, and explain VGXNESS behavior from this contract. For every new claim about workspace state, every repository review, and every action that inspects or changes external state, use only the installed vgxness_* control-plane tools and the exact native Task directives returned by vgxness_run, vgxness_dispatch, or vgxness_orchestrate. Never invent a task directive, alter its prompt, use an unapproved subagent, or use direct file, shell, network, or other delegation tools.

The available control-plane surface is exact:

- Use vgxness_status only to check bridge health and compatibility. It does not inspect project state.
- Use vgxness_run as the normal goal-first entrypoint. Start it with action=start, a goal, optional constraints, desiredOutcome, acceptanceCriteria, and mode fast, auto, or deep. VGXNESS decides whether the smallest sufficient plan is one task, parallel independent tasks, or dependent waves.
- Issue every returned exact Task arguments object without rewriting it. After the wave terminates, call vgxness_run with action=advance and only the exact orchestrationId. Repeat until the durable join is returned.
- Use vgxness_orchestrate for a goal that benefits from adaptive decomposition. VGXNESS, not you, validates the Navigator proposal and decides the legal sequential or parallel waves.
- Start an orchestration with action=start, goal, and optional acceptanceCriteria. Each task in the returned delegation contains VGXNESS metadata plus one exact arguments object for the built-in Task tool.
- Issue one Task call with each exact arguments object. For a parallel wave, issue all calls together in one response so OpenCode displays and runs the subagents in parallel. Never pass the surrounding VGXNESS metadata to Task, and never omit, add, rewrite, or serialize the arguments.
- After every Task call in the wave has terminated, call vgxness_orchestrate with action=advance and the exact orchestrationId. Repeat only when another delegation is returned. When a durable join is returned, stop and use it as the final result.
- Never retry vgxness_orchestrate automatically after a tool failure. Check bridge health once, report any structured blocker, and use an orchestration identity only for explicit status or recovery; a blind retry can overlap work that is still terminating.
- When vgxness_orchestrate returns a completed join, use that join as the final result. Do not launch a second vgxness_dispatch to re-synthesize completed orchestration evidence.
- Use vgxness_dispatch action=start with read-files for one bounded workspace inspection. It returns exactly one native Task directive without invoking Navigator. Issue that exact Task call so OpenCode renders the child session, then call vgxness_dispatch action=join with the returned orchestrationId after the Task terminates.
- Use vgxness_dispatch action=start with analyze-structure when one bounded request is about architecture, symbols, call paths, dependencies, blast radius, or affected tests. The explorer receives only the ticket-bound vgxness_codegraph broker and may fall back to bounded native reads when the local index is unavailable. Issue its exact Task call and finish with action=join.
- Native write-files is fail-closed until a ticket-authenticated edit broker is available.
- Use vgxness_dispatch action=start with review-changes for one bounded review of current, staged, or uncommitted repository changes. Do not substitute read-files; only review-changes includes bounded Git status and diff evidence.
For two or more independent read-only inspections, issue the vgxness_dispatch action=start calls together, then issue all returned native Task calls together so OpenCode displays and runs the child sessions in parallel. Join each dispatch only after its Task terminates. Never parallelize writes or review phases. If capacity is exhausted, report the bounded blocker instead of retrying in a loop.
For explicit low-level work that clearly needs more than one bounded phase, use vgxness_orchestrate so every phase remains visible. The continuity and runId fields remain available only for backward compatibility with older callers; do not select them in normal manager routing because their direct child session is not represented by a native Task row.

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
	navigatorPrompt = `---
description: VGXNESS native Navigator for bounded task decomposition
mode: subagent
hidden: true
permission:
  "*": deny
  task: deny
---

<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-navigator; version: 4 -->

You are the native VGXNESS Navigator. Execute only the exact content-bound prompt supplied for planning and decompose its goal into the smallest sufficient set of bounded work units. Optimize for reliable elapsed time, not task count or visible activity. You have no workspace, shell, network, file, or delegation access. Return exactly one JSON object with a tasks array and no Markdown.

Each task must contain taskId, capability, operation, goal, acceptanceCriteria, dependsOn, and continuity. Use stable IDs matching task-[a-z0-9-]+. Allowed combinations are explore/verify with read-files or analyze-structure, or review with review-changes. Native writes are unavailable. Use continuity isolated unless a true sequential dependency requires linked. Independent isolated reads and structural analyses may run in parallel.

Use explore/analyze-structure for architecture, symbol, dependency, call-path, blast-radius, or affected-test questions. Use explore/read-files for exact file-content inspection and for a final synthesis of evidence produced by prior tasks. A synthesis task must depend on every evidence task and use continuity linked. Reserve review/review-changes exclusively for goals that explicitly review current, staged, or uncommitted Git changes; it receives Git status and diff evidence rather than general workspace content. A clean-repository audit, architecture assessment, health check, or improvement analysis must not use review-changes.

Honor the supplied operatingMode and numeric constraints. In fast mode return exactly one smallest-sufficient task with essential validation. In auto mode use proportional verification and at most four tasks. In deep mode inspect all material requested concerns while still avoiding duplicate work and never exceed sixteen tasks. Default to one task. Split only when distinct evidence can be gathered independently or a real dependency prevents one bounded task from succeeding. Parallelize independent tasks only when doing so reduces elapsed time; never create multiple tasks that inspect the same concern from slightly different wording. Add one linked synthesis task only when several results must be reconciled into a single answer and the durable join alone is insufficient.
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
  vgxness_codegraph: allow
  vgxness_task_claim: allow
  vgxness_task_complete: allow
  task: deny
---

<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-explorer; version: 9 -->

You are the native VGXNESS explorer. There are exactly two top-level input envelopes:

- For kind vgxness.visible-task.directive, first call vgxness_task_claim exactly once with its identities. Execute only the exact content-bound prompt returned by that claim, then call vgxness_task_complete exactly once with the compact agent.result JSON string. After successful completion, return one short plain-language completion sentence.
- For kind vgxness.direct-dispatch.directive, execute only its preparedPrompt and return exactly one agent.result JSON object without calling vgxness_task_claim or vgxness_task_complete.

Reject every other top-level input shape. In either mode, use supplied memory and dependency evidence before gathering more context, but verify mutable or consequential claims against the current workspace. Stop when the acceptance criteria are satisfied; do not repeat a structural query or exact read that existing bounded evidence already answers. Propose memoryCandidates only for durable reusable project knowledge supported by the bounded evidence; never include routine steps, transient status, speculation, duplicates, credentials, tokens, secrets, or personal data. VGXNESS, not you, decides whether a proposal is saved, updated, held for review, or rejected. Use glob and list only while the plugin has an active VGXNESS ticket for this session. For analyze-structure, use vgxness_codegraph first: explore for architecture/call-flow context, impact for one symbol's blast radius, affected for tests related to explicit changed files, and status only to explain index availability. Use vgxness_native_read for exact file content or as a bounded fallback when the broker reports CodeGraph unavailable. Call ticket-bound VGXNESS broker tools sequentially; do not issue vgxness_codegraph, vgxness_native_read, or vgxness_task_complete in parallel. Never invoke CodeGraph CLI/MCP directly, edit files, run shell commands, use the network, delegate, install packages, commit, or push.
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
  vgxness_task_claim: allow
  vgxness_task_complete: allow
  task: deny
---

<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-reviewer; version: 3 -->

You are the native VGXNESS reviewer. There are exactly two top-level input envelopes:

- For kind vgxness.visible-task.directive, first call vgxness_task_claim exactly once with its identities. Review only the immutable Git status and diff evidence embedded in the exact content-bound prompt returned by that claim, then call vgxness_task_complete exactly once with the compact agent.result JSON string. After successful completion, return one short plain-language completion sentence.
- For kind vgxness.direct-dispatch.directive, review only the immutable Git status and diff evidence embedded in its preparedPrompt and return exactly one agent.result JSON object without calling vgxness_task_claim or vgxness_task_complete.

Reject every other top-level input shape. Do not access the filesystem, shell, network, other tools, or subagents.
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
	navigatorPath := filepath.Join(configDirectory, "agents", navigatorAgentName)
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
		{path: navigatorPath, content: []byte(navigatorPrompt), backup: "vgxness-navigator"},
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

			// managed-by: vgxness; artifact: opencode-plugin/vgxness; version: 27
	const VGXNESS_EXECUTABLE = ` + string(quoted) + `
	const VGXNESS_MODEL = ` + string(quotedModel) + `
	const MAX_OUTPUT_BYTES = __MAX_OUTPUT_BYTES__
	const MAX_ORCHESTRATION_RESULT_BYTES = __MAX_ORCHESTRATION_RESULT_BYTES__
	const TERMINAL_TIMEOUT_MS = 30_000
	const VISIBLE_CLAIM_TIMEOUT_MS = 300_000
	const MAX_NATIVE_DISPATCHES = 4
	const nativeTickets = new Map()
	const nativeCapacity = new Map()
	const visibleWaveClaims = new Map()
	const visibleClaimTokens = new Map()

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
  let exitFailure
  try {
    child = spawn(VGXNESS_EXECUTABLE, [...args, "--workspace", workspace], {
	      cwd: workspace,
	      shell: false,
	      stdio: [payload === undefined ? "ignore" : "pipe", "pipe", "pipe"],
	      signal,
      windowsHide: true,
    })
    if (!child.stdout || !child.stderr) throw new Error("VGXNESS bridge subprocess pipes are unavailable")
    exited = new Promise((resolve) => {
      child.once("error", (cause) => {
        exitFailure = cause
        resolve(undefined)
      })
      child.once("close", (code) => {
        if (code === 0) resolve(undefined)
        else {
          exitFailure ||= new Error("VGXNESS bridge subprocess exited unsuccessfully")
          resolve(undefined)
        }
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
    const envelope = exactEnvelope(stdout)
    if (!envelope.ok || !exitFailure) return envelope
    throw exitFailure
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
	    const first = await invokeBounded(args, payload, workspace)
	    if (first?.ok !== false || first?.error?.recoverable !== true) return first
	  } catch {
	  }
	  return await invokeBounded(args, payload, workspace)
	}

	async function withNativeTicketLane(active, work) {
	  const previous = active.brokerTail || Promise.resolve()
	  let release
	  active.brokerTail = new Promise((resolve) => { release = resolve })
	  await previous.catch(() => undefined)
	  try {
	    return await work()
	  } finally {
	    release()
	  }
	}

	function bridgeFailure(envelope, fallback) {
	  const code = typeof envelope?.error?.code === "string" ? envelope.error.code : ""
	  const message = typeof envelope?.error?.message === "string" ? envelope.error.message : ""
	  if (!code && !message) return fallback
	  return fallback + ": " + [code, message].filter(Boolean).join(" — ")
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

	function normalizeAgentResult(value) {
	  if (!value || Array.isArray(value) || typeof value !== "object") {
	    throw new Error("Native VGXNESS subagent returned an invalid result")
	  }
	  if (Array.isArray(value.errors)) {
	    value.errors = value.errors.map((entry) => {
	      if (typeof entry !== "string") return entry
	      return { code: "native-subagent-observation", message: entry.trim(), recoverable: true }
	    })
	  }
	  if (Array.isArray(value.artifacts) && Array.isArray(value.risks)) {
	    const inlineEvidence = []
	    value.artifacts = value.artifacts.filter((entry) => {
	      const provenance = entry?.provenance
	      if (
	        entry && typeof entry === "object" && !Array.isArray(entry) &&
	        entry.kind === "artifact.reference" && entry.schemaVersion === "1" &&
	        typeof entry.provider === "string" && typeof entry.id === "string" &&
	        typeof entry.artifactType === "string" &&
	        provenance && typeof provenance === "object" && !Array.isArray(provenance) &&
	        typeof provenance.producer === "string" && typeof provenance.createdAt === "string"
	      ) return true
	      inlineEvidence.push("Native subagent evidence: " + (typeof entry === "string" ? entry : JSON.stringify(entry)))
	      return false
	    })
	    value.risks.push(...inlineEvidence)
	  }
	  return value
	}

	function exactAgentResult(parts) {
	  const output = parts
	    .filter((part) => part?.type === "text" && typeof part.text === "string")
	    .map((part) => part.text)
	    .join("")
	    .trim()
	  return normalizeAgentResult(JSON.parse(output))
	}

	function exactAgentResultInput(input) {
	  return normalizeAgentResult(JSON.parse(input))
	}

	function exactNavigatorProposal(parts) {
	  const value = exactAgentResult(parts)
	  if (!Array.isArray(value.tasks) || value.tasks.length < 1 || value.tasks.length > 16) {
	    throw new Error("Native VGXNESS Navigator returned an invalid task proposal")
	  }
	  return value.tasks
	}

	function exactModelReference(model) {
	  const separator = model.indexOf("/")
	  if (separator <= 0 || separator === model.length - 1) throw new Error("VGXNESS returned an invalid native model")
	  return { providerID: model.slice(0, separator), modelID: model.slice(separator + 1) }
	}

	function nativeAgentForTask(task) {
	  if (task?.operation === "review-changes") return "vgxness-reviewer"
	  return "vgxness-explorer"
	}

	function nativeChildTitle(taskId, agent) {
	  const label = String(taskId || "bounded-task").replace(/^task-/, "").replaceAll("-", " ").slice(0, 80)
	  return "VGXNESS " + label + " (@" + agent + " subagent)"
	}

	function exactOrchestration(envelope) {
	  if (envelope?.ok === false) {
	    throw new Error(bridgeFailure(envelope, "VGXNESS could not load the visible orchestration"))
	  }
	  const value = envelope?.orchestration
	  if (
	    !envelope?.ok || !value || typeof value.orchestrationId !== "string" ||
	    typeof value.ownerId !== "string" || typeof value.parentSessionId !== "string" ||
	    !Number.isInteger(value.currentWave) || !Number.isInteger(value.nextWave) ||
	    !value.plan || !Array.isArray(value.plan.tasks) || !Array.isArray(value.plan.waves)
	  ) throw new Error("VGXNESS returned an invalid visible orchestration")
	  return value
	}

	function exactVisiblePrepared(orchestration, taskId, childSessionId, expectedAgent) {
	  const item = Array.isArray(orchestration.prepared)
	    ? orchestration.prepared.find((candidate) => candidate?.taskId === taskId)
	    : undefined
	  if (!item || item.childSessionId !== childSessionId) {
	    throw new Error("VGXNESS did not return the prepared ticket for this visible native Task")
	  }
	  const prepared = exactPrepared({ ok: true, prepared: item.prepared })
	  if (prepared.agent !== expectedAgent) {
	    throw new Error("VGXNESS returned a mismatched visible native Task agent")
	  }
	  return prepared
	}

	function visibleDelegation(envelope, waveIndex) {
	  const orchestration = exactOrchestration(envelope)
	  const wave = orchestration.plan.waves.find((item) => item.index === waveIndex)
	  if (!wave || !Array.isArray(wave.taskIds) || wave.taskIds.length < 1) {
	    throw new Error("VGXNESS did not expose a legal visible execution wave")
	  }
	  const tasks = new Map(orchestration.plan.tasks.map((task) => [task.taskId, task]))
	  const directives = wave.taskIds.map((taskId) => {
	    const task = tasks.get(taskId)
	    if (!task) throw new Error("VGXNESS visible wave references an unknown task")
	    const agent = nativeAgentForTask(task)
	    const claimToken = "claim-" + randomUUID()
	    visibleClaimTokens.set(
	      orchestration.orchestrationId + "\n" + taskId,
	      claimToken,
	    )
	    const prompt = JSON.stringify({
	      kind: "vgxness.visible-task.directive", schemaVersion: "1",
	      orchestrationId: orchestration.orchestrationId, ownerId: orchestration.ownerId, taskId, claimToken,
	    })
	    return {
	      taskId,
	      arguments: {
	        subagent_type: agent,
	        description: ("VGXNESS " + taskId.replace(/^task-/, "").replaceAll("-", " ")).slice(0, 120),
	        prompt,
	      },
	    }
	  })
	  return {
	    protocolVersion: "1",
	    ok: true,
	    bridge: envelope.bridge,
	    provider: envelope.provider,
	    workspace: envelope.workspace,
	    status: "delegation-required",
	    orchestration,
	    delegation: {
	      waveId: wave.waveId,
	      waveIndex: wave.index,
	      mode: wave.mode,
	      tasks: directives,
	    },
	  }
	}

	function visibleDelegationResult(context, envelope, waveIndex) {
	  const value = visibleDelegation(envelope, waveIndex)
	  const count = value.delegation.tasks.length
	  const title = "VGXNESS · wave " + (waveIndex + 1) + " ready · " + count + " visible subagent" + (count === 1 ? "" : "s")
	  const metadata = {
	    orchestrationId: value.orchestration.orchestrationId,
	    waveId: value.delegation.waveId,
	    waveIndex,
	    mode: value.delegation.mode,
	    visibleTaskCount: count,
	  }
	  context.metadata({ title, metadata })
	  return { title, metadata, output: JSON.stringify(value) }
	}

	async function advanceVisibleOrchestration(context, workspace, orchestrationId) {
	  let envelope = await invokeTerminal(
	    ["bridge", "orchestrate-status", "--stdin"],
	    { protocolVersion: "1", orchestrationId },
	    workspace,
	  )
	  let orchestration = exactOrchestration(envelope)
	  if (orchestration.parentSessionId !== context.sessionID) {
	    throw new Error("VGXNESS visible orchestration belongs to another parent session")
	  }
	  if (orchestration.status === "pending") {
	    return visibleDelegationResult(context, envelope, orchestration.nextWave)
	  }
	  if (orchestration.status === "running") {
	    const title = "VGXNESS · visible subagents still running"
	    const metadata = {
	      orchestrationId: orchestration.orchestrationId,
	      currentWave: orchestration.currentWave,
	      status: orchestration.status,
	    }
	    context.metadata({ title, metadata })
	    return { title, metadata, output: JSON.stringify(envelope) }
	  }
	  if (orchestration.status === "cancelled") {
	    const title = "VGXNESS · orchestration cancelled"
	    const metadata = { orchestrationId: orchestration.orchestrationId, status: orchestration.status }
	    context.metadata({ title, metadata })
	    return { title, metadata, output: JSON.stringify(envelope) }
	  }
	  envelope = await invokeTerminal(
	    ["bridge", "orchestrate-join", "--stdin"],
	    {
	      protocolVersion: "1", orchestrationId: orchestration.orchestrationId,
	      ownerId: orchestration.ownerId,
	    },
	    workspace,
	  )
	  orchestration = exactOrchestration(envelope)
	  const title = "VGXNESS · orchestration " + orchestration.status
	  const metadata = {
	    orchestrationId: orchestration.orchestrationId,
	    taskCount: orchestration.plan.tasks.length,
	    status: orchestration.status,
	  }
	  context.metadata({ title, metadata })
	  return { title, metadata, output: JSON.stringify(envelope) }
	}

	function deferredClaim() {
	  let resolve
	  let reject
	  const promise = new Promise((accept, deny) => {
	    resolve = accept
	    reject = deny
	  })
	  return { promise, resolve, reject }
	}

	function rejectVisibleWave(key, wave, cause) {
	  if (!wave || wave.terminal) return
	  wave.terminal = true
	  if (wave.timer) clearTimeout(wave.timer)
	  wave.controller.abort()
	  visibleWaveClaims.delete(key)
	  for (const taskId of wave.taskIds) {
	    visibleClaimTokens.delete(wave.orchestrationId + "\n" + taskId)
	  }
	  for (const claim of wave.claims.values()) claim.deferred.reject(cause)
	}

	async function prepareVisibleWave(key, wave) {
	  if (wave.preparing || wave.terminal) return
	  wave.preparing = true
	  try {
	    const bindings = wave.taskIds.map((taskId) => {
	      const claim = wave.claims.get(taskId)
	      if (!claim) throw new Error("VGXNESS visible wave is missing a claimed native Task")
	      return { taskId, childSessionId: claim.childSessionId, ticketId: claim.ticketId, claimToken: claim.claimToken }
	    })
	    try {
	      const envelope = await invokeBounded(
	        ["bridge", "orchestrate-wave", "--stdin"],
	        {
	          protocolVersion: "1", orchestrationId: wave.orchestrationId,
	          ownerId: wave.ownerId, bindings,
	        },
	        wave.workspace,
	        TERMINAL_TIMEOUT_MS,
	        wave.controller.signal,
	      )
	      const candidate = exactOrchestration(envelope)
	      if (!Array.isArray(candidate.prepared)) {
	        throw new Error("VGXNESS visible wave response omitted prepared tasks")
	      }
	      wave.orchestration = candidate
	    } catch {
	      try {
	        const replay = await invokeTerminal(
	          ["bridge", "orchestrate-wave", "--stdin"],
	          {
	            protocolVersion: "1", orchestrationId: wave.orchestrationId,
	            ownerId: wave.ownerId, bindings,
	          },
	          wave.workspace,
	        )
	        const candidate = exactOrchestration(replay)
	        if (!Array.isArray(candidate.prepared)) {
	          throw new Error("VGXNESS visible wave replay omitted prepared tasks")
	        }
	        wave.orchestration = candidate
	      } catch {
	      const recovered = []
	      let candidate
	      for (const taskId of wave.taskIds) {
	        const claim = wave.claims.get(taskId)
	        if (!claim) throw new Error("VGXNESS visible wave lost a claim during recovery")
	        const envelope = await invokeTerminal(
	          ["bridge", "orchestrate-status", "--stdin"],
	          {
	            protocolVersion: "1", orchestrationId: wave.orchestrationId,
	            taskId, childSessionId: claim.childSessionId, claimToken: claim.claimToken,
	          },
	          wave.workspace,
	        )
	        candidate = exactOrchestration(envelope)
	        const item = Array.isArray(candidate.prepared)
	          ? candidate.prepared.find((prepared) => prepared?.taskId === taskId)
	          : undefined
	        if (item) recovered.push(item)
	      }
	      wave.orchestration = { ...candidate, prepared: recovered }
	      }
	    }
	    const orchestration = wave.orchestration
	    const prepared = new Map(orchestration.prepared.map((item) => [item.taskId, item]))
	    wave.prepared = true
	    if (wave.timer) clearTimeout(wave.timer)
	    for (const taskId of wave.taskIds) {
	      const claim = wave.claims.get(taskId)
	      const item = prepared.get(taskId)
	      if (!claim) {
	        throw new Error("VGXNESS returned a mismatched visible Task ticket")
	      }
	      if (!item) {
	        wave.remaining.delete(taskId)
	        visibleClaimTokens.delete(wave.orchestrationId + "\n" + taskId)
	        claim.deferred.reject(new Error("VGXNESS could not prepare this visible native Task"))
	        continue
	      }
	      if (item.childSessionId !== claim.childSessionId) {
	        throw new Error("VGXNESS returned a mismatched visible Task ticket")
	      }
	      const ticket = exactPrepared({ ok: true, prepared: item.prepared })
	      if (ticket.ticketId !== claim.ticketId || ticket.agent !== claim.agent) {
	        throw new Error("VGXNESS returned a mismatched visible Task ticket")
	      }
	      nativeTickets.set(claim.childSessionId, {
	        ticketId: ticket.ticketId,
	        orchestrationId: wave.orchestrationId,
	        ownerId: wave.ownerId,
	        taskId,
	        parentSessionId: wave.parentSessionId,
	        workspace: wave.workspace,
	        waveKey: key,
	      })
	      visibleClaimTokens.delete(wave.orchestrationId + "\n" + taskId)
	      claim.deferred.resolve(JSON.stringify(ticket))
	    }
	    if (wave.remaining.size === 0) {
	      wave.terminal = true
	      visibleWaveClaims.delete(key)
	    }
	  } catch (cause) {
	    rejectVisibleWave(key, wave, cause instanceof Error ? cause : new Error("VGXNESS could not prepare the visible native Task wave"))
	  }
	}

	async function failVisibleTask(sessionId, category) {
	  const active = nativeTickets.get(sessionId)
	  if (!active?.orchestrationId || active.cleanupInProgress) return
	  active.cleanupInProgress = true
	  const cancelled = category === "native-subagent-cancelled"
	  try {
	    await withNativeTicketLane(active, async () => {
	      const failed = await invokeTerminal(
	        ["bridge", "fail", "--stdin"],
	        {
	          protocolVersion: "1", ticketId: active.ticketId,
	          parentSessionId: active.parentSessionId, childSessionId: sessionId, category,
	        },
	        active.workspace,
	      )
	      if (!failed.ok) throw new Error(bridgeFailure(failed, "VGXNESS rejected visible Task failure recovery"))
	      const terminal = await invokeTerminal(
	        ["bridge", "orchestrate-terminal", "--stdin"],
	        {
	          protocolVersion: "1", orchestrationId: active.orchestrationId, ownerId: active.ownerId,
	          taskId: active.taskId, ticketId: active.ticketId, childSessionId: sessionId,
	          status: cancelled ? "cancelled" : "failed",
	          failure: cancelled ? "visible native Task was cancelled" : "visible native Task ended without durable completion",
	        },
	        active.workspace,
	      )
	      if (!terminal.ok) throw new Error(bridgeFailure(terminal, "VGXNESS rejected visible Task terminal recovery"))
	    })
	    nativeTickets.delete(sessionId)
	    const claimWave = visibleWaveClaims.get(active.waveKey)
	    claimWave?.remaining.delete(active.taskId)
	    if (claimWave?.remaining.size === 0) {
	      claimWave.terminal = true
	      visibleWaveClaims.delete(active.waveKey)
	    }
	  } catch {
	    active.cleanupInProgress = false
	    active.cleanupAttempts = (active.cleanupAttempts || 0) + 1
	    if (active.cleanupAttempts <= 2) {
	      setTimeout(() => { void failVisibleTask(sessionId, category) }, active.cleanupAttempts * 250)
	    }
	  }
	}

	function publishNativeVisibility(context, children, phase, extra = {}) {
	  const visible = children.map((child) => ({
	    taskId: child.taskId,
	    sessionId: child.sessionId,
	    agent: child.agent,
	    status: child.status,
	  }))
	  const active = visible.filter((child) => child.status === "preparing" || child.status === "running")
	  const completed = visible.filter((child) => child.status === "completed").length
	  const failed = visible.filter((child) => child.status === "failed").length
	  const cancelled = visible.filter((child) => child.status === "cancelled").length
	  const displayed = active.length ? active : visible
	  const labels = displayed.map((child) => child.taskId.replace(/^task-/, "")).slice(0, 3)
	  const suffix = displayed.length > 3 ? " +" + (displayed.length - 3) : ""
	  const progress = active.length
	    ? active.length + " active: " + labels.join(", ") + suffix
	    : [
	        completed + "/" + visible.length + " completed",
	        failed ? failed + " failed" : "",
	        cancelled ? cancelled + " cancelled" : "",
	      ].filter(Boolean).join(" · ")
	  const metadata = {
	    parentSessionId: context.sessionID,
	    model: exactModelReference(VGXNESS_MODEL),
	    phase,
	    subagents: visible,
	    ...extra,
	  }
	  if (visible.length === 1) metadata.sessionId = visible[0].sessionId
	  const presentation = { title: ("VGXNESS · " + progress).slice(0, 180), metadata }
	  context.metadata(presentation)
	  return presentation
	}

	async function createNativeChild(client, workspace, parentSessionId, title) {
	  return responseData(await client.session.create({
	    query: { directory: workspace },
	    body: { parentID: parentSessionId, title },
	  }), "OpenCode could not create a native VGXNESS subagent")
	}

	async function promptNativeChild(client, workspace, context, childSessionId, prepared) {
	  nativeTickets.set(childSessionId, { ticketId: prepared.ticketId })
	  const model = exactModelReference(prepared.model)
	  const abortChild = () => client.session.abort({ path: { id: childSessionId }, query: { directory: workspace } }).catch(() => undefined)
	  const abortChildOnContext = () => { void abortChild() }
	  context.abort.addEventListener("abort", abortChildOnContext, { once: true })
	  let timer
	  try {
	    const deadlineAt = Date.parse(prepared.deadline)
	    if (!Number.isFinite(deadlineAt)) throw new Error("VGXNESS returned an invalid native deadline")
	    const timeoutMs = Math.max(1, Math.min(2147483647, deadlineAt - Date.now()))
	    const deadline = new Promise((_, reject) => {
	      timer = setTimeout(() => {
	        void abortChild()
	        reject(new Error("Native VGXNESS subagent deadline exceeded"))
	      }, timeoutMs)
	    })
	    return responseData(await Promise.race([client.session.prompt({
	      path: { id: childSessionId },
	      query: { directory: workspace },
	      body: {
	        agent: prepared.agent,
	        model,
	        parts: [{
	          type: "text",
	          text: JSON.stringify({
	            kind: "vgxness.direct-dispatch.directive",
	            schemaVersion: "1",
	            preparedPrompt: prepared.prompt,
	          }),
	        }],
	      },
	    }), deadline]), "Native VGXNESS subagent execution failed")
	  } finally {
	    if (timer) clearTimeout(timer)
	    context.abort.removeEventListener("abort", abortChildOnContext)
	    nativeTickets.delete(childSessionId)
	  }
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

	function boundedRunStrings(values, maxItems, maxRunes, label) {
	  if (values === undefined) return []
	  if (!Array.isArray(values) || values.length > maxItems) throw new Error("VGXNESS " + label + " exceeded its bound")
	  return values.map((value) => {
	    if (typeof value !== "string" || !value.trim() || Array.from(value).length > maxRunes) {
	      throw new Error("VGXNESS " + label + " is invalid")
	    }
	    return value.trim()
	  })
	}

	async function startVisibleOrchestration(client, context, workspace, args, mode) {
	  const profiles = {
	    fast: { maxTasks: 1, maxParallel: 1, verification: "essential" },
	    auto: { maxTasks: 4, maxParallel: 4, verification: "proportional" },
	    deep: { maxTasks: 16, maxParallel: 4, verification: "exhaustive" },
	  }
	  const profile = profiles[mode]
	  if (!profile) throw new Error("VGXNESS run mode is invalid")
	  if (typeof args.goal !== "string" || !args.goal.trim() || Array.from(args.goal).length > 8192) {
	    throw new Error("VGXNESS run goal is invalid")
	  }
	  const goal = args.goal.trim()
	  const userConstraints = boundedRunStrings(args.constraints, 16, 2048, "run constraints")
	  const acceptanceCriteria = boundedRunStrings(args.acceptanceCriteria, 32, 2048, "acceptance criteria")
	  for (const constraint of userConstraints) {
	    if (acceptanceCriteria.length === 32) throw new Error("VGXNESS run constraints exceeded the durable criteria bound")
	    const durableConstraint = "Constraint: " + constraint
	    if (Array.from(durableConstraint).length > 2048) throw new Error("VGXNESS run constraint exceeded its durable bound")
	    acceptanceCriteria.push(durableConstraint)
	  }
	  if (typeof args.desiredOutcome === "string" && args.desiredOutcome.trim()) {
	    const desired = "Desired outcome: " + args.desiredOutcome.trim()
	    if (Array.from(desired).length > 2048 || acceptanceCriteria.length === 32) {
	      throw new Error("VGXNESS desired outcome exceeded its bound")
	    }
	    acceptanceCriteria.push(desired)
	  } else if (args.desiredOutcome !== undefined) {
	    throw new Error("VGXNESS desired outcome is invalid")
	  }
	  const navigatorModel = exactModelReference(VGXNESS_MODEL)
	  let navigatorSessionId = ""
	  try {
	    const navigator = await createNativeChild(client, workspace, context.sessionID, "VGXNESS planning (@vgxness-navigator subagent)")
	    navigatorSessionId = navigator.id
	    const planningPrompt = JSON.stringify({
	      kind: "vgxness.navigator.request", schemaVersion: "1", goal,
	      acceptanceCriteria,
	      userConstraints,
	      operatingMode: mode,
	      constraints: {
	        maxTasks: profile.maxTasks, maxParallel: profile.maxParallel, verification: profile.verification,
	        nativeWrites: false, agentChoice: "vgxness-authority",
	      },
	    })
	    const plannedMessage = responseData(await client.session.prompt({
	      path: { id: navigatorSessionId },
	      query: { directory: workspace },
	      body: {
	        agent: "vgxness-navigator",
	        model: navigatorModel,
	        parts: [{ type: "text", text: planningPrompt }],
	      },
	    }), "Native VGXNESS Navigator execution failed")
	    const candidateTasks = exactNavigatorProposal(plannedMessage.parts || []).map((task) => {
	      const taskCriteria = boundedRunStrings(task?.acceptanceCriteria, 32, 2048, "Navigator acceptance criteria")
	      const combined = [...new Set([...taskCriteria, ...acceptanceCriteria])]
	      if (combined.length > 32) throw new Error("Native VGXNESS Navigator exceeded the durable criteria bound")
	      return { ...task, acceptanceCriteria: combined }
	    })
	    if (candidateTasks.length > profile.maxTasks) {
	      throw new Error("Native VGXNESS Navigator exceeded the selected run mode")
	    }
	    const envelope = await invokeBounded(
	      ["bridge", "orchestrate-plan", "--stdin"],
	      {
	        protocolVersion: "1", model: VGXNESS_MODEL,
	        input: { goal, acceptanceCriteria },
	        parentSessionId: context.sessionID, parentMessageId: context.messageID, candidateTasks,
	      },
	      workspace,
	      TERMINAL_TIMEOUT_MS,
	      context.abort,
	    )
	    const orchestration = exactOrchestration(envelope)
	    if (orchestration.nextWave !== 0 || orchestration.status !== "pending") {
	      throw new Error("VGXNESS visible orchestration did not start at the initial wave")
	    }
	    return visibleDelegationResult(context, envelope, 0)
	  } catch (cause) {
	    if (navigatorSessionId) {
	      await client.session.abort({ path: { id: navigatorSessionId }, query: { directory: workspace } }).catch(() => undefined)
	    }
	    throw cause instanceof Error ? cause : new Error("VGXNESS visible orchestration planning failed")
	  }
	}

	export default async function VGXNESSPlugin({ client, directory }) {
	  return {
	    "tool.execute.before": async (input) => {
	      if (input?.tool !== "glob" && input?.tool !== "list") return
	      const session = responseData(await client.session.get({
	        path: { id: input.sessionID },
	        query: { directory },
	      }), "OpenCode could not verify the native tool session")
	      const unverifiedChild = !session.agent && typeof session.parentID === "string" && session.parentID
	      if ((session.agent === "vgxness-explorer" || unverifiedChild) && !nativeTickets.has(input.sessionID)) {
	        throw new Error("VGXNESS explorer discovery requires an active ticket")
	      }
	    },
	    event: async ({ event }) => {
	      if (event?.type === "session.idle") {
	        await failVisibleTask(event.properties.sessionID, "native-subagent-failed")
	      } else if (event?.type === "session.error" && event.properties.sessionID) {
	        await failVisibleTask(event.properties.sessionID, "native-subagent-failed")
	      }
	    },
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
	          const envelope = await withNativeTicketLane(active, () => invokeBounded(
	            ["bridge", "read", "--stdin"],
	            { protocolVersion: "1", ticketId: active.ticketId, childSessionId: context.sessionID, path: args.path, offset, limit: 65536 },
	            workspace,
	            TERMINAL_TIMEOUT_MS,
	            context.abort,
	          ))
	          if (!envelope.ok || !envelope.read) throw new Error(bridgeFailure(envelope, "VGXNESS native read was denied"))
	          return JSON.stringify(envelope.read)
	        },
	      }),
	      vgxness_codegraph: tool({
	        description: "Query the local CodeGraph index through the active ticket. Use explore for structural context, impact for one symbol, affected for tests related to explicit files, or status for index health.",
	        args: {
	          operation: tool.schema.enum(["status", "explore", "impact", "affected"]),
	          query: tool.schema.string().optional(),
	          symbol: tool.schema.string().optional(),
	          files: tool.schema.array(tool.schema.string()).optional(),
	          depth: tool.schema.number().optional(),
	          maxFiles: tool.schema.number().optional(),
	        },
	        async execute(args, context) {
	          const workspace = context.worktree || context.directory
	          const active = nativeTickets.get(context.sessionID)
	          if (!active) throw new Error("No active VGXNESS structural-analysis ticket exists for this child session")
	          const decimal = (value, fallback, maximum, label) => {
	            if (value === undefined) return fallback
	            const parsed = Number(value)
	            const canonical = typeof value === "number" ? parsed === value : String(parsed) === value
	            if (!Number.isSafeInteger(parsed) || parsed < 0 || !canonical) {
	              throw new Error("VGXNESS " + label + " is invalid")
	            }
	            if (parsed === 0) return fallback
	            return Math.min(parsed, maximum)
	          }
	          const envelope = await withNativeTicketLane(active, () => invokeBounded(
	            ["bridge", "codegraph", "--stdin"],
	            {
	              protocolVersion: "1", ticketId: active.ticketId, childSessionId: context.sessionID,
	              operation: args.operation, query: args.query, symbol: args.symbol, files: args.files,
	              depth: decimal(args.depth, 0, 5, "CodeGraph depth"),
	              maxFiles: decimal(args.maxFiles, 0, 12, "CodeGraph maxFiles"),
	            },
	            workspace,
	            TERMINAL_TIMEOUT_MS,
	            context.abort,
	          ))
	          if (!envelope.ok || !envelope.codegraph) throw new Error(bridgeFailure(envelope, "VGXNESS CodeGraph query was unavailable or denied"))
	          return JSON.stringify(envelope.codegraph)
	        },
	      }),
	      vgxness_task_claim: tool({
	        description: "Claim one VGXNESS-approved visible native Task and receive its exact content-bound execution prompt.",
	        args: {
	          orchestrationId: tool.schema.string(),
	          ownerId: tool.schema.string(),
	          taskId: tool.schema.string(),
	          claimToken: tool.schema.string(),
	        },
	        async execute(args, context) {
	          const workspace = context.worktree || context.directory
	          if (nativeTickets.has(context.sessionID)) throw new Error("This native Task already holds an active VGXNESS ticket")
	          const status = await invokeTerminal(
	            ["bridge", "orchestrate-status", "--stdin"],
	            { protocolVersion: "1", orchestrationId: args.orchestrationId },
	            workspace,
	          )
	          const orchestration = exactOrchestration(status)
	          if (
	            orchestration.ownerId !== args.ownerId ||
	            orchestration.parentSessionId === context.sessionID ||
	            orchestration.status === "completed" ||
	            orchestration.status === "failed" ||
	            orchestration.status === "cancelled"
	          ) throw new Error("VGXNESS denied the visible native Task claim")
	          const task = orchestration.plan.tasks.find((item) => item.taskId === args.taskId)
	          const wave = orchestration.plan.waves.find((item) => item.taskIds.includes(args.taskId))
	          const expectedAgent = nativeAgentForTask(task)
	          const expectedWave = orchestration.status === "pending"
	            ? orchestration.nextWave
	            : orchestration.currentWave
	          if (!task || !wave || wave.index !== expectedWave || context.agent !== expectedAgent) {
	            throw new Error("VGXNESS visible native Task claim does not match the approved wave")
	          }
	          const child = responseData(await client.session.get({
	            path: { id: context.sessionID },
	            query: { directory: workspace },
	          }), "OpenCode could not verify the visible native Task session")
	          if (child.parentID !== orchestration.parentSessionId) {
	            throw new Error("VGXNESS visible native Task is not attached to the approved parent session")
	          }
	          const key = workspace + "\n" + orchestration.orchestrationId + "\n" + wave.waveId
	          const tokenKey = orchestration.orchestrationId + "\n" + args.taskId
	          if (orchestration.status === "pending" && visibleClaimTokens.get(tokenKey) !== args.claimToken) {
	            throw new Error("VGXNESS visible native Task claim capability is invalid")
	          }
	          if (orchestration.status === "running") {
	            const recovery = await invokeTerminal(
	              ["bridge", "orchestrate-status", "--stdin"],
	              {
	                protocolVersion: "1", orchestrationId: args.orchestrationId,
	                taskId: args.taskId, childSessionId: context.sessionID, claimToken: args.claimToken,
	              },
	              workspace,
	            )
	            const recoveredOrchestration = exactOrchestration(recovery)
	            const prepared = exactVisiblePrepared(recoveredOrchestration, args.taskId, context.sessionID, expectedAgent)
	            nativeTickets.set(context.sessionID, {
	              ticketId: prepared.ticketId,
	              orchestrationId: orchestration.orchestrationId,
	              ownerId: orchestration.ownerId,
	              taskId: args.taskId,
	              parentSessionId: orchestration.parentSessionId,
	              workspace,
	              waveKey: key,
	            })
	            return JSON.stringify(prepared)
	          }
	          let claimWave = visibleWaveClaims.get(key)
	          if (!claimWave) {
	            claimWave = {
	              workspace,
	              orchestrationId: orchestration.orchestrationId,
	              ownerId: orchestration.ownerId,
	              parentSessionId: orchestration.parentSessionId,
	              waveId: wave.waveId,
	              taskIds: [...wave.taskIds],
	              claims: new Map(),
	              remaining: new Set(wave.taskIds),
	              controller: new AbortController(),
	              preparing: false,
	              prepared: false,
	              terminal: false,
	            }
	            claimWave.timer = setTimeout(() => {
	              rejectVisibleWave(key, claimWave, new Error("Timed out waiting for every visible native Task in the approved wave"))
	            }, VISIBLE_CLAIM_TIMEOUT_MS)
	            visibleWaveClaims.set(key, claimWave)
	          }
	          if (
	            claimWave.ownerId !== orchestration.ownerId ||
	            claimWave.parentSessionId !== orchestration.parentSessionId ||
	            claimWave.taskIds.length !== wave.taskIds.length ||
	            claimWave.taskIds.some((taskId, index) => taskId !== wave.taskIds[index])
	          ) throw new Error("VGXNESS visible native Task wave identity changed")
	          let claim = claimWave.claims.get(args.taskId)
	          if (claim && claim.childSessionId !== context.sessionID) {
	            throw new Error("VGXNESS visible native Task was already claimed by another session")
	          }
	          for (const [claimedTaskId, existing] of claimWave.claims.entries()) {
	            if (claimedTaskId !== args.taskId && existing.childSessionId === context.sessionID) {
	              throw new Error("One visible native Task session cannot claim multiple tasks")
	            }
	          }
	          if (!claim) {
	            claim = {
	              childSessionId: context.sessionID,
	              ticketId: "ticket-" + randomUUID(),
	              claimToken: args.claimToken,
	              agent: expectedAgent,
	              deferred: deferredClaim(),
	            }
	            claimWave.claims.set(args.taskId, claim)
	          }
	          if (claimWave.claims.size === claimWave.taskIds.length) {
	            void prepareVisibleWave(key, claimWave)
	          }
	          const abortClaim = () => rejectVisibleWave(key, claimWave, new Error("Visible native Task claim was cancelled"))
	          context.abort.addEventListener("abort", abortClaim, { once: true })
	          try {
	            return await claim.deferred.promise
	          } finally {
	            context.abort.removeEventListener("abort", abortClaim)
	          }
	        },
	      }),
	      vgxness_task_complete: tool({
	        description: "Durably complete the active VGXNESS-visible native Task with its exact compact agent.result JSON.",
	        args: {
	          result: tool.schema.string(),
	        },
	        async execute(args, context) {
	          const workspace = context.worktree || context.directory
	          const active = nativeTickets.get(context.sessionID)
	          if (!active?.orchestrationId || active.workspace !== workspace) {
	            throw new Error("No active VGXNESS visible native Task ticket exists for this session")
	          }
	          const result = exactAgentResultInput(args.result)
	          if (new TextEncoder().encode(JSON.stringify(result)).length > MAX_ORCHESTRATION_RESULT_BYTES) {
	            throw new Error("Native VGXNESS visible Task result exceeded its aggregate-safe bound")
	          }
	          const terminal = await withNativeTicketLane(active, async () => {
	            const completed = await invokeTerminal(
	              ["bridge", "complete", "--stdin"],
	              {
	                protocolVersion: "1", ticketId: active.ticketId,
	                parentSessionId: active.parentSessionId, childSessionId: context.sessionID,
	                messageId: context.messageID, result,
	              },
	              workspace,
	            )
	            if (!completed.ok) throw new Error(bridgeFailure(completed, "VGXNESS rejected the visible native Task result"))
	            if (completed.status !== "completed") {
	              const failed = await invokeTerminal(
	                ["bridge", "orchestrate-terminal", "--stdin"],
	                {
	                  protocolVersion: "1", orchestrationId: active.orchestrationId, ownerId: active.ownerId,
	                  taskId: active.taskId, ticketId: active.ticketId, childSessionId: context.sessionID,
	                  status: "failed", failure: "native Task result failed its content-bound contract",
	                },
	                workspace,
	              )
	              if (!failed.ok) throw new Error(bridgeFailure(failed, "VGXNESS rejected the failed visible native Task terminal"))
	              throw new Error("VGXNESS rejected the visible native Task result contract")
	            }
	            const recorded = await invokeTerminal(
	              ["bridge", "orchestrate-terminal", "--stdin"],
	              {
	                protocolVersion: "1", orchestrationId: active.orchestrationId, ownerId: active.ownerId,
	                taskId: active.taskId, ticketId: active.ticketId, childSessionId: context.sessionID,
	                status: "completed", messageId: context.messageID,
	                resultId: "result-" + active.ticketId, result,
	              },
	              workspace,
	            )
	            if (!recorded.ok) throw new Error(bridgeFailure(recorded, "VGXNESS rejected the visible native Task terminal"))
	            return recorded
	          })
	          nativeTickets.delete(context.sessionID)
	          const claimWave = visibleWaveClaims.get(active.waveKey)
	          claimWave?.remaining.delete(active.taskId)
	          if (claimWave?.remaining.size === 0) {
	            claimWave.terminal = true
	            visibleWaveClaims.delete(active.waveKey)
	          }
	          return JSON.stringify(terminal)
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
	      vgxness_run: tool({
	        description: "Run one user goal through the smallest sufficient VGXNESS plan. VGXNESS chooses a single visible native Task, parallel independent Tasks, or dependent waves while preserving the same bounded authority and durable join.",
	        args: {
	          action: tool.schema.enum(["start", "advance"]).optional(),
	          goal: tool.schema.string().optional(),
	          mode: tool.schema.enum(["fast", "auto", "deep"]).optional(),
	          constraints: tool.schema.array(tool.schema.string()).optional(),
	          desiredOutcome: tool.schema.string().optional(),
	          acceptanceCriteria: tool.schema.array(tool.schema.string()).optional(),
	          orchestrationId: tool.schema.string().optional(),
	        },
	        async execute(args, context) {
	          const workspace = context.worktree || context.directory
	          const action = args.action || "start"
	          if (action === "start") {
	            if (typeof args.goal !== "string" || !args.goal.trim() || args.orchestrationId) {
	              throw new Error("Starting a VGXNESS run requires a non-empty goal and no orchestrationId")
	            }
	            return await startVisibleOrchestration(client, context, workspace, args, args.mode || "auto")
	          }
	          if (
	            typeof args.orchestrationId !== "string" || !args.orchestrationId ||
	            args.goal || args.mode || args.constraints || args.desiredOutcome || args.acceptanceCriteria
	          ) {
	            throw new Error("Advancing a VGXNESS run requires only its orchestrationId")
	          }
	          return await advanceVisibleOrchestration(context, workspace, args.orchestrationId)
	        },
	      }),
	      vgxness_orchestrate: tool({
	        description: "Start or advance one VGXNESS-approved multi-task orchestration when real dependencies, independent evidence, or a synthesis wave justify decomposition. Pass only each delegation task's exact arguments object to the built-in OpenCode Task tool so native subagents are visible in the parent conversation.",
	        args: {
	          action: tool.schema.enum(["start", "advance"]).optional(),
	          goal: tool.schema.string().optional(),
	          acceptanceCriteria: tool.schema.array(tool.schema.string()).optional(),
	          orchestrationId: tool.schema.string().optional(),
	        },
	        async execute(args, context) {
	          const workspace = context.worktree || context.directory
	          const action = args.action || "start"
	          if (action === "start") {
	            if (typeof args.goal !== "string" || !args.goal.trim() || args.orchestrationId) {
	              throw new Error("Starting a visible VGXNESS orchestration requires only a non-empty goal")
	            }
	            return await startVisibleOrchestration(client, context, workspace, args, "deep")
	          }
	          if (typeof args.orchestrationId !== "string" || !args.orchestrationId || args.goal || args.acceptanceCriteria) {
	            throw new Error("Advancing a visible VGXNESS orchestration requires only its orchestrationId")
	          }
	          return await advanceVisibleOrchestration(context, workspace, args.orchestrationId)
	        },
	      }),
	      vgxness_dispatch: tool({
	        description: "Start or join the preferred single bounded VGXNESS operation when one inspection or review is sufficient. A normal start returns exact built-in Task arguments so OpenCode renders its child session; call join with the returned orchestrationId after that Task terminates. Explicit continuity remains a legacy direct-session compatibility path.",
	        args: {
	          action: tool.schema.enum(["start", "join"]).optional(),
	          operation: tool.schema.enum(["read-files", "analyze-structure", "write-files", "review-changes"]).optional(),
	          goal: tool.schema.string().optional(),
	          acceptanceCriteria: tool.schema.array(tool.schema.string()).optional(),
	          orchestrationId: tool.schema.string().optional(),
	          continuity: tool.schema.enum(["start", "continue", "finish"]).optional(),
	          runId: tool.schema.string().optional(),
	        },
	        async execute(args, context) {
	          const workspace = context.worktree || context.directory
	          const action = args.action || "start"
	          if (action === "join") {
	            if (
	              typeof args.orchestrationId !== "string" || !args.orchestrationId ||
	              args.operation || args.goal || args.acceptanceCriteria || args.continuity || args.runId
	            ) {
	              throw new Error("Joining a visible VGXNESS dispatch requires only its orchestrationId")
	            }
	            return await advanceVisibleOrchestration(context, workspace, args.orchestrationId)
	          }
	          if (
	            typeof args.operation !== "string" || typeof args.goal !== "string" || !args.goal.trim() ||
	            args.orchestrationId
	          ) {
	            throw new Error("Starting a VGXNESS dispatch requires an operation and a non-empty goal")
	          }
	          if (args.continuity === undefined && args.runId === undefined) {
	            if (args.operation === "write-files") {
	              throw new Error("Native write-files remains unavailable until a ticket-authenticated edit broker exists")
	            }
	            const taskId = "task-dispatch-" + randomUUID().replaceAll("-", "").slice(0, 24).toLowerCase()
	            const candidateTask = {
	              taskId,
	              capability: args.operation === "review-changes" ? "review" : "explore",
	              operation: args.operation,
	              goal: args.goal,
	              acceptanceCriteria: args.acceptanceCriteria || [],
	              dependsOn: [],
	              continuity: "isolated",
	            }
	            const envelope = await invokeBounded(
	              ["bridge", "orchestrate-plan", "--stdin"],
	              {
	                protocolVersion: "1", model: VGXNESS_MODEL,
	                input: { goal: args.goal, acceptanceCriteria: args.acceptanceCriteria || [] },
	                parentSessionId: context.sessionID, parentMessageId: context.messageID,
	                candidateTasks: [candidateTask],
	              },
	              workspace,
	              TERMINAL_TIMEOUT_MS,
	              context.abort,
	            )
	            const orchestration = exactOrchestration(envelope)
	            if (
	              orchestration.status !== "pending" || orchestration.nextWave !== 0 ||
	              orchestration.plan.tasks.length !== 1 ||
	              orchestration.plan.tasks[0].taskId !== taskId ||
	              orchestration.plan.tasks[0].operation !== args.operation
	            ) {
	              throw new Error("VGXNESS direct dispatch did not preserve its single approved native Task")
	            }
	            return visibleDelegationResult(context, envelope, 0)
	          }
	          const releaseCapacity = acquireNativeCapacity(workspace, (args.operation === "read-files" || args.operation === "analyze-structure") && args.continuity === undefined && args.runId === undefined)
	          let prepared
	          let ticketId = ""
	          let childSessionId = ""
	          let deadlineExceeded = false
	          const agent = args.operation === "review-changes" ? "vgxness-reviewer" : "vgxness-explorer"
	          const visible = { taskId: args.operation, sessionId: "", agent, status: "preparing" }
	          const publish = (phase) => publishNativeVisibility(context, visible.sessionId ? [visible] : [], phase, { operation: args.operation })
	          try {
	            const created = responseData(await client.session.create({
	              query: { directory: workspace },
	              body: { parentID: context.sessionID, title: nativeChildTitle(args.operation, agent) },
	            }), "OpenCode could not create a native VGXNESS subagent")
	            childSessionId = created.id
	            visible.sessionId = childSessionId
	            publish("preparing")
	            ticketId = "ticket-" + randomUUID()
	            const envelope = await invoke(
	              ["bridge", "prepare", "--stdin"],
	              { ...args, protocolVersion: "1", ticketId, model: VGXNESS_MODEL, parentSessionId: context.sessionID, parentMessageId: context.messageID, childSessionId },
	              workspace,
	              context.abort,
	            )
	            if (envelope.ok && envelope.status === "recovered" && typeof envelope.runId === "string" && envelope.runId) {
	              await client.session.abort({ path: { id: childSessionId }, query: { directory: workspace } }).catch(() => undefined)
	              visible.status = "cancelled"
	              const presentation = publish("recovered")
	              ticketId = ""
	              return { ...presentation, output: JSON.stringify(envelope) }
	            }
	            prepared = exactPrepared(envelope)
	            if (prepared.ticketId !== ticketId) throw new Error("VGXNESS returned a mismatched native dispatch ticket")
	            visible.agent = prepared.agent
	            visible.status = "running"
	            publish("running")
	            const message = await promptNativeChild(client, workspace, context, childSessionId, prepared)
	            const result = exactAgentResult(message.parts || [])
	            const completed = await invokeTerminal(
	              ["bridge", "complete", "--stdin"],
	              {
	                protocolVersion: "1", ticketId: prepared.ticketId, parentSessionId: context.sessionID,
	                childSessionId, messageId: message.info.id, result,
	              },
	              workspace,
	            )
	            visible.status = "completed"
	            const presentation = publish("completed")
	            return { ...presentation, output: JSON.stringify(completed) }
	          } catch (cause) {
	            let cleanupFailure
	            if (childSessionId) await client.session.abort({ path: { id: childSessionId }, query: { directory: workspace } }).catch(() => undefined)
	            if (ticketId) {
	              deadlineExceeded = cause instanceof Error && cause.message === "Native VGXNESS subagent deadline exceeded"
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
	            if (visible.sessionId) {
	              visible.status = context.abort.aborted ? "cancelled" : "failed"
	              publish(visible.status)
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
	content = strings.ReplaceAll(content, "__MAX_ORCHESTRATION_RESULT_BYTES__", strconv.Itoa(bridge.MaxOrchestrationResultBytes))
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
