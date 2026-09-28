# IMP-005 — Manager adaptive direct work (bounded self-inspection exception)

- **Status:** **closed** — T01–T08 are `done` and recorded. Local document closure (nonce `imp005-close-20260924-i`) records the accepted local source candidate, its independent verification and CARE, and the correction rechecks. `closed` means the local plan slice is complete; it is **not** delivery or installation. No next action is required for the local slice; install is optional and only with future explicit authorization.
- **Owner:** Manager
- **Workspace:** `/Users/uzielvgx/Development/projects/vgxness`
- **Baseline / HEAD:** `c4c6450b779cd39c95a96fc3303bb4cc67ee0291`
- **Writer nonce:** `imp005-plan-20260924-b` (plan); `imp005-impl-20260924-c` (T03–T06 implementation); `imp005-review-correction-20260924-f` (review corrections); `imp005-close-20260924-i` (closure)
- **Recon evidence nonce:** `imp005-adaptive-self-20260924-a`
- **Created:** 2026-09-24
- **Delivery / install:** **excluded**; no `commit`/`push`/PR, no install, no memory. The running agent still serves the previously installed prompt contract (`a890f5453c72f97c30174041b27d28d62d5b60c1bd357b89a754b7d5c60f76ce`), and no runtime change is promised. No SDD.

## Objective

Give the Manager an explicit, shared-policy exception that permits **bounded, necessary direct project inspection and execution** for simple, low-risk, routine tasks — so the Manager may itself start a dev app or perform a local dev database reset without being forced to delegate through `explore`, write a formal Markdown plan/todo, or request CARE — while **preserving** delegation for broad, cross-cutting, independent, parallel, or higher-risk work, the single-writer invariant, the independent verification/CARE gate for substantial work, and the prohibition on bypassing a native deny.

Why it matters: at baseline before IMP-005 the shared policy required the Manager to delegate **all** project code exploration "with no simple exception" and granted only a shell-less operational-inspection authority. That was correct for heavy exploration but imposed disproportionate ceremony on routine local operations, and it left no shared guardrail for a potentially destructive local dev action such as a dev database reset.

## Scope and exclusions

- **In scope:** one bounded exception in the shared Manager policy and its provider-adapted renderings; bounded direct-inspection/execution rules; destructive-operation guardrails; positive and negative regression scenarios/assertions; coherent generated resources/goldens; honest documentation.
- **Excluded:** never weaken or bypass a native `deny`; never broaden any native role permission map; do not edit historic prompt snapshots/reconstruction chains; no install/reinstall; no Git delivery (`commit`/`push`/PR); no memory; no independent verification or CARE (separate missions); no new daemon/broker/DB/external transport; no broadening of the `explore`/worker roles; SDD is not used.
- **Mission history:** T01–T02 were docs-only (no source edits, no tests beyond read-only recon). T03–T06 modified the shared contract, tests, generated resources, and docs as recorded in the tasks table; no install or delivery was performed, and no current-code-unchanged claim is implied.

## Decisions and assumptions

Authoritative decisions live in [progress.md](progress.md). Reading summary:

- Ordinary, short, low-risk operations are **self-inspected** by the Manager; a formal plan/todo/CARE is **not** forced.
- Use **bounded codegraph before the Manager's own reads** when the target is indexed; **docs/configs may be read directly** when codegraph does not index them.
- Preserve **no deny bypass** and the **narrow external-config** read; the exception never expands host permissions.
- **Explicit evidence need not be independently reviewed** for generic/trivial tasks; verification + CARE remain required for substantive behavior changes, cross-cutting work, elevated risk, or delivery.
- **Delegation is chosen**, not mandatory, when work is broad multi-symbol, cross-cutting, independently parallelizable, or higher risk; the **single writer** is preserved.
- A potentially destructive local dev operation requires verifying the **exact project / environment / database / data-loss boundaries** first; only an **unresolved consequential decision** is asked; **production/reset authority is never inferred**.

## Acceptance criteria

