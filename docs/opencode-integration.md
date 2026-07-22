# OpenCode integration

VGXNESS installs a persistent OpenCode primary agent named `vgxness-manager`, four hidden permission-scoped subagents, and a managed plugin that exposes `vgxness_status`, `vgxness_dispatch`, and `vgxness_orchestrate`. OpenCode remains the user interface and native subagent runtime; VGXNESS remains the authority for prompt identity, permissions, routing, deterministic waves, bounded coordination, evidence, and durable state.

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
- `~/.config/opencode/agents/vgxness-explorer.md` — read-only native inspection whose file contents pass through the ticket-authenticated VGXNESS read broker.
- `~/.config/opencode/agents/vgxness-implementer.md` — reserved read-only profile; native writes stay fail-closed until a ticket-authenticated edit broker exists.
- `~/.config/opencode/agents/vgxness-reviewer.md` — review of immutable Git evidence with no direct tool access.
- `~/.config/opencode/plugins/vgxness.ts` — the managed plugin whose public tools become `vgxness_status`, `vgxness_dispatch`, and `vgxness_orchestrate`; its hidden `vgxness_native_read` tool is usable only by an active ticket-bound child session.

## Manager personality and language boundary

`vgxness-manager` is designed to feel like a thoughtful technical partner rather than a command router. It follows the language and register of the direct conversation, recommends one sensible next step, explains its reasoning briefly, and translates bounded receipts into a clear outcome, supporting evidence, meaningful limitations, and what should happen next. It stays candid when the available evidence is incomplete and avoids canned praise or artificial enthusiasm.

That personality belongs to the conversation layer. Code, generated documentation, commit-style text, and other technical artifacts remain neutral and use English by default unless the user requests another language or the project already establishes a different policy. This keeps the manager approachable without leaking a conversational voice into durable project material.

The personality does not expand authority. Claims about workspace state and every requested action must still pass through the exact `vgxness_*` surface, and the returned receipt remains the limit of what the manager may claim. The design borrows general interaction principles from effective orchestrators, but its wording and operating contract are original to VGXNESS; no third-party prompt text or SDD lifecycle is imported.

## CLI and Desktop runtime compatibility

OpenCode CLI and OpenCode Desktop load the same global artifacts, but they do not necessarily execute plugins with the same JavaScript runtime. The CLI can run them with Bun, while the Desktop sidecar runs them with Node. The generated plugin therefore uses the Node-compatible `node:child_process` API, which Bun also implements, and never depends on the global `Bun` object.

Both surfaces launch only the lightweight VGXNESS bridge with an executable plus an argument vector and `shell: false`. Standard input, standard output, standard error, cancellation, exit status, terminal-call timeouts, and the 6,356,992-byte per-stream encoded-response bound remain enforced identically. The bound covers worst-case JSON expansion of an accepted two-megabyte native result. The bridge does not execute a model. The plugin asks the already-running OpenCode host to create a native child session, so the child uses the host's MCP connections and the selected managed subagent's permissions. Repository tests execute the generated plugin under both Node and Bun when those runtimes are available.

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
- The manager permits only user questions and the managed VGXNESS tools. The child-only `vgxness_native_read` tool remains denied to it. It cannot call OpenCode Task directly; the managed plugin creates the Navigator and task sessions as native children of the active manager session, using only the profile selected by an approved plan and prepared ticket.
- Every subagent has its own fail-closed permissions and `task: deny`, so children cannot recursively delegate.
- The configured execution model is embedded in the managed tool, validated as one exact `provider/model` value, and injected outside the LLM-controlled argument schema.
- The plugin launches the trusted permanent VGXNESS launcher only for bounded status, native-ticket, and orchestration lifecycle bridge calls. The legacy nested-process `dispatch` route is not callable. Workspace and parent-session identity come only from OpenCode's tool context; request and output sizes are bounded. Child-session identity and message identity are content-bound into completion evidence.

