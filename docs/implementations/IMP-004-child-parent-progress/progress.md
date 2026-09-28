# IMP-004 — Progress

Authoritative record of decisions, blockers and the next action. Canonical task
state lives in [plan.md](plan.md) and is not duplicated here; evidence lives in
[validation.md](validation.md); the architecture note is [research.md](research.md).

## Decisions

- **D1 — scope decision (RESOLVED by user).** To *"Esta elección solo completa el
  plan; todavía no ejecutaremos implementación"* the user answered **"Pi primero
  (Recomendado)"**: a Pi-native **background-first POC gate**. This completes the plan
  only — **no** implementation/POC/tests/runtime is authorized. The cross-provider
  checkpoint alternative was discussed and **not selected**.
- **D2 — authorization boundary.** Investigation + planning only. Not authorized:
  implementation, source/tests, runtime, install, Git delivery, memory, independent
  verification, model calls, SDD. Active plan stays **none**.
- **D3 — evidence discipline.** Every claim is documented / repository / runtime; no
  runtime or feasibility claim without observed evidence. Earlier overclaims withdrawn.
- **D4 — three layers.** Event/UI visibility ≠ parent-context delivery ≠ parent-model
  action. Only the parent's model action updates the canonical TODO.
- **D5 — writer distinction.** The parent is the sole **plan/TODO** writer and persists
  the Markdown plan before projecting to `todowrite`; a child may continue its **own
  scoped code edits** under the existing mutation grant, but **plan files are reserved
  to the parent** and are never edited simultaneously on the same path, with **at most
  one code-workspace writer** preserved. A child claim never marks work done without
  evidence.
- **D6 — progress is untrusted and meaningful.** Bounded untrusted data; the parent
  updates at **meaningful milestones**, never fake percentage ticks.
- **D7 — child questions.** A child `needs_input` stops that child's **own** dependent
  work; the parent answers **within its authority** or escalates to the user.
- **D8 — version gating, no assumption.** SDK pinned `0.84.4` vs app `0.85.1` observed;
  the POC verifies the installed SDK and assumes no signatures.
- **D9 — truthful recovery.** A persisted recovery checkpoint states process status
  truthfully and does **not** imply surviving a restart; late events rejected by identity.
- **D10 — failure discipline (failure-only).** `T05f` state is exactly `pending`;
  it is **triggered only if any of T05a–T05e fails/unsupported**, where it **stops
  affected work**, records the reason, returns to the user, and makes **no silent
  fallback** and no new daemon/protocol adoption. If **all** of T05a–T05e PASS, T05f
  is set to **`cancelled` with a documented reason** (no other state is added).
  `T06` is **forbidden until T05 PASS and a new production authorization**.
- **D11 — Pi-only initial scope.** OpenCode and Codex are future phases with **no
  inherited authorization**; their code stays untouched, and no commitments are created.
- **D12 — POC hard gate.** True-live requires child alive + task call returned + event
  delivered + parent `todowrite` before the child terminal; a UI enqueue alone fails.
  **T05 PASS is aggregate** (user POC authorization + T05a–T05e evidence + no
  unresolved blocking failures), not the trace alone.
- **D13 — minimal POC rigor.** An isolated unit/integration sanity check runs **before
  any live call**, and the test/call budget must be approved first. **No tests were run.**
- **D14 — guide validated before reading.** `software-architecture-docs` hashed
  `4e3a9c90970bb70a24044dd8e7d43dec721de7b9bc320475f68c94b6248cf714` (exact match)
  before loading; only that guide and its relevant note, no catalog.
- **D15 — no SDD.**
- **D16 — finalization complete.** T04 finalized; CARE docs review
  `imp004-planreview-20260922-k` **PASS** on the planning criteria; T04 marked **done**.
  No separate review task ID created.
- **D17 — prior failures closed as history.** CARE `planreview-g` **FAIL** (F1 the SDK
  gate was not a leaf prerequisite; F2 the conditional failure path was not expressed;
  F3 writer distinction optional) and CARE-i **F2** were corrected under
  `imp004-plan-correction-20260922-h`. **Rejected text (retained as history):** the
  prior `pending (until outcome)` state cell and the `not-applicable` alternative are
  rejected; `T05f` state is exactly `pending`, and an all-pass outcome is `cancelled`
  with a documented reason. All prior failures are **closed history**.
- **D18 — readiness.** The Manager **accepts** the finalized plan per CARE
  `imp004-planreview-20260922-k` **PASS** (planning criteria; **no implementation
  candidate**). The plan stays **`pending`** (not closed; no implementation started);
  active plan **none**; ready pending the user's implementation authorization. T05
  onward remain pending and **unauthorized**.
- **D19 — T05 authorization (user, explicit, 2026-09-22).** The user authorized
  **only** the bounded Pi POC (T05): no T06 production, no install, no
  `commit`/`push`, no memory. Nonce `imp004-poc-build-20260922-n`. The plan becomes
  **`active`** with the single focused leaf **T05**; T06 onward stay **unauthorized**.
- **D20 — live budget (user, explicit).** At most **two whole scenarios total**
  ("Sí, hasta dos ejecuciones"), each **1 parent + 1 child**, **≤180 s** per
  scenario, using the **existing configured model/auth** with **no** model, settings,
  or credential change, **no retries**, and **no** automatic model fallback. **Zero**
  live calls until independent review; the live run is reserved to a separate
  Manager-authorized execution. A ledger enforces the two-scenario cap.
- **D21 — recorded technical defaults (not user questions).** Live target = the
  **installed Pi `0.85.1`**; test/dev SDK = **`0.84.4`**; **no dependency upgrade**.
  The typed child report travels as a **native tool event**; the
  auth/bootstrap/lifetime descriptors **fd 3/4 are never reused** for arbitrary
  progress; **no** extra protocol/broker daemon; the **parent is the sole
  plan/TODO writer** and the child is **read-only** in this POC. Future mutation
  rules (one code-workspace writer, plan files reserved to the parent) are
  **preserved but not exercised** here.
- **D22 — guide validated before reading.** `agent-evaluation` sha256
  `0cc075f59b6c6e3afcbfcd6a2695398f599266b0b1ff70e5ebbdf0244ad694bc` and
  `security-boundary` sha256
  `c0c49bc310ec6ee059582502b17963a8c0ffc522fc165b5ab43b4e88a4cfa9e4` matched the
  canonical source paths exactly before any body load. Only required relative
  resources are read; no catalog; a mismatch blocks that dependency and is never
  substituted silently. (The `uninstall-0` backup copy of `agent-evaluation` differs
  and was **not** loaded.)
- **D23 — POC scope and evidence discipline.** Authorized artifacts are **test-only**:
  new modules under `packages/pi/test/poc-child-progress/` and
  `packages/pi/test/child-progress-poc.test.ts`, plus this IMP-004 records set and the
  index row/note. **No** production `packages/pi/src/**` change, **no** generated
  resource/contract/golden change. Unit/integration tests use a **deterministic
  simulated transport**; the **live** path is gated behind an explicit authorization
  flag and is **not** executed in this mission. Simulation and live are reported
  separately.
- **D24 — AC05 stays unproven.** A test-only prototype cannot emit a real parent
  **model** tool call. The simulation proves only the scheduling/ordering machinery
  (early handle → bounded event → parent `sendMessage` enqueue → plan persist before
  `todowrite` → child terminal). **AC05 requires the separate live run plus
  independent reviews**; `T05e` stays `pending`.
- **D25 — prototype frozen (test-only, no live call).** The POC prototype is built
  under `packages/pi/test/poc-child-progress/` with
  `packages/pi/test/child-progress-poc.test.ts`. Permitted checks pass: `typecheck`,
  the full Pi suite (183/183, including 9 new tests), `generate-contract --check`,
  `verify-package`, offline Go `./internal/providers/pi ./internal/release`, and the
  redacted `--help`/`--preflight` smoke (zero model calls). No production `src`,
  generated resource, contract or golden was changed.
- **D26 — live harness built (test-only; no live call).** Continuation nonce
  `imp004-poc-livebridge-20260922-p`. The prototype now includes an **executable**
  live harness: a trusted parent Pi RPC extension (`parent-extension.ts`) exposing
  only `start_child`/`persist_plan`/`todowrite`/`poc_status`; a child RPC process
  whose `report_progress` emits a **native `onUpdate` partial result** (no custom
  stdout lines that would corrupt RPC); native `tool_execution_update` parsing; a
  host/model witness classifier (`host_*` vs `model_*`); a **durable** results-dir
  ledger (max 2, no reset, one child spawn per scenario, committed before parent
  spawn); and a UTF-8 byte-budget wrapper that **rejects** over-budget messages
  instead of slicing JSON. `runLive` is hard-gated and **not executed**. Proposed
  prototype-source sha256
  `88268e9eeda8d2a854149f2aa704fb579e10c1066f3aa10faf9ceebe438a9152` (per-file in
  [validation.md](validation.md)); the D25 hash is superseded.
- **D27 — defect-fix round (reviewer + verifier on frozen `88268e9e…`).** Nonce
  `imp004-poc-fix-20260922-s`. Prior FAIL findings were reproduced as concrete
  regressions and fixed (no TDD claim; tests were written against observed
  behaviour). **All closed on actual evidence:**
  - **CARE-F1** generated wrappers now embed the trusted absolute config path as a
    JSON string literal (no unset-env dependency). A **real zero-model probe**
    spawns the installed Pi `0.85.1`, loads the extension and reads `get_state` +
    `get_commands` (never `prompt`). Observed: `commands:["poc-status","llama"]`,
    `promptSent:false`, effective model `openai-codex/gpt-5.5`, thinking `medium`.
  - **Verifier-F1** one shared durable ledger root across runs; per-scenario
    reservations are unique; a second mock scenario no longer collides on
    filenames.
  - **CARE-F2** one host monotonic timeline (`order` + a single clock); the
    extension records a synchronous `host_todo_body` snapshot (`childAlive`,
    `childSettled`) and the gate closes on the parsed native `agent_settled`; the
    hard gate is **INCONCLUSIVE** on missing witnesses and **FAIL** on late/not-alive
    — never a fake PASS.
  - **CARE-F3** `persist_plan` never marks done: `result → in_progress` with
    `evidence: pending independent review`; `blocker/needs_input → blocked`;
    `todowrite` rejects `completed`.
  - **CARE-F4/Verifier-F2** `recordChildSpawn` runs under lock **before** the child
    spawn, returns the real count, and a restart cannot spawn again.
  - **CARE-F5** the child is spawned **non-detached** (parent group); a
    `SessionSupervisor` tracks own handles and SIGINT/SIGTERM aborts only them; no
    PID-file-based independent kill.
  - **Verifier-F3** parent argv uses the **host default** model (no
    `--provider/--model`); CLI verified at
    `/Users/uzielvgx/.nvm/versions/node/v24.15.0/bin/pi` v`0.85.1`, no fallback.
  - **Verifier-F6** one report per scenario + 75 s hold; exactly one `prompt` per
    process (asserted). Proposed prototype-source sha256
    `b04341316cd49aded00345ac94d798dd5d77a10b80915d40322d255c4bce2ff6`; the D26 hash
    is superseded.

- **D28 — wire-fix round (nonce `imp004-poc-wirefix-20260922-v`).** Continuation
  authorization for **only** T05 Pi POC source wiring; no model call, no install,
  no production `src`, no delegation, no Git delivery, no memory. Independent
  verifier/CARE source review established that the prior `b0434131…` candidate's
  **live `runLive` path was unwired**. Fixed at source: (1) child argv no longer
  appends a positional prompt (RPC ignores it and `/dev/null` stdin caused EOF
  exit) — the child is spawned with **PIPE stdin** and gets exactly ONE native
  `{type:"prompt"}` line after the spawn/load gate, stdin kept open; (2)
  `generateParentWrapper` now wires `recordObservation` (`observationAppender`)
  and `reserveChildSpawn` (`DurableLedger` at the BUDGET ROOT, nonce-bound) into
  the generated module; (3) one fixed BUDGET ROOT holds the ledger and each
  scenario gets an exclusively-created `${missionNonce}--${scenarioId}` subdir;
  (4) a FULL-ENTRY regression invokes the actual exported `runLive` twice on one
  root through a zero-LLM fake Pi CLI implementing the real stdin RPC and loading
  the generated wrapper; (5) the hard gate now binds the todo snapshot to the
  actual native parent `todowrite` tool-call id, mission nonce and child
  generation, and accepts an OWNED termination after the todo projection as a
  legitimate terminal; (6) docs state the model selection truthfully (host
  default `ctx.model`, no parent overrides, child mirrors provider/model/thinking;
  local dev SDK `0.84.4` cannot prove installed `0.85.1`). Zero-model load probes
  run for **both** parent (`poc-status`) and child (`poc-report-status`). Permitted
  checks pass: POC typecheck, POC tests **24/24**, full Pi suite **198/198**,
  workspace typecheck. Prototype aggregate sha256
  `a5f52c8338e1a2c03a29d9e29ba02a695d05f18b21d54396f2e9a9aa8fd537f3`
  (12 artifacts; supersedes `b0434131…`). **C1/C2 closed at source.** T05e live
  remains **pending, 0 live calls**.

- **D29 — live run authorized (nonce `imp004-poc-live-20260922-y`).** The user
  APPROVED **max 2 actual scenarios** (1 parent + 1 child each, ≤180 s) using the
  existing configured model/auth; the Manager authorized **RUN** of the frozen C3
  prototype after verifier `imp004-poc-verify-w` **PASS (pre-live)** and CARE
  `imp004-poc-care-x` **PASS (source readiness)**. Frozen aggregate
  `a5f52c83…37f3` (12 files) matches. **No source/test change**, no install/commit/
  push/memory/delegation; the 12 frozen files are read-only and re-hashed after the
  run. Single fixed BUDGET ROOT for both cases (durable ledger max 2);
  `VGXNESS_POC_LIVE=1` + `--run --scenario <id> --results-dir <root>` is the exact
  supported consent (there is **no** `--authorize` flag). Scenario 2 runs only if
  scenario 1 **PASS**es with no unexplained cleanup issue; the second is not
  consumed to “fish” for a pass. T05e is **`in_progress`**; T05f stays `pending`
  until the outcome; T06 onward remain **unauthorized**. This is a **controlled live
  authorized** run, not a protected holdout or natural production-performance
  benchmark.

