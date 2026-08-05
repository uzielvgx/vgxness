---
name: sdd-lifecycle
description: Use ONLY after a top-level Manager has explicit user request or acceptance for SDD; governs accepted SDD lifecycle changes, revisions, projections, and transitions. Do not use for ordinary planning, implementation, documentation, or casual spec/design language.
license: MIT
compatibility: Agent Skills hosts with delegated agents and SDD lifecycle tools
---

<!-- managed-by: vgxness; artifact: global-skill/sdd-lifecycle; version: 1 -->

# SDD lifecycle

The Manager is the sole lifecycle authority. This skill activates only after explicit SDD acceptance; absence or load failure must fail closed. Never infer SDD from planning, implementation, documentation, or the words spec/design.

## Order and modes

Run `explore -> proposal -> spec -> design -> tasks -> apply -> verify -> complete`. Create with one stable idempotency key; after uncertainty retry the same key and payload. Choose Automatic or Interactive only at accepted-change start; later changes require explicit `set_interaction_mode` with the latest `stateVersion`. Automatic advances validated gates without routine pauses. Interactive pauses after each validated candidate for approve, revise, or cancel. Cancellation is explicit and terminal.

## Authority and bindings

Only Manager creates changes, persists/accepts revisions, records projections, changes mode, and transitions. Phase agents are read-only and phase-bound; they never route, self-approve, mutate lifecycle state, or choose models. Every mission binds changeId, artifact, accepted input artifact/revision IDs, SHA-256 digests, evidence, and constraints. Validate bindings before every persistence or transition; use latest stateVersion. Conflict, stale stateVersion, or mismatch requires reload/reconciliation, never blind retry. `apply` composes a hash-bound candidate; managed general writes; verifier validates.

## Backends and projection

- **memory:** accepted structured revision is canonical; no workspace projection.
- **OpenSpec:** repository file is canonical. General writes only supplied exact relative path under `openspec/changes/<change-id>/`, rejects symlinks/path drift, reads back and verifies digest; Manager stores external location/identity.
- **hybrid:** accepted memory revision is canonical and deterministic OpenSpec bytes are its projection. General writes only exact rendered bytes at the approved non-symlink path; Manager compares readback and records projection evidence. Never import divergent bytes automatically.

A transition needs the accepted current revision and, for OpenSpec/hybrid, current projection evidence bound to it. On projection drift, require Manager reconciliation: overwrite from memory, inspect differences, or save OpenSpec bytes as a new candidate revision. Fail closed on missing evidence, backend uncertainty, path/symlink violation, drift, or invalid transition.
