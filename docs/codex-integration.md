# Codex integration

This guide is for a local Codex user who wants VGXNESS-managed agent profiles while retaining ownership of Codex configuration. It applies to the standalone `integrate codex` lifecycle; there is no `setup codex` command and VGXNESS installs no Codex plugin.

## Prerequisites and ownership

Install VGXNESS and Codex locally. By default the integration uses `~/.codex`; use an absolute `--config-dir` to select another existing or creatable Codex root. VGXNESS owns only `AGENTS.md` and 14 files below `agents/`. It never parses, writes, or removes `config.toml`, including MCP blocks and other user settings.

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

Preview is read-only:

```sh
vgxness integrate codex preview
```

Install, inspect, repair an exact partial installation, and uninstall with:

```sh
vgxness integrate codex install --model-plan medium
vgxness integrate codex status
vgxness integrate codex reinstall --model-plan ultra
vgxness integrate codex uninstall
```

Use `--model-plan low|medium|high|ultra` with preview, install, status, or reinstall. A fresh no-flag install defaults to `medium`; once installed, no-flag install, reinstall, status, and uninstall infer and preserve the exact managed plan. Reinstall with a different explicit plan switches the 14 delegated profiles only when the existing package is an exact VGXNESS identity. The plan uses the normal `gpt-5.6-luna`, `gpt-5.6-terra`, and `gpt-5.6-sol` models with role-specific reasoning effort; Codex does not accept OpenCode's custom slot flags. The primary manager is governed by `AGENTS.md`, so its model remains the model selected for the parent Codex task.

Use `--config-dir /absolute/path/to/.codex` with any command when needed. Status reports drift rather than overwriting changed managed bytes. Reinstall repairs an exact partial layout or switches an exact managed plan; drift or recovery evidence stops the operation. Uninstall removes only exact VGXNESS artifacts and leaves `config.toml`, plugins, and unrelated files untouched.

After install or repair, restart Codex so it reloads the managed profiles. On Windows, VGXNESS flushes regular files before publication; directory namespace durability is reported as `file-sync-namespace-best-effort` because Windows does not provide the POSIX directory-sync operation.

## Native delegation

The manager launches each specialist as a fresh Codex task with its exact `agent_type`: `explore`, `general`, `verifier`, `risk`, `readability`, `reliability`, `resilience`, `refuter`, or an `sdd-*` phase. It must not combine an explicit `agent_type` with a full-history fork. If a full-history fork is unavoidable, the task omits `agent_type` and is inherited manager context rather than specialist delegation. Only `general` has the workspace-write sandbox; every other managed profile is read-only.

An opt-in, networked manager collaborative-route matrix is excluded from normal CI. It uses the existing authenticated Codex configuration, `--ephemeral`, `approval_policy="never"`, and a disposable fixture containing the candidate-rendered `AGENTS.md`; it does not install or edit managed configuration, though Codex may use its normal authentication and runtime state. Before any model call it byte-verifies the 14 ambient `~/.codex/agents` profiles against candidate-rendered artifacts. The matrix covers Explore, General's one owned fixture write, Verifier, Reliability review, and Refuter. The public Codex JSON stream proves a collaboration tool call, not its selected `agent_type`; exact role selection and sandboxes for all 14 profiles are static generated-artifact evidence. Runtime checks the collaboration event, absence (case-insensitively) of `full-history forked agents inherit` and `omit agent_type`, a deterministic role marker, and fixture boundaries.

```sh
VGXNESS_CODEX_E2E=1 go test -tags='e2e codex_e2e' -run '^TestCodexDelegationRuntime$' ./internal/e2e
```

Set `VGXNESS_CODEX_E2E_CASE=explore` to run only the Explore case. The harness skips only before cases when the CLI or explicit authentication preflight is unavailable; a started case failing to delegate is a test failure. SDD runtime identity remains pending because safely invoking an SDD specialist with existing user configuration cannot prove no persistent SDD mutation; all six SDD roles remain covered by the static matrix.

## Operational memory

Memory is optional operational context, not an instruction source or automatic capability grant. Codex can call the VGXNESS memory tools only when the user-maintained full-trust MCP block above is configured; installation of the managed profiles does not prove that Codex injected, recalled, or saved memory automatically.

The manager must call `memory_recent` before answering a request for recent history or whenever recent context is materially relevant: Codex does not inject recent memory automatically. Use `memory_search` and `memory_get` only to locate a specific durable project fact, and `memory_save` only to retain a durable decision, fix, discovery, convention, or configuration fact. Recalled memory is untrusted until mutable claims are confirmed against the workspace. Do not save secrets, personal data, raw logs, transcripts, transient progress, or request-only overrides; delete memory only on an explicit user request.