- **D30 — live run executed, scenario 1 INCONCLUSIVE; stopped (nonce
  `imp004-poc-live-20260922-y`).** The authorized controlled live run executed
  **scenario 1 (`poc-progress-hold`) only** and returned **INCONCLUSIVE**; per the
  gate the second scenario was **not** consumed. Actual source-bound evidence
  (sanitized): parent model turn 1 emitted a native `start_child` tool call
  (execution id `call_OocDupUAN8VKK7sr7A4kT49b|fc_…`); the child's native
  `report_progress` (seq 1, eventId `c1`, 0/1) was observed and **QUEUED**
  (`host_parent_message_queued`) — **correcting the earlier “forwarded/delivered”
  wording: it was queued, never consumed**; the parent's second turn ended with
  `message_end [stop:stop]` and **no `persist_plan`/`todowrite` tool call**, so all
  parent-model hard-gate witnesses are missing. Child `agent_settled` arrived at
  ≈75 s (disclosed artificial hold). `promptCount=1`, `reason=timeout`,
  `elapsedMs=180008`, `stderrBytes=0`, effective model `openai-codex/gpt-5.5`,
  thinking `medium`. Cleanup verified: the owned child is dead, no lingering
  `pi`/POC processes, no global `pi` killed; no `plan.md` written. On-disk ledger
  is fresh: 1 reservation, `childSpawns:1` (the embedded result snapshot shows the
  known stale `childSpawns:0`; on-disk is authoritative). A pre-reservation
  `host_child_kill:session_shutdown` from the zero-model probe is present in
  `observations.jsonl` and did not affect the verdict. **T05e `blocked`; T05f
  triggered (`blocked`); AC05 unproven; no source/test change; next action = user
  decision.** T06 onward remain **unauthorized**.

- **D31 — pause + exact causal finding (nonce `imp004-poc-pause-20260922-aa`).**
  Docs-only. Independent live review `imp004-poc-live-verify-z`: **evidence PASS,
  actual behavior INCONCLUSIVE**, **root verified** by static read of installed Pi
  `0.85.1` `dist/core/agent-session.js` — `deliverAs:"nextTurn"` **pushes
  `pendingNextTurn` and ignores `triggerTurn:true`** (`:1099-1111`), drained only on
  the **next user prompt** (`:910`). The harness sends **one** prompt, so the child
  message was **QUEUED, never consumed** by the parent model. This is **not** a
  model refusal and **not** a Pi defect; it is a **harness wrong-delivery-mode**
  bug. A proper fix would use `followUp`/`steer` streaming, or `triggerTurn` while
  idle — **not implemented and not proven** here. No source change. Frozen 12
  aggregate `a5f52c83…37f3` unchanged (prod untouched). Ledger **1 of 2**,
  `childSpawns:1`, **remaining 1 UNSPENT**; budget artifacts **not reset**, no
  retries. Preserved findings (not fixed): the pre-reservation
  `order:0 host_child_kill` probe pollution caused no false PASS; the embedded
  result ledger snapshot `childSpawns:0` vs **on-disk** `1` is the known stale
  snapshot. Process cleanup: **none owned left**. Per the plan's explicit failure
  gate: **STOP and consult the user before any corrective follow-up**. Plan marked
  **paused**, active plan **none**, index state **paused** (not closed); **T05
  blocked, T05e blocked, T05f done** (stop/record/escalate; decision next pending);
  **T06+ unauthorized**. T05b–T05d remain **prior prototype unit-checked**, not
  runtime success; current candidate **AC05 unproven**. Next action asks the user to
  choose the **bounded POC correction + the one remaining live diagnostic** vs
  **stop** — not a broader production question.

- **D32 — resume + bounded delivery-fix correction (nonce
  `imp004-poc-deliveryfix-20260922-ab`).** IMP-004 resumed **active** (index back
  from `paused`). The user answered **"Corregir y probar (Recomendado)"**: authorizes
  a **bounded prototype correction** plus **ONE** remaining live diagnostic (≤180 s,
  same configured model; **no** production/install/budget increase). **This mission
  = fix + non-model tests only.** **T05f is `done`** and retained as **old failure
  history**; **T05e stays `blocked`** (historical); a new stable **T05g** correction
  task is **`in_progress`**. The ONE live diagnostic is reserved for a **separate
  Manager RUN after fresh independent reviews** — not executed here.
  - **Delivery mode (root cause fixed at source).** Installed Pi `0.85.1`
    `dist/core/agent-session.js`: the `nextTurn` branch pushes `_pendingNextTurnMessages`
    and ignores `triggerTurn` (`:1109-1111`), drained only on the next user prompt
    (`:910`). The trusted parent extension now delivers with
    **`deliverAs:"followUp"` + `triggerTurn:true`**, correct in **both** states:
    streaming → `agent.followUp` queues and is delivered as a new turn; idle →
    `triggerTurn` starts a new turn immediately. The simulated `SendMessage` in
    `parent-bridge.ts` is aligned accordingly. A native **consumption witness**
    (`host_parent_message_consumed`) is recorded from the extension `turn_start`
    hook — distinct from `host_parent_message_queued`, so QUEUED is never mistaken
    for CONSUMED, and the model remains the sole caller of `persist_plan`/`todowrite`.
    A strict test asserts the prototype never uses `nextTurn` for autonomous
    progress.
  - **Other small fixes:** probes relocated to an **ephemeral probe dir** (no more
    order-0 `host_child_kill` pollution of scenario data) and `terminate` records a
    kill only when an owned child exists and was signalled; result `ledger` is
    **refreshed from disk** after parent close (authoritative `childSpawns:1`), with
    `paths` + `finalChecks`; child **stdin `error`** now fail-closes
    (`host_child_stdin_error`). New third case `poc-progress-recovery`; ROOT ledger
    hard cap stays **2**.
  - **Tests/checks (no live, no model):** POC **28/28**, full Pi suite **202/202**,
    POC typecheck, workspace typecheck, `generate-contract --check`, `verify-package`
    all pass. Zero-model real-CLI load (`--preflight`, no prompt): parent
    `poc-status`, child `poc-report-status`, `promptSent:false`, model
    `openai-codex/gpt-5.5` medium. The FULL-ENTRY regression drives the ACTUAL
    `runLive` twice through a zero-LLM fake Pi that follows the real scheduling
    (only runs tools after the message is consumed) — **not** real proof.
  - **Real budget root untouched:** ledger still **1 of 2**, `childSpawns:1`, the old
    `poc-progress-hold` reservation (nonce `…wirefix-v`) remains **INCONCLUSIVE**,
    **remaining 1 UNSPENT**; no reset, no retries. New candidate aggregate over the
    12 files: **`65fdbb547f90a626ab4f15f01ab9db55f39e66ed857d067c9dfaffb453c62c8d`**.
    **No source AC05 PASS claim.**

- **D33 — final live recovery run FAIL; budget exhausted; plan paused (nonce
  `imp004-poc-live-final-20260922-ae`).** Manager-authorized **single remaining**
  case `poc-progress-recovery` on the SAME root after `imp004-poc-verify-ac`
  **PASS (pre-live)** and `imp004-poc-care-ad` **PASS (source)** on
  `65fdbb54…c62c8d`. **No source change.** Result: **FAIL**.
  - **Substantive witnesses actually observed (native events):**
    `host_parent_message_consumed [consumed:1]` (turn_start proxy after the queue);
    child `report_progress` (identity `planId imp004-poc / task T05 / missionNonce
    deliveryfix-ab / candidate c4c6450… / seq 1 / eventId r1`); actual model
    `tool_execution_start/persist_plan` then `tool_execution_start/todowrite` with
    native ids; `plan.md` written (T05 in_progress, event r1) **before** the todo
    body; `host_todo_body` snapshot `{childAlive:true, childSettled:false,
    childGeneration:1, missionNonce:deliveryfix-ab}`; owned terminal
    `child_agent_settled` after the todo; `promptCount 1`, `stderrBytes 0`,
    `elapsedMs 179942` (≤180 s), model `openai-codex/gpt-5.5` thinking `medium`.
  - **Sole failing check `todoBoundToParentCall`:** the native classifier's
    `bounded(event.toolCallId, 64)` truncated the id to its first 64 chars while the
    extension snapshot holds the full 83-char id — the **same** `todowrite` call.
    Harness artifact, not a protocol failure; **not fixed** (no source edits this
    mission).
  - **No retries.** Fresh ledger now **2/2** (`poc-progress-hold` …wirefix-v, and
    `poc-progress-recovery` …deliveryfix-ab), each `childSpawns:1`; the original
    case remains INCONCLUSIVE. Probe isolation confirmed (observations start at
    `host_child_spawn`, no order-0 kill). Cleanup: no owned process; no unrelated
    kill.
  - **State:** plan **paused**, active plan **none**; **T05 blocked, T05g blocked,
    T05e blocked (history), T05f done**; T06+ unauthorized. **AC05 unproven**; next
    action = report the exhausted budget to the user. Candidate aggregate unchanged
    `65fdbb54…c62c8d`; production untouched.

- **D34 — final evidence recorded; single next action (nonce
  `imp004-poc-record-final-20260922-ag`, docs only).** Independent offline review
  `imp004-poc-final-evidence-af` **corroborates** the observation: native parent
  `persist_plan` (order 10) then `todowrite` (order 15); synchronous `host_todo_body`
  (order 6) `childAlive:true / childSettled:false / gen 1 / correct nonce`; child
  `agent_settled` order 8 after the todo; plan `in_progress` event `r1` seq 1. The
  recorded gate **FAIL** is a **result-checker bug**: `classifyParentEvent` bounds
  `toolCallId` to 64 vs the full 83-char snapshot id — the **tail 19 chars are lost**
  in the raw event and unrecoverable; the prefix is unique in scope but is **not**
  exact full identity. **No PASS retrofit, no criterion weakening, no model blame.**
  First INCONCLUSIVE (`nextTurn`) retained; `followUp` correction observed only in
  the second run. Budget **2/2 exhausted** = 2 parent + 2 child scenarios, 180 s
  timeouts each (not "4 API calls"; pricing not measured). C4 aggregate
  `65fdbb54…c62c8d` unchanged; production unchanged; 202 unit tests are pre-live
  checks, not artifact acceptance; review evidence is against **C4** and does not
  transfer from **C3**. Owned processes clean; no budget reset. Plan **paused**, not
  closed.

- **D35 — T05h ID-fix correction (nonce `imp004-poc-idfix-20260922-ai`, source
  fix + non-model checks only).** IMP-004 resumed **active**. User "dale sigamos"
  authorizes the checker ID-fix plus **EXACTLY ONE NEW live 180 s** on a **new
  budget** (old 2 immutable); **no** prod/install/delivery. New stable **T05h**
  depends on the prior **T05g** outcome (blocked dependency acceptable). **T05e/T05g
  `blocked` historical retained; T05f `done`; T06+ unauthorized.**
  - **Observed RED first:** the pre-fix `classifyParentEvent` truncated an 83-char
    id to 64 for `tool_call`/`tool_execution_start`/`_update`/`_end`.
  - **Fix (lossless policy):** `native-events.ts` adds `TOOL_CALL_ID_MAX_BYTES=512`
    + `readToolCallId` (non-empty UTF-8 string, ≤512 **bytes**; over-cap/empty/
    wrong-type **dropped**, never truncated, never prefix-matched) and uses it for
    every model tool id. The **512 budget is a POC policy, not a native SDK
    guarantee**. `parent-extension.ts` validates the snapshot id with the same
    policy (invalid ⇒ omitted, never the literal `"unknown"`). `live-scenarios.ts`
    keeps **strict equality** and treats a missing/invalid/over-cap snapshot id as
    **INCONCLUSIVE**.
  - **Explicit budget:** `runLive` validates `maxScenarios` (integer 1..2, default
    2) and passes it consistently to `DurableLedger.open`, the wrapper config, both
    probes, the fresh-ledger read, and `finalChecks.maxScenarios`; CLI
    `--max-scenarios <1\|2>`. `DurableLedger.open` rejects a **cap mismatch** on
    reopen (no downgrade/upgrade).
  - **Tests/checks (no live, no model):** 31 POC tests / **205** full suite; POC +
    project typecheck; `generate-contract --check`; `verify-package`. New coverage:
    full 83 retained; ≤512 accept / 513 drop; multibyte byte-count; empty/wrong-type
    drop; **same-64-prefix distinct-tail ⇒ FAIL**; over-cap snapshot ⇒ INCONCLUSIVE;
    cap-1 first PASS then second blocked **before any spawn**; invalid cap rejected
    before model. FULL-ENTRY fake CLI now uses actual-shaped ids and asserts the
    **full native callback id** matches.
  - **Candidate:** source commit `c4c6450`; new 12-file prototype aggregate
    **`99f4d2b10f674fa701f7bda418dbdd0c58d368d61f9fba5c5b04739212d33820`**
    (distinct from `65fdbb54…c62c8d`). **Production unchanged.** Old root
    `…/imp004-poc-live` **read-only, 2/2 preserved**; new root
    `…/imp004-poc-id-live` **not created** (later Runner only). **No live call; no
    AC05 PASS claim.**

