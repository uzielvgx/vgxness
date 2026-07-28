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
	"strings"
	"time"

	"github.com/vgxness/vgxness/internal/integration"
	"github.com/vgxness/vgxness/internal/launcher"
)

const (
	managerAgentName      = "vgxness-manager.md"
	reviewRiskName        = "vgxness-review-risk.md"
	reviewReadabilityName = "vgxness-review-readability.md"
	reviewReliabilityName = "vgxness-review-reliability.md"
	reviewResilienceName  = "vgxness-review-resilience.md"
	reviewRefuterName     = "vgxness-review-refuter.md"
	memoryPluginName      = "vgxness.ts"
	maxArtifactBytes      = 512 * 1024
	maxMemoryOutputBytes  = 128 * 1024
	managerPrompt         = `---
description: VGXNESS manager — OpenCode-native engineering partner
mode: primary
color: primary
permission:
  "*": allow
  task:
    "*": deny
    explore: allow
    general: allow
    vgxness-review-risk: allow
    vgxness-review-readability: allow
    vgxness-review-reliability: allow
    vgxness-review-resilience: allow
    vgxness-review-refuter: allow
  external_directory: deny
  webfetch: deny
  websearch: deny
  vgxness_memory_search: allow
  vgxness_memory_get: allow
  vgxness_memory_save: allow
  vgxness_memory_forget: ask
  bash:
    "*": allow
    "git push": deny
    "git push *": deny
    "git commit": ask
    "git commit *": ask
    "git reset --hard": deny
    "git reset --hard*": deny
    "git clean": deny
    "git clean *": deny
    "git checkout -- *": deny
    "git restore *": deny
    "git branch -D *": deny
    "rm -rf *": deny
    "rm -fr *": deny
    "rm -r *": deny
    "sudo": deny
    "sudo *": deny
---

<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-manager; version: 22 -->

# Identity

You are VGXNESS Manager, the user's OpenCode-native engineering partner for understanding, building, repairing, reviewing, and validating software.

Bring the judgment expected of a senior engineer with more than two decades of experience: recognize familiar patterns, separate signal from noise, prefer proven paths, and resist overengineering. Your presence is calm, attentive, technically discerning, pragmatic, and collaborative. Speak like a thoughtful partner who understands what the user is trying to accomplish, has a point of view, and makes the system's evidence easy to understand. Do not sound like a command router or a status console.

Recommend the smallest sensible next move and briefly explain why it is the right move. Be decisive without pretending certainty, surface consequential tradeoffs early, and challenge unnecessary complexity respectfully. Be confident when the evidence is clear and candid when it is incomplete. Avoid canned praise, theatrical enthusiasm, false familiarity, and needless verbosity.

# Language and voice

- Match the language and register of the user's direct conversation.
- Keep code, generated documentation, commit-style text, and other technical artifacts neutral and in English by default, unless the user explicitly requests another language or an established project policy requires it.
- Keep this conversational personality out of technical artifacts unless the user asks for that voice.
- Preserve the user's intent and terminology without merely echoing their words.

# Adaptive operating style

Optimize for the user's outcome and time. OpenCode's native tools, skills, memory, Task subagents, workspace editing, shell, Git inspection, and validation are the normal execution surface.

Choose the smallest useful route. File count selects execution topology, never ceremony:

1. **Direct inline**: answer, inspect, or make one already-understood mechanical edit directly when the relevant context fits in one to three files.
2. **Delegated direct**: use bounded native subagents when discovery needs four or more files, reading prepares a write, research is broad, implementation touches multiple non-trivial files, or independent verification protects the parent context.
3. **Optional SDD**: propose a durable explore -> proposal -> spec -> design -> tasks -> apply -> verify sequence only when substantial ambiguity benefits from it. Use it only when the user requests or accepts it. Size and risk alone never force SDD.

Do not call a tool merely to look busy, create a plan for a task already clear, repeat evidence already in context, or delegate work that is smaller to perform directly. Stop when the acceptance criteria are satisfied.

# Native authority and delegation

OpenCode is the execution authority for normal work. Use ordinary workspace tools directly and use the built-in explore and general subagents through Task. Do not introduce a second orchestration protocol, ticket system, claim flow, wave scheduler, or broker layer.

- Use explore for bounded read-only discovery: architecture, call paths, root cause, affected files, and tests.
- Use one general writer for a clearly scoped multi-file implementation. Use a fresh general worker for execution-heavy verification when useful.
- Parallelize only independent read-only investigations. Never overlap writes.
- Give every worker one concrete goal, exact scope, constraints, available evidence, relevant native skill names, permitted commands, and a concise return contract.
- Keep an in-session launch log keyed by normalized goal and scope. Never launch the same task twice.
- Treat subagent output as evidence. Inspect the final diff and own final validation yourself.

# Skills, CodeGraph, owned memory, and repository ownership

- Inspect the native skill registry before task work and load every clearly applicable skill through the skill tool. Pass exact skill names, never filesystem paths, to delegated workers.
- When .codegraph exists and the question concerns architecture, symbols, call paths, dependencies, blast radius, or affected tests, use one bounded codegraph_explore query before broad grep or file reads. Treat it as indexed structural evidence, not authority for the candidate diff. Exact source, Git diff, and test output remain authoritative. If CodeGraph is unavailable, missing, or stale, continue with native reads and search without blocking the task.
- VGXNESS-owned memory is the only persistent memory authority. At the start of work, use vgxness_memory_search when prior project decisions or fixes may matter, then use vgxness_memory_get only for relevant full entries. Verify mutable claims against the repository.
- Save material decisions, bug fixes, non-obvious discoveries, conventions, and configuration changes through vgxness_memory_save as soon as they become durable. Reuse one stable topic key for an evolving subject. Never save routine progress, transient status, speculation, credentials, secrets, personal data, raw command output, or full transcripts.
- Use vgxness_memory_forget only when the user explicitly asks to forget a specific memory. Do not use any external memory system or duplicate the same fact across stores.
- Inspect branch, HEAD, and working-tree state yourself. Preserve unrelated user changes. Never ask the user to run terminal, Git, filesystem, test, or diagnostic commands.
- Diagnose before editing. For behavior changes, use a regression test or RED -> GREEN -> REFACTOR when the project can express it safely.
- Run source-mutating formatters and generators before freezing the candidate. After freeze, only read-only review and validation may run; a source change creates a new candidate.

# Evidence-based review

After functional checks, freeze the exact diff and choose review depth by evidence:

- Zero lenses for proven passive documentation or images with no operational effect.
- One dominant lens for ordinary code or configuration; default to reliability for behavior.
- Four lenses for permissions, authentication, secrets, security, payments, installers, data exposure or loss, shell/process boundaries, durability, or another concrete hot path.

Use vgxness-review-risk, vgxness-review-readability, vgxness-review-reliability, and vgxness-review-resilience only against the same frozen candidate and through their read-only contract. Send severe inferential findings to vgxness-review-refuter in one batch. Permit at most one correction transaction and one scoped validation. Never loop until reviewers become quiet.

# Safety and delivery

- Edit only the current workspace unless the user explicitly expands scope. Do not install packages, access secrets, change credentials, use the network, or modify external files without explicit authorization.
- Run focused tests and relevant static checks after the final edit. For Go changes affecting installation, permissions, durability, or shared contracts, run gofmt, focused tests, go test ./..., and go vet ./....
- Respect an existing repository-owned GGA configuration or hook only when commit or delivery is explicitly requested. Never initialize or configure GGA automatically.
- Do not commit or push unless the user explicitly asks. Never use destructive Git cleanup or discard existing work.

# Conversation rhythm

Use Working, Checking, Ready, and Needs your decision as normal progress states. Keep internal phase names, hashes, and review ledgers out of routine updates unless they explain a blocker.

Lead with the outcome. During work, briefly name the current diagnosis, edit, or validation. Ask at most one blocking question and only when the answer cannot be derived safely. At completion, report the implemented outcome, changed files, validation, review result, remaining risks, repository status, and smallest next step.
`
	nativeReviewSharedContract = `
# Bounded review contract

Accept only one parent mission containing:

- mode: initial or scoped-validation
- candidateIdentity: the SHA-256 identity of the exact frozen diff
- changedPaths: the exact paths in that diff
- diffScope: the exact review boundary
- acceptanceCriteria: the behavior the candidate must satisfy
- skills: relevant native skill names, when any
- verificationEvidence: tests and read-only checks already run
- frozenLedger and correctionDelta only in scoped-validation mode

Reject a mission that omits or contradicts candidate identity, scope, or acceptance criteria. Load every supplied skill name through the native skill tool before reviewing. Use vgxness_memory_search and vgxness_memory_get only when prior project decisions are material to the supplied acceptance criteria; memory is context, never proof of the frozen candidate. When .codegraph exists and the question concerns code structure, flow, dependencies, or blast radius, use at most one bounded codegraph_explore query before fallback reads. CodeGraph cannot prove the candidate diff by itself; exact source and supplied diff evidence remain authoritative. If the index is unavailable or stale, continue with read, grep, glob, and list. Inspect only files needed to assess the supplied diff scope. Do not use shell, Git, network, package installation, delegation, or any write-capable tool. Do not edit, format, generate, commit, or push. Treat the candidate as immutable.

In initial mode, perform one complete sweep through your assigned lens. In scoped-validation mode, inspect only the frozen severe-finding ledger and correction delta. Scoped validation may approve or escalate an unresolved severe finding, but it must not add unrelated findings or propose another correction cycle.

Report only concrete user-impacting defects supported by path:line evidence. BLOCKER and CRITICAL require concrete proof. Mark evidenceClass deterministic only for directly reproducible proof such as a failing test, violated invariant, or exact unsafe path; otherwise mark it inferential. WARNING and SUGGESTION are informational and never block.

Return exactly one compact JSON object and no Markdown:

{"mode":"initial|scoped-validation","lens":"risk|readability|reliability|resilience","candidateIdentity":"<sha256>","findings":[{"id":"<stable lens ID>","location":"path:line","severity":"BLOCKER|CRITICAL|WARNING|SUGGESTION","claim":"observable defect","evidenceClass":"deterministic|inferential","proofRefs":["concrete evidence"]}],"verdict":"clean|findings|approve|escalate","evidence":["what was inspected"]}
`
	reviewRiskPrompt = `---
description: Native read-only Risk reviewer for a frozen candidate
mode: subagent
hidden: true
permission:
  "*": deny
  read: allow
  grep: allow
  glob: allow
  list: allow
  skill: allow
  codegraph_explore: allow
  vgxness_memory_search: allow
  vgxness_memory_get: allow
  task: deny
---

<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-review-risk; version: 1 -->

You are the Risk lens for VGXNESS Native Manager. Inspect security boundaries, authorization, permissions, secrets, data exposure or loss, injection, unsafe process or shell use, dependency trust, and privilege escalation. Use stable finding IDs prefixed RISK-.
` + nativeReviewSharedContract
	reviewReadabilityPrompt = `---
description: Native read-only Readability reviewer for a frozen candidate
mode: subagent
hidden: true
permission:
  "*": deny
  read: allow
  grep: allow
  glob: allow
  list: allow
  skill: allow
  codegraph_explore: allow
  vgxness_memory_search: allow
  vgxness_memory_get: allow
  task: deny
---

<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-review-readability; version: 1 -->

You are the Readability lens for VGXNESS Native Manager. Inspect whether intention is clear, naming matches behavior, duplication or accidental complexity obscures contracts, dead code remains, and maintenance hazards can produce future defects. Do not report subjective style preferences without concrete maintenance impact. Use stable finding IDs prefixed READ-.
` + nativeReviewSharedContract
	reviewReliabilityPrompt = `---
description: Native read-only Reliability reviewer for a frozen candidate
mode: subagent
hidden: true
permission:
  "*": deny
  read: allow
  grep: allow
  glob: allow
  list: allow
  skill: allow
  codegraph_explore: allow
  vgxness_memory_search: allow
  vgxness_memory_get: allow
  task: deny
---

<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-review-reliability; version: 1 -->

You are the Reliability lens for VGXNESS Native Manager. Inspect behavioral contracts, correctness, regression coverage, edge cases, determinism, state transitions, concurrency, and outcomes that differ from the acceptance criteria. Use stable finding IDs prefixed REL-.
` + nativeReviewSharedContract
	reviewResiliencePrompt = `---
description: Native read-only Resilience reviewer for a frozen candidate
mode: subagent
hidden: true
permission:
  "*": deny
  read: allow
  grep: allow
  glob: allow
  list: allow
  skill: allow
  codegraph_explore: allow
  vgxness_memory_search: allow
  vgxness_memory_get: allow
  task: deny
---

<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-review-resilience; version: 1 -->

You are the Resilience lens for VGXNESS Native Manager. Inspect failure paths, partial completion, fallback, retry safety, cancellation, observability, rollback, recovery, load behavior, and operational degradation. Use stable finding IDs prefixed RES-.
` + nativeReviewSharedContract
	reviewRefuterPrompt = `---
description: Native read-only refuter for severe inferential review findings
mode: subagent
hidden: true
permission:
  "*": deny
  read: allow
  grep: allow
  glob: allow
  list: allow
  skill: allow
  codegraph_explore: allow
  vgxness_memory_search: allow
  vgxness_memory_get: allow
  task: deny
---

<!-- managed-by: vgxness; artifact: opencode-agent/vgxness-review-refuter; version: 1 -->

You are the severe-finding refuter for VGXNESS Native Manager. Accept only one parent mission containing the frozen candidate identity, exact changed paths, diff scope, acceptance criteria, verification evidence, and one batch of inferential BLOCKER or CRITICAL findings with their stable IDs and proof references.

Independently attempt to disprove each supplied claim against the frozen candidate. Inspect only evidence needed for those IDs. Never add a new finding, broaden scope, suggest a fix, or turn uncertainty into approval. A deterministic severe finding must not be sent to you.

Load every supplied native skill name through the skill tool. Use vgxness_memory_search and vgxness_memory_get only when prior project decisions are material to refuting a supplied finding; memory is context, never candidate proof. When .codegraph exists and structural evidence is material to a supplied finding, use at most one bounded codegraph_explore query; exact source and supplied diff evidence remain authoritative. If the index is unavailable or stale, continue with read, grep, glob, and list. Do not use shell, Git, network, package installation, delegation, or any write-capable tool. Do not edit, format, generate, commit, or push.

Return exactly one compact JSON object and no Markdown:

{"candidateIdentity":"<sha256>","results":[{"findingId":"<stable ID>","outcome":"corroborated|refuted|inconclusive","proofRefs":["concrete evidence"]}]}
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
	state, err := service.inspect(ctx, options)
	if err != nil {
		return integration.Result{}, err
	}
	state.result.Changed = state.result.State == integration.StateAbsent || state.result.State == integration.StatePartial
	return state.result, nil
}

func (service *Integration) Status(ctx context.Context, options integration.Options) (integration.Result, error) {
	state, err := service.inspect(ctx, options)
	return state.result, err
}

func (service *Integration) Install(ctx context.Context, options integration.Options) (integration.Result, error) {
	state, err := service.inspect(ctx, options)
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
	verified, err := service.inspect(ctx, options)
	if err != nil || verified.result.State != integration.StateInstalled {
		return integration.Result{}, fmt.Errorf("read back OpenCode integration artifacts: %w", integration.ErrDrift)
	}
	rollback = false
	verified.result.Changed = len(created) != 0
	return verified.result, nil
}

func (service *Integration) Uninstall(ctx context.Context, options integration.Options) (integration.Result, error) {
	state, err := service.inspect(ctx, options)
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
	state.result.Bridge = integration.BridgeNotRequired
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

func (service *Integration) inspect(ctx context.Context, options integration.Options) (inspection, error) {
	if err := ctx.Err(); err != nil {
		return inspection{}, err
	}
	configDirectory, err := integrationConfigDirectory(options)
	if err != nil {
		return inspection{}, err
	}
	managerPath := filepath.Join(configDirectory, "agents", managerAgentName)
	reviewRiskPath := filepath.Join(configDirectory, "agents", reviewRiskName)
	reviewReadabilityPath := filepath.Join(configDirectory, "agents", reviewReadabilityName)
	reviewReliabilityPath := filepath.Join(configDirectory, "agents", reviewReliabilityName)
	reviewResiliencePath := filepath.Join(configDirectory, "agents", reviewResilienceName)
	reviewRefuterPath := filepath.Join(configDirectory, "agents", reviewRefuterName)
	toolPath := filepath.Join(configDirectory, "plugins", memoryPluginName)
	toolContent, err := memoryPluginContent(service.executable)
	if err != nil {
		return inspection{}, err
	}
	result := integration.Result{
		Provider: "opencode", State: integration.StateAbsent, Path: managerPath, ArtifactSHA256: artifactSHA256([]byte(managerPrompt)),
		ToolPath: toolPath, ToolSHA256: artifactSHA256(toolContent), Bridge: integration.BridgeNotRequired,
	}
	exists, drifted, containerErr := inspectDirectory(configDirectory)
	if containerErr != nil {
		return inspection{}, fmt.Errorf("inspect OpenCode integration directory: %w", containerErr)
	}
	state := inspection{result: result, artifacts: []artifact{
		{path: managerPath, content: []byte(managerPrompt), backup: "vgxness-manager"},
		{path: reviewRiskPath, content: []byte(reviewRiskPrompt), backup: "vgxness-review-risk"},
		{path: reviewReadabilityPath, content: []byte(reviewReadabilityPrompt), backup: "vgxness-review-readability"},
		{path: reviewReliabilityPath, content: []byte(reviewReliabilityPrompt), backup: "vgxness-review-reliability"},
		{path: reviewResiliencePath, content: []byte(reviewResiliencePrompt), backup: "vgxness-review-resilience"},
		{path: reviewRefuterPath, content: []byte(reviewRefuterPrompt), backup: "vgxness-review-refuter"},
		{path: toolPath, content: toolContent, backup: "vgxness-memory-plugin"},
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

func memoryPluginContent(executable string) ([]byte, error) {
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
	quoted, err := json.Marshal(resolved)
	if err != nil {
		return nil, fmt.Errorf("%w: VGXNESS executable path", integration.ErrInvalid)
	}
	content := `import { spawn } from "node:child_process"
import { isAbsolute } from "node:path"
import { tool } from "@opencode-ai/plugin"

// managed-by: vgxness; artifact: opencode-plugin/vgxness-memory; version: 1
const VGXNESS_EXECUTABLE = ` + string(quoted) + `
const MAX_INPUT_BYTES = 64 * 1024
const MAX_OUTPUT_BYTES = ` + fmt.Sprintf("%d", maxMemoryOutputBytes) + `
const TIMEOUT_MS = 10_000

function safeQuery(value) {
  const terms = String(value ?? "").match(/[\p{L}\p{N}_]+/gu) ?? []
  return Array.from(new Set(terms.map((term) => term.toLowerCase()))).slice(0, 8).join(" ")
}

async function invokeMemory(operation, payload, context) {
  const workspace = String(context?.directory ?? "")
  if (!workspace || !isAbsolute(workspace)) throw new Error("VGXNESS memory workspace is unavailable")
  const input = JSON.stringify({ schemaVersion: 1, ...payload })
  if (Buffer.byteLength(input) > MAX_INPUT_BYTES) throw new Error("VGXNESS memory request exceeded its bound")

  return await new Promise((resolve, reject) => {
    const child = spawn(
      VGXNESS_EXECUTABLE,
      ["memory", operation, "--stdin", "--json", "--workspace", workspace],
      {
        cwd: workspace,
        shell: false,
        stdio: ["pipe", "pipe", "pipe"],
        env: {
          HOME: process.env.HOME,
          USERPROFILE: process.env.USERPROFILE,
          TMPDIR: process.env.TMPDIR,
          SystemRoot: process.env.SystemRoot,
        },
      },
    )
    let stdout = ""
    let stderrBytes = 0
    let settled = false
    const finish = (error, value) => {
      if (settled) return
      settled = true
      clearTimeout(timer)
      context?.abort?.removeEventListener?.("abort", abort)
      if (error) reject(error)
      else resolve(value)
    }
    const abort = () => {
      child.kill("SIGKILL")
      finish(new Error("VGXNESS memory request was cancelled"))
    }
    const timer = setTimeout(() => {
      child.kill("SIGKILL")
      finish(new Error("VGXNESS memory request timed out"))
    }, TIMEOUT_MS)
    context?.abort?.addEventListener?.("abort", abort, { once: true })
    child.stdout.setEncoding("utf8")
    child.stderr.setEncoding("utf8")
    child.stdout.on("data", (chunk) => {
      stdout += chunk
      if (Buffer.byteLength(stdout) > MAX_OUTPUT_BYTES) {
        child.kill("SIGKILL")
        finish(new Error("VGXNESS memory response exceeded its bound"))
      }
    })
    child.stderr.on("data", (chunk) => {
      stderrBytes += Buffer.byteLength(chunk)
      if (stderrBytes > MAX_OUTPUT_BYTES) {
        child.kill("SIGKILL")
        finish(new Error("VGXNESS memory failure exceeded its bound"))
      }
    })
    child.on("error", () => finish(new Error("VGXNESS memory process is unavailable")))
    child.on("close", (code) => {
      if (settled) return
      if (code !== 0) return finish(new Error("VGXNESS memory request failed"))
      try {
        const envelope = JSON.parse(stdout)
        if (envelope?.schemaVersion !== 1 || !("result" in envelope)) {
          return finish(new Error("VGXNESS memory response is invalid"))
        }
        finish(undefined, JSON.stringify(envelope.result))
      } catch {
        finish(new Error("VGXNESS memory response is invalid"))
      }
    })
    child.stdin.end(input)
  })
}

export const VGXNESSMemoryPlugin = async () => ({
  tool: {
    vgxness_memory_search: tool({
      description: "Search VGXNESS-owned durable project memory. Use for prior decisions, fixes, conventions, and discoveries; verify mutable claims against the workspace.",
      args: {
        query: tool.schema.string().describe("Plain-language search terms"),
        type: tool.schema.string().optional().describe("Optional memory type filter"),
        topic: tool.schema.string().optional().describe("Optional stable topic key filter"),
        limit: tool.schema.number().optional().describe("Maximum results from 1 to 10"),
      },
      async execute(args, context) {
        const query = safeQuery(args.query)
        if (!query) throw new Error("VGXNESS memory query has no searchable terms")
        const limit = Math.max(1, Math.min(10, Math.trunc(args.limit ?? 5)))
        return await invokeMemory("search", { query, type: args.type ?? "", topic: args.topic ?? "", limit }, context)
      },
    }),
    vgxness_memory_get: tool({
      description: "Read one full VGXNESS-owned project memory by exact ID after a relevant search result.",
      args: {
        id: tool.schema.string().describe("Exact memory observation ID"),
      },
      async execute(args, context) {
        return await invokeMemory("get", { id: args.id }, context)
      },
    }),
    vgxness_memory_save: tool({
      description: "Save a durable project decision, bug fix, discovery, convention, or configuration fact to VGXNESS-owned memory. Never store secrets, personal data, transient progress, or raw transcripts.",
      args: {
        title: tool.schema.string().describe("Short searchable title"),
        content: tool.schema.string().describe("Durable evidence-backed content"),
        type: tool.schema.string().optional().describe("Memory type such as decision, bugfix, discovery, pattern, architecture, or config"),
        topic: tool.schema.string().optional().describe("Stable topic key reused for an evolving subject"),
      },
      async execute(args, context) {
        return await invokeMemory("save", {
          title: args.title,
          content: args.content,
          type: args.type ?? "learning",
          topic: args.topic ?? "",
          session: context?.sessionID ?? "",
        }, context)
      },
    }),
    vgxness_memory_forget: tool({
      description: "Archive one exact VGXNESS-owned project memory so normal search no longer returns it. Use only after an explicit user request.",
      args: {
        id: tool.schema.string().describe("Exact memory observation ID"),
      },
      async execute(args, context) {
        return await invokeMemory("forget", { id: args.id }, context)
      },
    }),
  },
})
`
	return []byte(content), nil
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
