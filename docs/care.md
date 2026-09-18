# CARE architecture

Current identities are OpenCode Manager62 and Codex Manager21 (parity OpenCode-v62), rendered from `internal/orchestration/manager_contract.json`. Local receipts bind installed files for updates. Historical agent templates are removed; older unreceipted installations require the [migration bridge](agent-template-migration.md).

CARE roles provide evidence review for the Manager's selected scope. The current shared registry defines their authority; historical prompts remain only in Git history. This is not a Go provider runtime or a new schema/transport surface.

## Roles and candidate binding

The Manager binds the exact frozen candidate, changed paths, scope, acceptance criteria and observed checks. The verifier independently validates that candidate and returns PASS, FAIL, or INCONCLUSIVE. The care-reviewer reports demonstrated findings with severity, path and rationale. The care-specialist examines a bounded domain, separates facts from inferences and does not write. The care-challenger seeks disconfirming evidence for severe inferential conclusions. None approves delivery or changes the candidate.

The current contract requires independent verification and applicable review. It does not embed the predecessor's fixed 3/4/5 attempt budgets, named Execution Brief, mandatory fixed review matrix, or detailed typed challenge schema. Those historical policies must not be described as current enforcement. There are no current fixed-lens aliases. Missing evidence remains INCONCLUSIVE, and a source change invalidates prior candidate evidence.

## Coverage and continuations

Before a CARE mission, the Manager supplies authorized access to the frozen candidate, exact source or diff, sufficient context, criteria, and available evidence. Reviewers confirm that material is inspectable at the start; inaccessible required source is INCONCLUSIVE. A persuasive Manager summary does not replace exact source/diff or bypass permissions.

Review coverage binds each criterion to a usage scenario, evidence or assertion, and stated limits. Relevant flag/default and ambient-state variants are considered proportionately; this is not a universal matrix. Review can extend from a diff through concrete dependencies and newly reachable pre-existing paths. Reviewer, specialist, and verifier reports state result, effective coverage, findings, exclusions, and evidence basis. No findings alone does not establish PASS, and a required criterion without evidence blocks PASS.

After correction, retain prior findings and closure status, justify new coverage, and check closures, regressions, and reachable paths again. A source change invalidates prior candidate evidence; prior findings are context, not approval of a new candidate. Re-evaluate conclusions contradicted by new evidence and classify newly discovered findings as a preexisting omission, correction regression, scope change, or new evidence. Continuations are classified as defect, evidence, mission, access, infrastructure, or scope. They are not retry-until-PASS loops, and echoed task metadata is not technical review. For elevated-risk production changes, assess pertinent capability and identity assumptions before proceeding; this is a risk judgment, not a fixed process.

## Native inventory and limits

OpenCode has seven agents and 12 managed artifacts, including the auto-discovered `plugins/vgxness-memory-lifecycle.ts`, with no `opencode.json` plugin entry. Codex has `AGENTS.md`, six delegated profiles and its marketplace/plugin lifecycle artifacts. Pi uses the same registry with its own native SDK/RPC adapter and TypeScript runtime. Native transport, permissions, authentication and platform support differ; Windows Pi workers remain unsupported.

SDD lifecycle execution is retired. Manager alone owns delivery authority. Deterministic projection tests and local SDK fixtures do not prove selected model behavior. Historical macOS evaluations do not certify a new candidate; independent current-candidate model/platform evidence remains pending.

These are shared instructions and static projection rules, not automatic runtime enforcement, permission repair, or a claim of observed model behavior.

## External evaluation boundary

An independent evaluator outside the repository exclusively owns protected holdout registration, custody, partitioning, contents, labels, graders, digest computation, runs, evidence validation, and adjudication. Only opaque, evaluator-issued, digest-bound evidence may support protected-holdout adjudication.

User-provided, repository-derived, fabricated, placeholder, manifest, or disclosed-holdout material cannot support protected-holdout adjudication. Missing, stale, malformed, mismatched, insufficient, or unavailable evidence is INCONCLUSIVE or BLOCKED, never PASS or VERIFIED. Repository tests establish static conformance only; they do not establish a protected-holdout result.

The active flow uses a short plan when needed. Select a specialist for a concrete elevated domain and a challenger for a material inferential conclusion; do not recreate the retired phase machine through fixed reviewer quotas. Historical SDD records remain archival context only.
