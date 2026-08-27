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

After candidate freeze, CARE allocates assurance proportionally: no reviewer for proven passive docs/images, a reviewer for ordinary work, a reviewer plus specialist for elevated work, and reviewer, specialist, and challenger for concrete hot paths (including permissions, authentication, security, payments, installers, data loss, process boundaries, and durability).

At most five independent read-only subworks may overlap. Synthesis, revision acceptance, OpenSpec writes, patch application, validation, projection recording, phase transitions, and all workspace writes remain sequential. Manager owns lifecycle state, projections, and transitions; `vgxness-sdd-apply` owns accepted SDD workspace writes.

## SDD lifecycle

```text
explore -> proposal -> spec -> design -> tasks -> apply -> verify -> complete
```

Research, proposal, spec, design, and tasks profiles return evidence or candidate content and are read-only. `vgxness-sdd-apply` alone writes authorized workspace, OpenSpec, or hybrid targets for a hash-bound, accepted SDD apply; it cannot delegate or mutate memory or lifecycle state. General writes only ordinary authorized non-SDD repository work and rejects SDD apply/projection missions. CARE reviewer, specialist, and challenger inspect only their manager-assigned frozen-candidate assurance scope.

Each change stores a `memory`, `openspec`, or `hybrid` backend and an `automatic` or `interactive` mode. Creation is project-idempotent. Candidate revisions are immutable; acceptance and phase transitions use optimistic state versions and accepted-input digest bindings.

## Invocation-local readiness

Readiness is provider-neutral, invocation-local evidence for one delegated write, not approval, authorization, validation, review, lifecycle authority, host/runtime enforcement, prevention proof, or effectiveness proof. Classification is deterministic: direct/no-write or simple exact-read work is **exempt** with zero envelope, tools, task list, review, or ceremony; every other delegated repository write is **light** unless an SDD, frozen/delivery, cross-platform, lifecycle/recovery, identity/digest, provider/template, or other concrete high-risk predicate (or ambiguity) selects **full**.

For non-exempt work, Manager assembles the bound envelope and independently rechecks it immediately before launch. The writer rechecks and echoes the exact envelope and applicable mission/context/SDD bindings before writing. `READY` only says required evidence is current for that invocation; it never grants approval or authorization. `BLOCKED` and `INCONCLUSIVE` prevent writes.

The envelope is invalid on any change to its mission, task, accepted input, state version, replay identity, scope/path, target hash or no-symlink evidence, acceptance criteria, permitted validation, dependency, provider artifact/template, candidate, or material evidence. A source or path correction invalidates related candidate, validation, and review evidence; a new binding is required.

Copied observations are bounded and privacy-safe: only class, status, controlled reason/risk categories, invalidation trigger, elapsed bucket, and write-launched flag may be observed. They exclude mission, source, path, credentials, authorization, and identity; never persist, launch work, or influence readiness, delegation, or lifecycle state.

Rollback removes the new contract projection or returns to exact recognized predecessor artifacts (OpenCode manager v57; Codex manager v16). Those labels are predecessor-only, not current runtime identities. It does not claim automatic runtime rollback. These contracts are prompt and source behavior, not host enforcement, prevention proof, or effectiveness proof.

Baseline and controlled pilot work are deferred and separately authorized; no pilot or external holdout runs here. Any future matched cohorts require external opaque holdout custody and stop on: activation coverage <100%, binding-safety >0%, invalidation recall <100%, accidental exempt ceremony, seeded verifier/reviewer recall <100%, pilot recall below matched baseline, false-block >10%, p95 overhead >15 minutes, or unknown metric provenance. No proven-effectiveness claim follows from this documentation.

## Backend authority

| Backend | Canonical content |
| --- | --- |
| `memory` | Structured SDD revision bodies in SQLite schema v20; they remain distinct from semantic-memory records and OpenSpec projections. |
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

## CARE routing

CARE records the route, risk, evidence ledger, and invalidation markers for direct, assisted, action, engineering, and assured work; Manager retains lifecycle authority. See [CARE architecture](care.md) and the [development-visible evaluation plan](care-evaluation.md). Protected-holdout adjudication remains external to this flow.