## Bounded dispatch

The first protocol version exposes only:

- `read-files`
- `write-files` — currently denied in the native path until ticket-authenticated edits are available
- `review-changes`

### Persistent run continuity

A dispatch remains isolated by default. Multi-phase work can opt into persistent continuity without broadening the operation schema:

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

The native subagent receives only the prepared prompt, prior validated capsule, and up to three lexically relevant project-memory records within a fixed context budget. It never receives the full manager conversation, raw run history, or unrestricted memory. Memory IDs returned in `memoryRefs` identify both the records injected into the phase and the new phase summary.

Continuity is intentionally foreground-only and non-delegating. Only one bounded phase advances a continuity run at a time. For adaptive work, a hidden tool-denied Navigator proposes at most sixteen tasks; VGXNESS validates identities, operations, dependencies, review ordering, and a maximum parallelism of four before persisting the plan. Independent read tasks run in native OpenCode child sessions with `Promise.allSettled`; dependent waves start only after accepted prerequisite results are available, and receive only a bounded JSON evidence projection. Durable owner/epoch authority fences stale managers and supports `vgxness orchestrate status|resume|cancel|explain`. Parallel writes, parallel continuity, automatic semantic replanning after a failed task, and full SDD artifact supervision remain deferred. A blocked continuity run still resumes through another validated `continue`; omitting `continuity` preserves one-shot behavior.

Every dispatch passes through the exact Registry prompt and agent identity, balanced Gatekeeper policy, the provider-neutral Runner, bounded Coordinator, and Chronicle evidence. The plugin first creates a native child session and a collision-resistant ticket identity that it retains even if the preparation response is lost. Then `prepare` publishes the recovery ticket before recording the Chronicle task start and upgrades it to a prepared one-shot ticket bound to that child identity. `complete` accepts only a matching ticket, parent session, child session, message, agent result, prompt identity, and exact result digest before writing terminal evidence, memory, or a capsule. Duplicate terminal calls reconcile only their owned lease slot; expired unreturned tickets are terminalized independently without disturbing active siblings. `review-changes` still gives the reviewer only serialized Git evidence, so untracked contents remain explicitly unreviewed.

If the host stops after publishing a `preparing` ticket but before Chronicle records `task.started`, cleanup deliberately writes no synthetic terminal task event. The ticket response is `failed`, while a continuity run is published as `blocked` with that never-started task still `pending`; its capsule is also `blocked`. When this recovers a lost `start`, the triggering tool call returns `status: "recovered"` together with the recovered `runId`, capsule, and memory references instead of creating another child task. The user can inspect that evidence and resume explicitly with `continuity: "continue"` and the returned `runId`.

Shell commands, install/package, commit, push, release, network, permission expansion, secrets, configuration, and destructive-file operations are not part of the v1 tool schema. General command execution remains deferred until it can be constrained by enforceable exact-command policy rather than prompt instructions.

The native child may emit intermediate text while it works. The plugin accepts only the final child response and requires its combined text to be one pure JSON object with no Markdown fence, prose, or trailing value. The normal execution deadline is ten minutes; cancellation aborts the child session and records a failed or cancelled ticket through the bridge.

Internally, the plugin first sends one bounded preparation request:

```sh
printf '%s' '{"protocolVersion":"1","model":"openai/gpt-5.6-sol","operation":"read-files","goal":"inspect the bounded workspace","parentSessionId":"ses_...","parentMessageId":"msg_...","childSessionId":"ses_child_..."}' \
  | vgxness bridge prepare --workspace /absolute/project/path --stdin
```

After the native child returns, the plugin sends its structured result and exact OpenCode session evidence to `vgxness bridge complete`. VGXNESS returns one normalized JSON envelope containing the accepted result and bounded receipt. Provider stderr and internal error details are not exposed. `bridge=configured` reports artifact state; `vgxness_status` performs the live OpenCode version and compatibility handshake.