- **D36 — T05h new live case PASS (nonce `imp004-poc-id-live-20260922-al`; no
  source change).** Manager-authorized **ONE** new real diagnostic on the **new**
  root `…/imp004-poc-id-live` with `--max-scenarios 1`, after verifier
  `imp004-id-verify-aj` **PASS (pre-live)** and CARE `imp004-id-care-ak` **PASS
  (source)** on `99f4d2b1…d33820`. **Result: PASS — 12/12 gate checks.**
  - **Actual native witnesses (real, not SDK mock):** child `report_progress`
    (`missionNonce imp004-poc-idfix-20260922-ai`, candidate `c4c6450…`, seq 1,
    eventId `r1`, 0/1); `host_parent_message_queued` → `host_parent_message_consumed`
    (turn_start **proxy**, labelled as such); **real model**
    `persist_plan` (before) then `todowrite`; `plan.md` = `T05: in_progress
    (event r1, seq 1)` (no child claim of done); snapshot
    `{childAlive:true, childSettled:false, childGeneration:1, mission idfix-ai}`;
    child `agent_settled` after the todo.
  - **Exact full-id binding:** snapshot id == native classified id
    `call_oLYpK6ZpN2mEPmCKs3XDT8ZU|fc_0dbc9ed87ac83c56016ab3492aa8a487d188545516910ff3c6`,
    `EXACT_EQUAL true`, 83 chars = **83 UTF-8 bytes** (no prefix).
  - **Counters:** `reason timeout`, `elapsedMs 180018` (controlled deadline),
    `promptCount 1`, `stderrBytes 0`; model `openai-codex`/`gpt-5.5`/`medium`
    (observed, not hardcoded). No token/dollar estimate fabricated.
  - **Budgets:** new root ledger cap **1** → **1/1 exhausted**, `childSpawns:1`;
    old root `…/imp004-poc-live` **2/2 unchanged, no reset**. Frozen aggregate
    after **matches** `99f4d2b1…d33820`; production unchanged. No owned process
    left; observations begin at `host_child_spawn` (no probe pollution).
  - **State:** **T05h `done`** (evidence ready); **T05 `in_progress` awaiting
    independent artifact verification**; **T05e/T05g `blocked` historical**;
    **T05f `done`**; **T06 unauthorized even if the POC passes.** No retroactive
    PASS for the old 2.

- **D37 — Manager accepts T05 POC (nonce `imp004-poc-acceptance-record-20260922-an`,
  docs only).** Accepted on the exact **C5** aggregate
  `99f4d2b10f674fa701f7bda418dbdd0c58d368d61f9fba5c5b04739212d33820` (HEAD
  `c4c6450`, prototype 12 untracked), after source verifier `imp004-id-verify-aj`
  **PASS** + CARE `imp004-id-care-ak` **PASS** (same candidate) and independent
  **post-live** `imp004-id-liveverify-am` **PASS** (all 12 checks recomputed from
  real metadata; full **83 chars == 83 bytes**, snapshot exact, no prefix).
  - **Observed (real):** native parent model `persist_plan` orders **12–13** →
    `todowrite` orders **17–18**; `host_plan_persisted` order **5** → `host_todo_body`
    order **6**; `childAlive:true / childSettled:false / gen 1 / mission
    imp004-poc-idfix-20260922-ai`; child native `agent_settled` order **8**. Full
    83-byte ids from real RPC data match the callback.
  - **Fixture:** task `in_progress`, `evidence: none`; a child `result` claim is
    **not done**. Child hold **75 s ARTIFICIAL**; child is read-only; **no actual
    feature implementation**.
  - **Budgets:** one authorized new scenario (1 parent + 1 child); new root cap **1**
    → count **1**, `childSpawns:1`, exhausted; old root cap **2** → count **2**,
    children 1 each, unchanged. Total **3 scenarios across user budgets — not 3 API
    calls**; may be multi-turn; **cost unmeasured**.
  - **Timing:** latest `reason TIMEOUT`, controlled 180 s timer measured
    **180018 ms** (~18 ms overhead) — **not** natural completion or a performance
    pass; cleanup: **no owned processes**, verified.
  - **Model:** host default `openai-codex`/`gpt-5.5`/`medium`; app `0.85.1`, dev
    `0.84.4` unchanged. `host_parent_message_consumed` is a **turn_start proxy** with
    **no independent content proof**; acceptance is based on the **actual model
    tools + exact binding**.
  - **Scope:** no always-works/global-provider claim; no code installed, no
    production defaults changed, no OC/Codex work, no commit/push. Tests **205**
    (incl. **31** prototype) pass; fixture tsc + generated checks are **pre-live code
    checks**, separate from live evidence; CARE is **limited to source readiness**,
    no unavailable-artifact mask. Guides `agent-eval`/`security` validated by the
    worker; prior failures/bugs recorded, **no history discarded**. All new docs/POC
    remain **uncommitted** (no git-clean claim).
  - **State:** **T05 `done` (POC only); T05h `done`; T05e `done`** (abstract live
    gate via the T05h C5 trace; earlier attempts remain INCONCLUSIVE first / FAIL
    C4); **T05g `done`** (confirmed current C5; earlier result record still FAIL);
    **T05f `done`**. All T05 substates `done`. **T06+ pending, still unauthorized**
    (new production/install authorization required). Plan **paused**, active **none**,
    index **paused** (not closed; production phase pending).

