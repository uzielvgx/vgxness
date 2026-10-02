# Claude-only VGXNESS

Research backing this plan: `docs/research/claude-code-for-vgxness.md`
(notes in `docs/research/claude-code-notes/`). Facts below cite Claude Code
v2.1.287 docs as of 2026-10-01.

## Goal

Make VGXNESS a Claude Code product only. Ship the agent ecosystem as a
Claude Code plugin and keep the Go CLI for what the plugin cannot do on its
own: durable project memory (SQLite/FTS5), the MCP server that exposes it,
the session handoff, cloud sync, and diagnostics. Remove every other provider
and all code that exists only to serve them.

## Scope

**In**

- Plugin `vgxness` under `plugins/vgxness/`, with the marketplace manifest
  at the repo root (`.claude-plugin/marketplace.json`).
- Go changes the plugin needs: workspace resolution, MCP server
  instructions and tool descriptions, a Claude Code hook adapter, a setup
  helper that prints the recommended permission rules.
- Keep: `cmd/vgxness` (memory, mcp, status, doctor, version, memory sync,
  claude-code, tui), `cmd/vgxness-syncd`, `internal/tui` (rebuilt as the
  console, decision 14), `internal/memory`, `internal/mcp`,
  `internal/app` (trimmed), `internal/app/runtime`, `internal/cli` (trimmed),
  `internal/config`, `internal/inspection`, `internal/secrets`,
  `internal/sync*`, `internal/buildinfo`, `internal/testutil`, `deploy/`.
- Remove: OpenCode, Codex and Pi providers; setup/integration/receipt/
  recovery/backup machinery; the current TUI screens (setup wizard, model
  editor, recovery) and model selection; self-install and
  launcher; the current release pipeline; portable skills catalog and skill
  registry; orchestration contract package and CARE evals; Python eval
  tooling; legacy-compatibility and retirement code; obsolete docs.

**Out**

- Changing the memory schema. The 23 SQL migrations stay byte-for-byte so
  existing `~/.vgxness/memory.db` files keep working.
- Changing the sync protocol or the syncd deployment.
- New memory features.
- Agent teams (experimental, interactive-only, no worktree isolation).

## Key decisions

1. **Plugin instead of a custom installer.** Claude Code owns install,
   update and uninstall of plugin content, so receipts, root transactions,
   drift detection, backups and recovery have no job left. Installation is
   `claude plugin marketplace add uzielvgx/vgxness` then
   `claude plugin install vgxness@vgxness`.
2. **Plugin lives in `plugins/vgxness/`, not the repo root.** A root plugin
   would copy the whole Go repo into every user's plugin cache.
3. **Manager policy is injected, not installed as the main-thread agent.**
   A `SessionStart` command hook (`startup|resume|clear|compact`) prints
   `additionalContext` with the policy (3–5k chars, factual tone, opens with
   a precedence clause: user instructions and CLAUDE.md win) plus the prior
   same-project handoff, both produced by `vgxness`. MCP server
   `instructions` (≤2,048 chars) are the backstop when hooks are disabled.
   The plugin `agent` setting is rejected as default because it replaces
   Claude Code's whole system prompt and a user's own `agent` overrides it;
   `agents/manager.md` ships only as opt-in via `claude --agent
   vgxness:manager`. No `force-for-plugin` output style.
4. **Workers ship as plugin agents.** Plugin agents ignore `permissionMode`,
   `hooks` and `mcpServers`, but `permissionMode` is already ignored under
   the default `auto` mode; read-only is enforced through the `tools`
   allowlist. Roles: `vgxness:explore` (read tools + memory read tools, no
   Bash, haiku), `vgxness:general` (the only writer, sonnet, no `Agent`),
   `vgxness:verifier` (read tools + Bash, sonnet, high effort, fresh
   context, frozen SHA), `vgxness:reviewer` (the three CARE roles merged,
   read-only, opus, high effort). No `memory:` field (it auto-grants
   Read/Write/Edit); memory goes through the `vgxness` MCP. No worktree for
   the verifier: worktrees branch from the default branch, not HEAD.
   Built-in `Explore` cannot be overridden by a plugin, so the policy asks
   for `vgxness:explore` explicitly.
5. **Model selection disappears.** Claude Code picks models; agents use
   aliases in frontmatter. `modelplan`, `modelcatalog` and `agentmodels` go
   away. The TUI package stays but loses every setup, model and recovery
   screen (decision 14). No role references fast mode.
