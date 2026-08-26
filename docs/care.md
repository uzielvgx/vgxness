# CARE architecture

CARE is the repository-visible contract for choosing and assessing engineering work. It records architecture and deterministic development checks; it does not add a runtime, schema, transport, dataset, custody service, or protected-holdout execution.

## Authority and activation

Manager alone activates and allocates CARE, owns route selection and lifecycle decisions, and owns the evidence ledger. CARE is the default for non-exempt work. Standard allocates a reviewer, elevated allocates a reviewer plus specialist, and critical allocates all three roles; verifier is included and required attempts are respectively 3, 4, and 5.

The **reviewer** makes general assessment against an exact frozen Review Binding. The **specialist** accepts exactly one bounded manager-assigned assurance domain and fails closed on scope mismatch. The **challenger** checks at most five stable typed targets—claim, finding, evidence, and scope—once each, supports a claim challenge without a finding, handles severe inferential findings, and never invents claims, findings, scope, or fixes. Every OpenCode CARE v2 role names candidateDigest, exact changedPaths, diffScope, and acceptanceCriteria, validates matching candidate identity, echoes the complete binding, and returns PASS, FAIL, or INCONCLUSIVE with evidence, findings, claim recommendations, uncertainty, and blockers. CARE therefore records direct, assisted, action, engineering, and assured targets without relabeling its three roles.

The ledger binds target identity, route, risk, required checks, observed outcomes, and invalidation markers. Outcomes describe only the evidence observed for that target; they are not provider-runtime enforcement. Current markers identify the active contract. There are no current fixed-lens aliases. Exact predecessors are recognized only for lifecycle and upgrade handling, never as current identities.

## Provider inventory and limits

OpenCode currently has 13 agents: manager, explore, general, verifier, three CARE v2 roles, and six SDD roles. OpenCode current identity is CARE v2 with Manager58; OpenCode immediate predecessor is CARE-v1 with Manager58; OpenCode next predecessor is CARE-v1 with Manager57; OpenCode v56 is deeper. Codex currently has `AGENTS.md` plus 12 delegated profiles, including three CARE roles; Codex current identity is Manager17, Codex immediate predecessor is Manager16, and Codex Manager15/v14 are deeper. These inventories describe managed documentation identities, not a claim that a host ran them.

CARE lifecycle transitions remain Manager-owned; repository writers do not mutate lifecycle state. Runtime evidence is observed on macOS only. Target-native Windows and Linux behavior remains unverified.

## External evaluation boundary

An independent evaluator outside the repository exclusively owns protected holdout registration, custody, partitioning, contents, labels, graders, digest computation, runs, evidence validation, and adjudication. Only opaque, evaluator-issued, digest-bound evidence may support protected-holdout adjudication.

User-provided, repository-derived, fabricated, placeholder, manifest, or disclosed-holdout material cannot support protected-holdout adjudication. Missing, stale, malformed, mismatched, insufficient, or unavailable evidence is INCONCLUSIVE or BLOCKED, never PASS or VERIFIED. Repository tests establish static conformance only; they do not establish a protected-holdout result.