- **D38 — T06 production implementation authorized (nonce
  `imp004-prod-write-20260923-c`, Pi only).** The user ("dale hagamoslo") authorizes
  **T06 production implementation** and **T07 development checks** only. IMP-004 is
  resumed **active** with the single active execution plan **T06** (expanded into
  stable subIDs **T06a–T06g**; **T06a keeps its prior "truthful fallback gate when the
  native capability is absent" meaning**). **Not authorized:** install/reinstall, Git
  delivery, memory, independent verification, **live model calls** (all prior live
  budgets exhausted), delegation, or any new daemon/broker/DB/external transport.
  OpenCode/Codex bytes stay unchanged unless an unavoidable shared-contract change
  needs a Manager decision first. See the plan's "T06 production design defaults".
  - **State:** **T05 `done` (POC only, accepted on C5)**; **T06 `in_progress`**;
    **T06a–T06g `pending`**; **T07 `pending`**; **T08 pending/unauthorized** (install/
    delivery needs an explicit future authorization). Implementation is **not**
    acceptance and **no live/AC05 claim** is made by T06.

- **D39 — T06 production implementation delivered (nonce
  `imp004-prod-write-20260923-c`, Pi only; implementation, not acceptance).**
  Production modules added: `packages/pi/src/workers/progress.ts` (typed bounded
  schema + correlation gate + child `report_progress`), `workers/background.ts`
  (session-owned registry: early handle, bounded wait/lifetime/retention,
  generation suppression, cancel-all), `workers/mutation-guard.ts` (parsed
  `apply_patch` plan-prefix + native `tool_call` blocking), `tools/task-control.ts`
  (manager-only `status|wait|cancel|close`). Modified: `tools/task.ts` (opt-in
  `background:true` + explicit `progress {planId,taskId}`; sync default unchanged),
  `workers/runner.ts` (child `report_progress` registration only when the immutable
  mission opts in; RPC `tool_execution_update` ingestion), `workers/mission.ts` +
  `workers/context.ts` (mission progress binding in digest/prompt),
  `extension.ts` (registry wiring, `followUp`+`triggerTurn` notification, `tool_call`
  guard, session generation + cancel-all), `orchestration/adapter.ts` (documented
  behavior; prompt regenerated). New tests `test/background-task.test.ts` +
  `test/background-spawn.test.ts` (real fake-CLI spawn); `test/tools.test.ts` count
  updated deliberately to 15.
  - **Checks (all permitted; no install/live/git/memory):** Pi typecheck; new-test
    strict typecheck; POC typecheck; `node ./scripts/test.mjs` **223 pass / 0 fail**
    (205 pre-existing + 18 new); `generate-contract.mjs --check` pass with
    `sourceDigest` still **`a890…76ce`** (contract.json unchanged; only the manager
    prompt regenerated); `verify-package.mjs` pass; offline Go
    `./internal/providers/pi ./internal/release ./internal/orchestration ./internal/e2e`
    all `ok`.
  - **State:** T06 implementation delivered; **T06a–T06e, T06g `done`**; **T06 and
    T06f `in_progress`** (hard-crash parent-exit reaping is a residual); **T07
    `pending`** (independent verifier + CARE — not run here); **T08 unauthorized**.
    No live model call; no AC05 production claim; OpenCode/Codex unchanged.
  - **Residuals (reported, not silently cut):** no fd4/fd6 parent-exit watchdog (a
    hard Manager crash can orphan a detached background child); full-authority guard
    branch is unit-tested only; no mid-run `needs_input` resume; no automatic
    `todowrite`; Windows still fail-closed.

- **D40 — T06 hardening correction (nonce `imp004-prod-hardening-20260923-e`).**
  Read-only exploration of the delivered T06 candidate found **concrete HIGH bugs
  before independent review**; the candidate **cannot be frozen**. Required, not
  discretionary, findings and their disposition:
  - **F1 HIGH** `mutation-guard.patchTargets` scanned only `+++`, while the real
    `apply_patch.parsePatch` targets `newPath ?? oldPath!`. A parent patch could pair
    an allowed plan edit with a non-plan **delete** (`+++ /dev/null`) and pass the
    guard; leading `+++` hunk text could also be misread. **Fix:** reuse the real
    production parser via an exported pure helper (`patchTargetsFromDiff`) and treat
    an unparseable patch as fail-closed.
  - **F2 HIGH** reserved-target normalization differed between the guard and
    `mission.normalize`: case-insensitive filesystems (APFS) let `Docs/Implementations/`
    bypass. **Fix:** one canonical workspace-relative normalizer (resolve `.`/`..`,
    reject cross-OS/absolute escapes, Unicode **NFC + casefold for the reserved
    prefix only**) shared by the guard and the background-full admission check.
  - **F3 HIGH** `hasFullMutationAuthority` keyed off `queued|running` only; a
    cancel/expiry could settle `failed/recovery_pending` while the child was still
    alive, releasing the guard. Also `WorkerRunner` could start the next writer after
    an unconfirmed cleanup. **Fix:** an independent **authority-outstanding** flag
    that only clears on a **cleanup-confirmed** start result, plus a `WorkerRunner`
    recovery latch that blocks further writer/reader runs until a controlled restart.
  - **F4** the 1000 ms drain window was shorter than the real `rpc.stop` budget
    (SIGTERM+SIGKILL+fd4 ≈ 4 s). **Fix:** bounded cleanup drain default raised.
  - **F5** `session_start` only incremented a counter; old `readonly/full` jobs were
    not cancelled/drained before the new session context. **Fix:** an async
    `resetEpoch` that cancels + drains and retains any unconfirmed authority hold.
  - **F6 HIGH (previously left unimplemented)** parent hard-crash reaping: add a real
    owned fd4 lifetime watchdog in the worker extension.
  - **F7** oversized terminal notifications were silently dropped. **Fix:** a bounded
    terminal summary notification that always fires once, with retrieval via
    `task_control`.
  - **F8** the gate queue was drained immediately, so no rate/backpressure; late
    progress on a closed job used `cancel` instead of `close`; `close` could release
    a full hold. **Fix:** bounded coalescing/rate interval + conservative close.
  - **F9** update `docs/pi-typescript.md`; keep the shared contract (`a890…`) and
    OpenCode/Codex bytes unchanged.

- **D41 — T06h hardening delivered (nonce `imp004-prod-hardening-20260923-e`,
  implementation only).** All required F1–F9 findings were corrected in Pi production
  source with tests where a clear RED reproducer existed (no TDD claim):
  - **F1** the guard now reuses the real production parser (`patchTargetsFromDiff`
    exported from `tools/apply_patch.ts`); a mixed allowed-plan + non-plan delete is
    blocked, a plan-only patch (including a hunk line beginning `+++`) succeeds, and
    code create/delete are denied while a full job holds authority.
  - **F2** one canonical normalizer (separators, `.`/`..`, absolute/cross-OS reject,
    NFC + casefold for the reserved prefix) shared by the guard and background-full
    admission.
  - **F3** `authorityOutstanding` clears only on a cleanup-confirmed result; `close`
    and eviction cannot release an unconfirmed full hold; `WorkerRunner` latches
    `recovery_pending` and blocks later runs.
  - **F4** cleanup drain default raised to 5000 ms.
  - **F5** `resetEpoch` cancels + drains before the new session and suppresses late
    notifications.
  - **F6** owned fd4 lifetime watchdog reaps the worker's own process group on a hard
    Manager crash; normal stop is not `recovery_pending`; fd3/fd5 never reused.
  - **F7** bounded terminal summary always sent once; no raw large child text;
    delivery failures surfaced; full result retrievable via `task_control`.
  - **F8** bounded interval coalesce/latest (plus the `maxEvents` 64 cap; `maxPending` is declared config, not read); `close` uses `gate.close()`; stale
    progress not forwarded.
  - **F9** `docs/pi-typescript.md` updated; shared contract `a890…` and OpenCode/Codex
    bytes unchanged.
  - **Checks:** Pi typecheck; new-test strict typecheck; POC typecheck; full suite
    **226 pass / 0 fail** (205 pre-existing + 21 new); `generate-contract --check`
    pass; `verify-package` pass; offline Go providers/pi, release, orchestration, e2e
    all `ok`. **No live call.**
  - **State:** **T06a–T06h `done`**; T06 umbrella **`in_progress`** (implementation is
    not acceptance); **T07 `pending`**; **T08 unauthorized**. Current manifest digest
    **`b550b135fe2b018020d9b3540003fbe64bf935338878a4e2947cb93e7aa928d9`**.
  - **Residuals:** mid-run `needs_input` resume unimplemented (cancel + fresh mission);
    no automatic `todowrite`; `session_before_switch` not wired; recovery latch needs
    a controlled process restart to clear; no live model/production run; Windows
    fail-closed.

- **D42 — T06i correction: three independent reviews FAIL (nonce
  `imp004-prod-corrections-20260923-i`; recorded before code).** The manifest
  `b550b135…` reviewed as a **listed-order** row set is not the required sorted
  schema; and:
  - **VERIFIER-F1 HIGH** — `WorkerRunner.recoveryPending` was latched from a prior
    sync/read-only task as a plain `Error` with no `.report`/typed code; a later full
    background job's `failureTerminal` mapped it to `failed` with
    `cleanupConfirmed=true`, releasing the guard. Fix: typed runtime recovery error +
    guard consults the **same** shared `runner.recoveryPending`, and any latch/raw
    recovery forces `cleanupConfirmed=false` and a pinned hold even without a new
    background job.
  - **VERIFIER-F2** — `failureTerminal` must preserve `previous.reasonCode` and
    `report.recovery.pending`; cleanup is never inferred from an ordinary reason
    string, only from trusted transport cleanup or a never-launched queue case.
  - **CARE-A HIGH** — coalescing permanently disabled itself (`pending >= maxPending`
    never reset). Fix: bounded **scheduled periodic flush** of the latest event with a
    coalesced counter, at most one notification per interval, timer scheduled once.
    (Current wired rate control is the interval coalesce/latest + the `maxEvents` 64
    cap; `maxPending` remains declared config that is not read.)
  - **CARE-B MEDIUM** — async `sendMessage` rejection was swallowed. Fix: notify may
    return a thenable; the registry catches sync + async rejection and records
    `deliveryFailed`.
  - **CARE-C/D LOW** — old-generation handles must be externally unavailable
    (`status/wait/cancel/close`) while internal cleanup remains held and guarded;
    close reasons distinguish `job_running` vs `cleanup_unconfirmed`; `cancelAll`
    iterates internally and drains concurrently (bounded).
  - **CARE-E TOCTOU** — add an execution-time guard using the actual parsed patch
    targets rechecked before each mutation/commit; parent **code** patches are
    coordinated with the background writer; no OS-sandbox claim.
  - **SPECIALIST-F1 mandatory** — the fd4 watchdog must be **fail-closed**: an awaited
    bounded READY handshake before exposing writable/command tools or the model
    prompt; missing/invalid fd, spawn error or timeout ⇒ worker `unavailable`.
  - **Manifest schema** — exact: repo-relative full paths, lexicographically sorted,
    rows `path␠␠sha256\n`; recompute at the bottom with a central header binding.
  - Prior FAILs are retained as history, not overwritten.

- **D43 — T06i corrections delivered (nonce `imp004-prod-corrections-20260923-i`).**
  All three independent-review FAILs were corrected in Pi production source with
  tests (no TDD claim; some regressions observed RED first). VERIFIER-F1/F2, CARE-A/B,
  CARE-C/D, CARE-E and the mandatory SPECIALIST-F1 fail-closed watchdog are closed;
  prior FAILs retained as history. The manifest is now the **exact sorted schema**.
  - **Checks:** Pi typecheck; strict typecheck of the three candidate tests; POC
    typecheck; full suite **233 pass / 0 fail** (205 pre-existing + 28 new);
    `generate-contract --check` pass (`a890…` unchanged); `verify-package` pass;
    offline Go providers/pi, release, orchestration, e2e all `ok`. **No live call.**
  - **Sorted manifest digest:
    `cc9be0a16caa7facccabed936f30c189c0772cc0e4030bf5c04fc370c329dcec`** (18 rows;
    repo-relative sorted `path␠␠sha256\n`). Earlier `b550b135…` was a **listed-order**
    row set and is retained history.
  - **State:** T06a–T06i `done`; T06 umbrella `in_progress`; **T07 pending**; **T08
    unauthorized**. Residuals: `needs_input` resume; no automatic `todowrite`;
    `session_before_switch`; execution-time guard has no OS-lock (post-check window);
    runner latch needs controlled restart; no live run; Windows fail-closed.

- **D44 — T06j parent/child writer serialization delivered (nonce
  `imp004-prod-writerlock-20260923-j`).** The acknowledged **internal** CARE-E race
  is closed for the **managed** tool path, without a daemon, lockfile, or OS claim.
  `WorkerRunner.runExclusiveMutation` shares the exact `#activeWriters` slot with
  child full-worker runs (no fabricated mission, no nested runner re-entry); the
  parent managed code `apply_patch` reserves the slot for the whole commit/rollback
  span (released in `finally`), a queued full child cannot launch until it releases,
  a child writer already active **denies** the parent code patch (`ParentMutationConflictError`,
  not a hang), concurrent parent code patches serialize, and plan-only patches stay
  direct/allowed while a child is active. Tests
  `packages/pi/test/background-writerlock.test.ts` (6) cover all of these; full suite
  **239 pass / 0 fail**; typechecks; `generate-contract --check`; `verify-package`;
  offline Go `providers/pi`+`release`. **Externally invoked commands/processes and
  native tools outside the managed path remain out of scope (not an OS sandbox).**
  - **Sorted manifest digest:
    `0d792e13775ff8afd6ecb7646cd877bbda837e41956f51f7049bd51cb273483b`** (19 rows).
  - **State:** T06a–T06j `done`; T06 umbrella `in_progress`; **T07 pending**; **T08
    unauthorized**.

- **D45 — T06 source accepted pre-live; plan paused awaiting a bounded production
  live budget (nonce `imp004-prod-prelive-record-20260923-n`, docs only).** The
  Manager accepted the **T06 source candidate** on the exact sorted 19-row manifest
  `0d792e13775ff8afd6ecb7646cd877bbda837e41956f51f7049bd51cb273483b` (HEAD `c4c6450`
  + working diff) after independent verifier `imp004-prod-verify-k` **PASS**
  (239 tests, Go, typecheck, same exact manifest), CARE `imp004-prod-care-l` **PASS**
  (source; prior F closed) and specialist `imp004-prod-specialist-m` **PASS** (the
  security claim was corrected: fd5 is a **known regular revocation file** with
  explicit revocation/expiry, **not** an fd4-EOF channel).
  - **Limitations (explicit):** **live PRODUCTION is not observed**; the prior **T05
    POC** proof is **separate** and does not transfer. `T07` is **`in_progress`**:
    tests + reviews are done, the **optional** live run needs a new budget. **T08
    install/delivery is NOT authorized**; no install/commit was done.
  - **Budgets:** all prior live budgets are **exhausted**; no borrowing.
  - **Non-blocking CARE residue (bookkeeping correction):** the actual wired rate
    control is a **bounded interval coalesce/latest** plus the **`maxEvents` 64**
    cap; **`maxPending` is declared configuration that is not read** — do **not**
    claim it is wired or that a `maxPending` bound is enforced. No code fix this
    pass.
  - **Native hook:** outside the managed `apply_patch` path the native `tool_call`
    hook is **best-effort, not a sandbox**; the held **managed code queue** is the
    guarantee and is tested (6 writerlock tests).
  - **Manifest/state:** source SHA unchanged `0d792e13…`; HEAD `c4c6450` + diff;
    prior `c7ee…` and older doc-only hashes are history. Tests **239** (**205**
    baseline + **34** new). Guides `security-boundary`/`agent-evaluation` validated;
    no new skill load needed. Plan **paused**, active **none**, index concise.

- **D46 — Live production harness build authorized (nonce
  `imp004-prod-liveprep-20260923-o`, **no model call**).** The user authorized
  **one** isolated live production scenario (1 parent + 1 read-only child, ≤180 s,
  existing model/auth, **no** install/source-activation/global-settings change).
  This mission **builds and tests only**; the run is a separate Manager-authorized
  RUN after a fresh harness review. New harness lives under
  `packages/pi/test/production-background-live/` (its own manifest) and exercises the
  **actual production** `createPiExtension` via a trusted wrapper and the native Pi
  `0.85.1` RPC (no POC parent extension, no injected `executeWorker`, no manual
  registry callbacks). New budget root
  `/private/var/folders/4q/nlfcvv816_l0bh4wsl846_kw0000gn/T/opencode/imp004-production-live`,
  **cap 1**, durable, reserved before any prompt, no reset; old POC budgets
  (`…/imp004-poc-live` 2/2, `…/imp004-poc-id-live` 1/1) remain immutable and exhausted.
  - **Source frozen:** the 19 production files keep byte-identical digests
    (`0d792e13…`). The unused `maxPending` config is acknowledged as a docs-only
    residual; **no code fix now** (a source edit would invalidate prior reviews).
  - **State:** T06 source accepted; **T07 `in_progress`**; **T08 installation NOT
    authorized**. Active plan **T07**.
  - **Do NOT start implementation toward the live run without a new authorization.**

- **D47 — T07 live production harness built and tested; NO model call (nonce
  `imp004-prod-liveprep-20260923-o`).** New harness under
  `packages/pi/test/production-background-live/` (budget ledger cap 1, fixture,
  prototype guard, single prompt, evidence parser, native parent RPC driver, trusted
  wrapper importing the **actual production** `createPiExtension`, gated
  `runProductionLive`). Tests **249 / 248 pass / 0 fail / 1 skipped** (opt-in real-CLI
  zero-model preflight). The mocked native-RPC full-entry test proves the production
  extension loads through the wrapper and registers `task`/`task_control` only (plus
  model_resolve/apply_patch/todowrite) with **zero model calls**. Source 19 files
  byte-unchanged (`SOURCE19=0d792e13…`); `maxPending` unused residual acknowledged,
  no code fix. Production budget **0 of 1**; old POC roots untouched.
  - **Harness digest:
    `ceba7b540115a252a71e2eaf0fdeffdb06e5cd70fe7cb1c0e5417e9354c0d228`** (10 rows).
  - **Pending:** the **RUN** (real model) is a separate Manager-authorized execution;
    real full-path proof pending. **T08 installation not authorized.**

- **D48 — T07b harness fix for prior `ceba` FAIL (nonce
  `imp004-prod-liveharness-fix-20260923-r`, harness/docs only, no model call).**
  F1: `get_commands` unwrapped as command metadata only; truthful tool inventory from
  a `registerTool` interceptor + native `getActiveTools`/`getAllTools` written to
  isolated `startup.json` **only after the production factory completes** (no fake
  stdout marker). Live/preflight bound to installed `…/v24.15.0/bin/pi` **0.85.1**
  (`assertInstalledCli`); repo-local 0.84.4 not silently used. H1: explicit `gate.ts`
  (handle/binding, ≥1 progress notification, `apply_patch`→`todowrite` full-id order,
  plan pending→in_progress before projection, `task_control` running + owned child
  alive, terminal envelope commit+snapshot) — missing⇒INCONCLUSIVE, false⇒FAIL,
  terminal-only cannot PASS. H2: exactly one read-only `care-reviewer`, exact targets,
  no exploration/skills, candidate commit **and** snapshot, commands exactly the 20 s
  health argv. H3: per-slot `wx` lock, atomic cap-1, malformed holds capacity.
  - **Checks:** suite **253 / 252 pass / 0 fail / 1 skipped**; strict typecheck of both
    harness tests; `generate-contract --check`; `verify-package`. Source19
    `0d792e13…` byte-unchanged. Harness digest
    **`00187172a663ae01636cf2e8db86581f407d64a647c846aa7778d43c2d594a00`** (11 rows).
  - **RUN entry (after fresh reviews; not run):**
    `VGXNESS_PROD_LIVE=1 node --experimental-strip-types packages/pi/test/production-background-live/scenario.ts --run`.
    Budget **0 of 1**; real full-path model proof pending.

- **D49 — T07 wiring candidate (nonce `imp004-prod-livewire-20260923-u`, harness/docs
  only, NO live model call, NO `VGXNESS_PROD_LIVE` with a real driver).** Replaces the
  `ceba`/`00187172…` FAIL harness, whose scenario never fed actual observations to the
  gate. Fixes the three verifier/CARE blockers at the source call path:
  - **G1 (observations).** `wrapper.ts` now exports `createProductionWrapper(pi, …)`,
    imported by the generated live module and by tests. It wraps each **actually
    registered** production tool's `execute` (calling through to the original, never
    patching it) and, at the parent `todowrite`, reads the **actual** registered
    `task_control` tool (`action:"status"`) plus a numeric-only `ps -o pid=,ppid=`
    descendant snapshot for owned-child liveness. It records a bounded JSONL stream
    (tool/task/plan/todo/terminal/host) under `storageRoot/observations.jsonl`; the
    terminal record is the real `task_control` `details.background.terminal.envelope`
    (nonce + candidate commit/snapshot + runtime state only). No injected
    `executeWorker` and no POC extension on the live path.
  - **G2 (plan).** `fixture.ts#planTaskState` parses only the exact escaped `T1` row
    (sibling `T2` stays `pending`); the plan snapshot (`before`/`after` digests + T1
    state) is taken immediately before the real `todowrite` execute and only after a
    real `apply_patch` success (`isError===false`), with the full tool-call ids
    cross-checked against the native event stream.
  - **G3 (payload).** `evidence.ts#deriveNotificationKind` derives the kind from the
    **actual** production payload (`reportType`+`seq`/`eventId` ⇒ progress;
    `state`+`resultAvailable` ⇒ terminal) instead of requiring a `kind` field the
    sender never emits; `pi.sendMessage` is observed as a **host** invocation with
    bounded metadata (no raw summary/model text) and the original call-through is
    preserved.
  - **Gate.** Missing ⇒ INCONCLUSIVE, present-but-false ⇒ FAIL, terminal-only never
    PASS; added `applyPatchSucceeded` (real success + full id) and
    `toolCallIdsMatchNative` (wrapper observer id === native event id).
  - **Full entry.** `fake-driver.ts` loads the same `createProductionWrapper` through
    `runProductionLive({ rpcFactory })` and drives the real `task`/`apply_patch`/
    `todowrite`/`task_control` callbacks while emitting matching native events. Test
    doubles: worker transport (real `executePiWorker` against a zero-LLM fake child
    CLI), session backend stub, and the OS-snapshot helper — reported explicitly.
  - **Checks:** full Pi suite **259 / 258 pass / 0 fail / 1 skipped**; strict typecheck
    of the harness files/tests; `verify-package`; REAL zero-model preflight
    `VGXNESS_PROD_PREFLIGHT=1 node --test packages/pi/test/production-live-fullentry.test.ts`
    **7/7 pass** (native `get_state`/`get_commands` only, **zero prompts**). Source19
    `0d792e13…` byte-unchanged (19 rows re-hashed, 0 drift). Harness digest
    **`91a035f68244c4895fcb46844343f3f1c91e049547fd2ece6c1319c7dbae3efe`** (12 rows).
  - **Status:** **T07 active preparation — NOT a PASS.** No live production model call
    was made; the production budget root stays **0 of 1**. The RUN (1 parent + 1
    read-only child, ≤180 s, existing model/auth, bound installed Pi 0.85.1) remains a
    separate Manager-authorized execution. T08 unauthorized.

