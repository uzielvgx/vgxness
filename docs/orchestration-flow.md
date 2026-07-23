# VGXNESS Orchestration Flow

This document owns the request lifecycle, gates, phase operating modes, and recovery contract. The [VGXNESS Product Blueprint](product-blueprint.md) is the canonical product vision, taxonomy, status, and roadmap.

| Status | Current-candidate evidence and boundary |
| --- | --- |
| **Implemented** | Local binary/storage/memory; runtime contract validation; Chronicle events, task-state derivation, immutable crash-atomic snapshot publication and terminal repair; exact Registry/Gatekeeper decisions; prompt/provider runner; finite coordinator; OpenCode execution; strict bridge/setup; tests and receipts. |
| **Partial** | The delivered coordinator exposes status/read/write/review plus adaptive native orchestration rather than the full Navigator/SDD lifecycle. Explicit `start`/`continue`/`finish` preserves one foreground run. Native Navigator planning, validated plans, deterministic dependency waves, file-backed owner/epoch authority, prerequisite-gated task admission, prepared replay, checkpoint takeover, fail-closed dispatch classification, partial-failure stops, persisted results, authoritative joins, and lifecycle controls are implemented. Chronicle plan/artifact projection and full live SDD supervision remain planned. |
| **Contracts-only** | Schemas and rules for broader routing, SDD phases, artifact stores, continuity capsules, and event types not exercised by the delivered path. |
| **Planned** | Navigator intent/routing, complete SDD workflows, richer approval/autonomy UX, artifact/checkpoint lifecycle, and additional providers/adapters. |

**Implemented, bounded:** VGXNESS coordinates contract-validated executions through a provider-neutral runner, exact Registry identity, Gatekeeper policy, finite coordinator, and native OpenCode child sessions. `vgxness_orchestrate` creates a tool-denied Navigator child, validates its proposal, persists the approved plan, creates one native child per legal task, overlaps independent reads, carries bounded dependency results forward, and publishes one durable join. The file-backed authority verifies prerequisites, reserves logical slots, persists prepared replay and uncertain markers, accepts terminals, and fences stale owners through monotonic epochs. No automatic redelivery occurs. `vgxness orchestrate status|resume|cancel|explain` provides recovery controls without making them happy-path ceremony. Delivery Authority issues immutable content-bound receipts and validates the same receipt at four lifecycle gates. **Planned:** automatic delivery integration, complete SDD artifacts/checkpoints, richer UI, and additional adapters. OpenCode is the first implemented runtime adapter, not a core domain dependency.

**Implemented:** The VGXNESS-owned SQLite/FTS5 `MemoryStore` provides local semantic persistence; Registry, Gatekeeper, provider execution, bounded coordination, and crash-atomic Chronicle snapshot publication operate on validated contracts. **Partial:** Chronicle recovery remains bounded rather than implementing the future general checkpoint/artifact lifecycle. Optional Engram compatibility and full Navigator/SDD orchestration remain planned.

Navigator, Scout, Blueprint, Forge, Sentinel, and optional Challenger are product capabilities; Registry, Chronicle, and Gatekeeper are deterministic services. The explore, propose, spec, design, tasks, apply, verify, archive, fix, and recovery agents described here are operating modes that use those capabilities, not a competing capability taxonomy.

In this workflow, “manager” or “orchestrator” denotes Navigator's coordinating context; it is not an additional product capability.

## Quick path

The flow below is the **target full-product lifecycle**. The current binary implements its bounded execution core plus the pure Navigator plan/wave/join foundation—**contract validation → Chronicle events/snapshots/task state → Gatekeeper/Registry → provider → finite coordination → deterministic delegation waves**—but not the manager-facing adaptive runtime, selective SDD, or general artifact stores.

```text
User request
  -> manager negotiates required capabilities and records provider selection
  -> manager opens or resumes a run
  -> manager records routing rationale and selective SDD preflight
  -> manager asks a structured blocking question when required
  -> agent receives a thin context packet and exact versioned skill refs
  -> foreground agent executes bounded work sequentially
  -> optional background agents perform read-only advisory work
  -> agent returns structured result and immutable artifact refs
  -> manager validates result, records events, and handles gates
  -> manager writes a continuity capsule or terminal loop reason
  -> manager returns a short summary in the user's language
```

`vgxness status`, `vgxness doctor`, bridge status/dispatch, guided OpenCode setup, bounded orchestration, and recovery projection are **Implemented**. A general `run inspect` UX and full Navigator/SDD recovery workflow are **Planned**; inspection remains debugging/auditing rather than mandatory ceremony.

