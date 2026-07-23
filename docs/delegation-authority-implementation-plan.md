# Native Delegation and Delivery Authority Implementation Plan

This plan turns the existing one-dispatch/one-child OpenCode bridge into an adaptive, auditable orchestration path without introducing nested workers or a second agent runtime.

## Non-negotiable invariants

- Every delegated work unit executes in an OpenCode-native child session whose parent is the active `vgxness-manager` session.
- A subagent is a bounded role; its child session is an ephemeral execution container, not durable semantic memory.
- VGXNESS, not the planning model, computes dependency waves and has final authority over agent eligibility, permissions, concurrency, and acceptance.
- No nested `opencode run`, detached worker, alternate runtime, self-delegation, or subagent-to-subagent delegation is allowed.
- V1 parallelism is limited to independent, isolated, read-only work and at most four active native children per workspace.
- Continuity, review, and mutation remain exclusive. Isolated worktrees and a ticket-authenticated edit broker are implemented, but parallel mutation remains deliberately disabled until merge coordination and conflict policy are explicit.
- Existing `vgxness_dispatch` remains a compatible single-work-unit primitive while `vgxness_orchestrate` is introduced above it.

## Target lifecycle

```text
user goal
  -> VGXNESS Manager calls vgxness_orchestrate once
  -> native Navigator child proposes bounded candidate tasks
  -> VGXNESS validates contracts, Registry identities, Gatekeeper policy, and dependencies
  -> deterministic scheduler computes sequential/parallel waves
  -> OpenCode adapter creates native child sessions for the current wave
  -> each child completes or fails one durable ticket
  -> VGXNESS joins the wave, advances dependencies, and records Chronicle evidence
  -> verifier/reviewer runs only after the evidence it depends on exists
  -> Delivery Authority binds target, evidence, verdict, and lifecycle receipt
  -> Manager explains outcome, limitations, and the next safe action
```

## Delivery slices

### Slice 1 — Delegation contracts and deterministic scheduler

**Status: implemented, including the production file-backed authority used by Slice 3.**

- Add `delegation.request`, `delegation.plan`, `delegationTask`, `executionWave`, and `delegation.join` contracts.
- Accept only a high-level bridge orchestration goal; agent choice and parallelism are not tool arguments.
- Validate advisory task decompositions and reject duplicate/missing dependencies, cycles, incomplete review dependencies, and unsafe capability/operation combinations. Only `implement/write-files` may request mutation.
- Compute stable content-bound plan IDs and request digests.
- Schedule at most four independent isolated reads per wave.
- Construct a caller-owned factory from the exact authority dependency; never select an authority through a caller-supplied global identity. One factory singleflights concurrent opens for the same schedule/owner and locally fences its superseded handle, while the durable authority—not process memory—owns cross-process safety. The composition root bounds factory lifetime to one manager execution.
- The implemented contract/model requires one atomic admission transition: verify the current owner/epoch and a revocation-aware checkpoint in which every prerequisite task is `completed` with `dispatchStatus: confirmed`; reserve the logical `(scheduleId, planId, waveId, taskId)` slot; bind its single-use ticket/child record and trusted manager parent; and persist a prepared replay record before any native dispatch callback. Alternate tickets or children cannot alias an already reserved logical task across factories, processes, scheduler instances, or plans.
- Include owner identity and schedule epoch in every admission/terminal payload and idempotency key. If a durable commit loses its response, exact replay returns the original verdict only while the lease and ticket remain live; altered payloads, stale owners, revoked tickets, and expired tickets remain rejected.
- Persist an uncertain marker and the exact replay identity before invoking each per-task native child create/send callback, without holding a process-global authority lock. The authority deadline bounds admission waiting, not an uncooperative side effect: timeout is `uncertain`, a late return is ignored, and takeover stays fenced until the callback terminates or proof-backed reconciliation resolves it. The scheduler never redelivers automatically. The callback must otherwise classify that binding as `confirmed`, `not_started`, or `uncertain`; only confirmed children become running. `not_started` and `uncertain` fail closed with durable evidence. Mixed waves keep confirmed children observable until they terminate.
- Return the authority's validated task checkpoint with every granted lease, including an initially empty lease, and with every accepted admission. A takeover adopts confirmed running tasks and accepted terminal outcomes without changing their idempotency identity. The checkpoint must apply current ticket revocation/expiry, so invalidated terminal evidence becomes a normalized failure rather than a completed result.
- Revalidate the owner/epoch lease after admission and terminal responses as a local freshness probe. Authoritative publication uses `AcceptJoin`: in one authority transition it rechecks the current owner/epoch, rebuilds the revocation-aware terminal checkpoint, binds the content-derived join candidate, and returns the authoritative snapshot. Join is linearized at that acceptance point. A local factory also marks its previous owner handle superseded immediately; delayed success cannot reactivate or publish from a stale handle.
- Accept terminal child outcomes through the same context-bounded durable authority outside scheduler locks. Permanent fencing/revocation/expiration terminalizes with normalized failure and a joinable schedule; transient authority loss remains retryable.
- Compute result digests inside the scheduler from the exact accepted native result bytes; callers never supply authoritative digests.
- Stop dependent waves after failure or cancellation and produce a validated complete/partial/failed/cancelled join. Every public joined task preserves `dispatchStatus` (`confirmed`, `not_started`, or `uncertain`) so recovery never collapses uncertainty into a generic failure.

