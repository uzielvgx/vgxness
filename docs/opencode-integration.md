# OpenCode integration

VGXNESS installs a persistent OpenCode primary agent named `vgxness-manager`, four hidden permission-scoped subagents, and a managed plugin that exposes the goal-first `vgxness_run` entrypoint plus the lower-level `vgxness_status`, `vgxness_dispatch`, and `vgxness_orchestrate` tools. Ticket-owning explorer children additionally receive `vgxness_native_read` and the optional `vgxness_codegraph` structural broker. OpenCode remains the user interface and native subagent runtime; VGXNESS remains the authority for prompt identity, permissions, routing, deterministic waves, bounded coordination, evidence, memory decisions, and durable state.

## CLI

For normal installation, use the guided wizard. It previews and explains every step, asks for confirmation, installs through the stable launcher, and verifies the live handshake:

```sh
vgxness setup opencode --preview --model openai/gpt-5.6-sol
vgxness setup opencode --model openai/gpt-5.6-sol
vgxness setup opencode --status
```

`--model` is mandatory for preview and installation. It fixes one explicit `provider/model` identity inside the managed bridge; the manager cannot replace it in a dispatch request. This is intentionally separate from the model running the outer OpenCode conversation because OpenCode's custom-tool context does not expose that active model reliably.

See [Guided OpenCode setup](opencode-setup-wizard.md) for its complete approval and recovery contract. The lower-level commands below remain available for diagnostics and controlled automation.

Install VGXNESS first, then invoke integration through the permanent launcher. This makes the generated OpenCode tool retain `~/.local/bin/vgxness` across application updates and rollback:

```sh
./vgxness self install
~/.local/bin/vgxness integrate opencode install --model openai/gpt-5.6-sol
```

The launcher is accepted only when its managed sidecar, launcher hash, active-version hash, and running active binary all agree. Running `integrate` directly from an unmanaged development binary remains supported for controlled tests, but embeds that exact binary path.

Adaptive orchestration normally runs entirely through `vgxness-manager`. If a Desktop process stops mid-plan, use the identities shown in the tool metadata or envelope to inspect and take over the durable schedule:

```sh
vgxness orchestrate status --workspace /absolute/project/path --id orchestration-...
vgxness orchestrate explain --workspace /absolute/project/path --id orchestration-...
vgxness orchestrate resume --workspace /absolute/project/path --id orchestration-... --owner owner-...
vgxness orchestrate cancel --workspace /absolute/project/path --id orchestration-... --owner owner-...
```

`resume` advances the authority epoch and returns a new owner identity; the prior owner cannot prepare or publish more work. It repairs accepted projections and never silently redispatches an uncertain task. A confirmed native child must finish or be cancelled before takeover, so recovery cannot duplicate an in-flight execution.

If OpenCode stops the tool between dependency waves, the managed plugin reads the durable schedule before cleanup. A `pending` schedule is returned unchanged and remains explicitly resumable instead of being converted into a cancellation or hidden behind a generic bridge error. If admitted work is still active, cleanup remains fail-closed and returns the durable cancellation envelope when it succeeds.

The plugin also preserves a valid structured VGXNESS error envelope when the bridge command intentionally exits nonzero. Capacity, policy, compatibility, and validation blockers therefore remain distinguishable from subprocess transport failures. The manager checks health at most once and does not automatically retry `vgxness_orchestrate`; overlapping a still-terminating schedule can consume the same workspace admission pool and obscure the original blocker.

Before the bridge validates a native `agent.result`, the plugin converts string-only entries in `errors` into the contract's explicit `{code, message, recoverable}` form. It also keeps real `artifact.reference` objects while moving inline strings or report objects incorrectly placed in `artifacts` into labeled `risks` evidence. No persistent artifact is invented, the original content is preserved, and every other contract violation remains fail-closed.

Preview the proposed global artifact without writing anything:

```sh
vgxness integrate opencode preview --model openai/gpt-5.6-sol
```

Install and read back all exact managed artifacts:

```sh
vgxness integrate opencode install --model openai/gpt-5.6-sol
```

Inspect its state or remove it recoverably:

```sh
vgxness integrate opencode status
vgxness integrate opencode uninstall
```

The default targets are:

