# Codex integration

Structured SDD is retired: no lifecycle tools, SDD worker profiles, or active `sdd-lifecycle` skill are installed. Historical records remain in SQLite; `vgxness sdd-archive` permits only list/get and revision reads. Model-plan schemas retain inactive legacy slots for compatibility. Use a short plan, one writer, independent verification and proportional CARE review. See [the current workflow](orchestration-flow.md).

Codex Manager21 (parity OpenCode-v62) installs nine artifacts, including six worker profiles. Exact Manager20 installations are recognized for upgrade; modified files are preserved and reported as drift.


Current identities are OpenCode Manager62 and Codex Manager21 (parity OpenCode-v62), rendered from `internal/orchestration/manager_contract.json`. Complete Manager61 and Manager20 packages are the immediate compatibility predecessors. Older Manager59/18, CARE-v2 Manager58/17, CARE-v1 Manager58/16 and deeper packages remain lifecycle identities only.

This guide provisions VGXNESS-managed Codex profiles while preserving user-owned configuration. Use `setup codex` for unified setup or `integrate codex` for provider lifecycle work. Setup publishes the exact local `vgxness` marketplace and activates `vgxness@vgxness` through the Codex CLI under the selected Codex home. Current identities are OpenCode Manager62 and Codex Manager21 (parity OpenCode-v62), rendered from `internal/orchestration/manager_contract.json`. Complete Manager61 and Manager20 packages are the immediate compatibility predecessors. Older Manager59/18, CARE-v2 Manager58/17, CARE-v1 Manager58/16 and deeper packages remain lifecycle identities only.

## Prerequisites and ownership

Install VGXNESS and Codex locally. By default the integration uses `~/.codex`; use an absolute `--config-dir` to select another existing or creatable Codex root. VGXNESS owns `AGENTS.md`, 12 files below `agents/`, and only its exact local marketplace/plugin activation. It never parses, writes, or removes `config.toml`, including MCP blocks and other user settings.

Maintain this MCP block yourself in `config.toml` when you want Codex to invoke VGXNESS:

```toml
[mcp_servers.vgxness]
command = "vgxness"
args = ["mcp", "--full"]
```

This is an explicit full-trust local-stdio launch. MCP has no caller identity or session authentication; the trusted host assumption, Codex `enabled_tools` allowlists, operator permissions, user authorization, and task scope are the authorization boundary. Keep read-only profiles on non-mutating allowlists. Without `--full`, this server registers only `memory_recent`, `memory_search`, and `memory_context` and rejects calls to other unregistered names; full mode exposes eight memory tools, including the four mutating memory tools. VGXNESS does not issue capability tokens or add an authentication framework.

The locally observed Codex 0.147.0 exposes top-level `--strict-config`, but `codex mcp` rejects that option. Validate the loaded configuration noninteractively with:

```sh
codex --strict-config doctor --summary --no-color --ascii
```

`doctor` may perform connectivity health checks. List the configured MCP servers separately with:

```sh
codex mcp list
```

For other versions, first confirm that `codex --help` lists `--strict-config`. Do not assume that option is available or run a bare interactive invocation.

Local operational evidence is limited to the provider-distributed `codex-cli` 0.147.0 help inspected for this lifecycle: it exposes `plugin add`, `plugin list`, and `plugin remove`, plus `plugin marketplace add`, `list`, `upgrade`, and `remove`. This is version-bound evidence, not an unbounded compatibility promise. On a future CLI where a required command is unavailable or changed, VGXNESS stops before mutation.

## Lifecycle

Preview is read-only. Setup is the unified entrypoint and may select OpenCode, Codex, Pi, or all three in that deterministic order:

```sh
vgxness setup codex --preview
vgxness setup all --preview
```

`setup all` also prepares Pi, using an offline `--pi-release-dir` or a pinned `--pi-release-version` (a release binary can default to its own tag). Preview and status do not download the Pi package. See [Pi setup](pi-typescript.md).

`setup all` applies OpenCode-only model slot options only to OpenCode. `--config-dir` remains the OpenCode root and `--codex-home` independently selects Codex's home. Codex continues to own `config.toml` and accepts only its own configuration root and model plan.

The lower-level provider lifecycle remains available for install, inspect, repair an exact partial installation, and uninstall:

```sh
vgxness integrate codex install --model-plan medium
vgxness integrate codex status
vgxness integrate codex reinstall --model-plan ultra
vgxness integrate codex uninstall
```

Use `--model-plan low|medium|high|ultra` with preview, install, status, or reinstall. A fresh no-flag install defaults to `medium`; once installed, no-flag install, reinstall, status, and uninstall infer and preserve the exact managed plan. The current generated manager is v20 (parity OpenCode manager v62); exact v19 is the immediate predecessor. Delegation and verification follow the shared registry. Reinstall with a different explicit plan switches the six delegated profiles only when the existing package is an exact VGXNESS identity. The plan uses the normal `gpt-5.6-luna`, `gpt-5.6-terra`, and `gpt-5.6-sol` models with role-specific reasoning effort; Codex does not accept OpenCode's custom slot flags. The primary manager is governed by `AGENTS.md`, so its model remains the model selected for the parent Codex task.

