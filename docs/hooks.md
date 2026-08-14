# Safe hooks

## Current surface

VGXNESS installs no OpenCode plugin and therefore has no installed hook surface. It does not install or manage OpenCode callbacks, shell hooks, Git hooks, automatic memory injection, compaction, observability, or plugin session identity.

The internal Go runtime may synchronously emit best-effort lifecycle events for completed memory synchronization when it has a valid effective invocation workspace: an empty project directory uses the current working directory, and explicit paths are made absolute, cleaned, resolved through existing symlinks, and normalized to existing filesystem case. These events are internal-only, do not add an installed hook surface, and provide invocation correlation rather than synchronization data scope. A listener can block the caller. Emission is globally single-flight, so concurrent or reentrant events are dropped; there is no queue, retry, replay, persistence, or crash durability. Missing or invalid workspace identity suppresses the event. MCP and provider boundaries remain unchanged.

MCP uses local stdio and assumes its host is trusted. Requests carry no caller identity or session authentication. `vgxness mcp` is default-deny and exposes only `memory_recent` and `memory_search`; only the explicit `vgxness mcp --full` command exposes the full read/write set. Managed OpenCode and generated Codex use that explicit command, but their read-only agent/tool allowlists exclude every mutating MCP tool. Those host allowlists, operator permissions, user authorization, and task scope are part of the authorization boundary. This document makes no runtime-security claim.

## Historical retirement context

Plugin v1–v10 behavior is legacy retirement evidence, not current behavior. Exact historical `vgxness.ts` v1-v10 plugin bytes and `vgxness-autonomous-stacked-pr` v1/v2/v3 provider-skill bytes are removable; modified, malformed, foreign, unknown, or newer bytes block without removal. Historical descriptions of callbacks, memory injection, compaction, observability, or session correlation do not describe an installed current surface.

VGXNESS does not accept shell hook commands and does not install or manage Git hooks.