**Exit evidence:** focused Navigator/orchestrator/contract tests plus `go test ./...`.

### Slice 2 — Native Navigator and `vgxness_orchestrate`

**Status: implemented.**

- Add a hidden, tool-denied `vgxness-navigator` OpenCode subagent profile.
- Add a frozen prompt/return contract for a bounded candidate-task proposal.
- Add plan prepare/accept commands to the bridge. The native Navigator session is created with `parentID` equal to the manager session.
- Validate the proposal through Slice 1 and persist the approved plan before executing any task.
- Project one `vgxness_orchestrate(goal, acceptanceCriteria)` tool. It must not expose task, agent, wave, or concurrency arguments.
- Keep `vgxness_dispatch` for explicit single operations and compatibility fallback.

**Exit evidence:** an OpenCode integration test proves one orchestration call creates one native Navigator child and returns an approved content-bound plan.

### Slice 3 — Native wave execution, visibility, and recovery

**Status: implemented for deterministic native waves, durable plan state, owner/epoch recovery, status/resume/cancel, and settled parallel execution. Chronicle plan-event projection and richer UI remain hardening work.**

- Extend the bridge with plan status/advance/cancel operations and idempotency keys.
- Prepare tickets only for the current approved wave.
- Create one distinct OpenCode-native child session per planned task and execute parallel waves with bounded `Promise.allSettled` behavior.
- Display plan size, decision rationale, current wave, named child sessions, completion state, and blockers through tool metadata and the final envelope.
- Add Chronicle events for `plan.created`, `wave.started`, `wave.completed`, `join.completed`, and plan failure/cancellation.
- The file-backed Slice 1 authority now persists prerequisite validation, logical-slot uniqueness, prepared replay identity, owner epochs, callback fences, accepted terminal results, and authoritative joins before advancing the orchestration projection. Native child-session identities and restart commands use that authority. A takeover acquires a greater epoch before doing work; the previous owner stays fenced. An uncertain dispatch requires explicit audited resolution and is never silently redispatched. Chronicle plan-event projection remains separate hardening work, and semantic/schema failures are never retried automatically.

**Exit evidence:** crash-point integration tests plus a real OpenCode smoke proving two independent reads overlap and a dependent task waits.

### Slice 3b — Isolated native edit broker

**Status: implemented for exclusive text replacement/creation and durable worktree evidence. Explicit review, merge, cleanup, and parallel mutation remain rollout work.**

- Require a clean canonical Git repository root and bind every write ticket to the current immutable `HEAD`.
- Create a detached sibling worktree without checkout hooks or filter execution; never place a CodeGraph-dependent worktree in a generic temporary directory or reuse another checkout's index.
- Give the implementer only ticket-bound read/edit brokers. Existing files require an expected content digest; creation is explicit; sensitive paths, aliases, submodules, deletion, rename, and permission changes fail closed.
- Persist every edit receipt and reject completion if Git or filesystem state contains any change not explained by those receipts.
- Publish the worktree, base revision, final path/content receipts, and manifest digest through native completion and the durable orchestration projection.
- Keep the source checkout unchanged and retain the isolated worktree for explicit validation, Delivery Authority review, integration, or removal.

