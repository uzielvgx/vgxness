# VGXNESS Orchestration Flow

This document owns the request lifecycle, gates, phase operating modes, and recovery contract. The [VGXNESS Product Blueprint](product-blueprint.md) is the canonical product vision, taxonomy, status, and roadmap.

| Status | Current-candidate evidence and boundary |
| --- | --- |
| **Implemented** | Installed native manager routing; structured SDD lifecycle; `memory`, `openspec`, and `hybrid` backends; per-change `automatic`/`interactive` modes; six read-only SDD agents; five read-only reviewers; manager-only workspace/lifecycle writes; storage plugin authorization; bounded read-only parallelism. |
| **Partial** | Chronicle-backed recovery covers existing run state but not rich interrupted-SDD reconstruction. Compatibility bridge/control-plane orchestration, tickets, waves, edit broker, maintenance, and Delivery Authority remain implemented CLI/maintainer subsystems, not the installed scheduler. |
| **Contracts-only** | Schemas and rules for broader provider-neutral routing, continuity capsules, and event types not exercised by delivered paths. |
| **Planned** | Richer Chronicle-backed interrupted-SDD recovery, provider-neutral routing/catalog probes, automatic delivery integration, and additional adapters. |

**Active installed path:** The top-level `vgxness-manager` chooses direct work, bounded native read-only delegation, or structured SDD. It alone creates and transitions changes, accepts immutable revisions, writes OpenSpec files, applies patches, runs validation, records projection evidence, and edits the workspace. The storage-only plugin persists or transforms bounded data and fails every SDD mutation closed unless trusted OpenCode context identifies the tracked top-level manager session.

**Compatibility path:** `bridge`, `controlplane`, `orchestrate`, `maintenance`, `edit`, ticket/wave, and Delivery Authority commands retain their deterministic plans, leases, worktrees, receipts, and recovery controls for CLI and maintainer use. Setup does not install their OpenCode tools, and they do not schedule active native SDD work. See the [compatibility implementation plan](delegation-authority-implementation-plan.md).

Navigator, Scout, Blueprint, Forge, Sentinel, and optional Challenger are product capabilities; Registry, Chronicle, and Gatekeeper are deterministic services. Explore, proposal, spec, design, tasks, apply, and verify are installed SDD operating roles, not a competing capability taxonomy.

In this workflow, “manager” or “orchestrator” denotes Navigator's coordinating context; it is not an additional product capability.

## Quick path

The active native lifecycle is:

```text
User request
  -> manager selects direct, bounded read-only, or SDD route
  -> manager creates/resumes one change with memory|openspec|hybrid backend
  -> change stores automatic|interactive mode
  -> read-only phase agents return evidence or candidate content
  -> apply returns a hash-bound patch and validation plan, without writing
  -> manager synthesizes, accepts, projects, applies, and validates sequentially
  -> optional independent read-only subworks overlap, maximum four
  -> manager transitions explore -> proposal -> spec -> design -> tasks -> apply -> verify -> complete
```

The manager and storage plugin implement this lifecycle. `vgxness status`, `vgxness doctor`, guided setup, and native SDD CLI/storage operations are **Implemented**. Bridge dispatch and control-plane orchestration are **Implemented for compatibility**. Richer interrupted-SDD recovery is **Planned**.

## Core decisions

| Area | Decision |
| --- | --- |
| Manager role | **Implemented/Partial:** The native manager routes direct work or optional SDD, owns revision acceptance and phase transitions, enforces child boundaries, and validates every candidate. Richer interrupted-run recovery remains planned. |
| Subagent role | **Implemented, bounded:** All six SDD profiles are read-only. Five artifact profiles return schema-bound candidates, apply returns a hash-bound patch and validation plan, and frozen-candidate reviewers provide verification evidence. They cannot write, run commands, mutate lifecycle state, or delegate; the manager is the sole workspace writer and test runner. |
| Subagent nesting | **Implemented, bounded:** Each native child has `task: deny`; missing authority is reported instead of recursively delegating. |
| Operational truth | **Implemented/Partial:** Chronicle records validated events, digest-bound snapshots, atomic pointer commits, terminal repair, task/cancellation state, results, and recovery projection. General artifacts/checkpoints remain planned. |
| Semantic authority | **Implemented:** The owned `MemoryStore` persists typed observations with provenance and lifecycle state. Higher-level approval, summary, and capsule workflows are planned. |
| Recovery source | **Implemented/Partial:** Readable digest-bound snapshots and events drive bounded recovery reconstruction, including terminal commit repair; general artifacts/checkpoints and a dedicated UX remain planned. |
| User experience | Happy path is quiet. Detailed inspection is available on demand. |
| Provider selection | **Implemented for compatibility:** The provider runner compares declared capability/version/health evidence with policy. Provider-neutral native-path catalog probes remain planned; model availability is not claimed. |
| Routing | **Implemented/Partial:** The native manager selects direct, bounded read-only, or SDD work. Provider-neutral routing persistence and catalog probes remain planned. |
| Concurrency | **Implemented, bounded:** At most four independent read-only subworks may overlap. Final synthesis, review incorporation, patch application, validation, projection, acceptance, and every write remain sequential and manager-owned. |