6. **Memory DB stays in `~/.vgxness/`**, never in `${CLAUDE_PLUGIN_DATA}`
   (deleted on uninstall).
7. **Workspace identity** is resolved in this order: `--workspace` flag,
   `CLAUDE_PROJECT_DIR`, then cwd. `.mcp.json` passes
   `--workspace ${CLAUDE_PROJECT_DIR}`. MCP `roots` are not used (they
   change mid-session).
8. **Hooks adapter in Go, same binary.** `vgxness claude-code hook
   <session-start|pre-compact|session-end|subagent-start|pre-tool-use>`
   reads Claude's hook JSON on stdin and emits Claude's hook JSON. Fails
   closed (exit 0, empty stdout) when memory is unavailable. The current
   `memory hook --stdin` contract is not reused. Summaries stay model-driven
   through the MCP tool; hooks never read transcripts, and `PostCompact`'s
   `compact_summary` is never stored.
9. **`claude plugin eval` replaces the Python eval runner.** CI layering:
   `go test` → `claude plugin validate --strict` (marketplace and plugin
   separately) → `claude --bare -p --plugin-dir` smoke test asserting the
   `system/init` event (`plugins`, `plugin_errors`, `mcp_servers`) →
   `claude plugin eval` with pinned models and a cost ceiling.
10. **Clean legacy break.** No detection or retirement of old OpenCode,
    Codex, Pi or skill installations. Only DB migrations are preserved.
11. **Minimal release kept.** The plugin needs `vgxness` on PATH and a plugin
    cannot ship per-OS binaries, so a GoReleaser-style release (archives,
    checksums, Homebrew tap) replaces the current self-install pipeline.
12. **Delete in provider-sized slices.** Each slice leaves `make fast` green
    so a failure points at one removal.
13. **Minimum Claude Code version** documented as v2.1.284 (`sonnet` →
    Sonnet 5.5; hard floor v2.1.269 for `plugin eval`). CI pins the tested
    version (v2.1.287 today).
14. **The TUI stays as the Claude-only console.** `vgxness tui` is no longer
    an installer: it shows local state, guides the plugin setup (prints the
    `claude plugin` commands and the permission snippet, applies only on
    confirmation), browses memory (search, detail, forget with confirmation,
    session handoffs), manages sync and runs the doctor. Modules and every
    state are designed in the canvas (see `DESIGN.md`, decision D-001 in
    `docs/decisions.md`); UI copy is Spanish, identifiers and commands stay
    English. Code must match the artboards.

## Tasks

- [x] 1. Create branch `vgxness/claude-only`, run `make fast` and `make verify`
  to record a green baseline and current line counts.
  *Verify:* both targets pass on the untouched tree.
  *Result (2026-10-01):* 115,219 lines of Go/TS/Py under `internal`,
  `packages/pi/src` and `tools`. `go test -short` is green except
  `internal/e2e` (Pi package), `internal/providers/pi` and
  `internal/release`, which fail on environment only (an orphan worktree
  under `.claude/worktrees/` and a missing `proper-lockfile` fixture); all
  three packages are deleted in tasks 6 and 9. `make verify` was not run.