## Core decisions

| Area | Decision |
| --- | --- |
| Manager role | **Implemented/Partial:** The manager routes one-shot work or a validated adaptive plan, enforces native-child boundaries, and summarizes the authoritative join. Full SDD supervision remains planned. |
| Subagent role | **Implemented, bounded:** A hidden, tool-denied `navigator` proposes the task graph; hidden native `explorer`, `implementer`, and `reviewer` profiles execute one authorized operation and return a validated structured result. Capability-specific SDD agents remain planned. |
| Subagent nesting | **Implemented, bounded:** Each native child has `task: deny`; missing authority is reported instead of recursively delegating. |
| Operational truth | **Implemented/Partial:** Chronicle records validated events, digest-bound snapshots, atomic pointer commits, terminal repair, task/cancellation state, results, and recovery projection. General artifacts/checkpoints remain planned. |
| Semantic authority | **Implemented:** The owned `MemoryStore` persists typed observations with provenance and lifecycle state. Higher-level approval, summary, capsule, and optional Engram workflows are planned. |
| Recovery source | **Implemented/Partial:** Readable digest-bound snapshots and events drive bounded recovery reconstruction, including terminal commit repair; general artifacts/checkpoints and a dedicated UX remain planned. |
| User experience | Happy path is quiet. Detailed inspection is available on demand. |
| Provider selection | **Implemented, bounded:** Compare exact capability/version/health evidence with policy and fail closed before execution. |
| Routing | Persist normalized inputs, candidates, selected route, rationale, policy version, SDD decision, and attributable overrides. |
| Concurrency | **Implemented, bounded:** Independent reads can use four native child sessions per workspace, including deterministic waves in an adaptive foreground plan. A workspace admission guard serializes only pool membership changes; child execution remains parallel, while continuity advancement, reviews, and mutation remain exclusive. Unknown active lease state fails closed. Supervised background task graphs are still planned. |

## Adaptive contract gates

These gates combine delivered and future behavior. Registry resolution, Gatekeeper eligibility/permission checks, prompt composition, provider execution, structured results, finite coordinator limits, native Navigator execution, delegation-plan validation, dependency-wave computation, file-backed owner/epoch coordination, and explicit Delivery Authority CLI gates are **Implemented** on the bounded path. Automatic gate installation, SDD preflight, general artifact stores, and the full approval UX remain **Planned/Partial**.

### 1. Capability negotiation and provider selection

1. Normalize the run's required capabilities and constraints.
2. Evaluate every configured provider against those needs and the active policy version.
3. Record each candidate's eligibility and machine-readable exclusion reasons.
4. Select the preferred provider only when eligible. OpenCode wins by preference, never by hard dependency.
5. If none qualifies, return a structured `unsupported` result and do not delegate or advance state.

The selection record is immutable evidence. Provider-native configuration and mutable provider state do not belong in it.

### 2. Explainable routing and selective SDD

Routing classifies difficulty and risk, records candidate agents, chooses the smallest capable agent, and explains why. SDD is `required`, `skipped`, or `overridden` by policy—not automatically invoked for every request. Overrides include the approving identity and reason.

The classifier route `plan` produces a bounded approach when implementation is not authorized or full SDD is unnecessary. It is distinct from the SDD `tasks` operating mode, which converts approved requirements and design into implementation work units. A planning route must not be reported as an approved tasks artifact.

SDD preflight modes are deterministic contracts. The owned-backend contract already uses `memory`; `engram` is only an external-provider reference. No memory-backend migration remains pending. Runtime preflight and SDD artifact-store resolution are still **Planned**.

| Mode | Backend behavior | Failure behavior |
| --- | --- | --- |
| `required` | **Contracts-only:** resolve owned `memory`, repository `openspec`, or synchronized `hybrid`; never label owned memory as `engram`. | **Planned:** block with a recoverable structured error when the configured store is unavailable or invalid. |
| `auto` | Check the configured backend only when routing selects SDD. | Record an explicit fallback decision. |
| `off` | Use backend `none` and perform no artifact access. | Continue without SDD artifacts. |

### 3. Structured questions

A question carries `questionId`, prompt, expected answer shape, blocking status, and bounded choices when applicable. An answer repeats the ID and shape. Invalid or uncorrelated answers return validation details and leave a blocking question blocked.

### 4. Language and skill boundaries