**Exit evidence:** focused bridge/control-plane/orchestration tests cover stale digests, source isolation, direct mutation rejection, alias rejection, and authoritative artifact propagation.

### Slice 4 — Delivery Authority v1

**Status: implemented for the product-native receipt core and CLI gates. Automatic hook/branch-protection wiring remains rollout work.**

- Add `TargetSnapshot` for base/candidate identity, changed paths, policy, prompt, Registry, provider, and model identities.
- Add `EvidenceManifest` for focused commands/checks, exit status, output digest, runtime/toolchain versions, and timestamps.
- Add a content-bound `ReviewReceipt` with risk class, lenses, findings, corrections, verdict, and rollback boundary.
- Reuse the same receipt at post-apply, pre-commit, pre-push, and pre-PR gates. Gates validate; they never silently start a new review budget.
- Invalidate explicitly when candidate content, base, scope, policy, provenance, or evidence changes.

**Exit evidence:** mutation of every bound identity invalidates the receipt; unchanged evidence remains reusable across gates.

The delivered `vgxness delivery issue|status|validate|invalidate` boundary builds the candidate from a temporary Git index/object store, so an unchanged tree keeps the same identity while moving from worktree to index to commit. Receipt files are immutable and SHA-256 addressed; the atomic current pointer records active or explicitly invalidated state. Sensitive-path changes, failed checks, rejected reviews, unresolved critical/blocker findings, and risk/lens mismatches fail closed. `validate` accepts only the four lifecycle gates above and never starts or renews a review.

### Slice 5 — Product hardening and rollout

**Status: partial. The orchestration lifecycle CLI, generated-runtime smoke, and Delivery Authority inspection/gate CLI are implemented; incident bundles, automatic gate installation, broader crash matrices, and release rollout remain.**

- Add `vgxness orchestrate status|resume|cancel|explain` and review/gate inspection commands.
- Export a sanitized incident bundle with plan, timeline, identities, failures, and digests but no secret values or unrestricted prompts.
- Add race, cancellation, duplicate-owner/cross-factory alias, delayed admission response, stale-owner fencing, callback reentry, leader-context cancellation, checkpoint adoption, claim/admission/terminal response-loss replay, revoked/expired takeover, partial/uncertain dispatch, corrupt plan, partial wave, malformed result, capacity, and recovery tests.
- Update the explanatory setup wizard, English/Spanish product docs, generated OpenCode artifacts, compatibility checks, and rollback instructions.
- Reinstall the candidate binary/configuration and run real Desktop/CLI smoke tests before publishing.

## Delegation decision table

| Condition | Decision |
| --- | --- |
| One bounded task provides the full answer | One native child; do not add delegation ceremony. |
| Tasks are independent, isolated, read-only, and within budget | Run in one parallel wave, up to four native children. |
| A task consumes another task's result | Place it in a later wave. |
| A task uses continuity | Run exclusively. |
| Review depends on implementation or evidence tasks | Run review in a later exclusive wave. |
| A task mutates files | Run one exclusive `implement/write-files` task in its ticket-bound worktree; never merge silently. |
| Dependencies cycle, identity is reused, or policy/capacity is unclear | Fail closed with the smallest safe next action. |

## Definition of done

- A manager submits one high-level goal and cannot choose agents or force parallelism.
- VGXNESS returns an explainable `single`, `sequential`, or `parallel` decision.
- Every execution receipt correlates `planId`, `runId`, scheduler owner, schedule epoch, `taskId`, `ticketId`, `parentSessionId`, `childSessionId`, and `messageId`.
- Independent reads demonstrably overlap; dependent, continuity, review, and mutation work never overlaps illegally.
- Cancellation, partial failure, restart, and stale state preserve durable evidence and never advance an unverified wave.
- The UI exposes the delegation plan and child progress without dumping raw protocol envelopes.
- Delivery gates reject stale or mismatched evidence and never approve a tree that was not reviewed.