- [x] 2. Prototype plugin to answer the open questions before touching Go
  (hand-written `plugins/vgxness/` with `plugin.json`, `.mcp.json` pointing
  at the current `vgxness mcp --full`, one dummy agent, one `SessionStart`
  echo hook). Measure: whether the desktop app sees the shell PATH when
  starting the MCP command; exact `mcp__plugin_vgxness_<server>__<tool>`
  names; whether `PreToolUse`/`SubagentStart` input carries `agent_type`;
  whether plugin hooks wait for the workspace-trust dialog; what `/cd` does
  to a running stdio server; placement of `additionalContext` after
  `compact`. Record answers in `docs/research/claude-code-for-vgxness.md`
  and adjust decisions 3, 4, 7 and 8 if needed.
  *Verify:* answers recorded and agreed with Uziel before task 3.
  *Result (2026-10-02):* probe under `plugins/vgxness/` (to be replaced in
  task 5). Confirmed without a model turn: the desktop app launches plugin
  MCP commands with the user's shell PATH (`vgxness` resolved from
  `~/.local/bin`); tool names are
  `mcp__plugin_vgxness_memory__memory_<name>`; agents are
  `vgxness:<name>`; `${CLAUDE_PLUGIN_ROOT}` expands in `hooks.json`; hooks
  receive `CLAUDE_PLUGIN_ROOT`, `CLAUDE_PLUGIN_DATA` and
  `CLAUDE_PROJECT_DIR`; `SessionStart` input has `session_id`, `cwd`,
  `source` and no `agent_type`; `SessionEnd` fires with `reason`;
  `claude plugin validate --strict` passes; the MCP server answers
  `initialize`/`tools/list`/`memory_save`/`memory_search` over stdio but
  sends no `instructions`. Not measurable here (headless child sessions
  cannot refresh OAuth): `additionalContext` delivery, `agent_type` in
  `SubagentStart`/`PreToolUse`, `/cd` behavior. Decision taken: single
  writer is enforced by construction (only `vgxness:general` has
  `Edit`/`Write` tools), so no decision depends on `agent_type`; the
  `pre-tool-use` guard is optional hardening verified in task 16. Known
  gap: `vgxness:verifier` keeps `Bash` and can write through it; covered by
  its prompt until the guard is confirmed.
- [x] 3. Go: workspace resolution (`--workspace` on `mcp` and `claude-code`
  subcommands, then `CLAUDE_PROJECT_DIR`, then cwd); MCP server
  `instructions` (≤2,048 chars: when to search, when to save, what not to
  store); rewrite the eight tool descriptions to say *when* to use each
  (≤2,048 chars each); mark `memory_forget` with
  `_meta["anthropic/requiresUserInteraction"]`.
  *Verify:* unit tests; `claude mcp list`/`--debug` shows the instructions;
  tool search surfaces the tools from a natural prompt.
  *Result (2026-10-02):* `internal/cli/workspace.go`, `internal/mcp/instructions.go`
  (1,362 chars), descriptions rewritten, `memory_forget` carries
  `_meta["anthropic/requiresUserInteraction"]`; `.mcp.json` passes
  `--workspace ${CLAUDE_PROJECT_DIR}`. Verified over stdio from `cwd=/`
  with `CLAUDE_PROJECT_DIR` set: `initialize` returns the instructions and
  `memory_search` hits the right project. Tool-search discoverability from a
  natural prompt is deferred to task 16 (needs a model turn).
- [x] 4. Go: `vgxness claude-code hook` adapter (`session-start` injects
  policy + bounded handoff as untrusted data, ≤6k chars; `pre-compact`
  renews the session lease, never blocks; `session-end` finalizes well under
  1.5 s; `subagent-start` injects the single-writer rule) and
  `vgxness claude-code setup` that prints the recommended permission rules
  (allow read tools, ask for update/forget) since a plugin cannot ship them.
  *Verify:* table-driven tests with real Claude hook JSON fixtures; missing
  DB or binary paths exit 0 with empty stdout.
  *Result (2026-10-02):* `internal/cli/claudecode.go` (+ tests). Events:
  `session-start` (policy file via `--policy`, session handle, bounded
  handoff; ≤10,000 chars), `pre-compact` (lease renewal only),
  `session-end` (completes only when a draft summary exists, otherwise
  leaves the session to lease expiry), `subagent-start` (policy file only,
  no DB). No state is persisted between hook processes: the idempotent
  `StartProviderSession` reissues the lease token; a completed session id
  gets a `#n` successor on resume. `vgxness claude-code setup` prints the
  permission rules. `pre-tool-use` deliberately not implemented (decision in
  task 2). Verified with the real binary: start → `memory_session_summary`
  over MCP → end → next session receives the handoff; bad storage root
  exits 0 with an empty stdout.