- User-facing text matches the user's language.
- Technical artifacts and subagent instructions default to English unless an explicit user or project policy overrides them.
- Delegation uses exact skill identity, version, source, provenance, and allowed scope, including `user`.
- Names, aliases, version ranges, and unresolved path strings are not delegation identities.
- Unresolved, unprovenanced, or out-of-scope skills do not execute.

## 1. System flow: request to final summary

The following table is the **target manager lifecycle**. The bounded bridge path currently implements scoped skill/risk resolution, compact context, native selection among explorer/implementer/reviewer, up to four independent one-shot read children, structured result validation, receipts, Chronicle task/run evidence, and opt-in cross-process `start`/`continue` continuity. It does not yet advance every Navigator/SDD phase, schedule supervised background work, or approve dependency-aware parallel task graphs.

| Step | Owner | What happens | Run-store event |
| --- | --- | --- | --- |
| 1. Receive request | Manager | Parse the user goal, constraints, language/domain rules, and safety limits. | `run.started` or `run.updated` |
| 2. Resolve intent | Manager | Decide whether the request is exploration, planning, implementation, review, recovery, or mixed. | `checkpoint.created` |
| 3. Resolve skills | Manager | Select exact skills by trigger, path, registry entry, or injected payload. | `run.updated` |
| 4. Assess risk | Manager | Identify destructive, external, credential, install, commit, push, or network actions. | `checkpoint.created` |
| 5. Select subagent | Manager | Pick one subagent for the next bounded phase. | `phase.started` |
| 6. Pass compact context | Manager | Provide goal, scope, allowed paths/tools, relevant artifacts, and exact skill refs. | `phase.started` |
| 7. Execute work | Subagent | Read required context, perform the scoped work, and write allowed artifacts. | `artifact.written` as needed |
| 8. Return result | Subagent | Return status, files/artifacts changed, validations, risks, and next recommended action. | `phase.completed` or `phase.failed` |
| 9. Validate gate | Manager or reviewer | Check the result against requirements, safety rules, and validation expectations. | `validation.completed` |
| 10. Save memory | Manager | Save semantic memory only for durable learnings or decisions. | `memory.written` when saved |
| 11. Summarize | Manager | Return a concise final summary with status, files, decisions, risks, and next steps. | `run.completed` or `run.failed` |

The manager should repeat steps 5-10 for multi-phase work. Each phase should be independently inspectable.

## 2. Manager/orchestrator responsibilities and hard boundaries

The manager keeps the whole workflow coherent without becoming the worker.

### Responsibilities

- Understand the user's intent and constraints.
- Open, resume, update, and close runs.
- Select subagents and define a narrow scope for each one.
- Resolve skills and inject exact references or payloads.
- Enforce permissions, approval gates, and allowed edit roots.
- Keep subagent context small and relevant.
- Validate structured results before moving to the next phase.
- Save semantic memory when the result has long-term value.
- Produce the user-facing summary.

### Hard boundaries

| Boundary | Rule |
| --- | --- |
| Execution | The manager should not perform substantial implementation, review, or analysis when a scoped subagent exists for that capability. |
| Delegation | Only the manager delegates in v1. Subagents return `needs_followup` instead of creating more subagents. |
| Safety | Risky/destructive actions require explicit human approval before execution. |
| Context | The manager must not dump the full conversation or full repository into a subagent when a focused payload is enough. |
| Skills | The manager must pass exact skill refs or payloads, not informal summaries of skill behavior. |
| Truth | The manager must not treat memory as an event log or event logs as semantic memory. |

## 3. Subagent model

Subagents are hidden by default and purpose-built. They should advertise capabilities, permissions, skill refs, and output contracts in the agent registry.

| Subagent type | Purpose | Typical output |
| --- | --- | --- |
| Capability agents | Execute a workflow phase such as explore, propose, spec, design, `tasks`, apply, verify, or archive. | Structured phase result plus artifacts. The classifier's `plan` route is not a phase alias for `tasks`. |
| Review agents | Inspect work through one review lens such as risk, reliability, resilience, or readability. | Findings, severity, evidence, and verdict. |
| Fix agents | Apply narrowly scoped fixes after validation or review findings. | Patch summary, changed files, and retest notes. |
| Future specialized agents | Handle domains such as iOS, Android, TypeScript, database migrations, release, or security. | Domain-specific result following the same structured envelope. |

### Required subagent behavior