- `~/.config/opencode/agents/vgxness-manager.md` — the persistent primary agent.
- `~/.config/opencode/agents/vgxness-navigator.md` — tool-denied candidate-task planning; it cannot inspect the workspace or choose execution policy.
- `~/.config/opencode/agents/vgxness-explorer.md` — read-only native inspection whose exact file contents and optional CodeGraph structural queries pass through ticket-authenticated VGXNESS brokers.
- `~/.config/opencode/agents/vgxness-implementer.md` — reserved read-only profile; native writes stay fail-closed until a ticket-authenticated edit broker exists.
- `~/.config/opencode/agents/vgxness-reviewer.md` — review of immutable Git evidence with no direct tool access.
- `~/.config/opencode/plugins/vgxness.ts` — the managed plugin whose manager tools become `vgxness_run`, `vgxness_status`, `vgxness_dispatch`, and `vgxness_orchestrate`; visible Task children receive `vgxness_task_claim` and `vgxness_task_complete`, while the explorer also receives the ticket-bound `vgxness_native_read` and `vgxness_codegraph` brokers.

## Manager personality and language boundary

`vgxness-manager` is designed to feel like a thoughtful technical partner rather than a command router. It brings the pattern recognition, pragmatism, and resistance to overengineering expected of a senior engineer with more than two decades of experience. It follows the language and register of the direct conversation, recommends one sensible next step, explains its reasoning briefly, and translates bounded receipts into a clear outcome, supporting evidence, meaningful limitations, and what should happen next. It stays candid when the available evidence is incomplete, challenges unnecessary complexity respectfully, and avoids canned praise or artificial enthusiasm.

The manager optimizes for the user's outcome and elapsed time rather than for visible orchestration activity. Conversation, conceptual questions, and explanations grounded in evidence already present remain direct. Actionable workspace goals normally enter through `vgxness_run`, which selects the smallest sufficient visible plan: one task, parallel independent tasks, or dependent waves. `fast` permits exactly one smallest-sufficient task and essential validation; `auto` is the default and permits up to four tasks with proportional verification; `deep` permits a bounded exhaustive plan of up to sixteen tasks. User-supplied `constraints` are bounded planning constraints and are folded into the durable acceptance criteria of every proposed task; they never grant a capability or bypass Gatekeeper. The lower-level dispatch and orchestration tools remain available for explicit control and compatibility. Health checks, repeated inspection, and extra synthesis are not routine ceremony; work stops when its acceptance criteria are met.

That personality belongs to the conversation layer. Code, generated documentation, commit-style text, and other technical artifacts remain neutral and use English by default unless the user requests another language or the project already establishes a different policy. This keeps the manager approachable without leaking a conversational voice into durable project material.

The personality and adaptive routing do not expand authority. New claims about workspace state and every action that inspects or changes external state must still pass through the exact `vgxness_*` surface, and the returned receipt remains the limit of what the manager may claim. Memory and dependency evidence are reused before gathering more context, but mutable or consequential facts are revalidated. The design borrows general interaction principles from effective orchestrators, but its wording and operating contract are original to VGXNESS; no third-party prompt text or SDD lifecycle is imported.

## CLI and Desktop runtime compatibility

OpenCode CLI and OpenCode Desktop load the same global artifacts, but they do not necessarily execute plugins with the same JavaScript runtime. The CLI can run them with Bun, while the Desktop sidecar runs them with Node. The generated plugin therefore uses the Node-compatible `node:child_process` API, which Bun also implements, and never depends on the global `Bun` object.

Both surfaces launch only the lightweight VGXNESS bridge with an executable plus an argument vector and `shell: false`. Standard input, standard output, standard error, cancellation, exit status, terminal-call timeouts, and the 6,356,992-byte per-stream encoded-response bound remain enforced identically. The bound covers worst-case JSON expansion of an accepted two-megabyte native result. The bridge does not execute a model. For adaptive orchestration, the manager emits the exact built-in Task arguments approved by VGXNESS; OpenCode creates each native child session and renders its Task row. A normal one-shot dispatch now creates a one-task durable plan without Navigator and returns the same built-in Task contract, so that child is visible and navigable too. Only explicit legacy continuity still creates a direct host-SDK child. In both paths the child uses the host's MCP connections and the selected managed subagent's permissions. Repository tests execute the generated plugin under both Node and Bun when those runtimes are available.