- [x] 5. Plugin content: `.claude-plugin/marketplace.json` at the root,
  `plugins/vgxness/.claude-plugin/plugin.json`, `.mcp.json` (server key
  `memory`), `hooks/hooks.json`, `agents/{explore,general,verifier,
  reviewer,manager}.md`, policy text and the CARE rubric as a preloaded
  skill for the reviewer; port the Manager policy from
  `internal/orchestration/manager_contract.json` and the rendered OpenCode
  agents. Decide which of the 18 portable skills, if any, move into
  `plugins/vgxness/skills/` (`git-delivery` as
  `disable-model-invocation: true` is the candidate).
  *Verify:* `claude plugin validate --strict` on both manifests; install
  from the local marketplace; agents and the MCP tools are listed; a
  save/search round trip works; a second session receives the handoff.
  *Result (2026-10-02):* `plugins/vgxness/` holds `plugin.json` (0.1.0),
  `.mcp.json`, `hooks/hooks.json` (SessionStart, PreCompact, SessionEnd,
  SubagentStart `^vgxness:`), `policy/manager.md` (4,986 chars, factual
  tone, precedence clause first) and `policy/worker.md` (1,407 chars),
  `agents/{explore,general,verifier,reviewer}.md`, and
  `skills/git-delivery/SKILL.md` (`disable-model-invocation: true`; only
  this skill moves over). Content decisions: the CARE rubric is embedded in
  `reviewer.md` instead of a preloaded skill (plugin skill names in
  `skills:` are unverified and missing ones are skipped silently);
  `agents/manager.md` is not shipped, to avoid duplicating the policy text
  (the hook is the mechanism; `--agent` would also replace Claude Code's
  system prompt). Plans live at `docs/plans/<slug>.md`. Validation passes
  for both manifests; the real session-start context is 5,458 chars. Live
  install, save/search and handoff in interactive sessions are verified in
  task 16.
- *Tasks 6–10 (2026-10-02):* executed as one slice instead of per
  provider. The Pi, Codex and OpenCode wiring shared `internal/setup`,
  `internal/cli/setup.go`, `internal/cli/doctor.go`, `internal/app/app.go`
  and the TUI, so a per-provider cut meant editing files deleted two tasks
  later. Removed in one `git rm`: `internal/{providers,setup,tui,
  opencodebackup,integration,modelplan,modelcatalog,agentmodels,hooks,
  orchestration,selfinstall,launcher,release,skills,skillregistry,
  piartifact}`, `packages/pi`, `cmd/{vgxness-pi-backend,vgxness-release}`,
  `tools/`, root `package.json`/lock, the provider CLI files and their
  tests, Pi/CARE/Codex/self-install e2e tests. Rewritten: `app.go` (version,
  mcp, product runtime), `cli.go` (usage `version|status|doctor|memory|mcp|
  claude-code`, `failure` without provider cases), `runtime.go` (no hook
  emitter), `main.go` (no launcher forward). `go test -short ./...` is green
  on every remaining package.
  `internal/tui` was first deleted with the setup stack and then restored as
  the shell decision 14 asks for (`model.go`: options, size guard, brand
  header, placeholder; `run.go`; tests). The shell depends only on
  bubbletea, lipgloss and `x/ansi`; bubbles returns when task 17 needs
  inputs. `vgxness tui` is wired in `app.go` and errors cleanly outside a
  terminal.
- [x] 6. Remove Pi: `packages/pi`, root `package.json`, `package-lock.json`,
  `node_modules`, `cmd/vgxness-pi-backend`, `internal/providers/pi`,
  `internal/piartifact`, Pi code in `modelcatalog`, Pi compatibility tests in
  `internal/memory`, Pi e2e tests, `make pi-check`.
  *Verify:* `make fast`.
- [x] 7. Remove Codex: `internal/providers/codex`, Codex e2e, `make codex-e2e`,
  Codex wiring in `app`, `cli` and `setup`.
  *Verify:* `make fast`.
- [x] 8. Remove OpenCode and the setup stack: `internal/providers/opencode`,
  `opencodebackup`, `integration`, `setup`, `modelplan`, `modelcatalog`,
  `agentmodels`, `hooks` (its only listeners are the TUI and integration
  observers); CLI `setup`, `integrate` and model-selection commands; strip
  `internal/tui` to its shell (model, run, styles, TooSmall) so the package
  compiles until task 17 rebuilds it; simplify `internal/app` wiring.
  *Verify:* `make fast`; `vgxness` usage lists only the remaining commands.
- [x] 9. Remove self-install, old releases and skills: `selfinstall`,
  `launcher` (and `launcher.Forward` in `main.go`), `release`,
  `cmd/vgxness-release`, `tools/distribution`, the current
  `.github/workflows/release.yml`, `skills`, `skillregistry`, CLI `self`,
  `skills`, `registry`, and `memory hook --stdin`.
  *Verify:* `make fast`.
