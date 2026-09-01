# Provider integration preflight

This repository process applies before implementing or publishing a new provider integration, provider lifecycle change, or provider-dependent slice. It is a review and evidence contract, not runtime enforcement or a security/readiness certification.

## Required records before the first stack PR

### Provider Capability Contract

Record the provider/upstream version and, for every relied-on capability, its official source and the exact behavior relied upon. Mark source material as verified only when directly inspected; otherwise mark it supplied, unverified, or unresolved. The contract MUST identify:

- trusted and untrusted inputs, workspace/session identity, and authentication separately from authorization;
- replay and concurrency behavior, mutation ordering, terminal and no-op outcomes, interruption and recovery behavior;
- platform guarantees and unknowns; unsupported prerequisites; and
- a proof plan showing invalid input produces zero mutation, including external mutation where applicable.

An undocumented, ambiguous, or unavailable capability is not a capability contract. Do not replace it with unauthenticated fields, inferred identity, a static token, or a process-local guess.

| Field | Record |
| --- | --- |
| Provider / upstream version | |
| Capability | Official source; verified/supplied/unverified/unresolved; exact relied-on behavior |
| Trusted inputs | |
| Untrusted inputs and rejection boundary | |
| Workspace identity / session identity | |
| Authentication / authorization | |
| Replay / concurrency / mutation ordering | |
| Terminal / no-op behavior | |
| Interruption / recovery | |
| Platform guarantees / unknowns | |
| Unsupported prerequisite | |
| Invalid-input zero-mutation proof | Check and evidence location |

### Stack Dependency Map

Before the first stack PR, map every slice with its purpose and surface, dependencies, assumptions made by later slices, and whether it can merge independently. State whole-stack stop conditions: a missing prerequisite, contradictory upstream behavior, invalid capability contract, candidate/source drift, failed required check, or inability to preserve foreign state stops dependent slices and publication.

| Slice | Purpose / surface | Dependencies | Later assumptions | Independently mergeable? | Stop condition |
| --- | --- | --- | --- | --- | --- |
| | | | | | |

## Two gates

1. **Architecture and security preflight:** before production implementation, complete the capability contract, dependency map, trust boundary, mutation/no-mutation proof plan, recovery plan, and adversarial matrix below.
2. **Frozen-candidate verifier and CARE review:** before publication, freeze the exact candidate, run the required verifier and concrete-risk CARE allocation against that candidate, and invalidate those results after any source change.

External-process, installer, identity, memory, permission, and durability work MUST pass both gates; final code review alone is insufficient.

| Gate | Required evidence | Location / result |
| --- | --- | --- |
| Architecture and security preflight | Contract, dependency map, trust/mutation/recovery plan, matrix | |
| Frozen-candidate verifier and CARE review | Candidate digest, verifier, applicable CARE result | |

## Adversarial matrix

The preflight MUST specify observable checks for each applicable row and the expected mutation boundary.

| Case | Required observation | Applicable? | Check / evidence |
| --- | --- | --- | --- |
| Forged selectors or identity fields | Rejected before selecting or mutating foreign state. | | |
| Duplicate keys or trailing JSON | Parser rejects ambiguity; no mutation occurs. | | |
| Oversized or malformed input | Bounded rejection; no mutation occurs. | | |
| Terminal or rejected request | No-op is explicit and produces zero mutation. | | |
| Interruption before/after external mutation | Recovery preserves the durable boundary and reports unresolved state. | | |
| Replay or concurrency | Idempotency/order rule preserves the contract without duplicate mutation. | | |
| Windows, macOS, and Linux paths/environment | Platform-specific guarantees and unknowns are recorded; unsupported behavior stops. | | |
| Recovery tampering | Foreign/replaced/recovery evidence is preserved and stops automatic continuation. | | |
| Foreign-state preservation | The integration changes only its exact owned state. | | |

## Worktree and evidence integrity

For each candidate, record the exact worktree path, HEAD and base identities, clean status, changed-path manifest, and whether CodeGraph was aligned or direct reads were used. Keep external build artifacts outside the repository. Candidate, validation, verifier, and CARE evidence are invalid after a source, path, dependency, provider-template, or acceptance change; freeze a new candidate before publication.

| Evidence | Record |
| --- | --- |
| Worktree path | |
| HEAD / base identity | |
| Clean status | |
| Changed-path manifest | |
| CodeGraph alignment or direct-read evidence | |
| External build-artifact location | |
| Frozen candidate digest | |
| Invalidation check after last source change | |

## Stop and resume rule

If an upstream capability is absent or undocumented, implementation and every dependent PR stop. Record the unmet prerequisite and its affected slices. Resume only when the Provider Capability Contract changes with direct evidence or the scope is explicitly redefined. A reviewer may not waive this rule by substituting an unauthenticated field or process-derived value.

## Publication checklist

For applicable provider work, complete the [tracked PR template](../../.github/pull_request_template.md) with links or locations for the capability contract, dependency map, preflight/adversarial evidence, exact candidate identity, verifier result, CARE result, and unresolved platform limitations. Missing evidence blocks publication. Provider fields are not required for unrelated PRs.