The former nested `opencode run --pure` provider process and transient runtime-agent configuration are retired from the managed path. They remain temporarily in the binary only as an internal legacy implementation boundary during migration; current managed artifacts cannot call them and have no silent fallback to them.

After installing or updating the bridge, fully quit and reopen OpenCode Desktop and start a new session before testing `vgxness-manager`. Existing Desktop sessions may retain the agent and tool snapshot loaded when their local server started.

### Updating an earlier managed bridge

Managed artifacts are content-bound and are never overwritten merely because they carry a VGXNESS marker. When upgrading any exact earlier managed projection—including a manager personality revision, the Bun-only bridge, or the portable bridge that did not bind an execution model—remove it recoverably with the still-active previous launcher before activating the new candidate:

```sh
~/.local/bin/vgxness integrate opencode uninstall
./new-vgxness-candidate setup opencode --model openai/gpt-5.6-sol
```

The uninstall creates exact backups under `~/.config/opencode/.vgxness-backups/`; the wizard then installs and verifies the portable projection. Do not delete or edit the generated tool manually to force an update, because modified content is intentionally treated as drift.

Tests and controlled installations can select an absolute root with `--config-dir PATH`. `bridge=configured` means both managed projections are present and exact; a live provider handshake is available separately:

```sh
vgxness bridge status --workspace /absolute/project/path
```

## Safety behavior

- Preview is non-mutating.
- Install is explicit, idempotent, transactional across the manager, subagent profiles, and plugin, writes with restrictive permissions, and verifies exact content after writing.
- Existing foreign or modified content is never overwritten.
- Symlinked artifacts or OpenCode config directories are treated as drift.
- Uninstall accepts only exact managed artifacts and preserves each one in `.vgxness-backups/` instead of deleting it permanently.
- The manager permits only user questions, managed VGXNESS tools, and the built-in Task tool restricted to `vgxness-explorer` and `vgxness-reviewer`. The child-only claim, completion, read, and structural-intelligence tools remain denied to it.
- `vgxness_run action=start` accepts a goal plus optional constraints, desired outcome, acceptance criteria, and `fast|auto|deep` mode. It reuses the same Navigator, validated plan, native Task directives, tickets, waves, and durable join as the lower-level orchestration surface; it does not create a second execution authority.
- `vgxness_dispatch action=start` validates one caller-supplied bounded task without launching Navigator, persists it as a one-task schedule, and returns one exact native Task `arguments` object. After that visible Task terminates, `action=join` returns the durable result. OpenCode therefore displays and links the child session instead of showing only a custom-tool row.
- `vgxness_orchestrate action=start` returns VGXNESS task metadata separately from the exact native Task `arguments` object for one approved wave. The manager must pass only those arguments unchanged and emit every call together when the wave is parallel. OpenCode itself creates the native child sessions and renders their individual live Task rows in the parent conversation.
- Each Task child must call `vgxness_task_claim`. The plugin verifies its managed agent, native `parentID`, orchestration owner, task identity, ready wave, and one-session-per-task binding. Parallel claims meet at a bounded barrier; only after every expected child has claimed does VGXNESS atomically prepare the wave and release each exact content-bound prompt. Explorer `glob` and `list` calls are hard-blocked by the plugin until that session holds an active ticket.
- Each native Task arguments object carries a fresh per-task claim capability. Only the matching task, child session, and capability may replay that one prepared prompt and binding. This repairs a lost bridge response or reloaded plugin claim without treating the publicly reported orchestration owner as a secret or exposing prepared prompts through ordinary status.
- Each child publishes its compact result through `vgxness_task_complete`. VGXNESS accepts the ticket and terminal before the manager can advance to another wave. An idle or failed child that never completes is terminalized by the plugin event hook. Every subagent retains `task: deny`, so children cannot recursively delegate.
- The configured execution model is embedded in the managed tool, validated as one exact `provider/model` value, and injected outside the LLM-controlled argument schema.
- The plugin launches the trusted permanent VGXNESS launcher only for bounded status, native-ticket, structural-intelligence, and orchestration lifecycle bridge calls. The legacy nested-process `dispatch` route is not callable. Workspace and parent-session identity come only from OpenCode's tool context; request and output sizes are bounded. Child-session identity and message identity are content-bound into completion evidence.

## Ticket-bound CodeGraph intelligence

