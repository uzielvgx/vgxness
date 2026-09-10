# VGXNESS Go implementation architecture

Structured SDD is retired: no lifecycle tools, SDD worker profiles, or active `sdd-lifecycle` skill are installed. Historical records remain in SQLite; `vgxness sdd-archive` permits only list/get and revision reads. Model-plan schemas retain inactive legacy slots for compatibility. Use a short plan, one writer, independent verification and proportional CARE review. See [the current workflow](orchestration-flow.md).


Current identities are OpenCode Manager62 and Codex Manager21 (parity OpenCode-v62), rendered from `internal/orchestration/manager_contract.json`. Complete Manager61 and Manager20 packages are the immediate compatibility predecessors. Older Manager59/18, CARE-v2 Manager58/17, CARE-v1 Manager58/16 and deeper packages remain lifecycle identities only. Global `git-delivery` is a skill workflow; it introduces no Go/runtime writer, daemon or durable delivery state.

## Delivered boundaries

Compatibility execution packages and commands are not delivered. There is no Go provider runner, execution adapter, bridge, control plane, Chronicle, Gatekeeper, registry, prompt composer, coordinator, stack engine, worktree writer, delivery-state service, or custom Git/GitHub tool. `internal/orchestration` defines policy and evidence types, but it does not execute the policy or enforce it at runtime. Native delivery policy lives only in the installed manager and skill.

## Dependency rules

## OpenCode handshake

Setup validates an absolute existing workspace, resolves `opencode`, and runs a bounded `opencode --version` in that workspace. The probe has stable `healthy`, `unavailable`, and `incompatible` statuses, honors cancellation, requires major version 1, and enforces minimum version 1.18.4. It does not run OpenCode tasks or probe model availability.

## Storage

The default database is `~/.vgxness/memory.db`. Canonical workspace bindings isolate project data in one schema-v23 SQLite database. Explicit `--storage-root` and `--project-local` options remain isolated alternatives.

## Managed projection

The OpenCode projection contains 11 managed artifacts with exact identities:

- one model-plan manifest;
- one `opencode.json` default-agent selection using a semantic merge that preserves unrelated JSON values; existing `opencode.jsonc` bytes remain unchanged;
- one bounded `<config-dir>/vgxness/default-agent.json` restoration record of whether `opencode.json` existed and any prior explicit default, so uninstall can restore that default or remove a config created by setup;

The model plan contains exactly seven agents and does not contain the skill. OpenCode immediate predecessor is Manager59, followed by CARE-v2/Manager58 and CARE-v1/Manager58/Manager57; OpenCode v56/verifier-v6 and older catalogued manager, agent, and model-plan predecessors can be upgraded. Codex immediate predecessor is Manager18, followed by Manager17, Manager16, and deeper Manager15/v14 lifecycle recognition. The shared registry governs routing and evidence; it is not a Go runtime broker. Recall is relevant-context only, and durable memory is assessed under a stable topic without secrets, transcripts, transient status or automatic cloud synchronization. Unknown, foreign, modified, equal-version drifted, malformed, and newer content is never overwritten. Exact catalogued storage-plugin predecessors remain recognizable. The deprecated singular `--model` flag remains accepted as a no-op; plan and slot flags own model configuration.

## Verification

## CARE implementation boundary

CARE is a managed documentation and evidence-ledger contract, not a Go provider runtime or a new schema/transport surface. Current identities and evaluator-owned protected-holdout limits are documented in [CARE architecture](care.md) and [CARE evaluation](care-evaluation.md). Repository validation establishes static conformance only.


Current integration contract: OpenCode Manager62 and Codex Manager21 use one workspace writer and the same frozen candidate for verification and applicable CARE review. SQLite schema v23 is preserved. OpenCode installs 11 managed artifacts and Codex installs nine; each exposes six delegated profiles. The auto-discovered `plugins/vgxness-memory-lifecycle.ts` has no `opencode.json` plugin entry; missing it is partial. `vgxness mcp --full` exposes eight memory tools. Complete Manager61 and Manager20 packages are recognized predecessors. During retirement, modified, malformed, foreign, unknown, or newer bytes block without removal.
