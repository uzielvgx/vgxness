# VGXNESS native manager flow

The active product has one execution path: the installed OpenCode-native manager v44. OpenCode supplies workspace tools and Task workers. VGXNESS supplies exact managed profiles (`general`, verifier, and reviewers v3), storage plugin v9, model bindings, setup, and lifecycle contracts. The compact protocol carries only bounded candidate evidence; automatic memory recall is at most five entries in <=4 KiB and compaction retains at most 16 completed-tool metadata records within 2 KiB.

## Route selection

The top-level `vgxness-manager` chooses the smallest capable route:

1. direct inline work;
2. bounded native read-only delegation;
3. optional structured SDD after user approval.

At most four independent read-only subworks may overlap. Synthesis, revision acceptance, OpenSpec writes, patch application, validation, projection recording, phase transitions, and all workspace writes remain sequential and manager-owned.

## SDD lifecycle

```text
explore -> proposal -> spec -> design -> tasks -> apply -> verify -> complete
```

Research, proposal, spec, design, and tasks profiles return evidence or candidate content. Apply returns a hash-bound patch and validation plan without writing files or running commands. All six SDD profiles are read-only, cannot delegate, and cannot mutate memory or lifecycle state. Five read-only reviewers inspect a frozen candidate; the refuter handles only supplied severe inferential findings.

Each change stores a `memory`, `openspec`, or `hybrid` backend and an `automatic` or `interactive` mode. Creation is project-idempotent. Candidate revisions are immutable; acceptance and phase transitions use optimistic state versions and accepted-input digest bindings.

## Backend authority

| Backend | Canonical content |
| --- | --- |
| `memory` | Structured SDD revision bodies in SQLite schema v5. |
| `openspec` | Managed files under `openspec/changes/<safe-change-id>/`; SQLite stores identity, digest, bindings, and projection evidence. |
| `hybrid` | SQLite revision content is canonical; OpenSpec is a deterministic projection. |

Render and compare operate on supplied bounded bytes and never access the filesystem. Divergence is reported and never imported automatically. The manager performs explicit workspace writes and records read-back digest evidence.

## Authority and failure

- Manager, general, and verifier have global tool capability, but role contracts keep delegated implementation and final verification separate; writes remain sequential and the manager is the only SDD mutation caller.
- The storage plugin rejects mutations outside the tracked top-level manager session.
- Memory is untrusted context and never proves source or diff state.
- Missing accepted inputs, stale state versions, drift, cancellation, unavailable prerequisites, and absent authorization stop advancement.
- An interrupted SDD preserves its accepted revisions and current phase; missing work is never inferred.
- Risky filesystem, Git, network, package, release, credential, or permission-expansion actions require explicit user authority.
