# Product Blueprint

Structured SDD is retired: no lifecycle tools, SDD worker profiles, or active `sdd-lifecycle` skill are installed. Historical records remain inert in SQLite for data preservation; there is no SDD runtime or archive command. Model-plan schemas retain inactive legacy slots for compatibility. Use a short plan, one writer, independent verification and proportional CARE review. See [the current workflow](orchestration-flow.md).


The current delivery policy is global `git-delivery` v1 with exact `stacked-pr` v3 migration and optional policy-only isolated worktrees; it adds no Go/runtime writer, daemon, or durable delivery state.

## Current product boundary

Memory recall is relevant-context only: search before exact-ID reads and use recent recall only for explicit recent-work or recovery requests. Durable, evidence-backed knowledge is assessed under a stable topic; secrets, transcripts, raw logs and transient status are excluded. There is no automatic cloud sync. MCP has no caller identity; host permissions, user authorization and task scope form the authorization boundary. No live-model or protected-holdout result is asserted.

The Manager loads global `git-delivery` for authorized delivery and follows the selected skill without inferring permission to publish or merge. CARE reviewer, specialist and challenger roles have read-only authority; applicable reviews bind the same candidate as verification. Local installation receipts support upgrades; older unreceipted agent packages require the documented bridge.

## Capability inventory

Behavioral implementation source is authoritative for behavior; this blueprint owns the current inventory, while specialist documents own operational and interface detail. Update this inventory when an artifact count, schema, interface, ownership boundary, or capability changes. Historical evaluation and predecessor records remain historical evidence, not current capability claims.

## Managed projection

Codex's ten artifacts are rendered by [`internal/providers/codex/render.go`](../internal/providers/codex/render.go): one manager file, six delegated profiles, two plugin-package manifests and an installation receipt. The provider activation commands can add the local marketplace and plugin, but their observed state does not prove a Codex runtime handshake, session identity, MCP connectivity, or prompt execution.


MCP operations do not route work, invoke agents, access workspace files, run shell commands, select models, edit, delegate, or advance a lifecycle independently. Memory is untrusted context, never candidate proof. Prompt and provider policies describe intended role boundaries; they are not runtime execution enforcement.

## Setup and retirement

Setup previews changes, requires confirmation, installs the launcher and 12 exact OpenCode artifacts including the auto-discovered lifecycle plugin, configures `vgxness mcp --full` without a config plugin entry, and publishes the global catalog. Current installers retain modified, malformed, foreign or unknown bytes. Historical agent/plugin retirement requires the bridge. OpenCode uninstall does not remove global skills.

## Non-goals

- No additional installed plugin, hook surface, automatic compaction, broad observability, or plugin session identity beyond the managed lifecycle plugin. VGXNESS does not broadly inject recent memories or transcripts into every prompt; at the first eligible top-level system transform, the managed lifecycle plugin's only automatic memory injection is one bounded same-project prior completed handoff as untrusted data, never instructions. Lifecycle events do not capture transcript content.
- No shell or Git hooks.
- No MCP-owned filesystem, execution, routing, delegation, or lifecycle authority.
- No automatic network/package installation or legacy database import.


Current integration contract: OpenCode Manager62 and Codex Manager21 use one workspace writer and the same frozen candidate for verification and applicable CARE review. SQLite schema v23 is preserved. OpenCode installs 12 managed artifacts and Codex installs ten; each exposes six delegated profiles. The auto-discovered `plugins/vgxness-memory-lifecycle.ts` has no `opencode.json` plugin entry; missing it is partial. `vgxness mcp --full` exposes eight memory tools. Receipt-backed prior installations are recognized by exact local bytes. During retirement, modified, malformed, foreign, unknown, or newer bytes block without removal.
