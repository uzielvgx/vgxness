# IMP-005 — Progress

Authoritative record of decisions, blockers, and the next action. Canonical task
state lives in [plan.md](plan.md) and is **not** duplicated here; evidence lives in
[validation.md](validation.md).

## Decisions

- **D1 — bounded direct-work exception (default).** Ordinary, short, low-risk
  operations (start a dev app, local dev DB reset, small bounded reads) are
  performed by the Manager via **direct bounded inspection/execution**; no forced
  `explore` delegation, no formal plan/todo, no CARE. The exception is bounded,
  disclosed, and reversible. (AC01)
- **D2 — bounded codegraph before own reads.** When the target is indexed, use a
  bounded codegraph query before the Manager's own reads; escalate to delegated
  deep/parallel exploration only when the query is insufficient or the work is
  broad. (AC01, AC02)
- **D3 — docs/configs when unindexed.** Docs and configuration files that codegraph
  does not index may be read directly (bounded); this is not general project
  exploration. (AC01)
- **D4 — no deny bypass / narrow external config.** The exception never bypasses a
  native `deny`, never broadens any permission map, and keeps the external-config
  read narrow (single active host binding). (AC04)
- **D5 — explicit evidence need not be reviewed for generic tasks.** Evidence
  supplied explicitly for a decision may be used directly; independent verification
  + CARE are not universal for trivial edits and remain required for substantive
  behavior changes, cross-cutting work, elevated risk, or delivery. (AC01, AC06)
- **D6 — delegation is chosen.** Delegate when the work is broad multi-symbol,
  cross-cutting, independently parallelizable, or higher risk, or when deep/parallel
  exploration is worthwhile; preserve the single-writer invariant (at most one
  code-workspace writer; plan files reserved to the Manager). (AC02)
- **D7 — destructive local-op guardrails.** Before a potentially destructive local
  dev action (e.g. dev DB reset), verify the **exact project, environment, database,
  and data-loss boundaries**; ask only about an **unresolved consequential
  decision**; **never infer** production or reset authorization from a dev-scoped
  request. (AC03)
- **D8 — parity without escalation.** Change the shared policy once at its source
  and regenerate the OpenCode/Codex/Pi renderings; do not edit historic prompt
  snapshots/reconstruction chains and do not escalate permission maps. (AC04)
- **D9 — docs-only first slice.** This mission (`imp005-plan-20260924-b`) authors
  the plan records + index row only: no source edits, no tests, no install/Git/
  memory. (AC07)
- **D10 — honest evidence.** Behavior is not "proven" by substring tests; T06 records
  exact commands and limits, and T07 binds verifier/CARE to the same exact
  candidate. (AC07)
- **D11 — adaptive policy implemented at the source (T03).** The shared
  `manager.instructions` now classify the request, permit bounded direct
  inspection/execution for a simple low-risk authorized task with no mandatory
  delegation/plan/task list/skill load/CARE, choose delegation for broad,
  parallel, uncertain, cross-cutting, higher-risk, or independently-reviewed work,
  keep `explore` read-only for bounded missions with an explicit child nonce and
  criteria, and add the destructive local-operation guard. Only the OpenCode
  current adapter changed; the Codex render already consumes the shared contract
  and its adapter text did not conflict. (AC01–AC04)
- **D12 — no marker bump.** The OpenCode `version: 62` and Codex
  `version: 21; parity: opencode-v62` markers stay unchanged, following IMP-001
  D1: the canonical contract digest (`5ebfbe03243918ebc81778c6970de8e877be47c008efdf40809c67cf68b47e99`
  after this change) is the identity binding, and both providers render the same
  digest, so parity holds without a label change. (AC04)
- **D13 — regression coverage, not observed TDD.** No expected failure was observed
  before the source change, so the new corpus and assertions are documented as
  deterministic regression coverage rather than observed RED evidence. (AC05, AC07)
- **D14 — capability-honest, provider-neutral wording.** The contract says "use
  available native inspection tools" and report an unavailable capability rather
  than inventing another host's tools, so a provider whose inspection tooling
  differs (Codex/Pi) degrades honestly. `packages/pi/src/orchestration/adapter.ts`
  was not edited: it contained no conflict with the direct-work exception. (AC04)
- **D15 — destructive local-op guard is policy only.** The reset guard is contract
  text; no database was reset and no reset CLI was added. (AC03)
- **D16 — local closure (T07/T08).** Independent verifier `imp005-verify-d`
  returned **PASS** on the accepted local source candidate (full Go 7 packages;
  Pi 174/typecheck/generator/verify-package), and CARE `imp005-care-e` returned
  **PASS** on AC01–AC07, flagging two low findings (F1 stale Pi test title,
  F2 present-tense plan motivation). Corrections were applied under
  `imp005-review-correction-20260924-f`; the correction verifier
  `imp005-verify-correction-g` and correction CARE `imp005-care-correction-h`
  returned **PASS** with the F1/F2 closures, and the remaining F3 info finding —
  that the limitations assertions check only `evidenceKind`/`partition` — was
  recorded as non-blocking. T07 and T08 are recorded `done` in
  [plan.md](plan.md). This is **local document closure only**: no install, Git
  delivery, or memory occurred; the running agent still serves the previously
  installed prompt contract (`a890…76ce`) and no runtime change is promised.
  (AC06, AC07)

## Blockers

- **None.** T01–T08 are recorded; no local blocker remains. Delivery, install,
  and memory are outside this closed slice.

## Single next action

**None required for the local slice.** The IMP-005 plan is closed and the index
has no active plan. Installing the change is optional and only with future
explicit authorization; the running agent is unchanged until then. A subsequent
source change invalidates this closure and its prior acceptance evidence.
