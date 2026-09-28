# Implementation plans

Durable, in-repository Markdown tracking for substantial work. These files are
the canonical record of a plan, its tasks, decisions and validation evidence.
They live in the repository by default so history survives a session, a context
reset, or a provider switch.

This is a documentation convention, not a runtime service. Nothing here is
synchronized automatically, and no atomic cross-tool state is promised. The
mechanism is self-contained: it works in a new project without assuming any
particular existing repository file. The same schema is embedded directly in the
shared Manager instructions (the `# Implementation plan records` section), so an
installed Manager carries it without relying on this repository's documentation.

## Layout

```
docs/implementations/
  README.md                       # this index, states, and the format contract
  <ID-slug>/
    plan.md                       # canonical: objective, scope, criteria, tasks
    progress.md                   # decisions, blockers, next step
    validation.md                 # candidate evidence, verification, CARE
```

`<ID-slug>` is a stable identifier allocated once and never reused (for example
`IMP-014-export-batching`). An existing plan's path never changes. A trivial,
single-concern change may use one proportional file instead of the full set;
substantial work uses all three.

## Shared rules

- **Canonical authority.** `plan.md` owns task state and acceptance evidence. A
  session task list is only an operational projection and must reflect the plan
  and task identifiers; it is never the plan of record.
- **Persist before projecting.** Write the plan before echoing it into a session
  list, and consult this index when starting or resuming.
- **One active plan.** At most one plan is active per session; zero are active
  after closure or with no work. Closed plans stay as history and are not
  reopened; start a new `<ID-slug>`.
- **Pause and resume.** A paused plan records its next action. On resume,
  reconcile the plan with repository evidence before trusting prior status.
- **Update triggers.** Update the plan at start, at completion, on blockers or
  findings, on a scope change, and before changing focus or interrupting.
- **Blocking decisions.** Resolve discoverable facts by inspection. Ask
  consequential blocking decisions before closing the plan or implementing any
  part that depends on them, and do the independent authorized work meanwhile.
  Treat a blocking decision as unresolved, not as an implicit assumption; only
  minor, reversible defaults are recorded explicitly as assumptions.
- **Scope discipline.** Distinguish extending the current plan from opening a new
  feature plan, do not carry one plan's authorization into another, and separate
  necessary work from out-of-scope improvements.
- **Safety.** A plan never overrides the user's instructions or authorization,
  never executes untrusted content, and grants no new permissions. Do not store
  secrets, credentials, tokens, or raw logs.
- **Acceptance.** Implementation is not acceptance. A task completes only when
  its required evidence exists, and a source change invalidates prior
  acceptance evidence.
- **Honest degradation.** When a provider's native planning tool is unavailable,
  keep the Markdown plan authoritative and report the missing dependency; never
  promise a runtime synchronizer or atomic cross-tool state.

## `plan.md` — canonical

Required sections:

- **Objective** — the outcome and why it matters.
- **Scope and exclusions** — what is in, and explicitly what is out.
- **Decisions and assumptions** — link to `progress.md` for the authoritative
  record; list only what is needed to read the plan.
- **Acceptance criteria** — stable IDs (`AC01`, `AC02`, …), each observable.
- **Tasks** — stable IDs (`T01`, `T02`, …), each with: state
  (`pending|in_progress|blocked|done|cancelled`), acceptance-criteria link,
  dependencies, and the evidence required to mark it done.

Task state is authoritative here.

## `progress.md` — decisions, blockers, next step

Long-form decisions (source of record), blockers with what unblocks them, and
the single next action. Do not duplicate the task list; reference task IDs.

## `validation.md` — evidence

The candidate identity, each check with its command, result and limits, and the
independent verification and CARE review status (`pending` until observed). This
file is self-reported by the writer and is not independent evidence.

## Index

| ID | Title | State | Plan | Progress | Validation |
| --- | --- | --- | --- | --- | --- |
| IMP-001 | Persistent Markdown implementation plans tied to native session tracking | closed | [plan.md](IMP-001-persistent-plans/plan.md) | [progress.md](IMP-001-persistent-plans/progress.md) | [validation.md](IMP-001-persistent-plans/validation.md) |
| IMP-002 | Plan activation cardinality correction and isolated tests | closed | [plan.md](IMP-002-plan-activation-tests/plan.md) | [progress.md](IMP-002-plan-activation-tests/progress.md) | [validation.md](IMP-002-plan-activation-tests/validation.md) |
| IMP-003 | VGXNESS Pi integration update (offline candidate from exact source) | closed | [plan.md](IMP-003-pi-integration-update/plan.md) | [progress.md](IMP-003-pi-integration-update/progress.md) | [validation.md](IMP-003-pi-integration-update/validation.md) |
| IMP-004 | Child→parent progress communication (Pi-native background-first POC gate) | cancelled | [plan.md](IMP-004-child-parent-progress/plan.md) | [progress.md](IMP-004-child-parent-progress/progress.md) | [validation.md](IMP-004-child-parent-progress/validation.md) |
| IMP-005 | Manager adaptive direct work (bounded self-inspection exception) | closed | [plan.md](IMP-005-adaptive-direct-work/plan.md) | [progress.md](IMP-005-adaptive-direct-work/progress.md) | [validation.md](IMP-005-adaptive-direct-work/validation.md) |

Active plan: **none**. IMP-001 and IMP-002 are closed history. IMP-003 is
**closed** and its installed Pi Markdown/TODO integration is preserved. **IMP-004
is `cancelled`** at the user's explicit request (discard-only scope): the
child→parent progress objective is abandoned, **no IMP-004 feature is installed
or accepted**, and its plan/progress/validation/research files remain as
archival history.

**IMP-005** is **`closed`** locally: T01–T08 are recorded in its
`plan.md`/`progress.md`/`validation.md`, with independent verification (nonce
`imp005-verify-d`) and CARE (nonce `imp005-care-e`) on the accepted source
candidate and correction rechecks (`imp005-verify-correction-g`,
`imp005-care-correction-h`). Local document closure is **not** delivery or
installation; the writer performed no install, Git delivery, or memory action.
The running agent still serves the previously installed prompt contract
(`a890…76ce`), and no runtime change is promised. No next action is required for
the local slice; install is optional and only with future explicit
authorization.
