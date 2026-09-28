# IMP-004 — Research: child→parent progress communication

Architecture research note (guided by `software-architecture-docs`). Evidence-bound;
no runtime execution. Writer nonces `imp004-plan-draft-20260922-d` →
`imp004-finalize-plan-20260922-f` → `imp004-plan-correction-20260922-h` →
`imp004-plan-ready-20260922-l`.

> **CANCELLED (2026-09-24, nonce `imp004-discard-records-20260924-d`):** IMP-004 was abandoned at
> the user's request; this note is retained as **static archival research only** and makes no current
> approval claim. The IMP-004 source/tests were removed from the repository; no IMP-004 feature is
> installed. IMP-003 remains closed and preserved.

## Decision context (D1 resolved)

The user answered **"Pi primero (Recomendado)"**: the initial phase is a **Pi-native
background-first POC gate**. This note targets Pi first; OpenCode and Codex remain
future phases with **no inherited authorization**. The research is **static evidence**;
**no runtime behavior was observed**. Planning review `imp004-planreview-20260922-k`
returned **PASS**; this note makes **no runtime claim**.

## Architecture question and boundary

- **Question:** can a delegated child deliver progress/blockers/questions/partial
  results to the parent Manager in time for the parent to update its **canonical
  Markdown plan and projected `todowrite`** *before* the child terminates?
- **Boundary:** Pi first (per D1); Codex/OpenCode deferred. **No** new daemon, broker,
  protocol, or transport is assumed permissible.

## Evidence classes

- **Documented** — external official docs/SDK source, cited but **not re-run here**.
- **Repository** — static, verified in this workspace at HEAD `c4c6450`.
- **Runtime** — **none obtained in this mission.**

## Findings (reconciled; earlier overclaims withdrawn)

### Pi (selected initial phase)

- **Repository.** `task.ts:39-46` `await runner.run(...)` resolves only after the
  worker settles, then builds a terminal envelope. `result.ts:3` states are
  `succeeded|failed|cancelled|unavailable`. `runner.ts:108-154` buffers
  `message_update`/`tool_execution_end` and resolves on `agent_settled` (141-146)
  with **no parent-model callback while awaiting**. `runner.ts:44-57` serializes at
  most one writer. `runner.ts:87` spawns `--no-session`. `mission.ts:21-26,44-47`
  enforce nonce uniqueness + sha256 digest replay binding; `acceptMission` re-derives
  the digest. `packages/pi/test/task-candidate.test.ts` covers candidate binding.
- **Documented (SDK `0.84.4`, not re-run; app observed `0.85.1`).** `agent-loop.js:139`
  awaits `executeToolCalls` and `:455` awaits `tool.execute`; steering is polled only
  after completion (`:158`); custom messages are queued while streaming
  (`agent-session.js:1123-1128`) and flushed at `turn_end` after all tool results
  (`:425-426`); `onUpdate` emits UI `tool_execution_update` (`:455-464`) with no
  parent-model callback during await. `sendMessage` exists with `deliverAs`
  steer/nextTurn (types `:970-983`), but the queued path **alone is insufficient**.
  **Version difference (`0.84.4` vs `0.85.1`) must be gated, not assumed.**
- **Conclusion.** Event/UI visibility ≠ parent-context delivery ≠ model action. Truly
  live would require a **return-handle/background task extension first**, then a
  native event→parent notification, then an **observed** parent TODO update before
  child terminal. No unconditional feasibility is claimed.

### Codex (future phase — untouched, no inherited authorization)

- **Repository.** Host transport native-config only; `codex_delegation_test.go:115`
  runs `codex exec --json` and `:124` gates on `type=collab_tool_call` — a **test
  assertion**, not runtime evidence of parent-model child progress during an await.
  No app-server integration here.
- **Documented (not observed).** `turn/steer` (`expectedTurnId`), `turn/plan/updated`,
  `collabToolCall` sender/receiver IDs are client surfaces; `subagents.md`
  follow-up/wait routing does not demonstrate intermediate parent notification.

### OpenCode (future phase — untouched, no inherited authorization)

- **Repository.** `current_renderer.go` renders role permissions and the native-task
  adapter — a **renderer**, not a progress channel. `context_eval_test.go:387`
  requires missing child trace to be **INCONCLUSIVE, never PASS**.
