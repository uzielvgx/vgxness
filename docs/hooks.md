# Safe hooks

VGXNESS exposes two closed hook surfaces: typed in-process notifications for committed control-plane transitions, and a generated OpenCode plugin that maintains bounded manager context. Arbitrary shell hooks and Git hooks are intentionally not supported.

## Internal events

`internal/hooks` recognizes exactly these event names:

- `task.started`
- `task.succeeded`
- `task.failed`
- `candidate.frozen`
- `validation.completed`
- `delivery.installed`

Chronicle remains the sole durable task authority. Task lifecycle notifications are emitted only after the corresponding Chronicle append succeeds. `candidate.frozen` follows persistence of a completed native ticket with an edit artifact, including native repair. `validation.completed` follows persistence of a native validation receipt; an executed validation with `success=false` is still a valid completed receipt, while an infrastructure error emits nothing. `delivery.installed` means a delivery receipt was durably persisted and made active.

Events contain only bounded stable IDs, canonical digests, foreground/background mode, success, exit code, timestamps, and counts. They never contain prompts, goals, result bodies, validation output, file contents, tokens, raw errors, commands, findings, arbitrary maps, or absolute paths. Delivery payloads remain below 512 bytes.

Handlers run in registration order with a bounded timeout, panic recovery, classified diagnostics, a recursion-depth limit, and bounded process-local duplicate suppression. Each handler receives an isolated payload clone. Empty and nil dispatchers are safe. There is no global dispatcher and no second event log.

The internal dispatcher is an injection point for in-process callers, not active production telemetry. Internal handlers run only when a caller explicitly constructs a dispatcher with handlers and injects it into the relevant service. The shipped application registers none, so it invents no persistence side effect, shell sink, or duplicate Chronicle. The active shipped hooks are the generated OpenCode plugin hooks described below.

## Delivery guarantee

Internal notifications are best effort and post-commit. Task notifications dispatch from completed coordinator receipts only after all required Chronicle writes, including `result.accepted` when applicable, and after coordinator cleanup/slot release. Handler error, panic, timeout, or recursive misuse never changes the already committed operation result. Candidate, validation, and delivery notifications dispatch after their storage locks and native ticket guards are released, where re-entry could otherwise deadlock.

Duplicate suppression is bounded and process-local. It reduces replay noise but is not an exactly-once guarantee. Exactly-once external delivery would require a durable transactional outbox and an acknowledgement protocol; VGXNESS does not add either here.

## OpenCode hooks

The managed `vgxness.ts` plugin uses OpenCode's typed plugin hooks without changing user messages, tool arguments, or tool results:

- `event` synchronously tracks top-level session creation, known child sessions, and session deletion.
- `chat.message` marks the first `vgxness-manager` user turn as needing context.
- `experimental.chat.system.transform` performs one single-flight bounded recent-memory recall per top-level manager session and appends a tagged reference-data block by mutating `output.system` in place.
- `experimental.session.compacting` appends the cached memory block and a bounded summary of successfully completed tool observations to `output.context`; it never replaces `output.prompt`.
- `tool.execute.before` and `tool.execute.after` correlate only bounded tool name, session ID, call ID, start time, duration, and successful completion. They do not retain arguments, output, title, metadata, prompts, or errors.
- `dispose` aborts plugin-owned memory lookups and clears all closure state.

Memory is untrusted reference data, never an instruction source. Closing tags are escaped, context and observation collections are capped, stale unmatched tool starts are purged, child sessions are excluded, and every hook catches its own failures. A memory timeout or lookup failure therefore cannot abort chat, compaction, or tool execution.

## Excluded hooks

VGXNESS does not accept arbitrary shell hook commands. A shell hook would turn a passive observer into an unbounded process-execution boundary with ambient environment, filesystem, credential, timeout, quoting, and re-entry risks.

VGXNESS also does not install or manage Git hooks. Git hooks have repository mutation, workstation policy, portability, bypass, and hidden-execution consequences. Existing repository-owned policy may be respected during an explicitly requested delivery operation, but this hook set neither creates nor invokes Git hooks.
