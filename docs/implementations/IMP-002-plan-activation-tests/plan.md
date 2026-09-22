# IMP-002 — Plan activation cardinality correction and isolated tests

- **Status:** closed (Manager local declarative acceptance; see [progress.md](progress.md) and [validation.md](validation.md))
- **Owner:** Manager
- **Workspace:** `/Users/uzielvgx/Development/projects/vgxness`
- **Historical baseline:** `96a153ee91ffedd23b64389c0151de622286a3b4` (set at plan start; not a stale current freeze)
- **Current candidate:** `b0e6f266e1eec20441696672a162b6fcd5e9a093` (local commit, 32 files; source exact prior diff `909cc…`)
- **Active launcher:** `4057b712…` (previous `90a9aa78…`); OpenCode `dde8e333…`; Codex `8768b1…` — all installed, `changed=false`
- **Writer nonces:** `imp002-write-20260922-b`; live `imp002-live-20260922-g`; recovery `imp002-evidence-recovery-20260922-n`; close `imp002-close-20260922-p`
- **Created:** 2026-09-22
- **Closed:** 2026-09-22 (independent liveverify `imp002-liveverify-20260922-o` PASS; source verify+CARE as previously recorded)

## Authorization and scope

The user authorized the correction of the F3 plan-cardinality wording, local
tests, and preparation of the non-Git-delivery steps (installation and
configuration authentication), with no live model calls, no network install, no
provider mutations, no self-install, and no Git operations performed by this
worker. Scope is local only:

- In scope: the shared contract, the OpenCode/Codex/Pi rendered resources, the
  development corpus, the affected provider/e2e/Pi tests and goldens, and the
  touched docs.
- Out of scope for this worker: Git delivery, installation, lifecycle calls,
  durable memory, independent verification, and CARE (Manager/verifier-owned).
- Exploration is not delegated; edits are `apply_patch`-style.

## Objective

Make the "one active plan" summary consistent with the specific plan-state rule
that already governs the schema: a session keeps **at most one** active plan and
**zero** active plans when there is no work. The correction must reach every
current rendered resource (OpenCode, Codex, Pi) coherently, must preserve the
schema and the blocking-decision authority, and must leave historical snapshots
(pre-adaptive/bootstrap and IMP-001 records) byte-intact.

## Scope and exclusions

- In: the cardinality sentence in `manager.instructions`, the OpenCode/Pi native
  adapters, the corpus fragment, the touched docs, and the affected tests.
- In: negative tests proving the ambiguous phrase is absent from current prompts.
- Out: schema version, role inventory, model markers, blocking-decision clauses,
  permission maps, artifact counts, bootstrap/pre-adaptive snapshots, IMP-001
  history, live host execution, and delivery.

## Decisions and assumptions

Decisions are authoritative in [progress.md](progress.md). Reading summary:

- The schema clause "exactly one plan is active while executing, at most one is
  active otherwise, and zero are active after closure or with no work" already
  encodes the correct cardinality and is preserved unchanged.
- Only the *summary* sentence is corrected to "at most one active plan per
  session" plus an explicit zero-when-no-work qualifier.
- The corpus fragment is updated to the new phrase; the case count stays 23 and
  no new cases are added.

## Acceptance criteria

- **AC01** Cardinality wording is uniform across the shared contract, the
  OpenCode/Codex/Pi current projections, the corpus, and the touched docs: at
  most one active plan per session, zero when there is no work. The embedded
  schema and the blocking-decision authority are preserved unchanged.
- **AC02** The OpenCode, Codex and Pi current resources remain coherent
  (identical shared Manager text and digest chain); historical snapshots
  (pre-adaptive/bootstrap and IMP-001 records) stay byte-intact.
