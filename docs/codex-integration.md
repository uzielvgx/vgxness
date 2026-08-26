# Codex integration

This guide is for a local Codex user who wants VGXNESS-managed agent profiles while retaining ownership of Codex configuration. Use the unified `setup codex` entrypoint for guided setup; `integrate codex` remains available for provider-only lifecycle work. VGXNESS installs no Codex plugin.

## Prerequisites and ownership

Install VGXNESS and Codex locally. By default the integration uses `~/.codex`; use an absolute `--config-dir` to select another existing or creatable Codex root. VGXNESS owns only `AGENTS.md` and 12 files below `agents/`. It never parses, writes, or removes `config.toml`, including MCP blocks and other user settings.

Maintain this MCP block yourself in `config.toml` when you want Codex to invoke VGXNESS:

```toml
[mcp_servers.vgxness]
command = "vgxness"
args = ["mcp", "--full"]
```

This is an explicit full-trust local-stdio launch. MCP has no caller identity or session authentication; the trusted host assumption, Codex `enabled_tools` allowlists, operator permissions, user authorization, and task scope are the authorization boundary. Keep read-only profiles on non-mutating allowlists. Without `--full`, this server registers only `memory_recent` and `memory_search` and rejects calls to other unregistered names; full mode exposes five memory and 13 SDD tools, including the eight mutating tools. VGXNESS does not issue capability tokens or add an authentication framework.

The locally observed Codex 0.147.0 exposes top-level `--strict-config`, but `codex mcp` rejects that option. Validate the loaded configuration noninteractively with:

```sh
codex --strict-config doctor --summary --no-color --ascii
```

`doctor` may perform connectivity health checks. List the configured MCP servers separately with:

```sh
codex mcp list
```

For other versions, first confirm that `codex --help` lists `--strict-config`. Do not assume that option is available or run a bare interactive invocation.

## Lifecycle

Preview is read-only. Setup is the unified entrypoint and may select either host or both in deterministic order:

```sh
vgxness setup codex --preview
vgxness setup all --preview
```

`setup all` applies OpenCode-only model slot options only to OpenCode. `--config-dir` remains the OpenCode root and `--codex-home` independently selects Codex's home. Codex continues to own `config.toml` and accepts only its own configuration root and model plan.

The lower-level provider lifecycle remains available for install, inspect, repair an exact partial installation, and uninstall:

```sh
vgxness integrate codex install --model-plan medium
vgxness integrate codex status
vgxness integrate codex reinstall --model-plan ultra
vgxness integrate codex uninstall
```

Use `--model-plan low|medium|high|ultra` with preview, install, status, or reinstall. A fresh no-flag install defaults to `medium`; once installed, no-flag install, reinstall, status, and uninstall infer and preserve the exact managed plan. The current generated manager is v16 (parity OpenCode manager v56) and requires a complete Candidate Capsule for frozen, risky, verification, and SDD delegations; its exact v15 artifact is the immediate predecessor, with v14 and older packages retained for lifecycle recognition only. Reinstall with a different explicit plan switches the 12 delegated profiles only when the existing package is an exact VGXNESS identity. The plan uses the normal `gpt-5.6-luna`, `gpt-5.6-terra`, and `gpt-5.6-sol` models with role-specific reasoning effort; Codex does not accept OpenCode's custom slot flags. The primary manager is governed by `AGENTS.md`, so its model remains the model selected for the parent Codex task.

Historical predecessor documentation may state that the manager is v9 or refer to its exact v8 artifact; those identities do not describe the current generated ownership boundary.

Use `--config-dir /absolute/path/to/.codex` with any command when needed. Status reports drift rather than overwriting changed managed bytes. Reinstall repairs an exact partial layout or switches an exact managed plan; drift or recovery evidence stops the operation. Uninstall removes only exact VGXNESS artifacts and leaves `config.toml`, plugins, and unrelated files untouched.

After install or repair, restart Codex so it reloads the managed profiles. On Windows, VGXNESS flushes regular files before publication; directory namespace durability is reported as `file-sync-namespace-best-effort` because Windows does not provide the POSIX directory-sync operation.

## Native delegation