- **D50 — T07 pidfix candidate (nonce `imp004-prod-livepidfix-20260923-x`, harness/docs
  only, NO model call).** Fixes the CARE FAIL H1 residual: the previous
  `descendants(defaultOsSnapshot)` counted the `ps` helper itself, so
  `childAliveAtTodo` was always `true` and a detached worker could be missed.
  - **Owned-worker discriminator.** `wrapper.ts` now uses
    `spawnSync("ps", ["-axo","pid=,ppid=,pgid=,comm="])` (numeric ids + executable
    basename only; no argv/env/raw dump) and `ownedWorker()` selects only a **direct
    child of the parent Pi pid**, detached (`pgid === pid`, as the production
    `PiRpcRunner` spawns with `detached:true`) and node-executable. The snapshot
    helper's own pid and the parent root pid are excluded. `0 ⇒ false`, `1 ⇒ alive`
    with the matched `ownedWorkerPid`, `>1 ⇒ ambiguous ⇒ INCONCLUSIVE` (never a blind
    `true`). Real non-model test: only the helper ⇒ false; a real detached node child
    is identified/alive; killing only that child ⇒ false; no foreign process info
    emitted.
  - **Notification provenance.** The residual risk that a native-only event stream
    lacks the `vgxness` custom message is closed by wiring the wrapper's existing
    **real** `pi.sendMessage` HOST records into `progressNotifications` with
    provenance `production_sendMessage_attempt` (vs `native_event`), deduped by
    handle+seq/eventId. This is an authentic **producer invocation**, **not**
    content-inclusion proof; the actual parent MODEL tools remain mandatory. A new
    gate check requires a source progress event with the expected mission identity
    whose wrapper `obsSeq` precedes the real `apply_patch` start; native-only (no
    source record) ⇒ INCONCLUSIVE, source-only terminal or a progress after the plan
    update ⇒ FAIL. No synthetic notification injection.
  - **Checks:** full Pi suite **263 / 262 pass / 0 fail / 1 skipped**; strict typecheck
    of both harness tests + all `production-background-live/*.ts` clean;
    `verify-package` PASS; REAL zero-model preflight **7/7**. Source19 `0d792e13…`
    byte-unchanged (19 rows, 0 drift); production budget root **0 of 1**. Harness
    digest **`9611f62b6ecbdbe30be0565e124ad1c9d979da8ac4c06d8626f7a4fad78c958c`** (12 rows).
  - **Status:** **T07 active preparation — NOT a PASS.** No review of this pidfix
    candidate is recorded yet; the RUN remains separate and unauthorized to execute
    here.

- **D51 — T07 live RUN executed once (nonce `imp004-prod-LIVE-20260923-aa`), result
  INCONCLUSIVE, budget 1/1 consumed, plan PAUSED.** Manager-authorized single
  production scenario. Pre-run reconciliation: production root
  `…/opencode/imp004-production-live` had only `cap.json` (0 reservations); no owned
  parent/child lingering; `HEAD` `c4c6450`; bound native CLI `0.85.1`; source19
  `0d792e13…` and harness12 `9611f62b…` both matched before the run. Exact entry from
  the workspace root:
  `VGXNESS_PROD_LIVE=1 node --experimental-strip-types packages/pi/test/production-background-live/scenario.ts --run`.
  - **Actual result:** `state=INCONCLUSIVE` in ~14 s; `startup.factoryComplete=true`,
  registered `todowrite, apply_patch, model_resolve, task, task_control`. The
  observation stream had **2 records**: exactly one real model tool,
  `model_resolve` (`phase:end`, `isError:false`, full id
  `call_COBaCxZxS4galH6BBrYbb3Mx|fc_…`). The parent model settled without calling
  `task`, so no background child was spawned, no `apply_patch`/`todowrite` occurred,
  the fixture plan stayed `T1/T2 pending`, and **all 11 gate checks were
  `missing`** (`taskHandleBound`, `progressNotification`, `progressBeforePlanUpdate`,
  `applyPatchBeforeTodo`, `applyPatchSucceeded`, `todoFullId`,
  `toolCallIdsMatchNative`, `planChangedBeforeTodo`, `controlRunningAtTodo`,
  `childAliveAtTodo`, `terminalBound`).
  - **Post-run:** no owned parent/child lingering (driver `stop()` + watchdog
    cleanup); source19 `0d792e13…` and harness12 `9611f62b…` byte-unchanged; old POC
    roots (`imp004-poc-live`, `imp004-poc-id-live`) untouched. Production budget now
    **1 of 1** (`slot-0.lock` + reservation `imp004-prod-liveprep-20260923-o--prod-progress-1p1c`).
  - **Blocker / next action:** the single authorized parent turn produced only
    `model_resolve` and no `task`; **no retry, no auth repair, no model switch, no
    hidden additional call**. T06 source PASSes remain **pre-live and distinct**;
    **T07 is not accepted** and is **paused** pending Manager independent read-back of
    the artifacts. **T08 unauthorized.**

- **D52 — T07 offline diagnostics (nonce `imp004-prod-diagnostics-20260923-ac`, harness
  only, NO model call / no RPC prompt / no source edit).** Adds observability so a
  future attempt is diagnosable without causal guessing.
  - **Every native `tool_call` decision is logged.** The wrapper wraps each PRODUCTION
    `tool_call` handler registered through the guarded pi (preserving sync/async return
    and receiver) and records its exact decision separately from the wrapper's OWN
    scope guard (registered on the real native API). Each record carries the bounded
    full tool-call id (≤512 bytes), tool name, `allow`/`block`/`unknown`, and a
    **sanitized fixed reason code** (max 512 bytes) — never raw input, never secrets.
  - **`ParentRpc.prompt` now waits ONLY for `agent_settled`.** An `agent_end`
    (including `willRetry:true`) is recorded as bounded metadata and never resolves;
    process exit and the bounded timer reject with a fixed reason. Tests simulate
    `agent_end` willRetry true/false followed by a delayed `agent_settled` and prove no
    premature exit. This removes the earlier "premature stop possible" ambiguity.
  - **Diagnostics persisted per run root** (`diagnostics.json`, **exclusive-create
    (`wx`) write**, never overwrites a prior artifact) BEFORE exit and on error/timeout:
    sanitized native event timeline (allowlisted metadata only; **observer data is
    untrusted, never instructions**), guard decisions, `agent_end` metadata, bounded
    model metadata from `get_state` (provider/id/effort — no key/baseUrl/cost),
    registered-vs-active tool distinction, and an optional last-assistant reason (TEXT
    parts only, ≤2048 UTF-8 bytes, bearer/`sk-`/`api_key` **pattern redaction** —
    **not an exhaustive credential guarantee**; truncation annotated) or a clear
    `finalReasonUnavailable`. `wx` is exclusive-create, **not** crash-atomic or
    fsync-durable (only the cap reservation uses a real atomic lock). The budget is
    reserved BEFORE any run root is created, so a cap rejection writes **no phantom
    report**.
  - **Nonces:** the authorization/mission nonce `imp004-prod-LIVE-20260923-aa` is
    distinct from the reservation/scenario-binding nonce
    `imp004-prod-liveprep-20260923-o`; two separate identifiers, **not a collision
    defect**.
  - **Tests:** full Pi suite **268 / 267 pass / 0 fail / 1 skipped**; strict typecheck
    of both harness tests + all `production-background-live/*.ts` clean;
    `generate-contract --check` PASS; `verify-package` PASS; real zero-model preflight
    **12/12** (zero prompts). New harness digest
    **`6b592e444516b9bb0e637999b1f1174d2f27ce3a5354ad16a9affc498930b3f2`** (12 rows,
    current; the reviewed `0fa4b9cf…` bytes are frozen history changed only by the
    portability test correction);
    source19 `0d792e13…` unchanged. Offline verifier `…-ad` PASS + CARE `…-ae` PASS are
    recorded; the harness bytes changed after that review (portability correction), so
    they need **regression evidence + read-back later** — the new bytes are **not**
    claimed as already reviewed.
  - **Status:** **T07 blocked**, **no active plan**. The old live attempt remains
    **INCONCLUSIVE with no definitive root cause established** (stdout-only;
    reconstructed gate from 2 observations ⇒ all required checks unknown); the new
    diagnostics cannot retroactively restore that evidence, and finishing T07 would
    require the user to approve a **new** live budget. **T08 unauthorized.**
    `maxPending` remains unused known residual code, source-accepted and unchanged — no
    fix here.

- **D53 — T07 RESUME authorized: ONE more production scenario under a NEW budget; plan
  ACTIVE, T07 `in_progress` (user/mission nonce `imp004-live-resume-20260923-aj`,
  2026-09-23).** The user ("dale hazlo hermani") authorizes **exactly ONE** additional
  production live scenario — 1 parent + 1 read-only child, **≤180 s**, **existing default
  model/auth** — with **no** install, Git delivery, memory, or production expansion.
  **This mission makes NO live model call**: it only (a) corrects the resume harness
  prompt, (b) adds an explicit validated `--budget-root`/`--nonce` CLI override, (c)
  updates docs, and (d) runs non-model tests. New root
  `…/opencode/imp004-production-live-resume` cap **1**; **new scenario/binding nonce
  `imp004-prod-resume-20260923-aj`**. The **old root is preserved byte-for-byte** (no
  reset, no writes). The prior live attempt remains **INCONCLUSIVE** (exactly one real
  tool, `model_resolve`, cause unresolved) and is **not retroactively accepted**; the
  newer diagnostics are available **only** for a future attempt. **T08 unauthorized.**

- **D54 — T07 resume harness correction: dynamic `model_resolve` prompt + validated root
  override (nonce `imp004-live-resume-20260923-aj`, NO model call).**
  - **Prompt defect fixed.** `scenario.ts` previously called
    `buildParentPrompt(binding, { model: "resolve-via-model_resolve", effort: same })` — a
    literal, schema-invalid model value (the production `task` schema requires
    `provider/model-id`, and `task.ts` rejects any submitted model that differs from the
    configured user selection). `buildParentPrompt` now takes **no model placeholder** on
    the live path: STEP 1 instructs the parent to call the real native `model_resolve`
    once and copy `roles["care-reviewer"].taskModel` and `.effort` **exactly**, stopping
    and reporting **unavailable** if the entry/field is absent (no different model, no
    auto fallback); STEP 2 sets model/effort to those resolved values and declares the
    `provider/model-id` format **illustrative — DO NOT copy it**. No literal invalid model
    and no hardcoded provider remain. The optional resolved-model argument is
    **test/fake-only** and is never sent to a real Pi run.
  - **CLI root override made explicit and safe.** The gated entry now requires
    `--run --budget-root <ABS> --nonce <ID>`; `parseLiveCliArgs`/`normalizeBudgetRoot`
    are **pure** (no I/O, no spawn) and accept only an **absolute, canonical** path equal
    to the single approved resume root; any other root (including the old exhausted root),
    a relative/non-canonical path, duplicate flags, unknown args, or a candidate override
    is rejected. There is **no default-root fallback**: omitting the flags fails closed and
    never touches the old root. Validated values reach `runProductionLive({budgetRoot,
    nonce})` only after `VGXNESS_PROD_LIVE=1`. `ProductionBudget` cap stays **1**; **no
    `--max-scenarios`, no retries.**
  - **Checks (no model call):** full Pi suite **270 / 269 pass / 0 fail / 1 skipped**;
    strict `tsc` of both harness tests + all `production-background-live/*.ts` clean;
    production `tsc` clean; `generate-contract --check` PASS; `verify-package` PASS; real
    zero-model preflight **12/12** (`get_state`/`get_commands` only, zero prompts).
  - **Hashes:** source19 **`0d792e13…` byte-identical before/after**; resume harness12
    **`c89c72cee534bbdb7a33546146094f5cc60b9339fea90ece806b29a91c45da84`** (the prior
    `6b592e44…` is frozen history and does **not** review the new bytes).
  - **Budget:** new root **0 of 1**, **not created**; old root **1 of 1**, byte/mtime
    unchanged; old POC roots untouched.
  - **Status:** **T07 active preparation, NOT a PASS.** The new harness bytes need
    **regression + independent read-back** before any resume run. **T08 unauthorized.**