- **AC03** Negative tests assert the ambiguous summary ("exactly one active plan
  per session") is absent from the current prompts while the corrected phrase is
  present; the corpus count stays 23 with no new cases.
- **AC04** The exact frozen candidate passes the authorized checks, and isolated
  installation trials are proposed/run without touching any real host.
- **AC05** Any live-test proposal states explicit cost, count, and time limits.
- **AC06** No claim of installed/verified/delivered is made without the
  corresponding observed readback; this worker performs none of them.

## Tasks

Task state is authoritative in this table. Decisions are authoritative in
[progress.md](progress.md); evidence is in [validation.md](validation.md).

| ID | Task | AC | State | Dependencies | Required evidence |
| --- | --- | --- | --- | --- | --- |
| T01 | Research: confirm F3 scope, canonical sources, generated chain, and affected tests/goldens | AC01, AC02 | done | — | `grep` map of the phrase, generator/reader paths, corpus and golden locations |
| T02 | Correction: uniform cardinality wording in contract, adapters, corpus and docs; keep schema, blocking authority, and history intact | AC01, AC02 | done | T01 | changed-path list and the exact edits (see [validation.md](validation.md)) |
| T03 | Checks and isolated tests: regenerate Pi resources; `gofmt`, Go suites, Pi typecheck/tests, broader `go test -short`/`go vet`; recompute affected goldens; add negative assertions | AC02, AC03, AC04 | done | T02 | command output per check; regenerated resource equality; golden recomputation (see [validation.md](validation.md)) |
| T04 | Freeze the exact candidate; independent verification and applicable CARE | AC04 | done | T03 | verifier `imp002-verify-20260922-e` PASS (35 Go short, 7 fresh, vet, Pi 174/typecheck/generators); CARE `imp002-care-20260922-f` PASS (source/preflight, no blocker) — see [validation.md](validation.md) |
| T05 | Manager commit, installation and configuration authentication (Manager-only) | AC05, AC06 | done | T04 | local commit `b0e6f26` (32 files); self+provider install; intermittent Codex status root cause UNPROVEN, not fixed, no rollback — see [validation.md](validation.md) |
| T06 | Live behavior probe and installation readback closure (Manager-owned) | AC05, AC06 | done | T05, T07 | live probe captured + independent liveverify `imp002-liveverify-20260922-o` PASS (self `4057b712…`, OC `dde8e333…`, Codex `8768b1…`, all installed `changed=false`) — see [validation.md](validation.md) |
| T07 | Bounded recovery probe: one extra OpenCode invocation (budget 6→7) on the existing session to re-project the current plan | AC03, AC05 | done | T05 | sanitized report v2 plus probe observation — see [validation.md](validation.md) |

T04–T06 are Manager-owned; their results are recorded here as evidence, not
re-produced by this worker. T07 is the single bounded worker recovery probe
authorized for this mission (budget explicitly increased once from 6 to 7, fixed
300 s timeout); it never performs lifecycle, Git delivery, memory, installation,
delegation, or closure. The original OpenCode turn-3 timeout remains
INCONCLUSIVE history and is preserved even though the recovery probe later
succeeds; recovery is not treated as a retroactive PASS of the original run.
All tasks are done and the plan is closed; closure is Manager local declarative
acceptance of the bounded evidence, not a runtime guarantee.

## Evaluation design (agent-evaluation guidance)

Skill applied: `agent-evaluation`, `/Users/uzielvgx/.agents/skills/agent-evaluation/SKILL.md`,
sha256 `0cc075f59b6c6e3afcbfcd6a2695398f599266b0b1ff70e5ebbdf0244ad694bc`
(re-validated against source before reading).

- **Target/decision:** the shared Manager contract and its native projections;
  decide whether the corrected cardinality wording is present and the ambiguous
  summary absent in the exact frozen projection.
- **Development corpus:** `internal/orchestration/testdata/manager-scenarios.json`
  (deterministic contract-conformance assertions), a development partition.
  Protected holdouts are untouched and not described here.
- **Assertions:** deterministic substring assertions for the corrected phrase,
  plus explicit negative assertions that the ambiguous summary is absent, so a
  positive match cannot mask the contradiction.
- **Limits:** no feature-implementation or protected-holdout result. Independent
  verification and CARE are recorded as done (T04); live behavior is a bounded
  diagnostic only.

## Open assumptions

- The Codex native planning tool still has no evidenced name in this repository;
  the provider-neutral fallback remains the asserted behavior.
- Recomputing the OpenCode/Codex goldens is treated as an intentional,
  authorized consequence of the contract change (not a regression).

## Closure and residual (bounded scope)

- **Closure:** local, declarative, Manager-accepted; **not** a runtime guarantee.
- **Retractions:** a package-vs-file hash difference is not a defect; `Status`
  never performs a generic `ErrRecovery` rollback. The intermittent Codex status
  is real with an **unknown root, not fixed** (a later healthy state does not
  erase the earlier observation).
- **Live scope:** 7 model-host calls (4 OpenCode, 3 Codex) using the existing
  authenticated login only; no additional services installed. Elapsed listed
  separately: initial 851.82 s; recovery 99.6 s. Cost not measured.
- **Residual / optional future diagnostics (not active work):** intermittent
  Codex status; Codex session-scoped absence of a native planning tool; the two
  original timeouts retained as INCONCLUSIVE history; automatic/new-session full
  feature implementation **NOT tested** (a future model may differ). No
  protected-holdout or every-time claim.
- **Provenance:** source/repo 32-file commit `b0e6f26`; the upcoming records
  commit is Manager-owned and its hash is unknown here (not invented).
- **Guides:** `agent-evaluation` (writer) and `installer-lifecycle` (Manager).
