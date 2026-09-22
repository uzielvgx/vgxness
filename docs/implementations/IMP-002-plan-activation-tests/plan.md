# IMP-002 — Plan activation cardinality correction and isolated tests

- **Status:** active
- **Owner:** Manager
- **Workspace:** `/Users/uzielvgx/Development/projects/vgxness`
- **Baseline HEAD:** `96a153ee91ffedd23b64389c0151de622286a3b4` (dirty IMP-001 working tree, observed)
- **Writer nonce:** `imp002-write-20260922-b` (single workspace writer)
- **Created:** 2026-09-22

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
| T04 | Freeze the exact candidate and hand off to independent verification and applicable CARE | AC04 | pending | T03 | frozen candidate identity (HEAD + diff SHA) |
| T05 | Manager commit, installation and configuration authentication (Manager-only) | AC05, AC06 | pending | T04 | Manager-owned; not performed by this worker |
| T06 | Readback closure (Manager-only) | AC06 | pending | T05 | observed installation readback; not performed by this worker |

T04–T06 are outside the worker's authority. This worker prepares the candidate
and the exact commands only; it never performs lifecycle, Git delivery, memory,
independent verification, installation, or closure.

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
- **Limits:** no live model routing, behavioral equivalence, or protected-holdout
  result. Independent verification and CARE are pending (T04).

## Open assumptions

- The Codex native planning tool still has no evidenced name in this repository;
  the provider-neutral fallback remains the asserted behavior.
- Recomputing the OpenCode/Codex goldens is treated as an intentional,
  authorized consequence of the contract change (not a regression).