- **D55 — T07 resume ORDERING correction (verifier `…-verify-ak` FAIL; nonce
  `imp004-resume-preflight-budget-20260923-am`; HARNESS/DOCS only, NO model/RPC prompt).**
  - **FAIL corrected:** the earlier resume harness called `ProductionBudget.reserve`
    *before* the zero-model `ParentRpc.preflight`, so an injected/host preflight error could
    burn the ONLY authorized slot with no model prompt.
  - **New order (load-bearing):** authorization + bound installed CLI → **read-only**
    fail-closed cap pre-check (`precheckBudget`: reads `cap.json` + directory count, creates
    nothing) → zero-model preflight + tooling capability check in an isolated TEMP run root
    → **only then** atomic `ProductionBudget.reserve` cap1 → the single parent `prompt`. On
    preflight/capability error the owned driver is stopped and the TEMP run root removed;
    **no reservation, no budget consumed, no model retry**. On a reserve rejection no model
    prompt runs; a bounded `prefailure.json` is written in the ephemeral run root (distinct
    from the official `diagnostics.json`) and the call fails closed.
  - **Tests (no model call):** `F5` injected-preflight-error → **0 reservation, 0 prompt**,
    empty budget root; `F5` successful preflight → **cap1 reserved BEFORE the first prompt**
    (preflight sees 0 dirs, prompt sees 1) and a duplicate run is rejected with **no second
    prompt**; the prior `rpc-error`/`rpc-timeout` test now asserts fail-closed with no
    reservation. Full Pi suite **272 / 271 pass / 0 fail / 1 skipped**; strict + production
    `tsc` clean; `generate-contract --check` + `verify-package` PASS; real zero-model
    preflight **14/14** (`get_state`/`get_commands` only, zero prompts).
  - **Hashes:** source19 **`0d792e13…` byte-identical**; resume harness12
    **`c01c29aa30b0870d0f3b0310c675d1f081d0257026c1a17ba3e340595aed4476`** (prior
    `c89c72ce…` is history). New root **0 of 1**, **not created**; old root **1 of 1**,
    unchanged.
  - **Review history:** verifier `…-verify-ak` **FAIL** (this ordering defect) retained;
    CARE `…-care-al` **PASS** on a **narrow source scope**. **Next: fresh independent
    review** of the corrected bytes before any resume run. **T08 unauthorized.**

- **D56 — T07 resume live RUN executed once (scenario nonce `imp004-prod-resume-20260923-aj`,
  mission nonce `imp004-prod-resume-LIVE-20260923-ap`), result INCONCLUSIVE, new budget 1/1
  consumed, no retry (NO code/source/harness edit; docs only after the result).**
  - **Pre-run:** HEAD `c4c6450`; source19 `0d792e13…`; harness12 `c01c29aa…`; bound installed
    CLI `0.85.1`; no owned parent/child process; new root `…/imp004-production-live-resume`
    **absent (0/1)**; old root **1/1** byte-unchanged.
  - **Command (exact, from the workspace root):**
    `VGXNESS_PROD_LIVE=1 node --experimental-strip-types packages/pi/test/production-background-live/scenario.ts --run --budget-root /private/var/folders/4q/nlfcvv816_l0bh4wsl846_kw0000gn/T/opencode/imp004-production-live-resume --nonce imp004-prod-resume-20260923-aj`.
  - **Actual result:** `state=INCONCLUSIVE`, run root `…/T/pi-prod-live-A4WFTR` (~17 s; birth
    02:51:23 → diagnostics 02:51:40; well under 180 s). Native zero-model preflight + tooling
    check passed (`factoryComplete=true`, registered `todowrite, apply_patch, model_resolve,
    task, task_control`); reserve then cap1 at `08:51:24.419Z`. The parent resolved the
    configured model (`openai-codex/gpt-5.5`, effort `medium`) and **attempted `task`**, but the
    harness `scope_guard` **blocked** it (`reasonCode=candidate_mismatch`; the production
    `tool_call` handler had allowed it). `task` `tool_execution_end` `isError:true`; no
    background child, no `apply_patch`, no `todowrite`, no terminal; `agent_end`
    (`willRetry:false`) then `agent_settled`. All 11 gate checks **missing**.
  - **Interpretation (evidence-bounded):** `candidate_mismatch` is definitive that the candidate
    binding check failed. The prompt states only `candidate commit`, not the snapshot the guard
    also requires, so a missing/wrong snapshot is the likely cause — but the exact submitted
    input is **not persisted** (bounded metadata only), so the precise mismatch is **not
    definitively established**.
  - **Budget:** new root now **1 of 1** (`cap.json`, `slot-0.lock`,
    `imp004-prod-resume-20260923-aj--prod-progress-1p1c/reservation.json`); old root **1/1**
    byte/mtime-unchanged; old POC roots untouched. **No retry.**
  - **Status:** **T07 blocked/paused**, active plan **none**, T06 source accepted, **T08
    unauthorized.**

- **D57 — T07 resume reconciliation (mission nonce `imp004-prod-resume-recover-20260923-aq`,
  docs only; NO new live call).** The prior invocation's transport reported an **SSE read
  timeout**; reconciliation proves the run had already executed and settled.
  - No owned process alive; the approved new root has exactly **1** reservation (nonce
    `imp004-prod-resume-20260923-aj`, reservedAt `2026-09-23T08:51:24.419Z`); the run's
    `diagnostics.json` exists (schema `imp004-t07-diagnostics/v1`, state INCONCLUSIVE,
    `counts {observations:6, nativeEvents:434, agentEnds:1, guardDecisions:4}`), and no
    `prefailure.json` (the reserve succeeded). Raw model transcript / auth / host store **not**
    read.
  - The SSE timeout is classified as a **transport** timeout, **not** a model timeout or code
    error: the run settled in ~17 s (< 180 s) with `agent_end willRetry:false`.
  - source19 `0d792e13…` and harness12 `c01c29aa…` unchanged before/after; `git status`
  unchanged (no source/harness edit). **No retry.**

- **D58 — T07c offline prompt-binding correction (nonce `imp004-prod-prompt-candidate-20260923-as`;
  user "continua por fa"); HARNESS/DOCS only, NO live call, NO model call, NO budget consumed.**
  - **Finding (root-caused offline, source-backed):** `prompt.ts` `ScenarioBinding` carried only
    `candidateCommit`, and `buildParentPrompt` STEP 2 quoted only `candidate commit "<commit>"`;
    the strict harness `guard.ts` requires **both** `candidate.commit` **and** `candidate.snapshot`.
    `scenario.ts` already held the constant `PRODUCTION_CANDIDATE_SNAPSHOT=0d792e13…` and passed it
    to the wrapper/gate expectation, but it never reached the prompt — so a live parent could not
    supply the snapshot and the scope guard blocked the `task` with `candidate_mismatch`. The exact
    submitted input was **not persisted** (bounded metadata only), so the precise mismatch (omitted
    vs wrong snapshot) is **not definitively established**; the omission is the **source-proven
    likely cause**. **No model blame.**
  - **Correction (offline, bounded):** `ScenarioBinding` gains `candidateSnapshot`; `scenarioBinding`
    assigns the actual `PRODUCTION_CANDIDATE_SNAPSHOT`; `buildParentPrompt` STEP 2 quotes **both**
    the full 40-char commit and the full 64-char snapshot and tells the model to copy both exactly
    (never abbreviate/infer/substitute); `scenario.ts` `expected`/gate read `binding.candidateSnapshot`
    (single source). The **guard and production candidate schema are unchanged/not weakened**; the
    `prompt.ts` dynamic `model_resolve` roles (`care-reviewer` taskModel/effort) are unchanged, with
    no placeholder.
  - **Why the earlier fake full-entry test did not catch it:** the fake driver builds its `taskInput`
    **directly from `expected`** (`fake-driver.ts` `taskInput()`), not by parsing the prompt, so it
    structurally **cannot** detect a prompt omission. The new regression test parses the
    commit/snapshot out of the real `buildParentPrompt` output and requires the prompt-derived
    candidate to satisfy the strict guard; an omitted or incorrect snapshot still rejects (the
    existing H2 negatives are kept).
  - **Checks actually run (no live call):** full Pi suite **273 tests, 272 pass, 0 fail, 1 skipped**;
    strict `tsc` over both harness tests + all `production-background-live/*.ts` **clean**;
    production `tsc -p tsconfig.json` clean; `generate-contract.mjs --check` PASS; `verify-package`
    PASS; REAL zero-model preflight `VGXNESS_PROD_PREFLIGHT=1 node --test
    packages/pi/test/production-live-fullentry.test.ts` **14/14 pass** (`get_state`/`get_commands`
    only, zero prompts).
  - **Hashes:** source19 **`0d792e13775ff8afd6ecb7646cd877bbda837e41956f51f7049bd51cb273483b`
    byte-identical before/after** (19 rows, 0 drift); new harness12
    **`4747c5f95776b1e5aa7fcd8fb6d840a3e6dc3a3ee94bbeb4acd3d5f6b5bba4ee`** (prior `c01c29aa…` is
    history). Changed rows: `prompt.ts` `9aae9b34…`, `scenario.ts` `f81ede81…`,
    `production-live-harness.test.ts` `bbe88104…`.
  - **Budget:** new root `…/imp004-production-live-resume` **1 of 1**; old root
    `…/imp004-production-live` **1 of 1**; both **untouched** (no reserve, no reset, no writes);
    **0 remaining**. Old POC roots untouched.
  - **Status:** **T07c `done` (offline only)**, **T07 `blocked`**, plan **paused**, active **none**;
    **no live re-authorization**; **T08 unauthorized**. A T07 live validation would need a **new
    user budget decision**.

- **D59 — T07d final live-budget preparation (nonce `imp004-final-budget-20260923-aw`); HARNESS+DOCS
  only, NO model call, NO budget consumed.** The user authorized **ONE** more bounded live production
  Pi run (1 parent + 1 read-only child, ≤180 s, existing model/auth; **no install/commit/push/other
  provider**).
  - **Source change (only the approved root constant):** `scenario.ts` `APPROVED_BUDGET_ROOT` →
    `/private/var/folders/4q/nlfcvv816_l0bh4wsl846_kw0000gn/T/opencode/imp004-production-live-final`.
    `normalizeBudgetRoot` stays a **pure exact static allowlist** (absolute + canonical + string
    identity; **no symlink resolution, no env-derived root**) and rejects any non-final root; the
    gated CLI still requires `--run --budget-root <ABS> --nonce <ID>` with **no default** and rejects
    unknown/candidate/`--max-scenarios` overrides. **Guard, gate and the production candidate schema
    are unchanged; production `src/**` untouched.**
  - **New root:** `…/imp004-production-live-final` is **absent (cap 1, not created)**; the two
    previous roots `…/imp004-production-live` and `…/imp004-production-live-resume` remain **1 of 1,
    byte/mtime-unchanged**; old POC roots untouched. **No reservation, no RUN in this mission.**
  - **Checks:** strict `tsc` (both harness tests + all `production-background-live/*.ts`) **clean**;
    full Pi suite **274 tests, 273 pass, 0 fail, 1 skipped**; `generate-contract.mjs --check` PASS;
    `verify-package.mjs` PASS; REAL zero-model preflight `VGXNESS_PROD_PREFLIGHT=1 node --test
    packages/pi/test/production-live-fullentry.test.ts` **14/14 pass** (`get_state`/`get_commands`
    only, zero prompts).
  - **Tests added/updated:** CLI parse accepts the exact final root + `imp004-prod-final-20260923-aw`
    and **rejects both previous roots** and relative/other/non-canonical values; a fresh isolated
    `mkdtemp` root reserves cap 1 then blocks a second reservation (never the real root).
  - **Hashes:** source19 **`0d792e13775ff8afd6ecb7646cd877bbda837e41956f51f7049bd51cb273483b`
    byte-identical**; new candidate harness12
    **`f4408dc8ca908f57dd26e103008f4e2c195b6b3e9f9b4e7be25a53d79f85eea8`** (**UNRUN**). History:
    `c01c29aa…` (previous live run) and `4747c5f9…` (offline-corrected, superseded).
  - **Run command (supported; NOT executed here):** `VGXNESS_PROD_LIVE=1 node
    --experimental-strip-types packages/pi/test/production-background-live/scenario.ts --run
    --budget-root /private/var/folders/4q/nlfcvv816_l0bh4wsl846_kw0000gn/T/opencode/imp004-production-live-final
    --nonce imp004-prod-final-20260923-aw`.
  - **Status:** **T07d `done` (offline)**, **T07 `in_progress`** (new final budget 1), active plan
    **T07**; **T08 unauthorized**. The new candidate harness12 is **unrun** and needs a **fresh
    independent review** before the separate Manager RUN; **no live PASS claim.**

