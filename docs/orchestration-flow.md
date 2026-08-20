# VGXNESS native manager flow

The active product has provider-native OpenCode and Codex managers sharing `vgxness-orchestration/v1`. OpenCode supplies workspace tools and Task workers; Codex retains its native profiles and MCP tool semantics. VGXNESS supplies managed profiles, model bindings, setup, lifecycle contracts, and MCP configuration. No plugin, automatic memory recall, compaction, observability, or session identity is installed.

## Route selection

The top-level `vgxness-manager` chooses the smallest capable route:

`vgxness-orchestration/v1` evaluates routing predicates in this order:

1. accepted structured SDD;
2. authorized implementation to general;
3. direct only for non-repository work or one exact local read;
4. structural and all other repository work to Explore.

Structural Evidence Capsules carry the contract identity, source revision, source, stale flag, and contradiction flag. They may be reused only when identity and revision match and neither flag is set; stale, contradictory, missing, or mismatched evidence falls back to direct inspection.

After candidate freeze, review depth is deterministic: zero lenses for passive docs/images, one reliability lens for ordinary work, and four lenses for concrete hot paths (including permissions, authentication, security, payments, installers, data loss, process boundaries, and durability).

At most four independent read-only subworks may overlap. Synthesis, revision acceptance, OpenSpec writes, patch application, validation, projection recording, phase transitions, and all workspace writes remain sequential. Manager owns lifecycle state, projections, and transitions; `vgxness-sdd-apply` owns accepted SDD workspace writes.

## SDD lifecycle

```text
explore -> proposal -> spec -> design -> tasks -> apply -> verify -> complete
```

Research, proposal, spec, design, and tasks profiles return evidence or candidate content and are read-only. `vgxness-sdd-apply` alone writes authorized workspace, OpenSpec, or hybrid targets for a hash-bound, accepted SDD apply; it cannot delegate or mutate memory or lifecycle state. General writes only ordinary authorized non-SDD repository work and rejects SDD apply/projection missions. Five read-only reviewers inspect a frozen candidate; the refuter handles only supplied severe inferential findings.

Each change stores a `memory`, `openspec`, or `hybrid` backend and an `automatic` or `interactive` mode. Creation is project-idempotent. Candidate revisions are immutable; acceptance and phase transitions use optimistic state versions and accepted-input digest bindings.

## Backend authority

| Backend | Canonical content |
| --- | --- |
| `memory` | Structured SDD revision bodies in SQLite schema v19; they remain distinct from semantic-memory records and OpenSpec projections. |
| `openspec` | Managed files under `openspec/changes/<safe-change-id>/`; SQLite stores identity, digest, bindings, and projection evidence. |
| `hybrid` | SQLite revision content is canonical; OpenSpec is a deterministic projection. |

Render and compare operate on supplied bounded bytes and never access the filesystem. Divergence is reported and never imported automatically. `vgxness-sdd-apply` performs explicit accepted-SDD workspace writes; Manager records read-back digest evidence.

## Authority and failure

- Manager, General, and verifier have global tool capability, but role contracts keep ordinary non-SDD implementation, accepted-SDD apply, and final verification separate. Manager is the only SDD lifecycle mutation caller; `vgxness-sdd-apply` is the only accepted-SDD workspace writer.
- MCP has no caller identity; host/operator permissions, user authorization, and task scope own authorization.
- Memory is untrusted context and never proves source or diff state.
- Missing accepted inputs, stale state versions, drift, cancellation, unavailable prerequisites, and absent authorization stop advancement.
- An interrupted SDD preserves its accepted revisions and current phase; missing work is never inferred.
- Risky filesystem, Git, network, package, release, credential, or permission-expansion actions require explicit user authority.