- [x] 10. Remove orchestration and evals: `internal/orchestration` (its policy
  now lives in the plugin), CARE e2e tests, `tools/agent_eval`,
  `make eval-check`.
  *Verify:* `make fast`.
- [x] 11. Simplify `status`/`doctor` to storage, database, schema health and
  plugin prerequisites (binary on PATH, Claude Code version); drop `--all`
  provider sections; update tests.
  *Verify:* `make fast`; `vgxness doctor` output reviewed manually.
  *Result (2026-10-02):* `doctor --all` and its provider sections went with
  the setup stack; `status`/`doctor` keep storage root, database and
  schema health only. The "plugin prerequisites" idea was dropped: the
  binary checking that it is on PATH proves nothing, and the Claude Code
  version is documented instead (decision 13).
- [x] 12. Sweep for remaining dead code: provider-specific bits in
  `internal/memory` (e.g. `provider_session.go`), unused exported symbols,
  error codes and flags. Migrations stay untouched.
  *Verify:* `make fast`; `go vet ./...`; codegraph shows no callers for
  removed symbols.
  *Result (2026-10-02):* removed `memory hook --stdin` (replaced by
  `claude-code hook`), `internal/testutil/codex.go`, and the unreachable
  `mcp.RunStdio`, `jsonBoolean`, `jsonNullableArray` and
  `Store.validateRetryClaim` found by `golang.org/x/tools/cmd/deadcode`.
  `provider_session.go` stays: it backs the Claude Code session handoff.
  Code reachable only from tests inside `syncapi`/`syncpg`/`syncd` was
  left alone (sync is out of scope).
- [x] 13. Rename the module path to `github.com/uzielvgx/vgxness` and
  `go mod tidy` to drop unused dependencies (bubbletea,
  bubbles, lipgloss and friends).
  *Verify:* `make verify`.
  *Result (2026-10-02):* renamed across `go.mod` and every Go file;
  `go mod tidy` dropped the charm/bubbletea tree (go.mod is 38 lines).
  `go build`, `go vet` (also with `-tags=e2e`) and `go test -short ./...`
  pass; `make verify` waits for task 14 because the Makefile and
  `foundation_test.go` still describe the old lanes.
- [ ] 14. Release and CI: minimal GoReleaser config (archives, `SHA256SUMS`,
  Homebrew tap update); trim `go-ci.yml` and the
  `Makefile` to the remaining lanes; add the plugin lane from decision 9
  (`plugin validate`, `--bare -p` smoke, `plugin eval` with `evals/`
  cases for handoff injection, explore read-only and single writer); keep
  the syncd deploy e2e tests; pin the Claude Code version.
  *Verify:* CI passes on the PR.
