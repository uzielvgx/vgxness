# Safe hooks

## Current surface

VGXNESS installs one exact auto-discovered OpenCode lifecycle plugin at `plugins/vgxness-memory-lifecycle.ts`; it has no `opencode.json` plugin entry. For each top-level session it starts one lifecycle record, adds one bounded isolated context handoff, performs a transcript-free compaction checkpoint, observes exact summary completion without retaining tool arguments, results, or status, and ends the record as `completed` or `interrupted`. It does not install shell or Git hooks, automatic memory injection, capture transcripts, add broad observability, or add another plugin.

`vgxness memory hook --stdin` is a local host-only JSON lifecycle bridge, not an installed hook surface. Its single JSON request and output use `schemaVersion: 1`; unknown, duplicate, cross-operation, and trailing fields/documents are rejected. It supports `--storage-root` and `--project-local` consistently with other memory commands. Start, checkpoint, and end are supported; only end finalizes a provider session. `context` reads only the bounded, same-project prior completed handoff for an active handle. `summary` saves or replaces one local pending draft and accepts an optional RFC3339Nano `expected_updated_at` optimistic timestamp. Context may contain untrusted handoff text; summary output is metadata only. Neither output exposes draft text, external IDs, or provider hashes. Local drafts are never synchronized.

The internal Go runtime may synchronously emit best-effort lifecycle events for completed memory synchronization when it has a valid effective invocation workspace: an empty project directory uses the current working directory, and explicit paths are made absolute, cleaned, resolved through existing symlinks, and normalized to existing filesystem case. These events are internal-only, do not add an installed hook surface, and provide invocation correlation rather than synchronization data scope. A listener can block the caller. Emission is globally single-flight, so concurrent or reentrant events are dropped; there is no queue, retry, replay, persistence, or crash durability. Missing or invalid workspace identity suppresses the event. MCP and provider boundaries remain unchanged.

During an interactive TUI session only, the internal process may show a best-effort, in-memory activity list of up to 64 lifecycle events. Its listener is bounded and drops new events when full. The display uses per-session redacted subject aliases and never renders event payloads or raw identifiers. It has no persistence, replay, audit, installed callback, shell, Git, plugin, MCP, or authentication effect.

MCP uses local stdio and assumes its host is trusted. Requests carry no caller identity or session authentication. `vgxness mcp` is default-deny and exposes only `memory_recent`, `memory_search`, and `memory_context`; only the explicit `vgxness mcp --full` command exposes the full read/write set. Managed OpenCode and generated Codex use that explicit command, but their read-only agent/tool allowlists exclude every mutating MCP tool. Those host allowlists, operator permissions, user authorization, and task scope are part of the authorization boundary. This document makes no runtime-security claim.

## Historical retirement context

Plugin v1–v10 behavior is legacy retirement evidence, not current behavior. Exact historical `vgxness.ts` v1-v10 plugin bytes and `vgxness-autonomous-stacked-pr` v1/v2/v3 provider-skill bytes are removable; modified, malformed, foreign, unknown, or newer bytes block without removal.

VGXNESS does not accept shell hook commands and does not install or manage Git hooks.