Navigator selects `analyze-structure` for architecture, symbol, call-path, dependency, blast-radius, and affected-test questions. VGXNESS then assigns a visible native `vgxness-explorer` Task. Only that exact child session can call `vgxness_codegraph`, and only while its prepared ticket and deadline remain valid.

The broker invokes an already installed local CodeGraph CLI directly, never through a shell, MCP pass-through, provider adapter, or nested OpenCode worker. Its v1 allowlist contains only:

- `status`
- `explore`
- `impact`
- `affected`

Every call is bound to the ticket workspace, limited to 30 seconds and 512 KiB of output, and recorded as input/output digests plus timestamps in the durable ticket. A ticket permits at most sixteen structural queries. Lifecycle and administrative commands—including install, upgrade, index, uninit, and uninstall—are not exposed. `impact` and `affected` depth is capped at five; `explore` is capped at twelve source files; oversized numeric hints are clamped to those safe limits instead of failing the whole inspection. Affected paths must stay local and pass the sensitive-path policy. The managed plugin serializes CodeGraph queries, native reads, and completion on one per-ticket lane; the control plane also waits briefly for cross-process document locks, so valid concurrent tool emission becomes deterministic sequencing instead of a misleading policy denial.

Each Git worktree needs its own `.codegraph/` index because index roots and checked-out bytes are identity-bound. The broker does not create, copy, symlink, install, or repair an index. If the executable or the worktree-local index is unavailable, VGXNESS returns a structured unavailable result and the explorer falls back to bounded repository discovery plus `vgxness_native_read`. Exact file contents always use the read broker even when CodeGraph supplies the structural map.

## Bounded dispatch

The first protocol version exposes only:

- `read-files`
- `analyze-structure`
- `write-files` — currently denied in the native path until ticket-authenticated edits are available
- `review-changes`

For an isolated operation, the manager calls `vgxness_dispatch action=start`. VGXNESS creates exactly one validated task and returns `delegation-required` with the exact built-in Task arguments. The manager emits that Task unchanged, waits for its terminal state, and calls `vgxness_dispatch action=join` with the returned `orchestrationId`. This deliberately skips adaptive Navigator decomposition while retaining the visible OpenCode child row, ticket claim, durable terminal, and content-bounded join.

### Persistent run continuity

A dispatch remains isolated by default. The manager routes new multi-phase work through `vgxness_orchestrate` so every phase remains visible. The older direct-session continuity contract remains available to compatible callers without broadening the operation schema:

<!-- schema: bridge.schema.json#/$defs/dispatchInput -->
```json
{"operation":"read-files","goal":"inspect the bounded design","continuity":"start"}
```

The successful response includes an exact `runId`, `capsuleId`, `stateVersion`, and bounded `memoryRefs`. A later phase continues only that active run:

<!-- schema: bridge.schema.json#/$defs/dispatchInput -->
```json
{"operation":"read-files","goal":"inspect the next bounded concern","continuity":"continue","runId":"run-..."}
```

The final bounded phase uses `"continuity":"finish"` with the same `runId`. On success VGXNESS appends the terminal event, writes the stable terminal snapshot, and atomically removes `current-run.json`, allowing a new run to start. A failed final phase remains active and blocked so it can be inspected and continued rather than being mislabeled as complete.

`start` atomically publishes an active Chronicle snapshot. Every successful or blocked phase then writes an owned semantic-memory summary, an event-backed continuity capsule, and a new immutable snapshot before returning. `continue` recovers and validates the current pointer, run snapshot, event log, previous capsule, and memory store before provider execution. A compare-and-swap check on the recovered task and capsule prevents a stale process from replacing a run that another process already advanced.

Only `paused` or `blocked` runs may advance. If a process stopped while the current phase was still `running` or `recovering`, continuation fails closed instead of guessing whether provider side effects occurred; an explicit recovery control remains a later layer.

Every native task receives only the prepared prompt and up to three lexically relevant, fully hydrated project-memory records within a fixed context budget; continuity tasks may additionally receive their prior validated capsule. Retrieval uses bounded any-term matching so one irrelevant goal word cannot suppress every useful observation. The child never receives the full manager conversation, raw run history, or unrestricted memory. After a task reaches a terminal result, VGXNESS writes one idempotent semantic summary linked to the observations it used.