- **D60 — T07e FINAL live production RUN executed ONCE (scenario nonce `imp004-prod-final-20260923-aw`,
  mission nonce `imp004-prod-final-LIVE-20260923-az`), result FAIL, budget exhausted, no retry (NO
  code/source/harness edit; docs only after the outcome).**
  - **Pre-run (verified, read-only):** HEAD `c4c6450`; source19 `0d792e13…`; harness12 `f4408dc8…`;
    installed CLI `0.85.1`; no owned process; old root `…/imp004-production-live` **1 of 1**, resume
    root `…/imp004-production-live-resume` **1 of 1**, final root `…/imp004-production-live-final`
    **absent (0 of 1)**.
  - **Command (exact, from the workspace root):** `VGXNESS_PROD_LIVE=1 node
    --experimental-strip-types packages/pi/test/production-background-live/scenario.ts --run
    --budget-root /private/var/folders/4q/nlfcvv816_l0bh4wsl846_kw0000gn/T/opencode/imp004-production-live-final
    --nonce imp004-prod-final-20260923-aw`.
  - **Actual result:** `state=FAIL` (exit 1). Native zero-model preflight + tooling check passed
    (`factoryComplete=true`; registered `todowrite, apply_patch, model_resolve, task, task_control`);
    reserve then cap1 (`cap.json` createdAt `2026-09-23T16:43:59.763Z`). Run root
    `…/T/pi-prod-live-X6EMEG`; diagnostics written ~`10:45:06` local vs run-root birth `10:43:59` ⇒
    **elapsed ≈67 s (<180 s)**. Model `openai-codex/gpt-5.5`, effort `medium`.
    `counts {observations:37, nativeEvents:1688, agentEnds:0, guardDecisions:16}`;
    `finalReasonUnavailable=true` (no bounded last-assistant reason captured; none output here).
  - **Gate (11 checks):** **pass** `taskHandleBound`, `progressNotification`,
    `progressBeforePlanUpdate`, `applyPatchBeforeTodo`, `todoFullId`, `terminalBound`; **fail**
    `applyPatchSucceeded`, `toolCallIdsMatchNative`, `planChangedBeforeTodo`, `controlRunningAtTodo`,
    `childAliveAtTodo` ⇒ **FAIL** (present-but-false witnesses, never PASS).
  - **Bounded cause (wrapper observation stream + guard decisions only):** the `task` was **admitted**
    by both the production handler and the harness `scope_guard` (the T07c prompt-binding fix worked —
    **no `candidate_mismatch`**), a real background child ran (`bg_d170e3e3-…`), emitted a
    source-bound `sendMessage` progress before the patch, and reached a **terminal `succeeded`**
    envelope with the exact candidate `{commit 40, snapshot 64}`. However the parent's plan write did
    not land: `apply_patch` was **blocked twice** by the harness `scope_guard`
    (`reasonCode=patch_scope`, obsSeq 17/19), then a first admitted patch **errored**
    (`tool_execution_end isError:true`, obsSeq 23) and a later admitted patch **succeeded**
    (`isError:false`, obsSeq 27) — but the plan file digest was **unchanged**
    (`planState.changed:false`, `taskState:pending`, obsSeq 35) and the gate binds
    `applyPatchSucceeded`/`toolCallIdsMatchNative` to the **first** apply_patch end (the errored one).
    The parent's `todowrite` then occurred **after** the child had already succeeded: at the todo
    instant `control.state=succeeded` and `childAlive=false` ⇒ `controlRunningAtTodo` and
    `childAliveAtTodo` fail. The hard-trace requirement (plan persisted + projected **while the child
    is still running**) was therefore **not** met; this is a genuine **FAIL**, not an inconclusive
    observation gap.
  - **Budget/cleanup:** final root now **1 of 1** (`cap.json`, `slot-0.lock`,
    `imp004-prod-final-20260923-aw--prod-progress-1p1c/reservation.json`); old and resume roots
    **1 of 1 byte/mtime-unchanged** (`01:42`/`02:51`); POC roots untouched. **No owned
    `scenario.ts`/`pi-prod-live`/health-check process remained after the run** (watchdog/session
    cleanup normal). **No retry; no additional model command; no provider override or auth
    manipulation.**
  - **Hashes after:** source19 `0d792e13…` **unchanged**; harness12 `f4408dc8…` **unchanged**.
  - **Status:** **T07e `done` (FAIL)**, **T07 `blocked`**, plan **paused**, active **none**; **all
    budgets exhausted**; **no production-live PASS**; **T08 unauthorized**. A further attempt would
    need a **new user decision** (and, if pursued, a code/harness correction for the
    plan-write/todowrite ordering — **not** authorized here).

- **D61 — T07f offline strict full-ID native-binding correction (nonce
  `imp004-prod-patchgate-20260923-bc`; harness/docs only, NO model call, NO RPC prompt, NO
  budget consumed).** User "continua" authorizes a **safe offline follow-up** of T07 only; it
  does **not** authorize a new LLM run. **T07 remains `blocked`** and the executed `…-aw` FAIL
  is **retained unchanged** (no retrograde to PASS); T07f does **not** accept T07.
  - **Root cause (offline, nonce `imp004-fail-analysis-bb`).** `gate.ts` selected the **first**
    native `apply_patch` `end` (`findIndex`) for `applyPatchBeforeTodo`/`applyPatchSucceeded`/
    `toolCallIdsMatchNative`, while the trusted wrapper's `todoRecord.applyToolCallId` stores the
    **last** attempt id (`state.applyToolCallId` set at each apply end). On the T07e run the first
    apply end **errored** and a later one **succeeded**, so the gate bound the **errored first**
    attempt — a harness binding artifact, not a demonstrated product defect (independent verifier
    `…-ba` confirmed no product defect).
  - **Fix (bounded, no fallback).** The native `apply_patch` end is now selected by the **exact**
    wrapper-observed `observations.applyToolCallId`: it must appear **exactly once** among native
    apply end events **and before** the **bound** native `todowrite` start (also selected by the
    exact observed `todoToolCallId`). Missing ⇒ INCONCLUSIVE, present-but-unbound/ambiguous ⇒ FAIL;
    the gate **never** falls back to the first or the last successful patch. `applyPatchSucceeded`
    is evaluated **only** on the bound end (`isError===false`); `toolCallIdsMatchNative` is an
    independent cross-layer count (each observed id appears exactly once natively, lossless
    >64 chars and ≤512 UTF-8 bytes, no prefix/truncation). `planChangedBeforeTodo` (from the
    wrapper plan-file snapshot), `controlRunningAtTodo` and `childAliveAtTodo` stay **mandatory,
    unchanged**. The guard log now distinguishes bounded reason codes `patch_unparseable` vs
    `patch_scope_escape` (sanitized from the existing `evaluateProductionGuard` strings; **no raw
    patch input/text stored**).
  - **Prompt/manifest unchanged.** `prompt.ts`, `guard.ts`, `scenario.ts` and the production
    candidate schema are **not modified**; the synthetic 20 s child health argv is unchanged
    (no wall-clock gaming). Production `src/**` untouched. Earlier patch paths are **not**
    retroactively identifiable — no raw input was persisted.
  - **Tests added (5).** Strict gate unit: (a) first native apply id A errored then B succeeded,
    wrapper observed B, plan unchanged ⇒ overall **FAIL with only `planChangedBeforeTodo` failing**
    (binding A would also fail `applyPatchSucceeded`); a wrapper id with no unique native match
    fails closed (no fallback); duplicate same-id ends are ambiguous ⇒ FAIL; (b) bound B +
    plan changed + child alive ⇒ **PASS**, and a `todowrite` after the child succeeded ⇒ FAIL;
    (c) same-64-char-prefix different-tail ids never bind. Full-entry through the production
    wrapper callbacks: `patch-retry` (first attempt errors, retry succeeds but only touches
    `progress.md` so T1 stays `pending`) ⇒ **FAIL only `planChangedBeforeTodo`**, with the bound
    native end being the successful retry and a non-trivial compiled body (11 native events /
    41 observation records); `patch-retry-pass` (retry changes plan.md) ⇒ **PASS** (no false
    negative). **No test alters the public POC.**
  - **Checks actually run (no live call).** Full Pi suite **279 tests / 278 pass / 0 fail /
    1 skipped** (274 + 5 new); strict `tsc` over both harness tests + all
    `production-background-live/*.ts` clean; production `tsc -p tsconfig.json` clean;
    `generate-contract.mjs --check` PASS (`a890…` unchanged); `verify-package.mjs` PASS; real
    zero-model preflight **16/16** (`get_state`/`get_commands` only, zero prompts).
  - **Hashes.** source19 **`0d792e13775ff8afd6ecb7646cd877bbda837e41956f51f7049bd51cb273483b`
    byte-identical before/after** (19 rows, 0 drift). New harness12
    **`f12665e01f8956472378ebbd2ec7f9e33c5b4f64edab0c18a8bcd3fd967d698f`**; changed rows:
    `gate.ts 1dbb5a1e…`, `wrapper.ts d04d68cd…`, `fake-driver.ts 74c7a1f6…`,
    `production-live-harness.test.ts d8b17e11…`, `production-live-fullentry.test.ts 1a06e31f…`
    (the prior `f4408dc8…` is history).
  - **Budgets unchanged:** old `…/imp004-production-live` **1 of 1**, resume
    `…/imp004-production-live-resume` **1 of 1**, final `…/imp004-production-live-final`
    **1 of 1** — all byte/mtime-untouched; **no reservation, no reset, no RUN**; old POC roots
    untouched.
  - **Status:** **T07f `done` (offline only)**; **T07 `blocked`**, plan **paused**, active
    **none**; **T08 unauthorized**. Any production-trace acceptance requires a **new live
    budget decision that is not already approved** and an ordering/plan-write correction; none
    is authorized here.

- **D62 — T07g offline deterministic plan-write correction (nonce
  `imp004-planwrite-fix-20260923-bh`; harness/docs only, NO model call, NO RPC prompt,
  NO budget consumed).** User "hagamoslo mijo" authorized a **safe offline correction**
  of T07 only; it does **not** authorize a new LLM run and does not relax any
  guard/gate. **T07 remains `blocked`**; the executed `…-aw` FAIL is **retained
  unchanged**; T07g does **not** accept T07.
  - **Scope.** The T07e FAIL showed the parent projected `todowrite` only after the child
    had succeeded and the plan never changed. The exact prior patch input is
    **unrecoverable** (no raw input was persisted), so no root cause is claimed; the
    precision fix only makes the deterministic fixture guidance explicit.
  - **Fix (prompt only).** `prompt.ts` STEP 3 now names the exact workspace-relative
    target `docs/implementations/IMP-TEST/plan.md`, the exact pre-image row
    `| T1 | Fixture primary task | pending |`, the exact post-image
    `| T1 | Fixture primary task | in_progress |` (status `in_progress`, never `done`),
    and the untouched sibling `| T2 | Report target task | pending |`; it requires ONE
    native `apply_patch` before the `todowrite` **while the child runs**, forbids the
    `*** Begin Patch` wrapper, and says the untrusted `eventId` is a detail only (it
    cannot choose path/status/permission). STEP 1 (dynamic `model_resolve`), STEP 2
    (candidate commit+snapshot literal, strict guard) and the single-child scope are
    unchanged; no guard/gate was weakened, no new `read` tool, no health-delay increase,
    and no automatic production plan-writer was added.
  - **Identity reconciliation.** The sibling row is the **ACTUAL** fixture label
    `Report target task` (from `fixture.ts`); the mission brief's paraphrase
    `Fixture secondary task` does not exist in the fixture and was **not** introduced, so
    the prompt cannot desync from the fixture.
  - **Tests (no live, no model).** Harness: a new test **parses the REAL
    `buildParentPrompt` output** for the target + pre/post rows and builds the diff from
    those VALUES, then runs the PRODUCTION `patchTargetsFromDiff` and the REAL
    `createApplyPatchTool` on the fixture (`T1`→`in_progress`, `T2` pending, digest
    changed); a second test asserts the guard reason strings `unparseable patch` /
    `patch escapes the fixture plan prefix`. Full entry: the fake driver now builds its
    plan diff by **parsing the actual `runProductionLive` prompt** (never an independent
    `expected` construction), and new scenarios `patch-unparseable`, `patch-escape`,
    `progress-only`, `todo-after-child` assert INCONCLUSIVE/FAIL as appropriate plus the
    sanitized reason codes `patch_unparseable`/`patch_scope_escape`.
  - **Checks actually run (no live call).** Full Pi suite **282/281 pass/0 fail/1
    skipped** (279 + 3 new); strict `tsc` over both harness tests + all
    `production-background-live/*.ts` clean; production `tsc -p tsconfig.json` clean;
    `generate-contract.mjs --check` PASS; `verify-package.mjs` PASS; REAL zero-model
    preflight **17/17** (`get_state`/`get_commands` only, zero prompts). The real `--run`
    was **NOT** executed.
  - **Hashes.** source19
    **`0d792e13775ff8afd6ecb7646cd877bbda837e41956f51f7049bd51cb273483b`** byte-identical
    before/after (19 rows, 0 drift). New harness12
    **`69ec95fb826880faf1f8af7bf422de52fcc20ff40910f2746730e8da32e5ab2a`**; changed rows:
    `prompt.ts dc5531ce…`, `fake-driver.ts 989c36d6…`, `production-live-harness.test.ts
    a1f3ecca…`, `production-live-fullentry.test.ts 12b20326…` (the prior `22774813…` is
    history).
  - **CARE-bj INFO clarification (nonce `imp004-prompt-clarity-20260923-bk`).** CARE `…-bj`
    flagged that STEP 3 said "exactly ONE native apply_patch" **and** offered an optional
    second `progress.md` patch before the `todowrite`, so the last-patch binding could in
    principle track a `progress.md` write although `plan.md` changed. The optional
    `progress.md` patch was **removed**: STEP 3 is now **exactly ONE `plan.md` patch and the
    `todowrite` while the child is still alive**, with **no** second patch. A `progress.md`
    write belongs to a real Manager flow **outside** this synthetic hard-gate (after the
    child stops) and is **not** claimed as real-run progress documentation; `plan.md` state
    persistence is sufficient for the gate. Dynamic `model_resolve`, the candidate
    commit+snapshot literals, the 20 s child health argv, and all guard/gate code are
    **unchanged** (no timing tuning); scope was `prompt.ts` + `production-live-harness.test.ts`
    only. Re-checks: full Pi suite **282/281/0/1**; targeted harness **26/26**; full-entry
    **16 pass/1 skipped**; strict `tsc` clean; `generate-contract --check` + `verify-package`
    PASS. The prior CARE `…-bj` PASS applies to the **old** bytes (`22774813…`, now history);
    the current candidate needs a **read-only regression readback** (not performed here).
  - **Budgets unchanged.** Old `…/imp004-production-live` **1 of 1**, resume
    `…/imp004-production-live-resume` **1 of 1**, final `…/imp004-production-live-final`
    **1 of 1** — all byte/mtime-untouched; **no reservation, no reset, no RUN**; old POC
    roots untouched.
  - **Limits / next action.** Writer self-report, **not independent verification**. The
    20 s child health check may still fail for reasons unrelated to the prompt, and the
    live `todowrite`-before-terminal ordering is **not guaranteed** by these offline tests.
    The **next action is a user decision** whether to approve a **new one-scenario live
    budget** after independent review; **no implicit approval** is created here. (The
    canonical `plan.md` T07g row and the `README.md` T07g note were later corrected to the
    then-current binding `69ec95fb…` under nonce `imp004-current-hash-20260923-bl`; that
    binding is itself superseded by **T07h** `2dd03532…` below.)

