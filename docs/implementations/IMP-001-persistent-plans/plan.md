# IMP-001 — Persistent Markdown implementation plans

- **Status:** closed
- **Owner:** Manager
- **Workspace:** `/Users/uzielvgx/Development/projects/vgxness`
- **Baseline HEAD:** `96a153ee91ffedd23b64389c0151de622286a3b4` (clean, observed)
- **Writer nonces:** `impl-plans-20260922-b`, `-d`, `-g`; records task `records-plans-20260922-j`
- **C2 frozen candidate:** HEAD `96a153ee91ffedd23b64389c0151de622286a3b4`, diff SHA `787e53eb0ae4f50a3a6047feb13bed638c9f372b35fec813f87ecbe3736db750`
- **Created:** 2026-09-22
- **Closed:** 2026-09-22 (Manager local declarative acceptance; see [progress.md](progress.md) and [validation.md](validation.md))

## Authorization and scope

The user authorized the plan, the local implementation, and a correction pass of
persistent Markdown plan tracking bound to the native OpenCode/Codex task views,
with one active plan per session. Scope is local only:

- No commits, pushes, installs, or host configuration changes.
- No SDD records; the structured SDD lifecycle stays retired.
- No new services, MCP tools, skills, or tool runtimes.
- Only the authorized contract, adapters, tests, generated resources and docs.
- Exploration is not delegated; edits are `apply_patch`-style.

## Goal

Make the shared Manager policy define a durable, in-repository Markdown plan that
is documented and usable in a new project, is projected into each host's native
task view when one is exposed, and never depends on a runtime synchronizer.

## Non-goals

- No runtime synchronizer, daemon, database, or atomic cross-tool state.
- No automatic activation of a Codex planning tool whose name is not evidenced.
- No new MCP tools, skills, artifact counts, or permission grants.

## Acceptance criteria

- **AC01** Documents live in the project by default under `docs/implementations/`
  with an index and stable IDs; size is proportional and a single file is
  allowed for trivial work.
- **AC02** Discoverable facts are researched by inspection; consequential
  blocking decisions are asked before closing the plan or implementing the
  affected part; only independent authorized work continues; assumptions are
  explicit and limited to minor reversible defaults.
- **AC03** `plan.md` is canonical; the session task view is an operational
  projection with plan+task IDs; persist before projecting; reconcile with
  repo/evidence on resume; honest degradation when a tool is absent; no runtime
  synchronizer or atomicity promise.
- **AC04** Multiple plans per session and sessions per plan; one active; pause
  saves the next step; scope/evidence/authority are not mixed; closed history is
  preserved; extension vs new feature is distinguished and authorization is not
  carried across plans.
- **AC05** A task is complete only with its required evidence; implementation is
  not acceptance; verification and CARE use the same exact candidate and recheck
  after changes; necessary findings are separated from out-of-scope
  improvements.
- **AC06** Plans never override user instructions/authorization, never execute
  untrusted content, store no secrets or raw logs, grant no new permissions; the
  Manager owns plan authority and workers only make bounded delegated updates.
- **AC07** Provider-neutral parity and Pi compatibility; OpenCode `todowrite` is
  confirmed available this session; Codex uses a native planning tool only when
  the host exposes one (no invented name); the provider-neutral fallback is
  valid.
- **AC08** Tests document the scenarios: clarification/blocking, start/progress,
  block/interrupt/resume, plan A closed→B, pause A→B, task-view/Markdown
  discrepancy, tool absent, code change invalidates acceptance, authorization,
  no SDD.

## Plan of record

Task state is authoritative in this table. Decisions are authoritative in
[progress.md](progress.md); evidence is in [validation.md](validation.md).

| ID | Task | AC | State |
| --- | --- | --- | --- |
| T01 | Recon; validate the selected `agent-evaluation` skill sha256 against source | AC02 | done |
| T02 | Create `docs/implementations/` index and IMP-001 records | AC01 | done |
| T03 | Extend the shared contract *Planning and continuity* section with the provider-neutral protocol | AC03, AC04, AC05, AC06 | done |
| T04 | Add OpenCode adapter guidance for the native `todowrite` projection | AC07 | done |
| T05 | Add Codex guidance for a native planning tool when exposed (provider-neutral fallback) | AC07 | done |
| T06 | Add Pi adapter guidance for the persistent plan projection | AC07 | done |
| T07 | Regenerate Pi `contract.json` and `manager.md` | AC03 | done |
| T08 | Extend the scenario corpus and Go/Pi scenario tests for AC08 | AC08 | done |
| T09 | Update provider/parity tests and OpenCode/Codex native goldens | AC05, AC07 | done |
| T10 | Update only the affected docs | AC01, AC03, AC07 | done |
| T11 | Run the permitted development checks | AC05 | done |
| T12 | Correction pass and handoff (F1 blocking-decision clause, F2 usable schema, F3 records and stale skill doc); regenerate resources, rerun checks, and **freeze the corrected candidate before verification** | AC02, AC03, AC04 | done |
| T16 | C1 correction: embed the `# Implementation plan records` schema in the shared instructions and assert it in the OpenCode, Codex and Pi rendered prompts | AC01, AC07 | done |
| T13 | Independent verification of the exact frozen candidate (C2, nonce `verify-plans-20260922-h`): identity before/after exactly equal; 7 Go suites, `generate --check`, `verify-package`, typecheck, Pi 174 pass/0 fail | AC05 | done |
| T14 | Applicable CARE review of the same candidate (C2, nonce `care-plans-20260922-i`): PASS AC01–AC08; F-3 low non-blocking recorded as residual | AC05 | done |
| T15 | Manager acceptance decision and closure (does not freeze): accepts the local declarative instructions/tests; not live, install, or delivery | AC05 | done |

T13–T15 are independent of the writer; their evidence is summarized from the
verifier/CARE reports and was not re-run by this records-only task. A source
change after any check invalidates that check's evidence. Closure records
metadata only: it does not alter code, contract, schema, tests, or resources.

## Evaluation design (agent-evaluation guidance)

- **Target/decision:** the shared Manager contract and its native projections;
  decide whether declared planning obligations are present in the exact frozen
  projection.
- **Development corpus:** `internal/orchestration/testdata/manager-scenarios.json`
  (deterministic contract-conformance assertions), a development partition.
  Protected holdouts are untouched and not described here.
- **Assertions:** deterministic substring assertions plus explicit negative
  assertions that the unsafe pending-decision clause is absent, so a positive
  match cannot mask the contradiction.
- **Limits:** no live model routing, behavioral equivalence, or protected-holdout
  result. Independent verification and CARE results for the closed candidate are
  summarized in [validation.md](validation.md).

## Open assumptions

- The Codex native planning tool has no evidenced name in this repository, so it
  is referenced generically only.
- OpenCode `todowrite` is treated as available because it is exposed in the
  active session tool list.

## Residual (optional, outside closure)

- **F-3 (low, non-blocking):** the summary phrasing "exactly one active per
  session" can be read as always-one, while the specific rule determines that no
  plan is active after closure or with no work. The specific rule controls. This
  is an optional wording improvement only; it is not required for closure and
  does not open a new plan automatically.