- **AC01** A low-risk routine Manager task (e.g. start a dev app; local dev DB reset) is permitted as **direct bounded inspection/execution** with **no forced delegation**, **no formal plan/todo**, and **no CARE**; the exception is explicitly bounded, disclosed, and reversible.
- **AC02** Delegation to `explore` (deep/parallel) is **chosen** for broad multi-symbol, cross-cutting, independently parallelizable, or higher-risk work; the **single-writer** invariant is preserved (at most one code-workspace writer; plan files reserved to the Manager).
- **AC03** A potentially destructive local dev operation (e.g. dev DB reset) verifies the **exact project, environment, database, and data-loss boundaries** before acting; only an **unresolved consequential decision** is put to the user; the Manager **never infers production or reset authorization** from a dev-scoped request.
- **AC04** The shared policy renders with **parity** across OpenCode, Codex, and Pi from the single shared source, **without permission-map escalation** and **without editing historic prompt snapshots/reconstruction chains**.
- **AC05** Regression coverage asserts both **positive** (routine direct work allowed) and **negative** (no accidental blanket delegation; no reckless no-inspection/no-verification) behavior; any generated resource/golden change is coherent and passes generator `--check` and `verify-package`.
- **AC06** Substantial work keeps durable Markdown plans with stable AC/task IDs and TDD semantics (RED-first where a reproducer is feasible); independent verifier + CARE run on the **same exact candidate**; delivery/install stays excluded.
- **AC07** Documentation states behavior honestly and is **not** treated as proven by substring/regex tests alone.

## Tasks

Task state is authoritative in this table. Decisions are authoritative in [progress.md](progress.md); evidence is in [validation.md](validation.md).

| ID | Task | AC | State | Dependencies | Required evidence |
| --- | --- | --- | --- | --- | --- |
| T01 | Recon: current policy, provider renderings, permissions | AC01, AC02 | done | — | read-only recon; explore nonce `imp005-adaptive-self-20260924-a` |
| T02 | Author this plan/progress/validation set + index row | AC06, AC07 | done | T01 | this records set persisted; index row `active` |
| T03 | Shared policy + provider adaptation (source) | AC01, AC02, AC03, AC04 | done | T02 | shared contract `manager.instructions` (Adaptive flow / Evidence and delegation / Planning) + OpenCode current adapter only; Pi resources regenerated; Codex render uses the shared contract unchanged; no permission-map change |
| T04 | Scenarios / assertions / goldens generated | AC05 | done | T03 | 38-case corpus (positive + `absent` cases); OpenCode 28-hash golden + Codex `AGENTS.md` golden recomputed; Pi scenario count/`expect`; generator `--check`; `verify-package` |
| T05 | Docs updated to the as-built behavior | AC04, AC07 | done | T03 | provider/architecture/index docs match the rendered policy; capability-honest limits |
| T06 | Developmental checks | AC01, AC05 | done | T04, T05 | gofmt, generator `--check`, `verify-package`, required Go packages, Pi typecheck/tests, broader cli/mcp/config — exact commands and results in `validation.md` |
| T07 | Freeze + independent verification + CARE (same exact candidate) | AC06 | done | T06 | verifier `imp005-verify-d` PASS (full Go 7 packages; Pi 174/typecheck/generator/verify-package) and CARE `imp005-care-e` PASS AC01–AC07 on the accepted source; corrected candidate rechecks `imp005-verify-correction-g` and `imp005-care-correction-h` PASS closures; Go sources unchanged from the verified build |
| T08 | Closure records | AC06 | done | T07 | this closed plan/progress/validation set and the index row; **local document closure only — delivery/install excluded** |

All delivery/install remain **excluded**; a source change invalidates prior acceptance evidence. This closure is documentation, not delivery, installation, receipt, or readback.

## Open assumptions

- The exception can be expressed in the shared contract without changing any native role permission map; parity is enforced by the existing shared-contract tests.
- Codegraph indexing is available for the relevant source; docs/config reads are the documented fallback when it is not.
- "Routine/low-risk" is bounded by the enumerated operational scope and does not extend to production, delivery, or destructive non-dev targets.
- The OpenCode `version: 62` and Codex `version: 21; parity: opencode-v62` markers stay deliberately unchanged: the canonical contract digest is the real identity binding (OpenCode/Codex agent files and the Pi `sourceDigest`), so no label bump is needed and no stale parity mismatch arises because both providers render the same changed digest.

## Limits

T01–T02 performed no source, test, runtime, install, or Git action. T03–T06 changed source, tests, generated resources, and docs and ran local developmental checks; the independent verifier and CARE rechecks were performed by other roles on the frozen candidate. No install, Git delivery, memory, or live model run occurred. No live behavior (dev app start, database reset) was exercised, no installed activation/receipt/readback was observed, and the running agent still serves the old installed prompt contract. No new permissions are granted; a plan grants no authorization, and closure is not delivery.