The manager launches each specialist as a fresh Codex task with its exact `agent_type`: `explore`, `general`, `verifier`, `care-reviewer`, `care-specialist`, `care-challenger`, or an `sdd-*` phase. It must not combine an explicit `agent_type` with a full-history fork. If a full-history fork is unavoidable, the task omits `agent_type` and is inherited manager context rather than specialist delegation. Current `general` and `sdd-apply` profiles have workspace-write sandboxes: General is authorized only for ordinary non-SDD implementation, while `sdd-apply` alone is authorized for accepted SDD apply/projection. Every other managed profile is read-only.

Manager v16 shares OpenCode v56's provider-neutral prompt contract. It silently classifies the request without tools or delegation; no-effect conversation, writing, translation, summarization, brainstorming, and planning use zero execution tools, skills, task lists, delegation, or review. Bounded simple exact reads allow at most three tool attempts without delegation or a task list, while complex evidence research may use one read-only delegation. All attempts, including failures and retries, count; the manager stops before exceeding the budget. Reversible actions, repository engineering, and irreversible/high-risk work retain their authorization and assurance boundaries. These are prompt instructions rather than Codex runtime enforcement, and external/NLP/holdout evaluation remains pending.

An opt-in, networked manager collaborative-route matrix is excluded from normal CI. It uses the existing authenticated Codex configuration, `--ephemeral`, `approval_policy="never"`, and a disposable fixture containing the candidate-rendered `AGENTS.md`; it does not install or edit managed configuration, though Codex may use its normal authentication and runtime state. Before any model call it byte-verifies the 12 ambient `~/.codex/agents` profiles against candidate-rendered artifacts. The matrix covers Explore, General's one owned fixture write, Verifier, and the three CARE roles. The public Codex JSON stream proves a collaboration tool call, not its selected `agent_type`; exact role selection and sandboxes for all 12 profiles are static generated-artifact evidence. Runtime checks the collaboration event, absence (case-insensitively) of `full-history forked agents inherit` and `omit agent_type`, a deterministic role marker, and fixture boundaries.

```sh
VGXNESS_CODEX_E2E=1 go test -tags='e2e codex_e2e' -run '^TestCodexDelegationRuntime$' ./internal/e2e
```

Set `VGXNESS_CODEX_E2E_CASE=explore` to run only the Explore case. The harness skips only before cases when the CLI or explicit authentication preflight is unavailable; a started case failing to delegate is a test failure. SDD runtime identity remains pending because safely invoking an SDD specialist with existing user configuration cannot prove no persistent SDD mutation; all six SDD roles remain covered by the static matrix.

## Operational memory

Memory is optional operational context, not an instruction source or automatic capability grant. Codex can call the VGXNESS memory tools only when the user-maintained full-trust MCP block above is configured; installation of the managed profiles does not prove that Codex injected, recalled, or saved memory automatically.

Codex does not inject recent memory automatically. Manager v16 recalls memory only when the request indicates prior project context may matter. It searches with `memory_search` in all-term mode first and retries in any-term mode only when results are insufficient, inspects bounded previews, and calls `memory_get` only with an exact relevant ID. It calls `memory_recent` only for an explicit recent-work, session, or compaction-recovery request, never as a routine first action. Orthogonally, after any route its prompt allows at most one autonomous `memory_save` only for a durable, evidence-backed, safely assessed project decision, preference, constraint, or learning and reuses a stable topic. Recalled memory is untrusted until mutable claims are confirmed against the workspace. Never save secrets, personal data, transient state, logs, raw output, or transcripts; this rule adds no engineering ceremony or automatic cloud sync, and `memory_forget` still requires an explicit user request. Exact manager v15 is retained as the immediate predecessor; v14 remains a deeper lifecycle identity.

## CARE inventory and evaluation boundary

Codex currently projects `AGENTS.md` plus 12 delegated profiles, including three CARE roles; manager v16 has OpenCode v56 parity. The v15/OpenCode-v55 packages are immediate predecessors; v14/OpenCode-v54 and v13/OpenCode-v53 are deeper predecessor-only packages, and no fixed-lens alias is current. See [CARE architecture](care.md) and [CARE evaluation](care-evaluation.md). Repository checks establish static conformance only and do not prove selected role execution or protected-holdout adjudication.