- **D64 — T07h offline precise-budget preparation (nonce `imp004-precise-budget-20260923-bo`;
  harness/docs only, NO model call, NO budget consumed).** The user **explicitly authorized
  ONE further live production Pi scenario** (1 parent + 1 read-only child, ≤180 s, existing
  default model/auth; **no install/Git delivery**). This mission prepares the budget and CLI
  allowlist only; the RUN is reserved to a **later Manager authorization**. **T07 umbrella
  `in_progress`** under that single authorized slot; **T08 unauthorized**.
  - **Change (harness/docs only).** `scenario.ts`: `APPROVED_BUDGET_ROOT` →
    `…/imp004-production-live-precise`; `SCENARIO_NONCE` → `imp004-prod-precise-20260923-bo`.
    The pure CLI parser still requires `--run --budget-root <exact precise> --nonce <fresh>`
    with **no default fallback**, and rejects **all three** previous exhausted roots
    (old/resume/final), any other/relative/non-canonical root, candidate overrides and
    duplicate flags; cap **1**; the programmatic default `runProductionLive()` (no explicit
    root) still targets the old exhausted `PRODUCTION_LIVE_ROOT` and **fails closed before any
    prompt**. No prompt/guard/gate/prod/POC source change; the dynamic `model_resolve`
    resolver, candidate commit+snapshot (`c4c6450…` / `0d792e13…`), the 20 s child health argv
    and the T07g one-patch STEP 3 are **unchanged**.
  - **Checks actually run (no live call).** Full Pi suite **283/282 pass/0 fail/1 skipped**;
    targeted harness **27/27**; strict `tsc` + production `tsc` clean;
    `generate-contract --check` + `verify-package` PASS; REAL zero-model preflight **17/17**
    (`get_state`/`get_commands` only, no prompt). The real `--run` was **NOT** executed.
  - **Hashes / roots.** source19 `0d792e13…` unchanged; new harness12
    **`2dd035329c93a62514b5140a267f80bfeeb2d336ac1c29eb07726d954ce9e2ba`** (changed rows
    `scenario.ts 29ff761f…`, `production-live-harness.test.ts 509d3bf5…`). Old/resume/final
    roots **1 of 1** each, byte/mtime-unchanged (bounded read-only ownership readback); the
    new root `…/imp004-production-live-precise` is **absent (0/1, no `cap.json`)**.
  - **Status / next action.** **T07h `done` (offline preparation)**; **T07 umbrella
    `in_progress`**; plan **active** for the single authorized slot; **T08 unauthorized**.
    **Awaiting independent verifier/CARE readback** of the current bytes before the separate
    Manager RUN. No source live PASS; the `…-aw` FAIL and both INCONCLUSIVE runs remain
    separate history.

- **D65 — T07i live production RUN (scenario nonce `imp004-prod-precise-20260923-bo`, mission
  nonce `imp004-prod-precise-LIVE-20260923-br`; executed ONCE, result FAIL, budget consumed).**
  The exact authorized command was run once from the workspace; **no retry**, **no
  install/commit/memory/delegation**, no model/provider override, no auth read.
  - **Preflight (zero model, before reserve):** installed CLI `0.85.1`; `factoryComplete:true`;
    registered `[todowrite, apply_patch, model_resolve, task, task_control]`; the sole
    reservation happened only after this PASS. Model `openai-codex` / `gpt-5.5` / effort
    `medium`.
  - **Outcome:** state **FAIL**, exit 1, gate **8 pass / 3 fail**. Pass: `taskHandleBound`,
    `progressNotification`, `progressBeforePlanUpdate`, `applyPatchBeforeTodo`, `todoFullId`,
    `toolCallIdsMatchNative`, `controlRunningAtTodo`, `terminalBound`. Fail:
    `applyPatchSucceeded`, `planChangedBeforeTodo`, `childAliveAtTodo`.
  - **Native order (full 83-char ids captured):** `model_resolve` allow → `task` allow
    (handle `bg_9c39cb8c-e48e-4888-9f59-9a1b235d6fb2`, plan `IMP-TEST`/`T1`, nonce exact) →
    **one** `apply_patch` allowed by both guards then `tool_execution_end isError:true` (no
    second attempt) → `todowrite` allow → `task_control` ×2 allow. 693 native events, 32
    observations, 12 guard decisions (all `allow`; `patch_unparseable`/`patch_scope_escape`
    not triggered), `agentEnds:0`, `lastAssistantReason:null`.
  - **Projection instant (obsSeq 20):** `control.state:"running"` (the background job was
    still running) but `childAlive:false` from the OS owned-worker snapshot; plan digest
    before == after `0512ed4f…`, `taskState:"pending"`, `changed:false`; the bound
    `applyToolCallId` is the errored apply.
  - **Terminal (obsSeq 31):** `state:"succeeded"`, exact nonce, candidate
    `{commit c4c6450b…, snapshot 0d792e13…}`. The host `sendMessage` progress (`e1` seq 1) and
    result (`e2` seq 2) are **producer attempts only — not content proof**.
  - **Fixture after run:** `T1 | Fixture primary task | pending` and
    `T2 | Report target task | pending` (plan byte-unchanged). The exact `apply_patch` failure
    text is **not persisted** (no raw patch input stored) — **no cause is attributed** and no
    model refusal is claimed.
  - **Measured:** wrapper observation span **≈29.8 s** (first→last `at`), well under the 180 s
    parent deadline; this is a measured span, not a latency guarantee.
  - **Hashes / budget / cleanup:** source19 `0d792e13…` and harness12 `2dd03532…`
    **byte-identical before/after**; new root `…/imp004-production-live-precise` now **1 of 1**
    (`cap.json`, `slot-0.lock`, `reservation.json` with nonce `imp004-prod-precise-20260923-bo`);
    old/resume/final **1 of 1** byte/mtime-unchanged; **no owned process leak** (only owned
    groups reaped, no global kill).
  - **Status.** **FAIL retained, not doctored**; plan **paused**, active **none**, T07
    **blocked**; **T08 unauthorized**. Any further attempt needs a **new user decision** (no
    new budget exists).

- **D66 — T07j offline `apply_patch` error diagnostics + liveness honesty (nonce
  `imp004-patch-error-offline-20260923-bu`; harness/docs only, NO model call, NO budget
  consumed, NO install/commit/delivery).** The user authorized resolving the last
  failure offline; this pass makes future runs diagnosable and stops the liveness
  observer from fabricating a dead child. It is **not retroactive**: it cannot explain
  the `…-br` FAIL, whose exact error text was never retained.
  - **What changed (harness only).** New test-only `diagnostics.ts` classifies a REAL
    production `apply_patch` error into a CLOSED reason-code set
    (`patch_parse`/`path_escape`/`root_drift`/`target_drift`/`session_authority`/
    `parent_mutation_conflict`/`recovery_pending`/`fs_notfound`/`fs_permissions`/`other`)
    using only exact known message strings, trusted error `name`/`code`, and a fixed
    fs-code allowlist; it never returns, stores, or logs the original message, a path,
    or an arbitrary `e.code`. `wrapper.ts` now wraps the ACTUAL registered
    `apply_patch.execute`: it observes the thrown error BEFORE Pi converts it to native
    text, records `{kind:"tool_error", toolName:"apply_patch", toolCallId, reasonCode,
    provenance:"observer"}` (no raw text), and **rethrows the exact same error object**
    so native `isError` behavior is unchanged. It also records path-free target metadata
    (capped `targetCount` ≤64 + SHA-256 of the normalized sorted relative targets) and
    never the target paths. `evidence.ts` classifies native `tool_execution_end` errors
    from allowlisted fixed strings only (unknown ⇒ `other`) and merges observer/native
    tool errors by tool call with the observer preferred, so a specific code is never
    duplicated contradictorily. `gate.ts` gained the additive, informational
    `toolErrors` type only — the gate verdict is unchanged.
  - **Liveness.** `observeOwnedWorker` now returns UNKNOWN (omits `childAlive`) on a
    no-match or ambiguous OS snapshot instead of `false`; `childAlive:false` requires an
    explicit same-generation binding the live path does not establish. A missing OS
    witness is therefore INCONCLUSIVE, never a fabricated dead child; the strict gate
    still needs `childAlive === true` (a unique matched worker) to pass that check.
    Mandatory `planChangedBeforeTodo` / `applyPatchSucceeded` and the 20 s child-health
    argv are unchanged — no clock was gamed.
  - **Checks actually run (all offline).** Strict `tsc` (both harness tests + all
    `production-background-live/*.ts`) clean; production `tsc -p tsconfig.json` clean;
    full Pi suite **292 tests / 291 pass / 0 fail / 1 skipped**; `generate-contract
    --check` + `verify-package` PASS; REAL zero-model preflight **21/21**
    (`get_state`/`get_commands` only, **no prompt**). New tests: classifier allowlist +
    secret-free negatives; path-free target metadata; REAL `createApplyPatchTool`
    parse-error classification and exact-identity rethrow; `ownedWorkerLiveness`
    unknown/no-match; native `tool_execution_end` classification + non-contradictory
    merge; full-entry `failed-patch` records `target_drift` with no raw text; helper-only
    / pending-plan / post-child liveness cases stay non-PASS.
  - **Hashes / budget / cleanup.** source19 **`0d792e13…` byte-identical (19 rows, 0
    drift)**; new **harness13** `74a2b1d7d1f645e1f1afc4a64b71c0917d77c89a99c6aabccaf7d1b00abcbcb4`
    (12 prior rows + the new `diagnostics.ts`). All four roots — `…/imp004-production-live`,
    `…/imp004-production-live-resume`, `…/imp004-production-live-final`,
    `…/imp004-production-live-precise` — remain **1 of 1**, byte/mtime-untouched (bounded
    read-only readback); no reservation, no reset, no RUN.
  - **Status / next action.** **T07j `done` (offline)**; **T07 umbrella `blocked`**; plan
    **paused**, active **none**; **T08 unauthorized**. Writer self-report, **not
    independent verification**. The `…-br` FAIL is retained with its exact cause still
    **UNKNOWN**, and the earlier `childAlive:false` reading is **NOT** proven to mean the
    child had exited. Any further attempt needs a **new user decision**; no implicit
    budget is created here.

- **D67 — IMP-004 `cancelled` at the user's explicit request (DISCARD-only scope, nonce
  `imp004-discard-records-20260924-d`).** The child→parent progress objective is **abandoned**;
  the plan is **`cancelled`** (not closed). T07 (`blocked`) and T08 (`pending`) → **`cancelled`**;
  no task is `in_progress` or `blocked`; all `done` tasks remain **historical completed work** that
  cannot be used as current approval once the source is removed. **No IMP-004 code/test/prototype or
  feature is installed**; the local IMP-004 source/tests were **removed from the repository** and the
  11 tracked IMP-004 files **reverted to HEAD** (verified: 174 Pi tests, 4 Go packages, generated
  prompt check passed). Pre-live source-candidate reviews (verifier `…-k`, CARE
  `…-l`, specialist `…-m`, and later pre-live PASSes) are **invalidated** by that source removal;
  the live FAIL/INCONCLUSIVE results are retained only as **historic observations**. IMP-003 remains
  **closed and installed/preserved** (byte-exact).

## Findings (nuanced)

- **Pi.** `task.ts:39-46` awaits the runner and returns a terminal envelope only after
  the worker settles; `result.ts:3` states are `succeeded|failed|cancelled|unavailable`;
  `runner.ts:108-154` consumes `message_update`/`tool_execution_end` and resolves on
  `agent_settled` (141-146) with no parent-model callback; `runner.ts:44-57` serializes
  at most one writer; `runner.ts:87` uses `--no-session`. `mission.ts:21-26,44-47` bind
  nonce uniqueness + sha256 digest replay. Event visibility ≠ parent delivery.
- **Codex.** Host-transport native-config only; `codex_delegation_test.go:115` runs
  `codex exec --json` and `:124` gates on `collab_tool_call` — a test assertion, not
  runtime evidence. No app-server integration here. **Future phase, untouched.**
- **OpenCode.** `current_renderer.go` is a renderer, not a progress channel;
  `context_eval_test.go:387` requires missing child trace to be INCONCLUSIVE, never
  PASS. Native parent delivery is unverified/unwired, not "unavailable". **Future.**

## Withdrawn overclaims (retained as history)

Unconditional "truly live" feasibility; any provider "globally unavailable";
"sendMessage exists ⇒ live delivery" — all **withdrawn**.

## Blockers

- **IMP-004 `cancelled` (user request); prior state retained as history: T05 POC accepted; T06 source accepted (source only, pre-live); T07 `blocked`; T08 unauthorized.** Post-live POC `imp004-id-liveverify-am` PASS. **Three production live runs, none
  accepted:** `imp004-prod-LIVE-20260923-aa` **INCONCLUSIVE**, `imp004-prod-resume-LIVE-20260923-ap`
  **INCONCLUSIVE**, `imp004-prod-final-LIVE-20260923-az` **FAIL** (child ran + terminal bound; the
  parent projected `todowrite` after the child succeeded and the plan never changed). **live
  PRODUCTION not observed / not accepted**; none is retroactively accepted. Sorted candidate
  `0d792e13…` source-checked; **all four budget roots are 1 of 1 exhausted**. Plan **cancelled**, active
  **none**.

## Next action

- **Next action: none — no blocker.** The discard is complete and verified: the IMP-004 untracked
  code/tests/prototypes were **removed from the repository** and the 11 tracked Pi files
  **restored to HEAD** (174 Pi tests, 4 Go packages, generated-prompt check passed). No IMP-004
  feature is installed. Prior INCONCLUSIVE/FAIL results are preserved as history. The four temp run
  budget roots are intentionally **not** purged: they remain **historical external evidence outside
  the repository scope** and are **not** claimed removed. **T08 is `cancelled`.** No future plans
  are created and **no commitments are made for OpenCode/Codex**; their bytes remain unchanged.