- Accept only the assigned scope.
- Load only injected or directly relevant skills.
- Write only to allowed paths.
- Return structured status: `success`, `blocked`, `failed`, or `needs_followup`.
- Report deviations instead of silently changing the plan.
- Ask the manager for more context when blocked; do not guess.
- Do not create another subagent in v1.
- Return `status`, `summary`, `artifacts`, `nextRecommended`, `risks`, and machine-readable `errors` when applicable.

### Background work and bounded loops

Background work is advisory until the manager validates and incorporates its result. Every background task is manager-owned, independently cancelable, `readOnly: true`, `mayDelegate: false`, and `mayAdvanceRun: false`. A request to mutate, delegate, or advance the run is denied and recorded as an event.

Retry, clarification, routing, and agent loops declare a finite iteration budget and optional deadline. An exhausted loop cannot start another iteration; it terminates as `budget_exhausted` or `deadline_exceeded`, or with another explicit reason such as `completed`, `blocked`, `failed`, or `cancelled`.

## 4. How the orchestrator selects subagents

The manager should select the smallest capable subagent for the next decision or work unit.

| Signal | Selection rule |
| --- | --- |
| User asks “what is happening?” | Use an exploration or inspection agent. |
| User asks for proposal/scope | Use a proposal agent. |
| Requirements or acceptance criteria are missing | Use a requirements/spec agent. |
| Architecture or tradeoffs are needed | Use a design agent. |
| Work needs task breakdown | Use a planning agent. |
| Files need to change | Use an apply/fix agent with write permissions limited to the required paths. |
| Work needs proof | Use a verify or review agent. |
| Risk is high | Use a review-risk agent before execution and require human approval when needed. |

Selection should consider:

1. Intent: what phase is actually needed next.
2. Capability match: which subagent has the narrowest relevant capability.
3. Permissions: whether the subagent may read, write, run commands, use network, or use MCP.
4. Skill match: which exact skills are required.
5. Context size: whether the work can be completed with a compact payload.
6. Review budget: whether the work should be split before execution.

## 5. Context passing without overloading subagents

Context should be a curated work packet, not a transcript dump.

### Minimum context packet

| Field | Purpose |
| --- | --- |
| `runId` | Correlates all events and artifacts. |
| `phase` | Names the current phase. |
| `goal` | States the user outcome in one or two sentences. |
| `scope` | Defines what is in scope and out of scope. |
| `allowedPaths` | Limits file reads/writes. |
| `allowedTools` | Limits command/tool usage. |
| `artifacts` | Points to existing proposal/spec/design/tasks/results. |
| `skillRefs` | Exact versioned skill identities with source, provenance, and allowed scope. |
| `acceptanceCriteria` | What must be true when the phase is done. |
| `approvalState` | Whether risky actions are approved, denied, or pending. |
| `returnContract` | The structured result shape the subagent must return. |

The primary context contains only the current request, routing/preflight decision, one active packet, and immutable references to durable state. Full histories, large outputs, and background findings remain outside it and are fetched deliberately.

### Context rules

- Prefer references to files over pasted full contents when the subagent can read the file.
- Paste only the slice that matters when a file is large.
- Include previous decisions as short bullets with links or memory IDs.
- Include user constraints verbatim when they affect behavior.
- Do not include unrelated chat history, raw logs, or broad repository listings.

## 6. Skill resolution and exact skill/payload injection

Skills are behavior contracts. The manager resolves them before delegation and passes exact references.

### Resolution order

1. Explicit user-provided skill paths or skill names.
2. Agent registry `skillRefs` for the selected subagent.
3. Skill registry triggers matched against the task domain.
4. Project-local skills before user-global skills when both apply.
5. Runtime-shared skills only when no project-specific override exists.

### Injection contract

| Injection form | Use when | Example |
| --- | --- | --- |
| Registry-resolved source | The runtime can read a provider-native source. | `{ id, version, source, provenance }` |
| Frozen payload reference | The runtime must freeze content for repeatability. | A provider reference plus checksum and registry provenance. |

The manager should not paraphrase a skill and call that sufficient. If a skill governs behavior, the agent receives the exact contract reference. A filesystem path may appear inside a resolved source, but a bare path is not a valid identity.

## 7. Run-store event lifecycle

**Implemented/Partial:** Chronicle records the event types used by the bounded coordinator in append-only JSONL, validates task/cancellation ordering, publishes readable active snapshots through immutable SHA-256 files and an atomic pointer, repairs interrupted terminal publication, and reconstructs recovery state. The broader phase/artifact/checkpoint event lifecycle below remains planned.