- **Conclusion.** Native parent delivery is **unverified / unwired**, **not** proven
  "unavailable".
- **Documented (not observed).** Server/SDK expose children, todo, session messages,
  `prompt_async`, and event SSE as client APIs; these do not by themselves deliver
  progress into the parent model.

## Delivery model vs UI (layer separation)

Three layers: **(1)** event/UI visibility, **(2)** parent-context delivery, **(3)**
parent-model action. Only layer 3 updates the canonical TODO. Current evidence
supports layer 1 only — hence the POC hard-trace gate.

## Risks

- **Untrusted child messages.** Progress/blockers/questions/results are untrusted,
  bounded data; schema/bounds validated, never authority; **correlation ≠ permission**.
- **Writer distinction.** Only the parent writes the canonical plan/TODO; a child may
  continue its **own scoped code edits** under the existing mutation grant, but plan
  files are reserved to the parent and never edited simultaneously on the same path,
  with **at most one code-workspace writer**. A child claim never marks work done
  without evidence; the parent persists before projecting.
- **Version/provider drift.** `0.84.4` local vs `0.85.1` observed; behavior gated on
  observation, not assumed.

## Withdrawn overclaims (retained as history)

Unconditional "live" feasibility; any provider "globally unavailable";
"sendMessage exists ⇒ live delivery" — all **withdrawn**.

## Security boundary — POC threat note (guided by `security-boundary`)

Applied to the authorized T05 POC scope only (nonce `imp004-poc-build-20260922-n`).
Evidence labels: **fact** = repository/observed, **inference** = reasoned,
**unknown** = not verified.

- **Assets / actors / trust zones (fact).** The parent Manager session, the child Pi
  RPC process, the canonical Markdown plan under `docs/implementations/`, and the
  local model/auth configuration. The child's progress events are **untrusted data**,
  never authority or instructions; the parent's own model action is the only thing
  that writes canonical state.
- **Data / tool flow table (fact).**

  | Source | Data | Destination/tool | Effect | Trust transition | Approval required |
  | --- | --- | --- | --- | --- | --- |
  | Child model | bounded typed progress event (allowlisted counters + bounded summary) | parent `sendMessage` → parent context | queue + trigger parent turn | untrusted → untrusted (data only) | none (data) |
  | Parent model | plan update | Markdown plan persist | canonical write | trusted parent action | user T05 authorization |
  | Parent model | todo projection | `todowrite` (`appendEntry`) | session view | trusted parent action | none beyond T05 |
  | Host | existing model/auth | child provider call | **live only**, out of scope here | trusted host | separate live authorization + budget |

- **Untrusted-event rule (fact/control).** A progress event is **never** treated as an
  instruction. It cannot select tools, change the plan, mark work done, or widen
  authority; `correlation ≠ permission`. The parent decides the milestone update from
  its own evidence.
- **Least privilege (fact/control).** The POC child extension registers **only** the
  read-only `report_progress` tool — no commands, no memory, no ambient sessions, no
  filesystem mutation. Built-in tools are disabled by an explicit allowlist. No
  global/shared install and no permission expansion occur.
- **Secrets (fact/control).** No credential, token, or raw auth file is printed,
  copied, logged, or embedded in fixtures or reports. The auth/bootstrap/lifetime
  descriptors **fd 3/4 are never reused** for arbitrary progress. No host config or
  auth dump is performed.
- **Confused-deputy / composition (control).** Child output carries bounded data only;
  it cannot compose a sensitive-read-plus-send path, and no external mutation is
  reachable from the POC. The parent's `todowrite` is fixture-scoped in tests.
- **External mutation / destructive action (control).** None in this mission. The
  live run is gated and bounded; process-group cleanup targets only the owned group.
- **Residual risk / unknown.** Whether the installed `0.85.1` runtime actually
  delivers a child event into the parent **model** turn while the child is alive is
  **unverified** until the live run; the prototype's simulated ordering is **not**
  runtime proof. Host enforcement of these controls is **unknown** from static
  inspection alone.

## Selected direction and its gate