- [x] 15. Documentation: rewrite `README.md` for the Claude-only product
  (install binary, add marketplace, install plugin, permission snippet,
  sync, minimum Claude Code version, auto-memory coexistence and the
  opt-out snippet), delete docs for removed subsystems, keep and update
  `memory.md`, `sync.md`, `diagnostics.md`, add a `CHANGELOG.md` entry.
  All user documentation is written in Spanish (Uziel's choice); the
  changelog entry too.
  *Verify:* every remaining doc link resolves; no mention of OpenCode,
  Codex or Pi outside the changelog.
  *Result (2026-10-02):* Spanish `README.md`, new `docs/claude-code.md`
  (plugin layout, MCP, hooks, roles, permissions, CI, limits), rewritten
  `docs/memory.md`, `docs/diagnostics.md` and a full translation of
  `docs/sync.md`; `CHANGELOG.md` opens with a Spanish "Sin publicar" entry
  and keeps the English history below. Deleted 24 docs plus
  `docs/implementations/`, `docs/releases/` and four architecture notes;
  `docs/architecture/project-scoped-memory-sync.md` stays (sync design,
  English). Relative links verified by script; `go test -tags=e2e` passes
  with the README/docs contract tests. Left in English on purpose:
  `deploy/docker/README.md` and `deploy/ubuntu/README.md` (operator
  runbooks whose section headings are pinned by the deploy contract tests;
  translating them is a separate task if wanted), `docs/decisions.md`,
  `CLAUDE.md` and the plan/research files (project convention).
- [ ] 17. Rebuild `vgxness tui` as the console from the canvas (decision 14):
  Inicio (estado, avisos, primer uso, terminal pequeña), Setup del plugin
  (prerrequisitos, permisos, confirmar, aplicando, resultado, error), Memoria
  (buscar, vacío, primer uso, detalle, confirmar olvidar, handoffs), Sync
  (estado, iniciar sesión, en progreso, error) and Doctor (diagnóstico,
  crítico). Styles come from `DESIGN.md`; each screen references its
  artboard. Spanish copy per the `ui-copy-es` skill.
  *Verify:* table-driven view tests per screen and state; manual run at
  80×24 and 120×36; screens match the artboards.
  Split into five slices (decision D-004: only real data; each slice
  updates its artboards to the implemented copy):
  - [x] 17a. Shell: theme tokens from `DESIGN.md`, half-block banner,
    Header, Panel, KeyHelp (`bubbles/help`), ActionCards, Stepper, Confirm
    modal (compositor), TooSmall at 80×24, router, `Backend` interface;
    `internal/claudecli` (version, plugin and marketplace JSON, install
    commands behind a runner interface).
  - [x] 17b. Inicio (sano, avisos, primer uso, terminal pequeña) and Doctor
    (diagnóstico, crítico).
    *Result 17a+17b (2026-10-02):* `internal/tui` (theme, banner from the
    canvas bitmap, box/panel/cards/stepper/KeyHelp, compositor modals,
    router, `Backend`), `internal/claudecli`, `internal/app/console.go`
    (read-only backend), store reads `CountActive`, `SessionHandoffs`,
    `UserVersion`. Panels repaint their fill after every nested reset
    (lipgloss v2 resets the outer background). Verified: table-driven
    view tests per state at 120×35 and 80×24 (fit, copy, keys,
    navigation, clipboard, focus dimming); one frame rendered with the
    real backend on this machine (detected Claude Code 2.1.283 below the
    minimum, plugin absent, 376 memories, sync configured). Artboards
    updated: Inicio estado/avisos/primer uso, Diagnóstico normal/crítico.
    Memoria, Setup and Sync open a placeholder until their slices land.
  - [x] 17c. Memoria (buscar, sin resultados, primer uso, detalle, confirmar
    olvidar, handoffs).
    *Result (2026-10-02):* `internal/tui/memory.go` (debounced full-text
    search with `bubbles/textinput`, empty query lists recent, `[Tab]`
    cycles the project's real types, preview, detail with scroll and
    references, forget behind a compositor Confirm where only `[y]`
    confirms, handoffs list and detail); backend `SearchMemories`,
    `GetMemory`, `ForgetMemory` (the console's only write, through a
    separate write-capable runtime) and `Handoffs`; store `TypeCounts`.
    The search box owns the keyboard while focused (`q`, `h`, `r` are
    typed); `Esc` leaves it and the list keys apply. Inicio now treats a
    machine with memories but no plugin as complete (it was hiding
    Memoria behind the first-use screen; found rendering this machine's
    376 memories). Verified: view and interaction tests at 120×35 and
    80×24; frames rendered against the real database (search, preview,
    detail). Artboards updated: all six Memoria screens.
  - [ ] 17d. Setup del plugin (prerrequisitos, permisos, confirmar,
    aplicando, resultado, error).
  - [ ] 17e. Sync (estado, configurar, en progreso, error).
- [ ] 16. End-to-end check on a clean machine state: install the binary, add
  the marketplace, install the plugin, save and recall memory across two
  fresh Claude Code sessions, delegate to `vgxness:explore` and
  `vgxness:general`, run `vgxness doctor` and `vgxness tui`.
  *Verify:* observed manually and reported.

## Notes

- Design (2026-10-02): canvas https://claude.ai/artifact/55i278YA64Yz3tKGM9zgvX
  (pages Índice, Consola, Arquitectura; 22 console artboards + 1 diagram),
  design system https://claude.ai/artifact/5tnRiD2UwvqtFj58kyiKPf, tokens in
  `DESIGN.md`. Task 17 was added after the design session; tasks 16 and 17
  are listed in execution order (17 runs before 16).
- [ ] 19. Backlog from D-004, not scheduled (each needs its own plan if
  wanted): device pairing and a client-side device list for sync, a local
  periodic sync runner and sync history, memory tags, usage counters and
  related memories, an activity log, `vgxness memory restore`, a short
  `vgxness memory add` command, hook timing in `doctor`.
