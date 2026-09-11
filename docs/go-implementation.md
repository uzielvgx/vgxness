# VGXNESS Go implementation architecture

Structured SDD is retired: no lifecycle tools, SDD worker profiles, or active `sdd-lifecycle` skill are installed. Historical records remain inert in SQLite for data preservation; there is no SDD runtime or archive command. Model-plan schemas retain inactive legacy slots for compatibility. Use a short plan, one writer, independent verification and proportional CARE review. See [the current workflow](orchestration-flow.md).


Current identities are OpenCode Manager62 and Codex Manager21 (parity OpenCode-v62), rendered from `internal/orchestration/manager_contract.json`. Each agent has one current definition. Installed file hashes are tracked in a local installation receipt; older unreceipted agents require the bridge procedure in `docs/agent-template-migration.md`. Global `git-delivery` is a skill workflow; it introduces no Go/runtime writer, daemon or durable delivery state.

## Delivered boundaries

Compatibility execution packages and commands are not delivered. There is no Go provider runner, execution adapter, bridge, control plane, Chronicle, Gatekeeper, registry, prompt composer, coordinator, stack engine, worktree writer, delivery-state service, or custom Git/GitHub tool. `internal/orchestration` defines policy and evidence types, but it does not execute the policy or enforce it at runtime. Native delivery policy lives only in the installed manager and skill.

## Dependency rules

## OpenCode handshake

Setup validates an absolute existing workspace, resolves `opencode`, and runs a bounded `opencode --version` in that workspace. The probe has stable `healthy`, `unavailable`, and `incompatible` statuses, honors cancellation, requires major version 1, and enforces minimum version 1.18.4. It does not run OpenCode tasks or probe model availability.

## Storage

The default database is `~/.vgxness/memory.db`. Canonical workspace bindings isolate project data in one schema-v23 SQLite database. Explicit `--storage-root` and `--project-local` options remain isolated alternatives.

## Managed projection

The OpenCode projection contains 12 managed artifacts with exact identities:

- seven current agents;
- the memory lifecycle plugin;
- one model-plan manifest;
- one installation receipt;
- one `opencode.json` default-agent selection using a semantic merge that preserves unrelated JSON values; existing `opencode.jsonc` bytes remain unchanged;
- one bounded `<config-dir>/vgxness/default-agent.json` restoration record.

The model plan contains exactly seven agents. Receipts bind previously installed
files for updates; older unreceipted agents require the documented bridge.
Historical prompt renderers and templates are removed from the current tree.
The shared registry governs routing and evidence; it is not a Go runtime broker. Recall is relevant-context only, and durable memory is assessed under a stable topic without secrets, transcripts, transient status or automatic cloud synchronization. Unknown, foreign, modified, equal-version drifted, malformed, and newer content is never overwritten. The bridge handles exact historical storage-plugin retirement. The deprecated singular `--model` flag remains accepted as a no-op; plan and slot flags own model configuration.

## Verification

## CARE implementation boundary

CARE is a managed documentation and evidence-ledger contract, not a Go provider runtime or a new schema/transport surface. Current identities and evaluator-owned protected-holdout limits are documented in [CARE architecture](care.md) and [CARE evaluation](care-evaluation.md). Repository validation establishes static conformance only.


Current integration contract: OpenCode Manager62 and Codex Manager21 use one workspace writer and the same frozen candidate for verification and applicable CARE review. SQLite schema v23 is preserved. OpenCode installs 12 managed artifacts and Codex installs ten; each exposes six delegated profiles. The auto-discovered `plugins/vgxness-memory-lifecycle.ts` has no `opencode.json` plugin entry; missing it is partial. `vgxness mcp --full` exposes eight memory tools. Receipt-backed prior installations are recognized by exact local bytes. During retirement, modified, malformed, foreign, unknown, or newer bytes block without removal.
