# Safe hooks

VGXNESS ships one hook surface: typed callbacks in the generated OpenCode storage plugin v8. It does not ship arbitrary shell hooks, Git hooks, or an internal Go event dispatcher.

## OpenCode callbacks

- `event` tracks top-level session creation, known child sessions, and deletion.
- `chat.message` marks the first `vgxness-manager` user turn as needing context.
- `experimental.chat.system.transform` performs one single-flight bounded recent-memory recall and appends tagged, untrusted reference data.
- `experimental.session.compacting` appends cached memory and a bounded summary of successfully completed tool observations.
- `tool.execute.before` and `tool.execute.after` correlate only bounded tool name, session ID, call ID, start time, duration, and successful completion.
- `dispose` aborts plugin-owned lookups and clears closure state.

Memory never becomes instructions or candidate proof. Closing tags are escaped, collections are capped, stale tool starts are purged, child sessions are excluded, and hook failures cannot abort chat, compaction, or tool execution.

Hook correlation grants no mutation authority. Every SDD mutation independently verifies the tracked top-level `vgxness-manager` session and rejects child, reviewer, phase-agent, missing, or mismatched context. The plugin stores and transforms bounded data; it never executes, routes, edits, delegates, or advances a change by itself.

## Local manager observability

`VGXNESS_MANAGER_OBSERVABILITY=1` enables a separate, process-local diagnostic record only after an explicit top-level `session.created` lifecycle event identifies `vgxness-manager`. Chat's legacy synthesized memory state never establishes observability eligibility. It is off by default; unset, any value other than exactly `"1"`, ineligible callbacks, session deletion, and disposal synchronously retain no observability state and do not adapt or allocate observability input. Child, reviewer, SDD phase-agent, non-manager, malformed, and uncorrelatable inputs are excluded before allocation.

Records have schema version 1, opaque local UUIDs, per-workflow sequence, an allow-listed callback/kind, non-negative monotonic observed offset, and `unavailable` availability. They never claim a terminal outcome, duration, completeness, quality, latency, or security result. Retention is bounded to 128 workflows, 32 records per workflow, 256 records globally, and 128 pending correlations; global pressure evicts the true oldest record. Pending tool pairs retain validated tool identity only ephemerally and require exact session/call/tool equality before one-time consumption; no tool identity enters a record. Their ten-minute lifetime uses monotonic `globalThis.performance.now()` samples: missing, invalid, or backward clocks fail open without allocation.

The records are private to the running plugin: no file, database, memory store, export, console/log, network, subprocess, tool payload, model context, prompt, compaction, credential, Git/PR, or SDD route receives them. Instrumentation failures are discarded locally and cannot alter host callback behavior. Restarting OpenCode starts an empty local process scope. Generated-plugin tests are not installed-host proof; separately authorized, version-bound installed-host validation of delivery, ordering, correlation, exclusions, opt-in, and semantics is required before any capability can be marked `direct`.

## Exclusions

VGXNESS does not accept shell hook commands and does not install or manage Git hooks. Those mechanisms carry ambient process, credential, filesystem, mutation, portability, timeout, quoting, and re-entry risks outside the storage plugin's authority.