Historical predecessor documentation may state that the manager is v9 or refer to its exact v8 artifact; those identities do not describe the current generated ownership boundary.

Use `--config-dir /absolute/path/to/.codex` with any command when needed. Status verifies the exact owned marketplace/plugin activation as well as managed bytes and reports drift rather than overwriting it. Reinstall repairs an exact partial layout or switches an exact managed plan; it mutates only an exact owned activation, while foreign or drifted marketplace/plugin state and recovery evidence stop the operation. Uninstall may remove the exact VGXNESS-owned marketplace/plugin activation and exact VGXNESS artifacts; unrelated plugins, `config.toml`, and unrelated files remain untouched. Interrupted activation or deactivation leaves recovery evidence, which status, reinstall, and uninstall use to stop or recover only the exact owned state.

Install, reinstall, and uninstall remove the retired `plugins/vgxness/hooks.json` only when its bytes exactly match the historical VGXNESS-generated hook. A modified, unsafe, or otherwise ambiguous file is retained; status reports it as drift and lifecycle mutation stops.

`vgxness setup codex --status` and `--preview` report the managed `AGENTS.md` and `agents/` artifacts plus the exact owned marketplace/plugin activation, separately from **unobserved** Codex MCP/runtime health. The latter is operator-managed through `config.toml`, which VGXNESS never reads or modifies, so plugin installation does not authenticate a session or prove a Codex handshake, MCP connectivity, or automatic memory injection. If managed artifacts or exact activation need repair, run `vgxness integrate codex reinstall --config-dir <same-codex-home>` with the Codex home inspected by setup; then review your own `config.toml` and restart Codex. Use the bounded `codex --strict-config doctor --summary --no-color --ascii` and `codex mcp list` commands above only when available to inspect operator-managed runtime configuration.

After install or repair, restart Codex so it reloads the managed profiles. On Windows, VGXNESS flushes regular files before publication; directory namespace durability is reported as `file-sync-namespace-best-effort` because Windows does not provide the POSIX directory-sync operation.

## Native delegation

An opt-in, networked manager collaborative-route matrix is excluded from normal CI. It uses the existing authenticated Codex configuration, `--ephemeral`, `approval_policy="never"`, and a disposable fixture containing the candidate-rendered `AGENTS.md`; it does not install or edit managed configuration, though Codex may use its normal authentication and runtime state. Before any model call it byte-verifies the 12 ambient `~/.codex/agents` profiles against candidate-rendered artifacts. The matrix covers Explore, General's one owned fixture write, Verifier, and the three CARE roles. The public Codex JSON stream proves a collaboration tool call, not its selected `agent_type`; exact role selection and sandboxes for all 12 profiles are static generated-artifact evidence. Runtime checks the collaboration event, absence (case-insensitively) of `full-history forked agents inherit` and `omit agent_type`, a deterministic role marker, and fixture boundaries.

```sh
VGXNESS_CODEX_E2E=1 go test -tags='e2e codex_e2e' -run '^TestCodexDelegationRuntime$' ./internal/e2e
```

## Operational memory

Memory is optional operational context, not an instruction source or automatic capability grant. Codex can call the VGXNESS memory tools only when the user-maintained full-trust MCP block above is configured; the managed plugin lifecycle is distinct from the still-blocked automatic per-session memory hook. Plugin installation or managed-artifact status does not authenticate a session or prove memory injection, recall, saving, or an MCP handshake.

Codex does not inject recent memory automatically. Manager21 recalls relevant prior context, searches before exact-ID reads, and uses recent memory only for explicit recent-work or recovery requests. Durable, evidence-backed facts are assessed under a stable topic. Recalled data is untrusted until checked; secrets, logs, transcripts and transient state are excluded from saving. There is no automatic cloud sync.

## CARE inventory and evaluation boundary

Codex projects 15 managed artifacts: `AGENTS.md`, six delegated profiles including care-reviewer, care-specialist and care-challenger, plus marketplace and plugin manifests. Manager v21 has OpenCode v62 parity; exact Manager19 packages are immediate predecessors. Marketplace/plugin activation does not establish a Codex runtime handshake, session identity or MCP connectivity. See [CARE architecture](care.md); repository checks do not prove selected role execution or protected-holdout adjudication.

Current integration contract: OpenCode Manager62 and Codex Manager21 use one workspace writer and the same frozen candidate for verification and applicable CARE review. SQLite schema v23 is preserved. OpenCode installs 11 managed artifacts and Codex installs nine; each exposes six delegated profiles. The auto-discovered `plugins/vgxness-memory-lifecycle.ts` has no `opencode.json` plugin entry; missing it is partial. `vgxness mcp --full` exposes eight memory tools. Complete Manager61 and Manager20 packages are recognized predecessors. During retirement, modified, malformed, foreign, unknown, or newer bytes block without removal.