| Phase | Log at minimum | Why it matters |
| --- | --- | --- |
| Run start/resume | `run.started` or `run.updated` with goal and project. | Recovery can identify the active workflow. |
| Phase start | `phase.started` with phase, agent, scope summary, and allowed capabilities. | Auditors can see who did what. |
| Artifact write | `artifact.written` with path, kind, and checksum when available. | Recovery can verify outputs exist. |
| Memory write | `memory.written` with backend, ID, topic key, and type. | Semantic writes are traceable without duplicating memory content. |
| Validation | `validation.completed` with status and message. | The run records proof, failures, or skipped checks. |
| Checkpoint | `checkpoint.created` after important routing, approval, or recovery decisions. | Restart can continue from a known safe point. |
| Failure | `phase.failed` or `run.failed` with error category and next safe action. | Recovery does not have to infer what broke. |
| Recovery | `recovery.started` and `recovery.completed`. | Recovery itself becomes auditable. |
| Completion | `run.completed` with final status and summary artifact reference. | The run has an explicit end. |

The current-run pointer, its immutable `runs/<run-id>.<sha256>.json` active snapshot, the stable terminal `runs/<run-id>.json`, and `logs/<run-id>.jsonl` are readable and strictly validated today. The CLI must not become the only way to understand them. General artifact/checkpoint evidence remains planned.

## 8. Memory lifecycle

Memory is for meaning. Run events are for operations. Memory is **Implemented** for typed save/search/get, and Chronicle events are **Implemented** for the bounded coordinator; the complete product event taxonomy and continuity-memory workflows remain **Partial/Planned**.

| Write target | Save here | Do not save here |
| --- | --- | --- |
| Run store | Phase starts/completions, artifact refs, tool-level operational status, validation results, checkpoints, failures. | Durable decisions without explanation. |
| Semantic memory (owned `MemoryStore`; SQLite/FTS5-first) | **Implemented:** typed observations with topic, scope, provenance, lifecycle state, references, and save/search/get. **Planned:** richer approval, summary, capsule, comparison, review, and import workflows. | Raw command output, every event, temporary logs, repeated phase noise, or operational state resolution. |

### Save semantic memory when

- A design or architecture decision is made.
- A bug root cause is discovered and fixed.
- A non-obvious project behavior is learned.
- A user preference or workflow constraint should persist.
- A final summary would help the next session resume faster.

### Write only run events when

- A phase starts or completes.
- An artifact is written.
- A validation check runs.
- A checkpoint is created.
- A tool emits temporary output.
- A failure is operational but not a durable project lesson.

## 9. Human approval gates

The manager must stop before risky actions unless the current request explicitly approves them.

| Gate | Requires approval before |
| --- | --- |
| Destructive files | Deleting files, overwriting large areas, resetting state, or modifying generated state outside the requested scope. |
| Git operations | Staging, committing, amending, rebasing, pushing, force pushing, tagging, or opening PRs. |
| Package changes | Installing packages, changing lockfiles, upgrading runtimes, or modifying global config. |
| External side effects | Network calls, production APIs, cloud resources, billing systems, deployments, or release actions. |
| Secrets and credentials | Reading, printing, modifying, or transmitting secrets. |
| Permission expansion | Granting a subagent broader paths, tools, network, or write access than originally scoped. |

Approval should be recorded as a checkpoint with the approved action, scope, and any user-provided wording.