**Pi-native background-first POC gate** (D1). The **SDK-gate check (T05a) is a leaf
prerequisite** for T05b–T05d. **PASS is an aggregate** (user POC authorization + T05a–T05e
evidence + no unresolved blocking failures), not the trace alone; the hard trace is
**child alive + task call returned + event delivered + parent LLM emits `todowrite`
before the child terminal**, plus observed **plan persist before projection**; a UI
callback enqueue alone **fails**. A minimal isolated unit/integration sanity check and
an **approved budget** precede live calls. On failure/unsupported: **stop affected
work**, record the reason, return to the user — **no silent fallback/new daemon** (the
checkpoint alternative was **not selected**).

## Live causal finding — `deliverAs:"nextTurn"` semantics (nonce `imp004-poc-pause-20260922-aa`)

The authorized live run returned **INCONCLUSIVE**. Independent review
`imp004-poc-live-verify-z` root-verified the cause from the installed Pi `0.85.1`
source, `dist/core/agent-session.js`: a `sendMessage(..., { deliverAs: "nextTurn",
triggerTurn: true })` **pushes the message onto `pendingNextTurn` and ignores
`triggerTurn`** (`:1099-1111`); that queue is drained only when the **next user
prompt** arrives (`:910`). Because the harness sends exactly **one** prompt, the
child-progress message was **queued, never consumed** by the parent model — the
missing `persist_plan`/`todowrite` tool calls are **not** a model refusal and
**not** a Pi defect. This corrects the earlier “delivered/forwarded” description to
**QUEUED**. **Correction applied** (nonce `imp004-poc-deliveryfix-20260922-ab`, on
the user's "Corregir y probar"): the trusted parent extension now delivers with
`deliverAs:"followUp"` + `triggerTurn:true`, which the same source shows is correct
while **streaming** (`agent.followUp` queues, delivered as a new turn at turn end)
and while **idle** (`triggerTurn` starts a new turn immediately). A native
`turn_start`-derived `host_parent_message_consumed` witness distinguishes CONSUMED
from QUEUED, and `steer` is intentionally not used for a discrete progress
notification. The fix is proven only by non-model tests; the one remaining live
diagnostic is reserved for a separate Manager RUN after fresh reviews.

## Acceptance (nonce `imp004-poc-acceptance-record-20260922-an`)

The Manager **accepted T05 POC only** on the exact **C5** aggregate
`99f4d2b1…d33820`, after source verifier `imp004-id-verify-aj` PASS + CARE
`imp004-id-care-ak` PASS and independent post-live `imp004-id-liveverify-am` PASS
(all 12 checks recomputed from real metadata; full 83-char id exact, no prefix).
`host_parent_message_consumed` remains a **turn_start proxy** with no independent
content proof; acceptance rests on the **actual model `persist_plan`→`todowrite`
tools + exact binding**. No production claim; **T06 remains unauthorized**.

## Evidence limits and confidence

- **Confidence:** high for repository citations; **medium** for documented external
  surfaces (cited, not re-executed); **high** for the single accepted **C5** live
  trace (objective metadata + exact binding) — but **POC-only**, with a disclosed
  75 s artificial hold and no natural-throughput/performance claim.
- No raw logs or secrets included. Acceptance is **POC-scoped**; no always-works or
  global-provider claim.

## T06 source acceptance, pre-live (nonce `imp004-prod-prelive-record-20260923-n`)

The **T06 production source** was accepted **pre-live** on the exact sorted 19-row
manifest `0d792e13775ff8afd6ecb7646cd877bbda837e41956f51f7049bd51cb273483b` after
verifier `imp004-prod-verify-k` PASS, CARE `imp004-prod-care-l` PASS (source) and
specialist `imp004-prod-specialist-m` PASS (security claim corrected: fd5 is a
**known regular revocation file** with explicit revocation/expiry, **not** an fd4-EOF
channel). The parent/child **managed code-mutation race** is closed in-process by a
shared `WorkerRunner` writer slot; the native `tool_call` hook outside the managed
`apply_patch` path remains **best-effort, not a sandbox**. **Live production is not
observed**; the earlier **T05 POC** trace is **separate** and does not transfer. All
prior live budgets are exhausted. Plan **paused**, active **none**; **T08
install/delivery unauthorized**.