## Adaptive contract gates

Native manager SDD preflight, backend/mode selection, phase gates, immutable revision acceptance, projection checks, and mutation authorization are **Implemented**. Registry/Gatekeeper/provider/coordinator and Delivery Authority gates remain implemented on the compatibility path. Automatic delivery integration, provider-neutral routing/catalog probes, and richer Chronicle SDD recovery are **Planned**.

### 1. Capability negotiation and provider selection

The following deterministic selection record belongs to the compatibility provider path and the provider-neutral target. The active native SDD installation uses its configured model plan; it does not probe runtime model availability.

1. Normalize the run's required capabilities and constraints.
2. Evaluate every configured provider against those needs and the active policy version.
3. Record each candidate's eligibility and machine-readable exclusion reasons.
4. Select the preferred provider only when eligible. OpenCode wins by preference, never by hard dependency.
5. If none qualifies, return a structured `unsupported` result and do not delegate or advance state.

The selection record is immutable evidence. Provider-native configuration and mutable provider state do not belong in it.

### 2. Explainable routing and selective SDD

The native manager classifies difficulty and risk and chooses the smallest direct, read-only, or SDD route. Provider-neutral candidate records, policy-level `required`/`skipped`/`overridden` decisions, and attributable overrides remain planned.

The classifier route `plan` produces a bounded approach when implementation is not authorized or full SDD is unnecessary. It is distinct from the SDD `tasks` operating mode, which converts approved requirements and design into implementation work units. A planning route must not be reported as an approved tasks artifact.

SDD preflight is deterministic. Each change stores `automatic` or `interactive` execution and may change it only through optimistic versioning. Creation uses a project-scoped idempotency key; saves, acceptance, and transitions are limited to the current phase. Change statuses are `active`, `completed`, and `cancelled`; artifacts are `draft`, `accepted`, or `stale`; revisions are `candidate` or `accepted`; projection evidence is `absent`, `current`, `stale`, `drift`, or `failed`.

| Backend | Implemented canonical behavior |
| --- | --- |
| `memory` | Structured SQLite SDD revisions contain canonical bodies, isolated from semantic observations. |
| `openspec` | Repository files under `openspec/changes/<safe-change-id>/` are canonical; SQLite records bounded identity, digest, bindings, and projection evidence. |
| `hybrid` | Memory is canonical and OpenSpec is a deterministic projection. Divergent content is never auto-imported. |

| Interaction mode | Implemented behavior |
| --- | --- |
| `automatic` | Advance validated reversible phase gates without routine pauses; stop for authorization, drift, missing evidence, or consequential ambiguity. |
| `interactive` | Pause at each candidate boundary for approve, revise, or cancel. |

### 3. Structured questions

A question carries `questionId`, prompt, expected answer shape, blocking status, and bounded choices when applicable. An answer repeats the ID and shape. Invalid or uncorrelated answers return validation details and leave a blocking question blocked.

### 4. Language and skill boundaries

- User-facing text matches the user's language.
- Technical artifacts and subagent instructions default to English unless an explicit user or project policy overrides them.
- Delegation uses exact skill identity, version, source, provenance, and allowed scope, including `user`.
- Names, aliases, version ranges, and unresolved path strings are not delegation identities.
- Unresolved, unprovenanced, or out-of-scope skills do not execute.

## 1. System flow: request to final summary

The following table describes the native manager lifecycle. Compatibility Chronicle event names remain illustrative where the richer SDD recovery projection is still planned.

