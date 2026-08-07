# Safe hooks

## Current surface

VGXNESS installs no OpenCode plugin and therefore has no installed hook surface. It does not install or manage OpenCode callbacks, shell hooks, Git hooks, automatic memory injection, compaction, observability, or plugin session identity.

Managed OpenCode and generated Codex use `vgxness mcp --full`. MCP requests carry no caller identity; host/operator permissions, user authorization, and task scope own authorization. This document makes no runtime-security claim.

## Historical retirement context

Plugin v1–v10 behavior is legacy retirement evidence, not current behavior. Exact historical `vgxness.ts` v1-v10 plugin bytes and `vgxness-autonomous-stacked-pr` v1/v2/v3 provider-skill bytes are removable; modified, malformed, foreign, unknown, or newer bytes block without removal. Historical descriptions of callbacks, memory injection, compaction, observability, or session correlation do not describe an installed current surface.

VGXNESS does not accept shell hook commands and does not install or manage Git hooks.