An agent result may also propose up to eight `memoryCandidates` containing a durable type, title, content, stable topic key, reason, and confidence. The agent cannot write memory directly. VGXNESS accepts only project-scoped candidates from the fixed durable taxonomy at confidence `>= 0.8`, rejects malformed or sensitive-looking content, namespaces topics under `agent/<type>/...`, and links every accepted observation to the durable task-result observation. A changed value for an existing candidate topic is saved as `needs_review`, not active truth. Invalid or low-confidence proposals are rejected without invalidating the completed task. Memory IDs returned in `memoryRefs` identify the injected records, task summary, and any accepted or review-pending candidates.

Continuity is intentionally foreground-only and non-delegating. Only one bounded phase advances a continuity run at a time. For adaptive work, a hidden tool-denied Navigator proposes at most sixteen tasks; VGXNESS validates identities, operations, dependencies, review ordering, and a maximum parallelism of four before persisting the plan. Independent `read-files` and `analyze-structure` tasks are returned as one parallel set of native OpenCode Task calls; dependent waves are not exposed until every prerequisite terminal is accepted and receive only a bounded JSON evidence projection. A final synthesis of read-only repository evidence is itself a linked `read-files` task, so a clean-tree audit closes inside the same orchestration. `review-changes` is reserved for explicit review of current, staged, or uncommitted Git changes. The manager advances the orchestration after each visible wave and returns the completed durable join instead of launching a second compensating dispatch. Durable owner/epoch authority fences stale managers and supports `vgxness orchestrate status|resume|cancel|explain`. Parallel writes, parallel continuity, automatic semantic replanning after a failed task, and full SDD artifact supervision remain deferred. A blocked continuity run still resumes through another validated `continue`; omitting `continuity` preserves one-shot behavior.

Every dispatch passes through the exact Registry prompt and agent identity, balanced Gatekeeper policy, the provider-neutral Runner, bounded Coordinator, and Chronicle evidence. The plugin first creates a native child session and a collision-resistant ticket identity that it retains even if the preparation response is lost. Then `prepare` publishes the recovery ticket before recording the Chronicle task start and upgrades it to a prepared one-shot ticket bound to that child identity. `complete` accepts only a matching ticket, parent session, child session, message, agent result, prompt identity, and exact result digest before writing terminal evidence, memory, or a capsule. Duplicate terminal calls reconcile only their owned lease slot; expired unreturned tickets are terminalized independently without disturbing active siblings. `review-changes` still gives the reviewer only serialized Git evidence, so untracked contents remain explicitly unreviewed.

If the host stops after publishing a `preparing` ticket but before Chronicle records `task.started`, cleanup deliberately writes no synthetic terminal task event. The ticket response is `failed`, while a continuity run is published as `blocked` with that never-started task still `pending`; its capsule is also `blocked`. When this recovers a lost `start`, the triggering tool call returns `status: "recovered"` together with the recovered `runId`, capsule, and memory references instead of creating another child task. The user can inspect that evidence and resume explicitly with `continuity: "continue"` and the returned `runId`.

Shell commands, install/package, commit, push, release, network, permission expansion, secrets, configuration, and destructive-file operations are not part of the v1 tool schema. General command execution remains deferred until it can be constrained by enforceable exact-command policy rather than prompt instructions.

The native child may emit intermediate text while it works. The plugin accepts only the final child response and requires its combined text to be one pure JSON object with no Markdown fence, prose, or trailing value. Common incomplete artifact-shaped evidence is moved into the result's bounded risk notes instead of being presented as a durable artifact reference. A bridge response with `ok: true` and `status: failed` is terminalized as a failed task rather than mistaken for successful completion. The normal execution deadline is ten minutes; cancellation aborts the child session and records a failed or cancelled ticket through the bridge.

Internally, the plugin first sends one bounded preparation request:

```sh
printf '%s' '{"protocolVersion":"1","model":"openai/gpt-5.6-sol","operation":"read-files","goal":"inspect the bounded workspace","parentSessionId":"ses_...","parentMessageId":"msg_...","childSessionId":"ses_child_..."}' \
  | vgxness bridge prepare --workspace /absolute/project/path --stdin
```

After the native child returns, the plugin sends its structured result and exact OpenCode session evidence to `vgxness bridge complete`. VGXNESS returns one normalized JSON envelope containing the accepted result and bounded receipt. Provider stderr and internal error details are not exposed. `bridge=configured` reports artifact state; `vgxness_status` performs the live OpenCode version and compatibility handshake.
