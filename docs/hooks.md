# Safe hooks

VGXNESS ships one hook surface: typed callbacks in the generated OpenCode storage plugin v5. It does not ship arbitrary shell hooks, Git hooks, or an internal Go event dispatcher.

## OpenCode callbacks

- `event` tracks top-level session creation, known child sessions, and deletion.
- `chat.message` marks the first `vgxness-manager` user turn as needing context.
- `experimental.chat.system.transform` performs one single-flight bounded recent-memory recall and appends tagged, untrusted reference data.
- `experimental.session.compacting` appends cached memory and a bounded summary of successfully completed tool observations.
- `tool.execute.before` and `tool.execute.after` correlate only bounded tool name, session ID, call ID, start time, duration, and successful completion.
- `dispose` aborts plugin-owned lookups and clears closure state.

Memory never becomes instructions or candidate proof. Closing tags are escaped, collections are capped, stale tool starts are purged, child sessions are excluded, and hook failures cannot abort chat, compaction, or tool execution.

Hook correlation grants no mutation authority. Every SDD mutation independently verifies the tracked top-level `vgxness-manager` session and rejects child, reviewer, phase-agent, missing, or mismatched context. The plugin stores and transforms bounded data; it never executes, routes, edits, delegates, or advances a change by itself.

## Exclusions

VGXNESS does not accept shell hook commands and does not install or manage Git hooks. Those mechanisms carry ambient process, credential, filesystem, mutation, portability, timeout, quoting, and re-entry risks outside the storage plugin's authority.