| Step | Owner | What happens | Run-store event |
| --- | --- | --- | --- |
| 1. Receive request | Manager | Parse the user goal, constraints, language/domain rules, and safety limits. | `run.started` or `run.updated` |
| 2. Resolve intent | Manager | Decide whether the request is exploration, planning, implementation, review, recovery, or mixed. | `checkpoint.created` |
| 3. Resolve skills | Manager | Select exact skills by trigger, path, registry entry, or injected payload. | `run.updated` |
| 4. Assess risk | Manager | Identify destructive, external, credential, install, commit, push, or network actions. | `checkpoint.created` |
| 5. Select subagent | Manager | Pick one subagent for the next bounded phase. | `phase.started` |
| 6. Pass compact context | Manager | Provide goal, scope, allowed paths/tools, relevant artifacts, and exact skill refs. | `phase.started` |
| 7. Execute work | Subagent and manager | Read-only agents return evidence/candidates; apply returns a hash-bound patch. Only the manager writes artifacts or workspace files. | `artifact.written` as needed |
| 8. Return result | Subagent | Return status, candidate artifacts or patch scope, validations, risks, and next recommended action. | `phase.completed` or `phase.failed` |
| 9. Validate gate | Manager or reviewer | Check the result against requirements, safety rules, and validation expectations. | `validation.completed` |
| 10. Save memory | Manager | Save semantic memory only for durable learnings or decisions. | `memory.written` when saved |
| 11. Summarize | Manager | Return a concise final summary with status, files, decisions, risks, and next steps. | `run.completed` or `run.failed` |

The manager should repeat steps 5-10 for multi-phase work. Each phase should be independently inspectable.

## 2. Manager/orchestrator responsibilities and hard boundaries

The manager keeps the workflow coherent while retaining exclusive responsibility for synthesis and every write.

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
| Execution | The manager delegates bounded read-only analysis when useful but remains the sole patch applier, workspace writer, validation runner, persistence caller, and lifecycle authority. |
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
| Apply/fix operating role | Compose a narrowly scoped hash-bound patch after accepted inputs or findings. | Patch and validation plan; no file or lifecycle mutation. |
| Future specialized agents | Handle domains such as iOS, Android, TypeScript, database migrations, release, or security. | Domain-specific result following the same structured envelope. |

### Required subagent behavior

- Accept only the assigned scope.
- Load only injected or directly relevant skills.
- Remain read-only; return bounded candidate content or a hash-bound patch for the manager to apply.
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
| Files need to change | Use the read-only apply composer when SDD is active; the manager applies the accepted hash-bound patch to the required paths. |
| Work needs proof | Use a verify or review agent. |
| Risk is high | Use a review-risk agent before execution and require human approval when needed. |

Selection should consider:

1. Intent: what phase is actually needed next.
2. Capability match: which subagent has the narrowest relevant capability.
3. Permissions: installed SDD and review agents remain read-only; any future capability expansion must be explicit and policy-checked.
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
| `allowedPaths` | Limits child reads and manager-owned writes. |
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
| Permission expansion | Granting a subagent broader paths, tools, or network access, or making a read-only role write-capable. |

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
| Compatibility orchestration stops between dependency waves | Return the durable `pending` orchestration unchanged; resume or cancel it explicitly instead of inventing a terminal join. |
| Native SDD is interrupted | Preserve the accepted revisions and current phase; do not infer missing work. Richer Chronicle-backed reconstruction remains planned. |
| Permission denied | Stop and request explicit approval or narrower scope. |

## 12. Anti-patterns to avoid

- Letting the manager bypass accepted SDD inputs or silently rewrite a phase agent's hash-bound patch.
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
- [x] **Implemented:** Add native SDD phases, structured artifact stores, backends, interaction modes, projection checks, and manager-only lifecycle authority.
- [ ] **Planned:** Add richer Chronicle-backed interrupted-SDD recovery, provider-neutral routing/catalog probes, automatic delivery integration, and additional adapters.
- [ ] **Planned:** Keep future `run inspect` and recovery commands focused on debugging/auditing, not happy-path ceremony.

## Next step

Use this document with [OpenCode Integration](opencode-integration.md), [Native Memory](memory.md), and the [contract schema boundary](schemas/README.md). The delegation/Delivery Authority plan is compatibility-only. Remaining native work is richer Chronicle-backed interrupted-SDD recovery, provider-neutral routing/catalog probes, and automatic delivery integration. Native Windows runtime/distribution smoke remains deferred.