The [canonical autonomy profiles and capability-lease contract](product-blueprint.md#autonomy-profiles) reduce repeated prompts for ordinary scoped work. They never waive the gates above. A lease is least-privilege, revocable, limited to one work unit, roots, tools, risk ceiling, and deadline, and cannot expand or renew itself.

## 10. Review and validation gates

Validation should prove that the result matches the request and the documented contract.

| Gate | Owner | Pass condition |
| --- | --- | --- |
| Scope check | Manager | Work stayed inside requested scope and allowed paths. |
| Contract check | Manager or verifier | Required artifacts and structured result fields exist. |
| Requirements check | Verify agent | Acceptance criteria are satisfied or explicitly blocked. |
| Safety check | Review-risk agent | No risky action occurred without approval. |
| Review burden check | Manager or review agent | Work is small enough to review, or it is split into follow-up slices. |
| Recovery check | Manager | Run events, snapshots, and artifacts agree. |

If validation fails, the manager should either route a scoped fix agent or return a blocked/failed summary with the next safe action.

## 11. Failure and recovery flow

**Implemented/Partial:** The bounded coordinator records failure/cancellation evidence; Chronicle recovery validates digest-bound authority, completes an interrupted terminal pointer removal, and fails closed on malformed, corrupt, or inconsistent state. General phase checkpoints/artifacts remain planned.

```text
Failure detected
  -> write phase.failed or run.failed
  -> create checkpoint with last safe state
  -> compare current-run snapshot, run snapshot, event log, and artifacts
  -> identify recoverable vs blocked state
  -> resume with the smallest safe phase or ask the user for approval/context
```

| Failure type | Recovery behavior |
| --- | --- |
| Missing context | Mark phase `blocked`; ask for the one missing input. |
| Subagent result incomplete | Ask the same subagent for correction if safe, or route a verifier/fix agent. |
| Validation failed | Record failed validation and route a fix agent with only the failing evidence. |
| Snapshot/event mismatch | Stop and report inconsistency; do not guess the correct state. |
| Interrupted run | Read `current-run.json`, latest JSONL event, run snapshot, artifacts, and memory summaries. |
| OpenCode stops between dependency waves | Return the durable `pending` orchestration unchanged; resume or cancel it explicitly instead of inventing a terminal join. |
| Permission denied | Stop and request explicit approval or narrower scope. |

## 12. Anti-patterns to avoid

- Letting the manager implement substantial work while pretending it is coordinating.
- Passing the entire conversation or repository map to every subagent.
- Delegating to a generic “do everything” agent instead of a narrow capability agent.
- Allowing subagents to spawn more subagents silently.
- Treating memory as a log sink.
- Treating JSONL events as long-term semantic memory.
- Hiding recovery state behind CLI-only behavior.
- Writing artifacts without logging `artifact.written`.
- Saving memory for every minor phase event.
- Running risky commands first and asking for approval afterward.
- Summarizing skills from memory instead of injecting exact skill references or payloads.
- Making inspection commands part of the normal happy path.
- Selecting a preferred provider without proving capability compatibility.
- Letting background agents mutate state, advance a run, or delegate.
- Retrying after a loop budget or deadline is exhausted.

## 13. First implementation checklist

- [x] **Implemented:** Build the binary, storage resolution, status/doctor, owned SQLite/FTS5 memory, and memory save/search/get.
- [x] **Implemented/Partial:** Read/write Chronicle events, publish digest-bound snapshots atomically, derive task/cancellation state, repair terminal publication, and reconstruct bounded recovery; general checkpoint/artifact continuity remains.
- [x] **Implemented/Contracts-only:** Define embedded schemas plus owned `memory` and external `engram` reference semantics; validate the contracts used by current runtime paths.
- [x] **Implemented:** Enforce contract validation at bounded runtime boundaries.
- [x] **Implemented:** Append, sync, reread, validate, and roll back Chronicle events.
- [x] **Implemented:** Write immutable active snapshots, commit through one atomic pointer replacement, and recover terminal pointer-removal failures.
- [x] **Implemented:** Enforce the legal task/cancellation state machine used by the coordinator.
- [x] **Implemented:** Add Gatekeeper/Registry policy and exact resolution.
- [x] **Implemented:** Execute the eligible OpenCode provider after policy evaluation.
- [x] **Implemented, bounded:** Coordinate compact context packets, approvals, foreground/background constraints, cancellation, and finite loops for status/read/write/review operations.
- [x] **Implemented/Partial:** Add delegation request/plan/join contracts, deterministic dependency waves, execution-scoped factory singleflight, durable logical-slot and owner/epoch authority, atomic confirmed-prerequisite admission, prepared replay, fail-closed dispatch classification, checkpoint takeover, authority-accepted Join, native Navigator/task sessions, plan persistence, and lifecycle controls.
- [x] **Implemented/Partial:** Add content-bound Delivery Authority receipts and explicit lifecycle validation; automatic Git/branch-protection wiring remains rollout work.
- [ ] **Planned:** Add Chronicle plan-event projection, full SDD phases/artifact stores, richer approval UX, and additional adapters.
- [ ] **Planned:** Keep future `run inspect` and recovery commands focused on debugging/auditing, not happy-path ceremony.

## Next step

Use this document with the contract schemas in [`docs/schemas/README.md`](schemas/README.md) and the [native delegation/Delivery Authority implementation plan](delegation-authority-implementation-plan.md). The next major slice is Delivery Authority rollout plus broader SDD artifacts and Chronicle plan events. Native Windows runtime/distribution smoke is deferred.
