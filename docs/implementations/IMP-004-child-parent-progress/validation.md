# IMP-004 — Validation

Self-reported evidence by the single documentation writer
(`imp004-plan-draft-20260922-d` → `imp004-finalize-plan-20260922-f` →
`imp004-plan-correction-20260922-h` → `imp004-plan-ready-20260922-l` →
`imp004-poc-build-20260922-n`). This file is the **writer's** record; it is **not**
independent verification of an implementation.

**CANCELLED (2026-09-24, nonce `imp004-discard-records-20260924-d`):** IMP-004 was abandoned at the
user's request and the plan is `cancelled`; all evidence below is retained as **archival history**.
The Manager has since removed the IMP-004 source/tests and reverted the 11 tracked IMP-004 files to
HEAD, so the pre-live source-candidate reviews/bindings here are **invalid as current approval**
(the reviewed source no longer exists), and the live FAIL/INCONCLUSIVE results remain only as
**historic observations**. No IMP-004 feature is installed or accepted. The four temp run budget
roots are intentionally **not** purged: they remain **historical external evidence outside the
repository scope** and are **not** claimed removed.

**Historical source binding (IMP-004 source now removed; NOT the current candidate or an approval):**
**T06j** manifest digest **`0d792e13775ff8afd6ecb7646cd877bbda837e41956f51f7049bd51cb273483b`**
(19 rows, sorted lexicographically, rows `repo-relative-path␠␠sha256\n`). See the
**T06j** section at the bottom. Every earlier section — including the T06i
`cc9be0a1…`, T06h `b550b135…`, `65fdbb54…` and `99f4d2b1…` digests — is **retained
history** and is **not** the current binding.

## Candidate identity

- **Source revision (read-only):** `c4c6450b779cd39c95a96fc3303bb4cc67ee0291`
  (unchanged by this plan; the POC artifacts are new, uncommitted test-only files).
- **Plan state:** **`cancelled`**, **active plan: none** (current). At the time this evidence was recorded the plan was **active IMP-004**, single focused leaf **T05** — retained as history only, **not** a current approval.
- **D1:** resolved by the user as **"Pi primero (Recomendado)"** — Pi-native
  background-first POC gate.
- **T05 authorization:** user, explicit, nonce `imp004-poc-build-20260922-n`; live
  budget **≤2 scenarios total, 1 parent + 1 child each, ≤180 s**, existing
  configured model/auth, no model/settings/credential change, **zero live calls in
  this mission**.
- **Guides (validated before body load, exact bytes):** `agent-evaluation` sha256
  `0cc075f59b6c6e3afcbfcd6a2695398f599266b0b1ff70e5ebbdf0244ad694bc` and
  `security-boundary` sha256
  `c0c49bc310ec6ee059582502b17963a8c0ffc522fc165b5ab43b4e88a4cfa9e4`, both matching
  the canonical source paths. No other skill body loaded.

## Evidence obtained (T05a capability gate — zero model calls)

| Check | Method | Result | Limits |
| --- | --- | --- | --- |
| Baseline HEAD | read-only `git rev-parse HEAD` | `c4c6450b…ee0291` | no branch-clean claim |
| Working tree | read-only `git status --porcelain` | ` M docs/implementations/README.md`; `?? IMP-003…/`; `?? IMP-004…/` | IMP-003 preserved untouched |
| Guide hashes | `shasum -a 256` of the selected SKILL.md files | exact matches (above) | validated before load |
| Installed CLI version | `pi --version` | `0.85.1` | read-only, no model call |
| Installed CLI flags | `pi --help` | `--mode rpc`, `--extension`, `--no-builtin-tools`, `--tools`, `--offline`, `--no-session`, `--no-skills`, `--no-prompt-templates`, `--no-themes`, `--no-context-files`, `--approve`, `--thinking` present | help text, not a live RPC run |
| Local dev SDK version | `node_modules/@earendil-works/pi-coding-agent/package.json` | `0.84.4` | pinned; **not** upgraded |
| Global SDK version | global `package.json` behind the `pi` symlink | `0.85.1` | read-only |
| `sendMessage` API | source read of `dist/core/extensions/types.d.ts` (local `0.84.4` **and** global `0.85.1`) | present at the same lines (298/971/1247), `deliverAs ∈ {steer,followUp,nextTurn}`, `triggerTurn` | static source; behavior gated on the live run |
| SDK exports | `index.d.ts` symbol scan | `ExtensionRunner`, `defineTool`, `createExtensionRuntime`, `runRpcMode`, `RpcClient`, `createAgentSession` present in both versions | names only; no runtime execution |
| Worker transport | static read of `task.ts:39-46`, `runner.ts:44-57,87,108-154`, `mission.ts:21-26,44-47` | task awaits terminal; single-writer serialization; `--no-session`; no parent-model callback while awaiting | repository, not runtime |
| `todowrite` semantics | static read of `src/tools/todowrite.ts` | `pi.appendEntry("vgxness.todos", {todos})` | reused by the POC parent projection |

## Behavior tests (test-only prototype; zero model calls)

| Check | Command | Result | Limits |
| --- | --- | --- | --- |
| Typecheck (src) | `npm run typecheck --workspace packages/pi` | pass (exit 0) | `tsconfig` includes `src/**` only |
| Typecheck (fixture) | `npx tsc -p test/tsconfig.poc.json` | pass (exit 0) | POC fixtures + test file only; no new deps |
| POC tests | `node --test packages/pi/test/child-progress-poc.test.ts` | **24/24 pass** (wire-fix round) | deterministic simulation + a zero-LLM fake Pi CLI; **not live** |
| Full Pi suite | `npm test --workspace packages/pi` | **198/198 pass** (wire-fix round) | includes the 24 POC tests |
| Workspace typecheck | `npm run typecheck --workspace packages/pi` | pass (exit 0) | `src` only |
| POC typecheck | `node node_modules/typescript/bin/tsc -p packages/pi/test/tsconfig.poc.json --noEmit` | pass (exit 0) | fixtures + test |
| Zero-model load probe | `node live-scenarios.ts --preflight` | CLI `0.85.1` verified; extension command `poc-status` observed; `promptSent:false`; effective model `openai-codex/gpt-5.5`, thinking `medium` | real Pi RPC `get_state`/`get_commands` only; **no prompt** |
| Contract check | `node packages/pi/scripts/generate-contract.mjs --check` | ok | no generated change |
| Package verify | `node packages/pi/scripts/verify-package.mjs` | ok | package metadata unchanged |
| Go offline | `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./internal/providers/pi ./internal/release -count=1` | ok | new fixtures are not packaged |
| Smoke `--help` | `node packages/pi/test/poc-child-progress/live-scenarios.ts --help` | usage printed, no model call | — |
| Smoke `--preflight` | `node packages/pi/test/poc-child-progress/live-scenarios.ts --preflight` | redacted JSON; no auth/config dump | `live:false`, `liveRunImplemented:false` |

Covered scenarios: structural/bounds validation; identity and ordering rejection
(stale, duplicate, out-of-order, wrong plan/task/mission/candidate, closed plan,
late cancel, flood, backpressure); identity-bound child tool emitting a **native
`onUpdate` partial result** with a disclosed artificial hold; UTF-8 wrapper sizing
(exactly-max accepted, over rejected, no JSON slicing); early-return handle;
buffer-before-handle-delivery then flush; single bounded parent-message forward
while the child is alive; **model-driven** `persist_plan` before `todowrite`
(reusing the real `src/tools/todowrite.ts` semantics) with the extension never
invoking them itself; native witness host/model classification and monotonicity;
rejected/late events not forwarded; spawn failure, cancel, budget-timeout and child
exit handling; durable ledger restart/duplicate/child-spawn-cap; hard-gated live
path; bounded child supervisor timeout/group kill.

Defect-fix regressions (prior reviewer/verifier FAILs, now covered): generated
wrappers embed the absolute config path (no env dependency); child-spawn reservation
precedes spawn and blocks a second spawn; one monotonic timeline; hard gate
INCONCLUSIVE on missing witnesses and FAIL on late-or-not-alive todo; `result` never
marks done and `completed` todos are rejected; supervisor aborts only its owned real
process group; exactly one prompt per process; parent argv uses the host default
model.

- **Not covered / open:** a live parent **model** tool call (AC05); real `0.85.1`
  event delivery; natural child throughput. **No live model call was made.** No
  install, network fetch, memory call, Git delivery, or production source change.

## Candidate identity — **HISTORICAL** (wire-fix prototype, nonce `imp004-poc-wirefix-20260922-v`; superseded, retained, not erased)

> Superseded by the current T05h manifest below. Kept as history; earlier C3/C4
> receipts are **not** erased and do not transfer across candidates.

- **Prototype-source aggregate sha256:**
  `a5f52c8338e1a2c03a29d9e29ba02a695d05f18b21d54396f2e9a9aa8fd537f3` — sha256 over
  the sorted twelve `path  sha256` rows below (repo-relative paths, two spaces,
  newline-joined, trailing newline). Docs are excluded so the digest is not
  self-referential. Supersedes `b0434131…`, `88268e9e…` and `82f46ab3…`.
- Per-file sha256 (12 artifacts; all under `packages/pi/test/`):
  - `child-progress-poc.test.ts` `9a97cd2ac8ad6a22b8fa1a9f8dc6780e54fc81eab43faae5eec5524fd5281e10`
  - `tsconfig.poc.json` `8739d19342d78cd05b12627d27c1f9786877bd95869be657f1e3694c373a7ce1`
  - `poc-child-progress/protocol.ts` `644c795eacc129c2ae7d026c66f97787e20c426634e76635415ed514a7a0fc2d`
  - `poc-child-progress/child-extension.ts` `5d6c22e612537241e654398720b06cbe9698a3328fb63d33a60a2da378c6a226`
  - `poc-child-progress/parent-bridge.ts` `9ce9de534456bfd4942bfb28c30d9c7f3197aa9d15174153c83b1103eabe95a2`
  - `poc-child-progress/simulated-transport.ts` `1df0a76b3390f38408f113f02269a88a799d8fb4e24ca94f9133853f6e7bd3d2`
  - `poc-child-progress/live-scenarios.ts` `1cd38758d5acd94b593a0c92fcc4679690776923ee14f14984d3042a352b3987`
  - `poc-child-progress/ledger.ts` `f50dd6f1c1277e90c86fedd71edfb318fade1b46cdd82a1487cde695eeda9b6b`
  - `poc-child-progress/native-events.ts` `57f102f24bdec6da2ff572807d71cc574c85cd3735006adfe159569ade3a30d9`
  - `poc-child-progress/parent-extension.ts` `a30016e5e1fdfb1602d33c65c2d113124ecc9cad6c6be84bc70a8345e4e3399e`
  - `poc-child-progress/mock-rpc.ts` `506289b01139677651e20c832a59e1751f4637b429d13b829f2b54d0cb37a36e`
  - `poc-child-progress/probe.ts` `834ffdc6c144822848d9f906a01cbf9125c3adc326ff16666e379285e76f5e97`
- These hashes describe the **self-reported writer snapshot**; they are not
  independent verification and will change with any further edit to those files.
- **Candidate commit binding:** `c4c6450b779cd39c95a96fc3303bb4cc67ee0291` (frozen
  at mission start). The twelve prototype files are **untracked**; the aggregate
  digest is the runtime binding, not a commit claim. No snapshot hash is invented.

## Candidate identity — **HISTORICAL at time of attempt; source removed, not current approval** (T05h ID-fix prototype, nonce `imp004-poc-idfix-20260922-ai`)

- **Prototype-source aggregate sha256:**
  `99f4d2b10f674fa701f7bda418dbdd0c58d368d61f9fba5c5b04739212d33820` — sha256 over
  the sorted twelve `path  sha256` rows below (repo-relative, two spaces,
  newline-joined, trailing newline; docs excluded so the digest is not
  self-referential). Supersedes `a5f52c83…` (historical block above) and `65fdbb54…`.
- Per-file sha256 (12 artifacts; all under `packages/pi/test/`):
  - `child-progress-poc.test.ts` `4906b0bb4ba7d55d7b351315398d4c5f4a290b6215caf0cfb883b557ba3ea8ed`
  - `tsconfig.poc.json` `8739d19342d78cd05b12627d27c1f9786877bd95869be657f1e3694c373a7ce1`
  - `poc-child-progress/protocol.ts` `644c795eacc129c2ae7d026c66f97787e20c426634e76635415ed514a7a0fc2d`
  - `poc-child-progress/child-extension.ts` `5d6c22e612537241e654398720b06cbe9698a3328fb63d33a60a2da378c6a226`
  - `poc-child-progress/parent-bridge.ts` `20fca27ed4b67ae960d54b34280bec43adf4e7d6b2c68e643f3a568439444e99`
  - `poc-child-progress/simulated-transport.ts` `1df0a76b3390f38408f113f02269a88a799d8fb4e24ca94f9133853f6e7bd3d2`
  - `poc-child-progress/live-scenarios.ts` `a8efcc7d946c92d07ded9e99234185825ebc9b0e115302f96acaacce051b89f1`
  - `poc-child-progress/ledger.ts` `86b2f2dcc227e5aef2951d80435cd933ba7daaf6100d102b3ce6fa7cd9cf3c85`
  - `poc-child-progress/native-events.ts` `9b98e43c1c53d653fc5d3096821a4b45e78de977abf8ea9963bac2313202f6f9`
  - `poc-child-progress/parent-extension.ts` `38b84abac72397155d3926df8fa0ee1bc482ca7beff16f705e681710f474b1e4`
  - `poc-child-progress/mock-rpc.ts` `a616809ca0e308a7f019c5f6267141099d061edfd7e4eb35c9eda4f53edd94e8`
  - `poc-child-progress/probe.ts` `834ffdc6c144822848d9f906a01cbf9125c3adc326ff16666e379285e76f5e97`
- **Candidate commit binding:** `c4c6450b779cd39c95a96fc3303bb4cc67ee0291`; production
  unchanged. These are self-reported writer snapshots, not independent verification.

### T05h live RUN start (nonce `imp004-poc-id-live-20260922-al`)

Manager-authorized **ONE new real diagnostic** (`poc-progress-recovery`, ≤180 s,
1 parent + 1 child) on the **new** root `…/imp004-poc-id-live` with
**`--max-scenarios 1`**, after verifier `imp004-id-verify-aj` **PASS (pre-live)** and
CARE `imp004-id-care-ak` **PASS (source)** on `99f4d2b1…d33820`. **No source change.**
Old root `…/imp004-poc-live` read-only, 2/2 preserved. **T05h `in_progress`.** No
retry after whatever result; ledger exhausts **1 of 1**.

## Proposed live scenarios — **SUPERSEDED (F9 history; kept, not current)**

> **Superseded.** The two blocks below describe the **pre-wire-fix** proposal and are
> retained as history only. They are **not** the current contract: (a) the child argv
> shown here appends a **positional `<child-prompt>`**, which `--mode rpc` ignores and
> which caused the child to hit stdin EOF and exit; the frozen contract sends ONE
> native `{type:"prompt"}` line on a **PIPE stdin** instead. (b) the scenarios shown
> here are **multi-report** (`seq1 … → seq2 …`) and one is misnamed
> `poc-result-barrier`; the frozen contract is **single-report, one report per
> scenario**, named `poc-progress-hold` / `poc-result-hold`, each with a disclosed
> **75 s artificial hold**. (c) no automatic/default edit is made to the frozen
> criteria. See "Wire-fix round" below for the actual source.

_Historical proposal (do not use):_

Child argv (both scenarios, **historical**):
`<cli> --mode rpc --offline --no-session --no-extensions -e <child-ext> --no-skills --no-prompt-templates --no-themes --no-context-files --no-builtin-tools --tools report_progress --provider <host> --model <host> --thinking <host> <child-prompt>`

Parent argv (**historical**):
`<cli> --mode rpc --offline --no-session --no-extensions -e <parent-ext> --no-skills --no-prompt-templates --no-themes --no-context-files --no-builtin-tools --tools start_child,persist_plan,todowrite,poc_status --provider <host> --model <host> --thinking <host>` then a `prompt` RPC line.

| Scenario (historical names) | Child steps | Timeout | Stream caps |
| --- | --- | --- | --- |
| `poc-progress-hold` | seq1 progress(0/1) → seq2 needs_input(summary) → 75 s hold | 180 s | stdout/stderr 256 KiB each |
| `poc-result-barrier` | seq1 progress(1/1) → seq2 result(summary) → 75 s hold | 180 s | stdout/stderr 256 KiB each |

**Current frozen criteria (authoritative).** Child argv has **no positional prompt**
(stdin RPC). Two scenarios only: `poc-progress-hold` (ONE `progress` report, seq 1,
eventId `c1`, completed 0 / total 1) and `poc-result-hold` (ONE `result` report,
seq 1, eventId `d1`, summary "bounded POC result"); each has a disclosed **75 s**
artificial hold. `--tools` allowlists are parent
`start_child,persist_plan,todowrite,poc_status` and child `report_progress` only.
Model selection is the **host default** supplied by the parent session at run time
(`ctx.model` + effective thinking via `get_state`); **no** `--provider`/`--model`/
`--thinking` override on the parent, the child mirrors that provider/model/thinking,
and auth is never read. Budget: **max 2 scenarios total**, enforced by the durable
BUDGET ROOT ledger (committed before parent spawn, no reset, one child spawn per
scenario); the 180 s timeout covers the whole parent+child lifecycle and kills only
owned process groups. CLI: `--help`, `--preflight` (redacted, no model),
`--run --scenario <id> --results-dir <dir>`; consent is `VGXNESS_POC_LIVE=1` (there
is **no** `--authorize` flag — confirmed from `--help`).

## Live run start record (nonce `imp004-poc-live-20260922-y`)

- **Status:** T05e set **`in_progress`** for this mission; **not** closed. T06 onward
  remain **unauthorized**; T05f stays **pending** until the outcome.
- **Candidate binding:** aggregate sha256
  `a5f52c8338e1a2c03a29d9e29ba02a695d05f18b21d54396f2e9a9aa8fd537f3` over the 12
  frozen test-only artifacts (matches the wire-fix freeze), source HEAD
  `c4c6450b779cd39c95a96fc3303bb4cc67ee0291`. **No source/test change** in this
  mission; the 12 files are read-only and re-hashed after the run.
- **Prereviews (Manager reports, prior mission):** verifier `imp004-poc-verify-w`
  **PASS (pre-live)**; CARE `imp004-poc-care-x` **PASS (source readiness)**.
- **User live authorization:** explicit — **≤2 whole scenarios** (1 parent + 1 child
  each, **≤180 s**), existing configured model/auth, **no** retries, **no** model/
  settings/credential change, **no** auto fallback; run is **sequential** and the
  second scenario runs only if the first **PASS**es with no unexplained cleanup issue.
- **Zero-model preflight (this mission, no budget consumed):** frozen aggregate
  re-hashed to match; `--help` inspected (flags confirmed; no `--authorize`); budget
  root absent at
  `/private/var/folders/4q/nlfcvv816_l0bh4wsl846_kw0000gn/T/opencode/imp004-poc-live`
  with an existing, owned, writable parent directory.

## Known risks / open (not proven)

- **Live delivery unproven.** Native `0.85.1` delivery of a child
  `tool_execution_update` into the parent **model** turn is not observed; the mock
  proves only the handling code, not runtime delivery. AC05 stays OPEN.
- **Trusted extension is not a sandbox.** The parent extension has full host APIs;
  it is bounded only by fixture-fixed ids/paths and its own logic.
- **Artificial hold.** The 75 s child hold is disclosed and is not natural
  throughput; it could mask a race that natural timing would expose.
- **`persist_plan`/`todowrite` are model-driven in live only.** The mocked test
  invokes them as the model stand-in; it is not a model claim.
- **Cleanup is bounded best-effort.** The supervisor aborts only its own tracked
  handles and the child is non-detached (parent group); verified against mocks and
  real non-model node processes, not the live Pi model path.
- **Static API evidence.** `sendMessage`/`onUpdate`/event shapes are read from the
  installed source; runtime behavior remains version-gated.
- **Prior misproofs are closed only on actual evidence** (zero-model load probe,
  deterministic regressions); the earlier `runLive` stub is recorded as history and
  no longer present.

## Plan review vs acceptance

- **CARE planning review `imp004-planreview-20260922-k`: PASS** on the planning
  criteria. Prior failures — CARE `planreview-g` (F1/F2; F3 optional) and CARE-i F2 —
  are **closed history**.
- **AC05 is unproven.** A test-only prototype cannot emit a real parent **model**
  tool call; the simulation proves only scheduling/ordering. **Independent
  verification and CARE against the frozen POC candidate are pending**, and the live
  hard-trace run is reserved to a separate Manager authorization.
- The **Manager readback is a documentation review** — internal consistency and
  evidence labels. It is **not** source acceptance.
- **Verifier:** not run — the prototype is frozen but independent verification is
  **pending**. **CARE:** planning PASS recorded; POC review **pending**. **No
  `ACCEPTED` T05 claim is made** until the live run and independent reviews.

## Source-tree status (read-only)

Changed by this writer: `docs/implementations/README.md` (IMP-004 index row/note),
`docs/implementations/IMP-004-child-parent-progress/*` (plan/progress/research/
validation), and **new test-only** files under
`packages/pi/test/poc-child-progress/` plus `packages/pi/test/child-progress-poc.test.ts`.
`IMP-003` content is byte-preserved; production `packages/pi/src/**`, generated
resources, contract and goldens are **untouched**; OpenCode and Codex are untouched.
Records are **local (uncommitted)**; no commit authorized. No Git-clean claim.

## Limits

Build/test/freeze only. No live behavior was exercised, so no live-delivery,
performance, security, or acceptance claim is made. Static SDK/API evidence is
version-gated and not runtime proof. AC05 remains **OPEN**.

## Wire-fix round (`imp004-poc-wirefix-20260922-v`) — actual wire source

Independent verifier/CARE source review found the prior `b0434131…` candidate's
live path was **unwired**; this round fixes the wiring in the actual source (not
helper tests). Guides re-validated before load: `agent-evaluation`
`0cc075f5…4ad694bc` and `security-boundary` `c0c49bc3…4cfa9e4` (exact matches).

| # | Defect (evidence) | Actual wire fix | Source |
| --- | --- | --- | --- |
| 1 | Child argv appended a positional prompt while `--mode rpc` ignores it and stdin was `/dev/null`, so the child hit EOF and exited | Child spawned with **PIPE stdin**; exactly ONE native `{id,type:"prompt",message}` line written after the spawn/load gate; no positional prompt; stdin kept open for the child's life | `parent-extension.ts` `pocChildArgv`/`start_child`; `live-scenarios.ts` `pocScenarioArgv` |
| 2 | `generateParentWrapper` passed only `{config}`, so `recordObservation` and `reserveChildSpawn` were undefined → no `observations.jsonl`, no durable child count | Generated wrapper imports `observationAppender` + `DurableLedger` and wires both callbacks; the sink is synchronous `appendFileSync` (strong same-file ordering); the reservation is nonce-bound before the child spawn | `live-scenarios.ts` `generateParentWrapper`; `ledger.ts` |
| 3 | Fixed `wx` filenames all in one dir, colliding across scenarios | ONE fixed BUDGET ROOT (`options.resultsDir`) holds the ledger; each scenario gets an exclusively-created subdir `${missionNonce}--${scenarioId}` for configs/wrappers/report/markers. Duplicate/exhausted rejected by `precheck` before any spawn | `live-scenarios.ts` `runLive`; `ledger.ts` `precheck` |
| 4 | “runLive passes” was only helper-level | Structural FULL-ENTRY regression calls the ACTUAL exported `runLive` twice on one root with a zero-LLM fake Pi CLI implementing the real stdin RPC and loading the generated wrapper; asserts 2 reports, no collision, ledger 2 with `childSpawns:[1,1]`, non-empty observations with a native parent model witness, and a third call rejected before any spawn | `child-progress-poc.test.ts` `FAKE_PI_CLI` + FULL ENTRY |
| 5 | Gate accepted a todo body not bound to a native parent call and had no owned-termination terminal | Gate requires the todo snapshot bound to the mission nonce + current child generation + the actual native parent `todowrite` `toolCallId`, alive and not-settled, before the child terminal; an OWNED `host_child_kill` after the todo projection is a legitimate terminal; a missing terminal is INCONCLUSIVE | `live-scenarios.ts` `evaluateHardGate` |
| 6 | Docs overstated model selection / version proof | Model selection documented as host default `ctx.model` + effective thinking via `get_state`; parent CLI passes no model overrides, child mirrors provider/model/thinking; `VGXNESS_POC_PARENT_MODEL` is an equality assertion only. Local dev SDK `0.84.4` cannot prove the installed `0.85.1`. Zero-model probes added for BOTH parent (`poc-status`) and child (`poc-report-status`) | `live-scenarios.ts`; `probe.ts`; `child-extension.ts` |

- **The real installed CLI was not re-probed in this mission's permitted check
  set** (typechecks + suites only). The earlier `openai-codex/gpt-5.5`, `medium`
  observation remains a **prior-mission** observation, not a re-run here. With
  `--no-extensions` an ambient builtin `llama` command still appears; that is a
  builtin host command, not the memory tool, and the POC does not reconfigure
  providers.
- **Process supervisor** kills only its own tracked groups; the child is
  non-detached (parent PGID). No PID-file-based independent kill, no daemon, no
  credential copy/print, no global settings change, no model override.
- **Not live.** The fake CLI is a test double; the two `runLive` invocations are
  zero-LLM and never claim AC05.
- **FULL ENTRY observed result (this mission):** both scenarios `verdict.status =
  PASS`; ledger 2 reservations with `childSpawns:[1,1]` and matching nonce; the
  duplicate/exhausted third call rejected before any process spawn. Measured
  runtime ≈ 5.7 s for both scenarios (finite, bounded; well inside the 180 s live
  cap).

### C1/C2 failure-history closure

- **C1 — helper-only “runLive passed”.** Closed on the FULL-ENTRY regression
  above: it drives the generated wrapper and the actual exported `runLive`.
- **C2 — live path unwired / child argv+stdin defect.** Closed at source:
  `pocChildArgv`/`pocScenarioArgv` carry no positional prompt, the child is
  spawned with PIPE stdin and one native RPC prompt, and the generated wrapper
  wires `recordObservation` + `reserveChildSpawn` against the nonce-bound durable
  ledger.
- **T05e live count (wire-fix mission): 0 executed.** (Superseded by the
  `imp004-poc-live-20260922-y` result below, which executed **1** scenario.)

## Live run result — nonce `imp004-poc-live-20260922-y` (controlled, authorized)

- **Outcome: INCONCLUSIVE. Scenario 1 only; scenario 2 NOT run.** A first
  non-PASS stops the dependent T05f path; the second case is not consumed to
  “fish” for a pass.
- **Exact command (scenario 1):**
  `VGXNESS_POC_LIVE=1 VGXNESS_POC_PI_CLI=/Users/uzielvgx/.nvm/versions/node/v24.15.0/bin/pi node packages/pi/test/poc-child-progress/live-scenarios.ts --run --scenario poc-progress-hold --results-dir <BUDGET_ROOT>`
  where `<BUDGET_ROOT> = /private/var/folders/4q/nlfcvv816_l0bh4wsl846_kw0000gn/T/opencode/imp004-poc-live`.
  Consent is `VGXNESS_POC_LIVE=1` (no `--authorize` flag exists; confirmed from
  `--help`). No model/provider/thinking override; no auth read.
- **Frozen candidate:** aggregate `a5f52c83…37f3` (12 files) re-hashed to match
  before the run; source HEAD `c4c6450…`. **No source/test change.**
- **Harness counters:** `promptCount = 1`, `reason = timeout`, `elapsedMs =
  180008`, `exitCode = null`, `stderrBytes = 0`. Effective model from host
  `get_state`: **`openai-codex` / `gpt-5.5` / thinking `medium`**.
- **Native parent trace (actual source-bound, sanitized):** `host_turn_start` →
  `model_message_end [stop:toolUse]` → **`model_tool_start/start_child`** with
  native execution id
  `call_OocDupUAN8VKK7sr7A4kT49b|fc_0974b0d861eece3e016ab2f1808e048` →
  `model_tool_end/start_child [ok]` → `turn_end` → `turn_start` →
  `model_message_end [stop:stop]` → `turn_end` → `agent_end` → `agent_settled`.
  **No `persist_plan` and no `todowrite` tool call appear** — the parent's second
  turn stopped after text only.
- **Native child trace (sanitized):** `host_child_prompt_sent` →
  **`child_progress`** `{planId:"imp004-poc", taskId:"T05", missionNonce:
  imp004-poc-wirefix-20260922-v, candidate.commit:c4c6450…, seq:1, eventId:"c1",
  reportType:"progress", counters:{completed:0,total:1}}` →
  `host_parent_message_queued [c1]` → `child_agent_end` →
  `child_agent_settled` at ≈ **+75 s** (the disclosed artificial hold).
  The child report's identity/candidate is stable and matched the gate config.
- **Hard-gate checks:** `childProgressObserved` **pass**,
  `parentForwardedProgress` **pass**; `modelPersistPlan`, `modelTodoWrite`,
  `persistBeforeTodo`, `todoBoundToParentCall`, `todoMissionBound`,
  `todoChildGeneration`, `childAliveAtTodo`, `childNotSettledAtTodo`,
  `todoBodyBeforeChildTerminal` all **missing** ⇒ overall **INCONCLUSIVE**.
  No `plan.md` was written (persist never called); no false “completed” claim.
- **Cleanup / lifecycle:** the owned child pid (`6256`, from `child.pid`) is
  **dead** after the run; no `pi-coding-agent`/POC processes linger; no global
  `pi` was killed; only the owned parent group was terminated. The parent stayed
  alive until the 180 s harness timeout (RPC keeps stdin open), then was reaped.
- **Ledger (fresh, on-disk — authoritative):** `ledger.json` at the BUDGET ROOT
  contains **one** reservation, `poc-progress-hold`, `nonce
  imp004-poc-wirefix-20260922-v`, **`childSpawns: 1`**. The **embedded** result
  snapshot shows `childSpawns: 0` because it was serialized from the runLive
  instance opened before the extension's separate ledger instance incremented it —
  this is the **stale-snapshot known low risk**; the **on-disk** ledger is fresh
  and correct. No hash/domain confusion: run nonce `…live-20260922-y`, code
  mission nonce `…wirefix-20260922-v`.
- **Artifact note (truthfulness):** `observations.jsonl` contains one pre-reservation
  entry `order:0 host_child_kill:session_shutdown` written by the **zero-model
  load probe** (the probe loads the same wrapper/config in the same scenario dir and
  its `session_shutdown` handler appends). It did **not** affect the verdict
  (a later `child_agent_settled` supplies the terminal); recorded for honesty, not
  fixed under this “no source change” mission.
- **Interpretation:** this is a **native runtime outcome**, not a test-double
  claim: the delivered child-progress custom message did not lead the parent model
  to call `persist_plan`/`todowrite`. **AC05 remains unproven**; T05f is
  **triggered**; next action is a **user decision** per the existing failure gate.
  Scenario 2 (`poc-result-hold`) remains **unconsumed** (ledger has 1 of 2).
- **Explicitly not claimed:** no natural-throughput/performance result; this was a
  controlled, authorized run with a disclosed 75 s artificial hold, not a protected
  holdout or production benchmark.

## Pause + exact causal finding — nonce `imp004-poc-pause-20260922-aa` (docs-only)

- **Independent live review `imp004-poc-live-verify-z`: evidence PASS; actual
  behavior INCONCLUSIVE.**
- **Root-verified causal finding (static read of installed Pi `0.85.1`
  `dist/core/agent-session.js`):** `deliverAs:"nextTurn"` **pushes `pendingNextTurn`
  and IGNORES `triggerTurn:true`** (`:1099-1111`); it is drained only on the **next
  user prompt** (`:910`). The harness sends **one** prompt, so the child-progress
  message was **QUEUED but never consumed** by the parent model. Therefore the
  absence of `persist_plan`/`todowrite` is **not a model refusal** and **not a Pi
  defect** — it is a **harness wrong-delivery-mode** bug. **Correction recorded:**
  prior “delivered/forwarded” wording is corrected to **QUEUED**.
- **Potential fix (not implemented, not proven):** use `followUp`/`steer` streaming,
  or `triggerTurn` while the session is idle. No source change was made.
- **Preserved, not fixed:** (a) the pre-reservation `order:0 host_child_kill` from
  the zero-model probe (no false PASS on the current trace); (b) the embedded result
  ledger snapshot `childSpawns:0` vs **on-disk** `childSpawns:1` (stale snapshot).
- **Budget:** ledger **1 of 2** used, `childSpawns:1`, **remaining 1 UNSPENT**; no
  reset of budget artifacts, no retries. Process cleanup: **no owned process left**.
  Frozen 12 aggregate **`a5f52c83…37f3` unchanged**, production untouched.
- **Review history (stored correctly):** C1 **FAIL**, C2 **FAIL** (earlier),
  C3 **pre-live PASS** (`imp004-poc-verify-w` + `imp004-poc-care-x`), **live
  INCONCLUSIVE** (`imp004-poc-live-20260922-y`). T05b–T05d remain **prior prototype
  unit-checked**, not runtime success; current candidate **AC05 unproven**.
- **Plan state:** **paused**, active plan **none**, index state **paused** (not
  closed); **T05 blocked, T05e blocked, T05f done**, T06+ unauthorized. Per the
  explicit failure gate, **STOP and consult the user before corrective follow-up**.

## Delivery-fix correction — nonce `imp004-poc-deliveryfix-20260922-ab` (fix + non-model tests)

- **User decision:** "Corregir y probar (Recomendado)" — bounded prototype
  correction + ONE remaining live diagnostic. **This mission performed no live
  call and no model inference.** Plan resumed **active**; **T05g `in_progress`**,
  **T05e `blocked`** (history), **T05f `done`** (old failure record). T06+
  unauthorized.
- **Exact mode branch (installed `0.85.1`, `dist/core/agent-session.js`
  `sendCustomMessage`):**
  - `deliverAs:"nextTurn"` → `this._pendingNextTurnMessages.push(appMessage)`
    (`:1109-1111`) — **ignores `triggerTurn`**, drained only in `_runAgentPrompt`
    on a user message (`:910`). With one harness prompt the message is **never
    consumed** (the live INCONCLUSIVE).
  - `isStreaming && triggerTurn !== false` + `deliverAs:"followUp"` →
    `this.agent.followUp(appMessage)` — delivered at turn end as a new turn.
  - `triggerTurn` while idle → `await this._runAgentPrompt(appMessage)` — a new
    turn **immediately**.
  - **Chosen fix:** `{ deliverAs:"followUp", triggerTurn:true }` — correct in
    **both** states without an arbitrary second harness user prompt; `steer` would
    only be the streaming default. `dist/core/agent-session.js` unchanged.
- **Consumption witness:** the trusted parent extension records
  `host_parent_message_consumed` from `pi.on("turn_start")` when a queued forward
  exists; the hard gate now requires `parentConsumedProgress`, so **QUEUED is
  distinct from CONSUMED** and the `sendMessage` callback alone can never pass.
  `persist_plan`/`todowrite` remain **model-driven only**; UNTRUSTED labelling and
  the no-done-without-acceptance gate are preserved.
- **Test expectation — old vs new (characterization, `createSchedulingSession`):**
  - OLD `nextTurn` (streaming): outcome `{parked:true, mode:"nextTurn"}`; after
    `endTurn()` consumed length **0**; only `userPrompt()` yields **1**.
  - NEW `followUp` (streaming): `{parked:false, mode:"followUp"}`; `endTurn()`
    returns **1** and consumed becomes **1**.
  - NEW `followUp` (idle): `{parked:false, mode:"triggerTurnIdle"}`; consumed
    becomes **1** immediately.
  - Strict source test: the prototype contains no
    `deliverAs:"nextTurn"`; the extension uses `followUp` and derives consumption
    from `turn_start`.
- **FULL-ENTRY regression (zero-LLM fake Pi that follows scheduling):** the fake
  now CONSUMES the queued followUp (streaming: at turn end; idle: immediately) and
  only then runs `persist_plan`/`todowrite` — it no longer auto-executes tools from
  a `sendMessage` callback. Two real `runLive` invocations (`poc-progress-hold`,
  `poc-progress-recovery`) on one fresh root are **PASS**, with
  `finalChecks.ledgerChildSpawns = 1` and `consumedObserved = true`; a duplicate
  third is rejected before any spawn. **This is not real proof** (fake CLI, no LLM).
- **Anomaly closures:** probes moved to an **ephemeral probe dir**, so the
  zero-model load no longer writes into scenario data; `terminate` records
  `host_child_kill` only when an owned child exists and was signalled (test
  asserts a no-child shutdown records none); report `ledger` is re-read from disk
  after parent close (authoritative `childSpawns:1`); child stdin `error`
  fail-closes with `host_child_stdin_error`.
- **Checks (all pass):** POC **28/28**, full Pi suite **202/202**, POC typecheck,
  workspace typecheck, `generate-contract.mjs --check`, `verify-package.mjs`.
  Zero-model real-CLI load (`--preflight`, **no prompt**): parent `poc-status`,
  child `poc-report-status`, `promptSent:false`, model `openai-codex/gpt-5.5`,
  thinking `medium`; dev pin `0.84.4` unchanged.
- **Frozen candidate (12 files, docs excluded):** aggregate
  **`65fdbb547f90a626ab4f15f01ab9db55f39e66ed857d067c9dfaffb453c62c8d`**;
  supersedes `a5f52c83…37f3`. Production `packages/pi/src/**` and resources
  untouched.
- **Real budget unchanged:** the fixed budget root still holds **1 of 2**
  reservations (`poc-progress-hold`, nonce `…wirefix-v`, `childSpawns:1`), which
  remains **INCONCLUSIVE** and was **not modified/reset**; **remaining 1 UNSPENT**.
  The reserved recovery case is `poc-progress-recovery`.
- **No source AC05 PASS claim.** AC05 remains unproven pending the separate live
  RUN and fresh independent reviews.

### Final live RUN start (nonce `imp004-poc-live-final-20260922-ae`)

Manager-authorized **single remaining actual model scenario** `poc-progress-recovery`
on the **SAME** budget root, after independent verifier `imp004-poc-verify-ac`
**PASS (pre-live)** and CARE `imp004-poc-care-ad` **PASS (source)** on the frozen
candidate `65fdbb54…c62c8d` (12 files, HEAD `c4c6450`). **T05g `in_progress`.** No
source edits; no install/Git/memory/delegation. Budget cap ≤180 s, 1 parent + 1
child, existing default model/auth, no overrides, **no retries**; the original
`poc-progress-hold` reservation (nonce `…wirefix-v`, `childSpawns:1`, INCONCLUSIVE)
is preserved untouched. Preflight: aggregate re-hashed to match; no owned process.

### Final live RUN outcome — `poc-progress-recovery`: **FAIL** (nonce `imp004-poc-live-final-20260922-ae`)

Exact command: `VGXNESS_POC_LIVE=1 VGXNESS_POC_PI_CLI=/Users/uzielvgx/.nvm/versions/node/v24.15.0/bin/pi node packages/pi/test/poc-child-progress/live-scenarios.ts --run --scenario poc-progress-recovery --results-dir /private/var/folders/4q/nlfcvv816_l0bh4wsl846_kw0000gn/T/opencode/imp004-poc-live`. **No retry.**

- **Hard-gate verdict FAIL** — checks: `childProgressObserved` **pass**,
  `parentForwardedProgress` **pass**, `parentConsumedProgress` **pass**,
  `modelPersistPlan` **pass**, `modelTodoWrite` **pass**, `persistBeforeTodo`
  **pass**, **`todoBoundToParentCall` fail**, `todoMissionBound` **pass**,
  `todoChildGeneration` **pass**, `childAliveAtTodo` **pass**,
  `childNotSettledAtTodo` **pass**, `todoBodyBeforeChildTerminal` **pass**.
  Run: `reason timeout`, `elapsedMs 179942` (≤180 s), `promptCount 1`,
  `exitCode null`, `stderrBytes 0`; model `openai-codex` / `gpt-5.5` / `medium`.
- **Actual native witnesses (source-bound, bounded metadata only):** child
  `report_progress` `{planId imp004-poc, task T05, missionNonce
  imp004-poc-deliveryfix-20260922-ab, candidate c4c6450…, seq 1, eventId r1,
  counters 0/1}`; `host_parent_message_consumed [consumed:1]` (a **turn_start
  proxy** after the queue — **not** content-inclusion proof); model
  `tool_execution_start/persist_plan` then `tool_execution_start/todowrite`
  (native ids); `plan.md` = “T05: in_progress (event r1, seq 1)” written **before**
  `host_todo_body`; snapshot `{childAlive:true, childSettled:false,
  childGeneration:1, missionNonce:…deliveryfix-ab}`; `child_agent_settled` after
  the todo (owned terminal). No raw model text is stored.
- **Sole failing check:** `todoBoundToParentCall` compared the native classified
  id (`bounded(toolCallId,64)` → 64 chars) against the extension snapshot’s full
  83-char id for the **same** `todowrite` call. It is a **harness truncation
  artifact**, not a protocol violation; **not fixed** here (no source edits).
- **Budget/cleanup:** fresh ledger **2/2** (`poc-progress-hold` …wirefix-v and
  `poc-progress-recovery` …deliveryfix-ab), each `childSpawns:1`, `remaining 0`;
  original case preserved INCONCLUSIVE. Probe isolation confirmed (observations
  begin at `host_child_spawn`; no order-0 kill). No owned process lingered; no
  unrelated process killed. Post-run 12-file aggregate **unchanged**
  `65fdbb54…c62c8d`; production untouched.
- **State:** plan **paused**, active plan **none**; **T05 blocked, T05g blocked,
  T05e blocked (history), T05f done**; T06+ unauthorized. **AC05 unproven.** No
  source AC05 PASS claim; not self-accepted.

## ID-fix correction — nonce `imp004-poc-idfix-20260922-ai` (source fix + non-model checks)

- **Authorization:** user "dale sigamos" — checker ID-fix + **EXACTLY ONE NEW live
  180 s** on a **new budget** (old 2 immutable); **no** prod/install/delivery. **This
  mission ran no live call and no model inference.** Plan resumed **active**; new
  stable **T05h** `in_progress` (depends on the prior T05g outcome; a blocked
  dependency is acceptable). **T05e/T05g `blocked` historical retained; T05f
  `done`**; T06+ unauthorized.
- **Observed RED (pre-fix):** `classifyParentEvent` sliced `toolCallId` to 64 chars
  for `tool_call`, `tool_execution_start`, `tool_execution_update` and
  `tool_execution_end` (83 → 64, `equal=false`), which is why the live
  `todoBoundToParentCall` failed against the full 83-char snapshot.
- **Lossless policy (chosen, reversible):** `native-events.ts`
  `TOOL_CALL_ID_MAX_BYTES = 512` + `readToolCallId(value)`: accept a **non-empty
  UTF-8 string ≤512 BYTES**; **drop** (fail-closed) over-cap, empty, or wrong-type
  values — **never truncate and never prefix-match**. **This 512 budget is a POC
  policy, NOT a native SDK guarantee.** Applied to all four model tool id fields;
  display `bounded()` is retained only for other metadata. `parent-extension.ts`
  validates the snapshot id with the same policy (invalid ⇒ omitted, never the
  literal `"unknown"`). `live-scenarios.ts` keeps **strict equality** and treats a
  missing/invalid/over-cap snapshot id as **INCONCLUSIVE**.
- **Explicit budget:** `runLive` validates `maxScenarios` (integer 1..2, default 2)
  and passes it to `DurableLedger.open`, the wrapper config, both probes, the
  fresh-ledger read, and `finalChecks.maxScenarios`; CLI `--max-scenarios <1|2>`.
  `DurableLedger.open` rejects a **cap mismatch** on reopen (no downgrade/upgrade).
- **Tests (no live, no model):** 31 POC / **205** full suite; POC + project
  typecheck; `generate-contract --check`; `verify-package`. New: full 83 retained on
  all four events; ≤512 accept / 513 drop; multibyte byte-count; empty/wrong-type
  drop; **same-64-prefix distinct-tail ⇒ FAIL**; over-cap snapshot ⇒ INCONCLUSIVE;
  cap-1 first PASS then second **blocked before any spawn**; invalid cap rejected
  **before** the model. FULL-ENTRY fake CLI uses actual-shaped ids and asserts the
  **full native callback id** matches.
- **Candidate:** source commit `c4c6450`; new 12-file aggregate
  **`99f4d2b10f674fa701f7bda418dbdd0c58d368d61f9fba5c5b04739212d33820`**
  (distinct from the prior `65fdbb54…c62c8d`); production unchanged.
- **Budget roots:** old `…/imp004-poc-live` **read-only, 2/2 preserved, not
  modified/reset**; new `…/imp004-poc-id-live` **not created** (later Runner only).
- **No source AC05 PASS claim.** AC05 unproven; the ONE new live case waits on fresh
  reviews and a separate Runner. This mission's checks are pre-live unit/protocol
  checks, not artifact acceptance.

## T05h live RUN outcome — `poc-progress-recovery`: **PASS** (nonce `imp004-poc-id-live-20260922-al`)

Exact command: `VGXNESS_POC_LIVE=1 VGXNESS_POC_PI_CLI=/Users/uzielvgx/.nvm/versions/node/v24.15.0/bin/pi node packages/pi/test/poc-child-progress/live-scenarios.ts --run --scenario poc-progress-recovery --results-dir /private/var/folders/4q/nlfcvv816_l0bh4wsl846_kw0000gn/T/opencode/imp004-poc-id-live --max-scenarios 1`. **No source change; no retry.**

- **Gate verdict PASS — 12/12:** `childProgressObserved`, `parentForwardedProgress`,
  `parentConsumedProgress`, `modelPersistPlan`, `modelTodoWrite`, `persistBeforeTodo`,
  **`todoBoundToParentCall`**, `todoMissionBound`, `todoChildGeneration`,
  `childAliveAtTodo`, `childNotSettledAtTodo`, `todoBodyBeforeChildTerminal` — all
  **pass**.
- **Real parent/child trace (not an SDK mock):** child `report_progress`
  `{planId imp004-poc, task T05, missionNonce imp004-poc-idfix-20260922-ai,
  candidate.commit c4c6450…, seq 1, eventId r1, counters 0/1}` →
  `host_parent_message_queued [r1]` → `host_parent_message_consumed [consumed:1]`
  (a **turn_start proxy**, labelled as such) → `host_plan_persisted [in_progress]` →
  **real model** `persist_plan` (before) then `todowrite` → `host_todo_body` →
  `child_agent_settled`. `plan.md` = `T05: in_progress (event r1, seq 1)` — no child
  claim of done.
- **Exact full-id binding:** snapshot id == native classified id
  `call_oLYpK6ZpN2mEPmCKs3XDT8ZU|fc_0dbc9ed87ac83c56016ab3492aa8a487d188545516910ff3c6`,
  `EXACT_EQUAL true`, **83 chars = 83 UTF-8 bytes** (no prefix).
- **Snapshot:** `{childAlive:true, childSettled:false, childGeneration:1,
  missionNonce:imp004-poc-idfix-20260922-ai}`.
- **Metrics:** `reason timeout`, `elapsedMs 180018` (controlled ≤180 s deadline),
  `promptCount 1`, `stderrBytes 0`; model `openai-codex`/`gpt-5.5`/`medium`
  (observed via `get_state`, not hardcoded). No token/dollar figure fabricated.
- **Paths (bounded metadata only):** root
  `/private/var/folders/4q/nlfcvv816_l0bh4wsl846_kw0000gn/T/opencode/imp004-poc-id-live`;
  scenario dir `imp004-poc-idfix-20260922-ai--poc-progress-recovery/` with
  `result-poc-progress-recovery.json`, `observations.jsonl`, `plan.md`, `child.pid`,
  configs/wrappers. No auth/secret/raw-model-text persisted.
- **Budgets:** **new** root ledger cap **1** → **1/1 exhausted**, `childSpawns:1`;
  **old** root `…/imp004-poc-live` **2/2 unchanged, not reset**.
- **Cleanup:** owned child pid `55491` dead; no `pi-coding-agent`/POC process left;
  only the owned group signalled; observations begin at `host_child_spawn` (no probe
  pollution).
- **Integrity:** post-run 12-file aggregate **matches**
  `99f4d2b10f674fa701f7bda418dbdd0c58d368d61f9fba5c5b04739212d33820`; production
  `packages/pi/src/**`/resources unchanged.
- **Acceptance:** **T05h `done`** (evidence ready); **T05 `in_progress` awaiting
  independent post-live artifact verification**. **No full T05 acceptance; no
  retroactive PASS for the old 2; T06 production remains unauthorized even if the
  POC passes.** This PASS is a **controlled, authorized** run, not a protected
  holdout or performance claim; `reason timeout` is a controlled deadline, not
  natural session termination.

## Manager acceptance — T05 POC on C5 (**historical; not current acceptance/approval**) (nonce `imp004-poc-acceptance-record-20260922-an`, docs only)

- **Accepted candidate:** exact **C5** aggregate
  `99f4d2b10f674fa701f7bda418dbdd0c58d368d61f9fba5c5b04739212d33820` (see the
  **T05h ID-fix** manifest block above; the earlier wire-fix block is marked
  **HISTORICAL**), HEAD `c4c6450`, prototype 12 untracked.
- **Independent evidence:** source verifier `imp004-id-verify-aj` **PASS** + CARE
  `imp004-id-care-ak` **PASS** (same candidate), and **post-live**
  `imp004-id-liveverify-am` **PASS** — all 12 gate checks recomputed from **real**
  metadata (no raw text/auth); full **83 chars == 83 bytes** snapshot id exact, no
  prefix.
- **Observed (real RPC):** native parent model `persist_plan` orders **12–13** →
  `todowrite` orders **17–18**; `host_plan_persisted` order **5** → `host_todo_body`
  order **6**; `childAlive:true / childSettled:false / gen 1 / mission
  imp004-poc-idfix-20260922-ai`; child native `agent_settled` order **8**. Fixture
  task `in_progress`, `evidence: none`; a `result` claim is **not done**. Child hold
  **75 s ARTIFICIAL**, read-only child, **no actual feature implementation**.
- **Budgets:** one authorized new scenario (1 parent + 1 child); new root cap **1**
  → count 1, `childSpawns:1`; old root cap **2** → count 2, children 1 each. Total
  **3 scenarios across user budgets — not 3 API calls**; may be multi-turn; **cost
  unmeasured**.
- **Timing:** latest `reason TIMEOUT`; controlled 180 s timer measured **180018 ms**
  (~18 ms overhead) — **not** natural completion or a performance pass; cleanup
  **no owned processes**, verified.
- **Model:** host default `openai-codex`/`gpt-5.5`/`medium`; app `0.85.1`, dev
  `0.84.4` unchanged. `host_parent_message_consumed` is a **turn_start proxy** with
  **no independent content proof**; acceptance rests on the **actual model tools +
  exact binding**.
- **Scope/limits:** no always-works or global-provider claim; no code installed, no
  production defaults changed, no OC/Codex work, no commit/push. Tests **205** (incl.
  **31** prototype) pass; fixture tsc + generated checks are **pre-live code
  checks**, separate from live evidence; CARE is **limited to source readiness**, no
  unavailable-artifact mask. Guides `agent-eval`/`security` validated; prior
  failures/bugs recorded, **no history discarded**. All new docs/POC remain
  **uncommitted** (no git-clean claim).
- **State:** **T05 `done` (POC only); T05h `done`; T05e `done`** (abstract live gate
  via the T05h C5 trace; earlier attempts remain INCONCLUSIVE first / FAIL C4);
  **T05g `done`** (confirmed current C5; earlier result record still FAIL);
  **T05f `done`**. **T06+ pending, still unauthorized.** Plan **paused**, active
  **none**, index **paused** (not closed). **Next action:** present results and
  request the user's T06 production authorization; no additional budget now.

### Final evidence record — nonce `imp004-poc-record-final-20260922-ag` (docs only)

Independent **offline** review `imp004-poc-final-evidence-af` corroborates the
observed behavior and the recorded gate FAIL:

- **Native parent model order:** `persist_plan` at order **10**, `todowrite` at
  order **15** (plan written before projection).
- **Synchronous host todo body:** order **6**, `childAlive:true`,
  `childSettled:false`, `childGeneration:1`, correct mission nonce.
- **Child terminal after todo:** `child_agent_settled` at order **8**.
- **Plan content:** `in_progress`, event `r1`, seq 1.
- **Gate FAIL cause (result-checker bug, not a model fault):**
  `native-events.classifyParentEvent` bounds `toolCallId` to **64** chars while the
  extension snapshot holds the full **83**-char id; the **tail 19 chars are LOST in
  the raw event and cannot be recovered**. The recorded prefix is unique in the
  observed scope but is **not** exact full identity. **Do not** retrofit a PASS and
  **do not** weaken the criterion.
- **Interpretation:** the verdict is **corroborated observation**, not full T05
  acceptance; no raw event holds a reconstructed id.
- **History preserved:** the first run's INCONCLUSIVE (wrong `nextTurn` mode) is
  retained; the `followUp` correction is observed **only** in the second run.
- **Budget:** **2/2 exhausted** = exactly **2 parent + 2 child** scenarios, each a
  180 s timeout. Do **not** describe this as "4 API calls"; each scenario is
  multiple model turns, and pricing/token cost was **not measured**.
- **Candidate:** source **C4** aggregate **`65fdbb54…c62c8d`** unchanged; production
  unchanged; no install/commit.
- **Receipts:** this is **review evidence against C4**; C3 pre-live receipt does
  **not** transfer to C4 source acceptance. All **202** unit tests pass as pre-live
  checks — not artifact acceptance.
- **Safety:** all owned processes clean and verified; **no budget reset**.
- **Plan** stays **paused**, not closed; **single next action** below.

## T06 production candidate — self-reported evidence (nonce `imp004-prod-write-20260923-c`)

This section is the **writer's own record**. It is **not** independent verification;
**T07** verifier + CARE have **not** run, and **T08** install/delivery is
**unauthorized**. No live model call was made (all prior live budgets exhausted).

**Candidate source list (sha256, workspace-relative):**

| Path | sha256 |
| --- | --- |
| `packages/pi/src/workers/progress.ts` | `d8edc6ec237156feb2d39d73c5f334447fcdefb3b68dfbe1f6b94906e4da37d3` |
| `packages/pi/src/workers/background.ts` | `e616acf00a414d8e1867ebc9fb0499a021932f6f2f84e0d2263d729232243f93` |
| `packages/pi/src/workers/mutation-guard.ts` | `653793382d32b7d195503fe1ae70bc3a114a53dc204392bcb9f657f3dc82f861` |
| `packages/pi/src/tools/task-control.ts` | `a11abffbc5156aeab89b5ad0dfbd6c9726c984b89cf39e8696b5491bc83f49cb` |
| `packages/pi/src/workers/runner.ts` | `9ee7947c1c6ef0d621a0dce0c86e651c0ce911f5d12d144599c86425a750611f` |
| `packages/pi/src/workers/mission.ts` | `fadf726015166ceb9bc77112c1424bae67613141d9f007bcc89f8d7038fa9066` |
| `packages/pi/src/workers/context.ts` | `59e2604843de2a0d59dbe64aadfe961149675b53efd293106a3c0d078cb07dc1` |
| `packages/pi/src/tools/task.ts` | `b0fc9bb22d9b0ce80d771fd460e7640de648d53763e4bdde86aa538c01e73784` |
| `packages/pi/src/extension.ts` | `22b7bf01629de0ddb759dd3d94d7c87180cded9eea998ddef2629e523cf8b75e` |
| `packages/pi/src/orchestration/adapter.ts` | `70ef6c0125d3b6c6cbd4153e78f9f6be78463aa4389772118e7722845226df99` |
| `packages/pi/resources/prompts/manager.md` (regenerated) | `fb7eca63d6d61467ed2b3bf38ac8430db6115437f7910a52b0dd3b4f7e68b658` |
| `packages/pi/test/background-task.test.ts` | `e67d74ac479cb34c1de282381f1051665c6c9724726d3e086ec9f08b17da083d` |
| `packages/pi/test/background-spawn.test.ts` | `d746f92e69f8fe48056c355c1d43a3fed66c31b5d282429e8d48aca5dcf4c530` |
| `packages/pi/test/tools.test.ts` (deliberate update) | `997f9634d7af52b90016aa7b7639840f5b4c123fc14383864407b6649d143470` |

- **Shared contract:** generated `packages/pi/resources/orchestration/contract.json`
  **unchanged**; `sourceDigest` still
  `a890f5453c72f97c30174041b27d28d62d5b60c1bd357b89a754b7d5c60f76ce`. Only the
  Manager prompt was regenerated from `adapter.ts`. OpenCode/Codex bytes unchanged.

**Checks actually run (all permitted; no install/live/git):**

| Check | Command | Result |
| --- | --- | --- |
| Pi typecheck | `npm run typecheck --workspace @vgxness/pi` | pass |
| New-test strict typecheck | `tsc --noEmit … test/background-task.test.ts test/background-spawn.test.ts` | pass |
| POC typecheck | `tsc -p test/tsconfig.poc.json` | pass |
| Full Pi suite | `node ./scripts/test.mjs` | **223 pass / 0 fail** (205 pre-existing + 18 new) |
| Generator check | `node packages/pi/scripts/generate-contract.mjs --check` | pass (no drift) |
| Package verify | `node packages/pi/scripts/verify-package.mjs` | pass |
| Go (offline) | `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./internal/providers/pi ./internal/release ./internal/orchestration ./internal/e2e -count=1` | all `ok` |

**Implemented behavior (Pi only):** synchronous `task` unchanged; opt-in
`background:true` + explicit `progress {planId,taskId}` returns a session-owned
opaque handle immediately; manager-only `task_control` `status|wait|cancel|close`
with bounded wait (default ≤1000 ms) and bounded lifetime (default 5 min, clamped
1–15 min); typed progress gate; child `report_progress` only when the immutable
mission opts in; native `tool_execution_update` ingestion → parent `followUp` +
`triggerTurn:true`; sole-writer guard via the native `tool_call` hook; session
generation suppression on `session_start`/`session_shutdown`.

**Known limits / residuals (superseded by the T06h hardening section below where noted):**

1. ~~**Parent hard-crash reaping is incomplete.**~~ **Resolved in T06h:** an owned fd4
   lifetime watchdog now reaps the worker's own process group on a hard Manager crash;
   verified by a real SIGKILL test. See T06h below.
2. **Full-authority guard branch** is unit- plus extension-integration-tested (T06h).
3. Not implemented: mid-run child `needs_input` resume (cancel + fresh mission is the
   documented mechanism); any automatic `todowrite`/plan writer; `session_before_switch`
   hook (relies on `session_start`/`session_shutdown`).
4. **No live call, no AC05 production claim, no install, no Git delivery.**
5. Windows remains fail-closed (no new claim).

## T06h hardening candidate — self-reported evidence (nonce `imp004-prod-hardening-20260923-e`)

The initial T06 candidate **could not be frozen**; read-only exploration found the
F1–F9 findings. All were corrected in production source, with tests where a clear RED
reproducer existed (no TDD claim). This is the **writer's own record**, not
independent verification; **T07** has not run and **T08** install/delivery is
**unauthorized**. No live model call.

**Disposition (all required findings):**

| ID | Finding | Correction | Test |
| --- | --- | --- | --- |
| F1 | guard scanned only `+++`; mixed plan + non-plan delete passed; hunk `+++` text misread | `mutation-guard` now reuses the real production `patchTargetsFromDiff` (exported from `tools/apply_patch.ts`, same `parsePatch`, no semantics change); unparseable ⇒ fail-closed | `background-task.test.ts` F1 unit + F1/F7 extension integration (mixed plan+delete blocked; plan-only + hunk `+++` succeeds; code create/delete denied) |
| F2 | reserved-prefix normalization diverged; APFS case bypass | one canonical normalizer (separators, `.`/`..`, absolute/cross-OS reject, Unicode NFC + casefold for the reserved prefix only) shared by guard and background-full admission | `background-task.test.ts` F2 |
| F3 | authority keyed off job state only; cancel/expiry released the guard while the child could be alive; next writer could start | `authorityOutstanding` clears only on a **cleanup-confirmed** start result; `close`/eviction cannot release it; `WorkerRunner` latches `recovery_pending` and blocks later runs | `background-task.test.ts` F3 (unconfirmed hold, lingering cancel, close retained) |
| F4 | 1000 ms drain < real stop budget | `drainMs` default raised to **5000 ms** | covered through registry tests with explicit small drains |
| F5 | `session_start` only bumped a counter | `resetEpoch` cancels + drains before the new session; unconfirmed holds survive; late notifications suppressed | `background-task.test.ts` generation + resetEpoch tests |
| F6 | no parent hard-crash reaping | owned fd4 lifetime watchdog in the worker extension; detached, reads fd4 from stdin via `resume()`, kills only the worker's own group; never reuses fd3/fd5; exits on worker death | `background-watchdog.test.ts` (real SIGKILL of the simulated manager: worker gone within bound; normal stop not `recovery_pending`) |
| F7 | oversized terminal notifications silently dropped | bounded terminal **summary** always sent once (identity/state/`resultAvailable`), no raw large child text; async `sendMessage` errors caught and surfaced via `deliveryFailed`; `task_control` retrieves the full bounded result | `background-task.test.ts` F1/F7 |
| F8 | gate drained immediately; no rate control; close released holds | bounded interval coalesce/latest + visible drop count + the `maxEvents` 64 cap (`maxPending` is declared config, not read); `close` calls `gate.close()`, stale progress not forwarded; unconfirmed hold not closable | `background-task.test.ts` coalescing + closed-gate tests |
| F9 | docs | `docs/pi-typescript.md` documents the opt-in API, defaults/limits, read-only child report, reserved parent plans, lifecycle, and Windows; framework not live-proven | this file |

**Checks actually run (all permitted; no install/live/git/memory):**

| Check | Command | Result |
| --- | --- | --- |
| Pi typecheck | `npm run typecheck --workspace @vgxness/pi` | pass |
| New-test strict typecheck | `tsc --noEmit … background-task / background-spawn / background-watchdog` | pass |
| POC typecheck | `tsc -p test/tsconfig.poc.json` | pass |
| Full Pi suite | `node ./scripts/test.mjs` | **226 pass / 0 fail** (205 pre-existing + 21 new) |
| Generator check | `generate-contract.mjs --check` | pass; `sourceDigest` **`a890…76ce`** unchanged |
| Package verify | `verify-package.mjs` | pass |
| Go (offline) | `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./internal/providers/pi ./internal/release ./internal/orchestration ./internal/e2e -count=1` | all `ok` |

**Historical candidate manifest at time of attempt (source now removed; not current approval) — freeze reference:**

| Path | sha256 |
| --- | --- |
| `packages/pi/src/workers/progress.ts` | `d8edc6ec237156feb2d39d73c5f334447fcdefb3b68dfbe1f6b94906e4da37d3` |
| `packages/pi/src/workers/background.ts` | `859c6c36ca3b80930b94d3d8e9231e25983d488d8c21d09e2156d639863f6426` |
| `packages/pi/src/workers/mutation-guard.ts` | `08bab25f5ca2b2300a6fa51c2f94b65621fe88fe6c7928ff27ac13737f30ff31` |
| `packages/pi/src/tools/task-control.ts` | `bc098f0bfbd2b4c3bb88c5c4424504ba53460e4a76a3c4e17e144f380770ff99` |
| `packages/pi/src/tools/apply_patch.ts` | `a2744767798ec3088d5e4bcc0f59ea23adec46825d775d51ebbdded6bdd18138` |
| `packages/pi/src/tools/task.ts` | `01df00d9d6b325b460d79faafacc20fa2f61b7a5243ea6c4b9ace300ae64281f` |
| `packages/pi/src/workers/runner.ts` | `1fa8f6d2ec29399968227197db4625090df5a1dc8771c24b7687b7ff369991cb` |
| `packages/pi/src/workers/mission.ts` | `fadf726015166ceb9bc77112c1424bae67613141d9f007bcc89f8d7038fa9066` |
| `packages/pi/src/workers/context.ts` | `59e2604843de2a0d59dbe64aadfe961149675b53efd293106a3c0d078cb07dc1` |
| `packages/pi/src/extension.ts` | `ff9f9d7bfe861214cd1c6fde43d47e4e3358b1dfb61f2b1f17481aa4fdb77a5a` |
| `packages/pi/src/orchestration/adapter.ts` | `70ef6c0125d3b6c6cbd4153e78f9f6be78463aa4389772118e7722845226df99` |
| `packages/pi/resources/prompts/manager.md` | `fb7eca63d6d61467ed2b3bf38ac8430db6115437f7910a52b0dd3b4f7e68b658` |
| `packages/pi/test/background-task.test.ts` | `43c49b1de958b7a024d33da8f9a27d9a2228198ff9423289d6ecabd45a278dd3` |
| `packages/pi/test/background-spawn.test.ts` | `d746f92e69f8fe48056c355c1d43a3fed66c31b5d282429e8d48aca5dcf4c530` |
| `packages/pi/test/background-watchdog.test.ts` | `f0a6014063e8c06c6c9fe7c7121238025d17844a704eb3511e7b9a43db8661ab` |
| `packages/pi/test/tools.test.ts` | `997f9634d7af52b90016aa7b7639840f5b4c123fc14383864407b6649d143470` |
| `docs/pi-typescript.md` | `1b0cfaaa409a010f10f06284c380effb0f51d2859ba647ae4649c7dedad6c49c` |

**Manifest digest (sha256 over the sorted `path␠␠sha256\n` rows above):
`b550b135fe2b018020d9b3540003fbe64bf935338878a4e2947cb93e7aa928d9`.**

**Residuals after T06h (not silently cut):** mid-run child `needs_input` resume is
still unimplemented (cancel + fresh mission is the documented mechanism); no
automatic `todowrite`/plan writer; `session_before_switch` is not wired (relies on
`session_start`/`session_shutdown`); `WorkerRunner` recovery latching blocks later
runs until a controlled process restart (no hidden state reset); no live model or
live production run; Windows remains fail-closed. **Not ready for acceptance until
T07 independent verifier + CARE run on this exact manifest.**

## T06i — correction of three independent review FAILs (nonce `imp004-prod-corrections-20260923-i`)

**Prior FAILs preserved as history (not overwritten):** T06h manifest `b550b135…`
was reviewed as a **listed-order** row set, which does not match the required sorted
schema. All three reviews FAILed and are retained above.

**Disposition of required findings (all implemented with tests):**

| ID | Fix | Test |
| --- | --- | --- |
| VERIFIER-F1 HIGH | `WorkerRecoveryPendingError` (typed, `code`/`recoveryPending`); shared `WorkerRunner` latch consulted by the extension guard even without a new background job; `task`/`task_control` share the extension's single runner instance (no new instance on session reset) | `VERIFIER-F1/F2` runner latch, `VERIFIER-F1` extension pin |
| VERIFIER-F2 | `failureTerminal` preserves only a truthful `recovery_pending` and `recovery.pending`; arbitrary spoofed `previous.reasonCode` is not propagated; cleanup is `cleanupConfirmed` only from a trusted transport return or a never-launched queue case | `VERIFIER-F2` + existing spoof-guard test |
| CARE-A HIGH | scheduled bounded periodic flush of the LATEST event, one notification per interval, timers cleared on cancel/close/settle/epoch, `flood`/`backpressure` counted as `dropped`, `maxEvents` cap retained (64) — `maxPending` is declared config, **not read** | `CARE-A` burst/resume + cross-session timer |
| CARE-B MEDIUM | notify may return a thenable; registry catches sync + async rejection and records `deliveryFailed`; no swallow, no unhandled promise | `CARE-B` extension async rejection |
| CARE-C/D LOW | old-generation handles externally `unavailable` for `status/wait/cancel/close`; internal `cancelAll` iterates directly and drains concurrently; `wait` revalidates the epoch; close reasons `job_running` / `cleanup_unconfirmed` / `closed` | epoch-isolation + `task_control` close-reason tests |
| CARE-E TOCTOU | `host.mutationTargetsGuard` rechecks the actual parsed targets at admission and before each commit; parent code patches blocked; plan-only patches still allowed; **no OS-sandbox claim** (residual post-check window documented) | `CARE-E` execution-time guard + extension F1/F7 |
| SPECIALIST-F1 mandatory | fail-closed watchdog: awaited bounded READY handshake on a private stdout pipe before exposing writable/command tools; invalid fd / spawn error / timeout ⇒ worker `unavailable`; production worker always passes fd4; inventory callers may omit it explicitly | `SPECIALIST-F1` invalid-fd + READY-timeout + no-tool-exposed + hard-kill/normal-stop |
| Schema docs | `task` `commands`/`targets` **original descriptions restored**; only minimal `background`/`progress` additions | schema-description test |

**Checks actually run (all permitted; no live/model/install/git/memory):**

| Check | Result |
| --- | --- |
| `npm run typecheck --workspace @vgxness/pi` | pass |
| strict `tsc --noEmit` on the three candidate test files | pass |
| `tsc -p test/tsconfig.poc.json` | pass |
| `node ./scripts/test.mjs` | **233 pass / 0 fail** (205 pre-existing + 28 new) |
| `generate-contract.mjs --check` | pass; `sourceDigest` a890 unchanged |
| `verify-package.mjs` | pass |
| offline Go providers/pi, release, orchestration, e2e | all `ok` |

**Historical manifest — EXACT schema (source now removed; not current approval) (repo-relative full paths, sorted lexicographically, rows `path␠␠sha256\n`, LF):**

```
docs/pi-typescript.md  41cd8844dff4530bc5f0f1c50ec16b4de9622d3e0814aa2a66dd539bc0e3455f
packages/pi/resources/prompts/manager.md  fb7eca63d6d61467ed2b3bf38ac8430db6115437f7910a52b0dd3b4f7e68b658
packages/pi/src/extension.ts  81735a040f12e2ddbae46228ea368b70560905979cfec6b980b6394d4cdd0591
packages/pi/src/orchestration/adapter.ts  70ef6c0125d3b6c6cbd4153e78f9f6be78463aa4389772118e7722845226df99
packages/pi/src/tools/apply_patch.ts  31f50afa868694a2a666791f1d991048ca8c8e1eb57cd8e5ed3d77d4436c6886
packages/pi/src/tools/memory.ts  e8fd235c9f2a9c91ceb13079bbd683f021f072132dca70c9c872e66db9b49dff
packages/pi/src/tools/task-control.ts  43c57d1edeaab7aa21bd575cb05a840b2b23ad262738d431f29a3f5cbf4983a1
packages/pi/src/tools/task.ts  d9a11a8291ba22fb6dc83b19d92046850a46da75df267f72c760cf92d8aef37f
packages/pi/src/workers/background.ts  1264e6361a05df0bf94f3e345dc028514367da667f65f98caa5fb19155cd204b
packages/pi/src/workers/context.ts  59e2604843de2a0d59dbe64aadfe961149675b53efd293106a3c0d078cb07dc1
packages/pi/src/workers/mission.ts  fadf726015166ceb9bc77112c1424bae67613141d9f007bcc89f8d7038fa9066
packages/pi/src/workers/mutation-guard.ts  08bab25f5ca2b2300a6fa51c2f94b65621fe88fe6c7928ff27ac13737f30ff31
packages/pi/src/workers/progress.ts  d8edc6ec237156feb2d39d73c5f334447fcdefb3b68dfbe1f6b94906e4da37d3
packages/pi/src/workers/runner.ts  8b8412f0fffcac0e78db02fe0c9980ef0127779ad98ed33f29da0999895dfcfb
packages/pi/test/background-spawn.test.ts  d746f92e69f8fe48056c355c1d43a3fed66c31b5d282429e8d48aca5dcf4c530
packages/pi/test/background-task.test.ts  e21f65718bb0404e11dcb217c1404c121b803bed86c2bbf01586043a15317292
packages/pi/test/background-watchdog.test.ts  f334b4008f82e1085c3b60b740c1bfe84e2987a1db64b8dc95ff7089e7dc5bd8
packages/pi/test/tools.test.ts  997f9634d7af52b90016aa7b7639840f5b4c123fc14383864407b6649d143470
```

**Sorted manifest digest (sha256 over the rows above):
`cc9be0a16caa7facccabed936f30c189c0772cc0e4030bf5c04fc370c329dcec`** (18 rows).

**Residuals after T06i:** mid-run child `needs_input` resume unimplemented (cancel +
fresh mission); no automatic `todowrite`; `session_before_switch` not wired; the
execution-time guard cannot close the residual window between the final recheck and
an already-dispatched filesystem operation (no OS sandbox); `WorkerRunner` recovery
latching clears only on a controlled process restart; no live model or live
production run; Windows fail-closed. **Not ready for acceptance until T07
independent verifier + CARE run on the `cc9be0a1…` candidate.**

## T06j — in-process parent/child writer serialization (nonce `imp004-prod-writerlock-20260923-j`)

Closes the acknowledged **internal** CARE-E race for the **managed** tool path. This
is **not** an OS sandbox: externally invoked commands/processes and native tools
dispatched outside the managed `apply_patch` remain outside this in-process queue
(that limitation stays).

**Mechanism.** `WorkerRunner` gains `runExclusiveMutation(work, signal?)` — a
`kind:"parent"` writer request that shares the exact `#activeWriters` slot with
child full-worker `run`s and does **not** fabricate a `WorkerMission`. A parent code
patch reserves the slot for its entire commit/rollback span (before the first write,
released in `finally`); a queued child full launch cannot start until it releases;
a child writer that already owns the slot makes `runExclusiveMutation` reject with
typed `ParentMutationConflictError` (deny, not hang); two parent mutations serialize;
a recovery latch rejects parent mutations and child runs. `apply_patch` calls
`host.runPatchMutation(actualParsedTargets, work)` only for the parent path (the
worker/child path never does, avoiding a nested deadlock); the extension routes
plan-only targets directly (allowed while a child is active) and all other targets
through `runExclusiveMutation`. The native `tool_call` hook remains best-effort.

**Tests (`packages/pi/test/background-writerlock.test.ts`, production
`createApplyPatchTool` + shared `WorkerRunner`/`BackgroundRegistry`):** parent code
patch paused after staging holds the slot so a registered full child callback does
not run until it commits; child-active denies a parent code patch and leaves files
unchanged; plan-only patch proceeds while child active; two concurrent parent code
patches serialize; a failing parent patch rolls back and releases the slot so the
child then runs; a recovery latch inhibits parent code + child runs while still
allowing a plan patch.

**Checks:** full suite **239 pass / 0 fail** (205 pre-existing + 34 new);
Pi typecheck; strict typecheck of the four candidate test files; POC typecheck;
`generate-contract --check` pass (`a890…` unchanged); `verify-package` pass; offline
Go `providers/pi` + `release` `ok`. No live call.

**Historical manifest — EXACT schema (source now removed; not current approval) (sorted lexicographically, `path␠␠sha256\n`):**

```
docs/pi-typescript.md  c933e558a8a5a49490ebb767ab9d0c7bf07f772fc8353ad7876beedbf01c9427
packages/pi/resources/prompts/manager.md  fb7eca63d6d61467ed2b3bf38ac8430db6115437f7910a52b0dd3b4f7e68b658
packages/pi/src/extension.ts  8cb955f107cdb1131782ffd782cf716f47531eba2a8c572e278cd6d0a174c916
packages/pi/src/orchestration/adapter.ts  70ef6c0125d3b6c6cbd4153e78f9f6be78463aa4389772118e7722845226df99
packages/pi/src/tools/apply_patch.ts  4dae02cf4fbb220b32d179bd74d68ab3920807a37adc8b1132529b9e482b2157
packages/pi/src/tools/memory.ts  20ee92af13f33f0207c8d299824c0e34259367412351c1614033abd039ca2cbf
packages/pi/src/tools/task-control.ts  43c57d1edeaab7aa21bd575cb05a840b2b23ad262738d431f29a3f5cbf4983a1
packages/pi/src/tools/task.ts  d9a11a8291ba22fb6dc83b19d92046850a46da75df267f72c760cf92d8aef37f
packages/pi/src/workers/background.ts  1264e6361a05df0bf94f3e345dc028514367da667f65f98caa5fb19155cd204b
packages/pi/src/workers/context.ts  59e2604843de2a0d59dbe64aadfe961149675b53efd293106a3c0d078cb07dc1
packages/pi/src/workers/mission.ts  fadf726015166ceb9bc77112c1424bae67613141d9f007bcc89f8d7038fa9066
packages/pi/src/workers/mutation-guard.ts  08bab25f5ca2b2300a6fa51c2f94b65621fe88fe6c7928ff27ac13737f30ff31
packages/pi/src/workers/progress.ts  d8edc6ec237156feb2d39d73c5f334447fcdefb3b68dfbe1f6b94906e4da37d3
packages/pi/src/workers/runner.ts  6a2db738570c1f5972e8cf72ece6d002dedee1c896df1ffa3e00d3d9d9120335
packages/pi/test/background-spawn.test.ts  d746f92e69f8fe48056c355c1d43a3fed66c31b5d282429e8d48aca5dcf4c530
packages/pi/test/background-task.test.ts  e21f65718bb0404e11dcb217c1404c121b803bed86c2bbf01586043a15317292
packages/pi/test/background-watchdog.test.ts  f334b4008f82e1085c3b60b740c1bfe84e2987a1db64b8dc95ff7089e7dc5bd8
packages/pi/test/background-writerlock.test.ts  7a4529b817bb75d4062b769fc083e7345d11ab08384e0b37093e938d3830d013
packages/pi/test/tools.test.ts  997f9634d7af52b90016aa7b7639840f5b4c123fc14383864407b6649d143470
```

**Sorted manifest digest: `0d792e13775ff8afd6ecb7646cd877bbda837e41956f51f7049bd51cb273483b`**
(19 rows).

**Residuals after T06j:** the managed parent↔child race is closed for the in-process
tool path; **externally invoked commands/processes and native tools outside the
managed path are not covered** (not an OS sandbox). Mid-run `needs_input` resume,
automatic `todowrite`, `session_before_switch`, controlled-restart-only latch clear,
no live model/production run, and Windows fail-closed remain. **Not ready for
acceptance until T07 verifier + CARE on `0d792e13…`.**

## T06n — Manager source acceptance, pre-live (nonce `imp004-prod-prelive-record-20260923-n`, docs only)

The Manager **accepted the T06 source candidate** on the **exact sorted 19-row
manifest** `0d792e13775ff8afd6ecb7646cd877bbda837e41956f51f7049bd51cb273483b`
(HEAD `c4c6450` + working diff) based on independent evidence:

- verifier `imp004-prod-verify-k` **PASS** — 239 tests, Go, typecheck, **same exact
  sorted manifest**;
- CARE `imp004-prod-care-l` **PASS** — source review, prior F closed;
- specialist `imp004-prod-specialist-m` **PASS** — the earlier security claim was
  **corrected**: fd5 is a **known regular revocation file** with explicit
  revocation/expiry, **not** an fd4-EOF channel.

**Scope/state:** T06 source criteria are **done (source-checked)**; **T07 is
`in_progress`** (tests + reviews done; the **optional** live run is pending budget);
**T08 install/delivery is NOT authorized**. Plan **paused** (not closed); active
plan **none**.

**Limitations (explicit):** **live PRODUCTION is not observed**; the prior **T05 POC**
proof is **separate** and does not transfer to production. **All prior live budgets
are exhausted; no borrowing.**

**Non-blocking CARE residue (bookkeeping correction):** the wired rate control is a
**bounded interval coalesce/latest** plus the **`maxEvents` 64** cap; **`maxPending`
is declared configuration that is not read** — no claim that it is wired or that a
`maxPending` bound is enforced. **No code fix in this pass.** The native `tool_call`
hook **outside** the managed `apply_patch` path is **best-effort, not a sandbox**;
the held **managed code queue** is the guarantee and is covered by the 6
`background-writerlock` tests.

**Identity:** source SHA unchanged `0d792e13…`; HEAD `c4c6450` + diff; prior `c7ee…`
and older doc-only hashes are history. Test count **239** (**205** baseline + **34**
new). Guides `security-boundary`/`agent-evaluation` validated; no new skill load
needed.

**Next action:** authorize **ONE** isolated live **production** run — 1 parent + 1
**read-only** child, **≤180 s**, existing model/credentials — with **no** source
installation. The test must guard scope (parent model tools fixed, no other-model
experiments, production pipeline rather than a POC-only replay). **Not two runs; no
model override.**

## T07 — live production harness build (nonce `imp004-prod-liveprep-20260923-o`; NO model call)

**Source frozen:** the 19 production files are byte-unchanged;
`SOURCE19 = 0d792e13775ff8afd6ecb7646cd877bbda837e41956f51f7049bd51cb273483b` (before
and after this pass). The unused `maxPending` config is acknowledged as a docs-only
residual; **no code fix now**.

**Budget:** durable production-live root
`/private/var/folders/4q/nlfcvv816_l0bh4wsl846_kw0000gn/T/opencode/imp004-production-live`,
cap **1**; current usage **0 of 1**. Old POC roots (`…/imp004-poc-live` 2/2,
`…/imp004-poc-id-live` 1/1) are untouched and exhausted, no reuse.

**Harness (`packages/pi/test/production-background-live/`, separate manifest):** a
durable budget ledger; synthetic fixture (`note.txt` + canonical
`docs/implementations` README/plan/progress with T1 pending / T2 report pending); a
prototype parent guard (fixed allowlist; exactly one read-only background child with
immutable scope/report binding; parent patches confined to the fixture plan prefix);
a single deterministic parent prompt + child goal; native-event evidence extraction
(reusing the POC pure id parser, **no POC execution**); a native parent RPC driver;
a trusted wrapper that imports the **actual production `createPiExtension`** (no POC
parent extension, no injected `executeWorker`); and a gated `runProductionLive` that
**throws unless `VGXNESS_PROD_LIVE=1`** and reserves the budget before any prompt.

**Checks run:** new tests `production-live-harness.test.ts` (7) +
`production-live-fullentry.test.ts` (3; the opt-in real-CLI zero-model preflight is
**skipped** unless `VGXNESS_PROD_PREFLIGHT=1`); strict typecheck of both; full suite
**249 tests / 248 pass / 0 fail / 1 skipped**. The mocked native-RPC full-entry test
proves the **actual production extension loads through the wrapper and registers
`task`/`task_control`/`model_resolve`/`apply_patch`/`todowrite`** and no `bash` — with
**zero model calls**.

**Harness manifest (separate, sorted):**

```
packages/pi/test/production-background-live/budget.ts  6aeecd4f3f3a50114e2cba44bad27a4ddb479763329fc6c4d07b0963aad718a9
packages/pi/test/production-background-live/evidence.ts  7b3679bdfd135a4fc81e701c4679fd85e2506297197c6d11ff569ddae6290a5f
packages/pi/test/production-background-live/fixture.ts  2f4d2175cb2520324cf7aa0133e3633535bae30d0a7ae313ee1bba31af3d3ece
packages/pi/test/production-background-live/guard.ts  a0f7c19dd66b751bb6364ff62dc1f2f1d3453332502d9aed50262c4b14b24cad
packages/pi/test/production-background-live/prompt.ts  9daca2c60624586af5e876cb2f5a2fb7eaf75cbc740c1ddf220878005d94611e
packages/pi/test/production-background-live/rpc.ts  26fc94cbe8bb84e803bad5a4aa2b564b93a5e5ced7efb7543f0613dac5e5ad42
packages/pi/test/production-background-live/scenario.ts  9456ca300a25f605d6164e561ae13381ccd42281e7a85b79639810abcb3747b2
packages/pi/test/production-background-live/wrapper.ts  a206c206e9b951281c8faab9770a40e4ed95eee24eae691a222c62c9d2b4f45f
packages/pi/test/production-live-fullentry.test.ts  7a853d3c7ddf15f86d4553ac811e57cb759746e8385c7f6701381b32db9fbeb5
packages/pi/test/production-live-harness.test.ts  f1eb24ed7ade08aa63f2b373ef1557dcd1cacd3ba3890383b4ce3fb2201b6627
```

**Harness digest: `ceba7b540115a252a71e2eaf0fdeffdb06e5cd70fe7cb1c0e5417e9354c0d228`**
(10 rows).

**Limits / pending:** **no live model call was made**; the real full path
(model resolution, native `sendMessage` delivery, parent `apply_patch`/`todowrite`
before child terminal, `task_control` retrieval) is **pending the RUN**. The opt-in
real-CLI **zero-model** preflight was skipped in this pass. Child health check is a
disclosed 20 s artificial read-only command with no performance claim. Cost unknown
unless actual SDK usage is observed. **T08 installation not authorized.**

## T07b — harness fix for the prior `ceba` FAIL (nonce `imp004-prod-liveharness-fix-20260923-r`; NO model call)

Fixes applied (harness/docs only; `SOURCE19=0d792e13…` byte-unchanged):

- **F1 get_commands shape.** `ParentRpc.preflight` now unwraps
  `data.commands` as **command metadata only** and never treats it as a tool
  inventory. Tool inventory is recorded **truthfully** by the trusted wrapper via a
  `registerTool` interceptor + native `getActiveTools`/`getAllTools` when present,
  written to an isolated `startup.json` **only after the production factory completes**
  (`factoryComplete:true`). No fake `ext_ready` stdout marker.
- **F1/version.** The live/preflight target is bound to the installed
  `/Users/uzielvgx/.nvm/versions/node/v24.15.0/bin/pi` and must report **0.85.1**
  (`assertInstalledCli`); the repo-local dev SDK 0.84.4 is not silently used.
- **H1 explicit gate** (`gate.ts`): production handle + plan/task/nonce binding; ≥1
  **progress** notification (not terminal); `apply_patch` **before** `todowrite` with
  full native ids; fixture plan pending→in_progress **before** projection; observer
  `task_control` **running** at projection; owned child alive; terminal envelope
  candidate commit+snapshot + nonce. Missing ⇒ **INCONCLUSIVE**, present-but-false ⇒
  **FAIL**; terminal-only can never PASS.
- **H2 guard**: exactly one read-only `care-reviewer` child; targets exactly the note
  hash; **no** exploration roots/skills; candidate **commit AND snapshot**; commands
  exactly the single 20 s health argv; wrapper binds original `pi` methods.
- **H3 budget**: per-slot `wx` lock makes check-and-commit atomic (concurrent
  reservations cap at 1); malformed/partial reservations hold capacity fail-closed.

**Checks:** full suite **253 tests / 252 pass / 0 fail / 1 skipped**
(`VGXNESS_PROD_PREFLIGHT=1` opt-in real-CLI zero-model preflight is skipped by
default); strict typecheck of both harness tests; `generate-contract --check` PASS;
`verify-package` PASS. The mocked native-RPC full-entry test proves the actual
production extension loads and registers `task`/`task_control` while `get_commands`
returns a **commands** array.

**Harness manifest (separate, 11 rows):**

```
packages/pi/test/production-background-live/budget.ts  76d3e6a8c507fa7fedbbf6ee30322dd03708a4f9e59701a2d5b50944ac51ab4b
packages/pi/test/production-background-live/evidence.ts  7b3679bdfd135a4fc81e701c4679fd85e2506297197c6d11ff569ddae6290a5f
packages/pi/test/production-background-live/fixture.ts  2f4d2175cb2520324cf7aa0133e3633535bae30d0a7ae313ee1bba31af3d3ece
packages/pi/test/production-background-live/gate.ts  96f46900e77b51b967d6ca64bb2c09bccc9fc4e87745cf11972947d3c87f5818
packages/pi/test/production-background-live/guard.ts  a4e2d2644b5a308f82c817002242a0affe6f6021b1c10cc252cb281edde93603
packages/pi/test/production-background-live/prompt.ts  9daca2c60624586af5e876cb2f5a2fb7eaf75cbc740c1ddf220878005d94611e
packages/pi/test/production-background-live/rpc.ts  ba126f7823795d756743accf89fe57cb0f8e9d24073e9c6daa2aba1ad0b01f50
packages/pi/test/production-background-live/scenario.ts  7174d60bf2a11675f0aa69d2b519e678143e47330e19785b666494165cc043ca
packages/pi/test/production-background-live/wrapper.ts  0a69ddbe5e8bd6d1f0fd4cdfab718ca85ba6c2bf737869f5ac8ff6128a09efa3
packages/pi/test/production-live-fullentry.test.ts  cb5483acadc9d59ccd98906641129c2b40ff0e9783d601ac633c2eab42b43651
packages/pi/test/production-live-harness.test.ts  82e728e136415ecb84ed4284775a2944506c9dd1d72a55f69dbb28491b6ac397
```

**Harness digest: `00187172a663ae01636cf2e8db86581f407d64a647c846aa7778d43c2d594a00`.**

**RUN entry (after fresh reviews; not executed here):**
`VGXNESS_PROD_LIVE=1 node --experimental-strip-types packages/pi/test/production-background-live/scenario.ts --run`
— 1 parent + 1 read-only child, ≤180 s, bound installed 0.85.1, existing model/auth,
no install. Budget **0 of 1**; old POC roots untouched. Real full-path model proof
remains **pending the RUN**.


## T07 wiring candidate — actual-observation gate (nonce `imp004-prod-livewire-20260923-u`)

Self-reported writer evidence. **No live model call was made** and **no
`VGXNESS_PROD_LIVE` run was performed with a real driver**; the production budget
root stays **0 of 1**. This is **T07 active preparation, NOT a PASS**. The earlier
`ceba`/`00187172…` FAIL history above is retained unchanged.

**What changed (source call path):** the gate is now fed by **actual** production
observations, not a hand-fed object. `createProductionWrapper` (imported by the
generated live module and by the tests) wraps the **actually registered** production
tools and, at the parent `todowrite`, reads the **actual** `task_control` status and a
numeric-only `ps` descendant snapshot; it records bounded JSONL to
`storageRoot/observations.jsonl` including the real terminal envelope. The plan
snapshot is taken immediately before the real `todowrite` and only after a real
`apply_patch` success, parsed from the exact `T1` row only. Notification `kind` is
derived from the real production payload shape (`reportType`/`state`), not a field
the sender does not emit.

**Checks actually run:**

- Full Pi suite (`npm --prefix packages/pi run test`): **259 tests, 258 pass, 0 fail,
  1 skipped** (the opt-in preflight).
- Strict `tsc` over both harness tests and all `production-background-live/*.ts`:
  **clean**.
- `npm --prefix packages/pi run build` (`verify-package`): **PASS**.
- REAL zero-model preflight
  `VGXNESS_PROD_PREFLIGHT=1 node --test packages/pi/test/production-live-fullentry.test.ts`:
  **7/7 pass** — native `get_state`/`get_commands` only, **zero prompts**.
- Source19 re-hashed: **19 rows, 0 drift**; digest recomputed
  **`0d792e13775ff8afd6ecb7646cd877bbda837e41956f51f7049bd51cb273483b`** (recipe:
  sorted `path  sha256` rows joined with `\n` + trailing newline).

**Harness manifest (separate, 12 rows):**

```
packages/pi/test/production-background-live/budget.ts  76d3e6a8c507fa7fedbbf6ee30322dd03708a4f9e59701a2d5b50944ac51ab4b
packages/pi/test/production-background-live/evidence.ts  9bbd4a5720d9b62a60e38c6eda841b1e21bcdd36aa3ed5d3d11d45d3da28b221
packages/pi/test/production-background-live/fake-driver.ts  51821ea7f0eb4668aa5be7755a1453361d86645e29f84dd99a6ad59202b90c8b
packages/pi/test/production-background-live/fixture.ts  d145636a2f4b919cc90a2bf259f81d6c171d51b2d4e9d4ec8664ea5639b568b5
packages/pi/test/production-background-live/gate.ts  95d6c0c556b2975915d5a23ab7af85cab712c04fbe44a4fa958c7683405f5ed2
packages/pi/test/production-background-live/guard.ts  a4e2d2644b5a308f82c817002242a0affe6f6021b1c10cc252cb281edde93603
packages/pi/test/production-background-live/prompt.ts  9daca2c60624586af5e876cb2f5a2fb7eaf75cbc740c1ddf220878005d94611e
packages/pi/test/production-background-live/rpc.ts  ba126f7823795d756743accf89fe57cb0f8e9d24073e9c6daa2aba1ad0b01f50
packages/pi/test/production-background-live/scenario.ts  82cbdd051ae5370afc6c78e51c53e23ba27baa3a0200c1ea9c9be3a811ee7e5a
packages/pi/test/production-background-live/wrapper.ts  9ad11a69e68e945a4d79cf0ed78efdeec38f10dbc46181c3a07e16c1ac8f1264
packages/pi/test/production-live-fullentry.test.ts  42984fd61fce26f46336c4bee8858b3cc9ec2ad9dfb094ba1aeef5c44eb54c84
packages/pi/test/production-live-harness.test.ts  33f0f68389c5c121e25c1d3f0392ecf2fc329695fa150218103dcd84851467dd
```

**Harness digest: `91a035f68244c4895fcb46844343f3f1c91e049547fd2ece6c1319c7dbae3efe`.**

**Limits (honest):** the full-entry test uses test doubles for the worker transport
(the real `executePiWorker` against a zero-LLM fake child CLI), the session backend
stub, and the OS-snapshot helper. The production layers exercised are the real
`BackgroundRegistry`, `task` handle, `apply_patch`, `todowrite`, the real
`task_control` status/wait decode and terminal envelope, and the wrapper observers.
The full-entry test is **not** live-model proof, and the RUN has not been executed.
**No live production observation exists yet.** T08 install/delivery unauthorized.

## T07 pidfix candidate — owned-worker discriminator + source notification provenance (nonce `imp004-prod-livepidfix-20260923-x`)

Self-reported writer evidence. **No live model call was made**; the production
budget root stays **0 of 1**. **T07 active preparation, NOT a PASS.** The prior
`91a035f6…` wiring-candidate section above is retained as history; **no review of
this pidfix candidate is recorded yet.**

**H1 residual fixed:** the previous `descendants()` OS observer counted the `ps`
helper used to take the snapshot, so `childAliveAtTodo` was always `true` and a
detached worker could be silently missed. `ownedWorker()` now selects only a
**direct child of the parent Pi pid** that is detached (`pgid === pid`) and whose
executable basename is the node binary, using `spawnSync("ps", ["-axo",
"pid=,ppid=,pgid=,comm="])` — **numeric ids + executable name only**, excluding the
snapshot helper's own pid and the parent root pid; no argv, environment, or raw
process dump is read or persisted. `0 ⇒ false`, `1 ⇒ alive` (with the matched
`ownedWorkerPid`), `>1 ⇒ ambiguous ⇒ INCONCLUSIVE`.

**Notification provenance:** the wrapper's existing real `pi.sendMessage` HOST
records are wired into the notification witnesses with provenance
`production_sendMessage_attempt` (vs `native_event`), deduped by handle+seq/eventId,
with the wrapper `obsSeq` kept distinct from the production event `seq`. A gate
check requires a source progress event with the expected mission identity whose
`obsSeq` precedes the real `apply_patch` start. This is an authentic **producer
invocation**, **not** proof that the model consumed the content; the actual parent
MODEL tool witnesses remain mandatory. No synthetic notification injection.

**Checks actually run:**

- Full Pi suite (`npm --prefix packages/pi run test`): **263 tests, 262 pass, 0 fail,
  1 skipped** (opt-in preflight).
- Strict `tsc` over both harness tests + all `production-background-live/*.ts`: clean.
- REAL zero-model preflight
  `VGXNESS_PROD_PREFLIGHT=1 node --test packages/pi/test/production-live-fullentry.test.ts`:
  **7/7 pass** — native `get_state`/`get_commands` only, zero prompts.
- Full-entry fake cases: PASS; negatives `terminal-only`, `no-todo`, `no-terminal`
  not PASS; `failed-patch` FAIL; `helper-only` snapshot FAIL; `late-notification` FAIL.
- Source19 re-hashed: **19 rows, 0 drift**, digest
  `0d792e13775ff8afd6ecb7646cd877bbda837e41956f51f7049bd51cb273483b`. Production budget
  root **0 of 1** (only `cap.json`).

**Harness manifest (separate, 12 rows):**

```
packages/pi/test/production-background-live/budget.ts  76d3e6a8c507fa7fedbbf6ee30322dd03708a4f9e59701a2d5b50944ac51ab4b
packages/pi/test/production-background-live/evidence.ts  856fc48a82b4a417774cd20dc86eabcbd8c0b042bf64067b3af791ff15c82a11
packages/pi/test/production-background-live/fake-driver.ts  e3a002309066a724b6692537755a9fcddc3001d9863c10319788f7cff7c8306e
packages/pi/test/production-background-live/fixture.ts  d145636a2f4b919cc90a2bf259f81d6c171d51b2d4e9d4ec8664ea5639b568b5
packages/pi/test/production-background-live/gate.ts  c116aaa7447e31e267a2dffd4cc30fb8ae4829a6b773a8695abc73747f90d4ac
packages/pi/test/production-background-live/guard.ts  a4e2d2644b5a308f82c817002242a0affe6f6021b1c10cc252cb281edde93603
packages/pi/test/production-background-live/prompt.ts  9daca2c60624586af5e876cb2f5a2fb7eaf75cbc740c1ddf220878005d94611e
packages/pi/test/production-background-live/rpc.ts  ba126f7823795d756743accf89fe57cb0f8e9d24073e9c6daa2aba1ad0b01f50
packages/pi/test/production-background-live/scenario.ts  82cbdd051ae5370afc6c78e51c53e23ba27baa3a0200c1ea9c9be3a811ee7e5a
packages/pi/test/production-background-live/wrapper.ts  3fd18c319abf02a4d60fbb33b0d4abae920fc542079b8e8da6ce7383905b1f23
packages/pi/test/production-live-fullentry.test.ts  fe20894c4b21b130c07af1cc3337d31b10b3724022dee3effd2eeb4de7c8549e
packages/pi/test/production-live-harness.test.ts  ed81ce0acc8bbd6c35073a4651532a7cca6d97be02552841a6ed2188e914d7bf
```

**Harness digest: `9611f62b6ecbdbe30be0565e124ad1c9d979da8ac4c06d8626f7a4fad78c958c`.**

**Limits (honest):** the full-entry test still uses a fake RPC driver with a
zero-LLM fake child CLI, a session-backend stub, and an injected OS snapshot; the
notification provenance is a source-bound producer attempt, not content-inclusion
proof; the pid check is an OS heuristic, **not a hard OS sandbox**, and any external
process outside the managed path remains uncovered. **No live production
observation exists yet; the RUN has not been executed.** T08 unauthorized.

## T07 live RUN — actual result (nonce `imp004-prod-LIVE-20260923-aa`)

Manager-authorized single production scenario. **No code change** to production or
harness; only owned runtime artifacts (TEMP) and this docs bookkeeping. Pre-run:
production root `…/opencode/imp004-production-live` held only `cap.json` (0
reservations); no lingering owned parent/child; `HEAD` `c4c6450`; bound native CLI
`0.85.1`; source19 `0d792e13…` and harness12 `9611f62b…` matched before the run.

**Run command (from the workspace root):**

```
VGXNESS_PROD_LIVE=1 node --experimental-strip-types packages/pi/test/production-background-live/scenario.ts --run
```

**Actual result — INCONCLUSIVE (exit code 1), ~14 s:**

- `startup.factoryComplete=true`, registered `todowrite, apply_patch, model_resolve,
  task, task_control`; wrapper imported the actual `createPiExtension`.
- Observation stream (`…/pi-prod-live-DhrTZv/storage/observations.jsonl`): **2 records**
  — one real model tool `model_resolve` (`phase:end`, `isError:false`, full id
  `call_COBaCxZxS4galH6BBrYbb3Mx|fc_…`).
- No `task` call ⇒ no background child, no `apply_patch`, no `todowrite`, no
  `task_control` terminal. Fixture plan unchanged (`T1`/`T2` pending).
- All 11 gate checks **missing**: `taskHandleBound`, `progressNotification`,
  `progressBeforePlanUpdate`, `applyPatchBeforeTodo`, `applyPatchSucceeded`,
  `todoFullId`, `toolCallIdsMatchNative`, `planChangedBeforeTodo`,
  `controlRunningAtTodo`, `childAliveAtTodo`, `terminalBound` ⇒ `state=INCONCLUSIVE`
  (the gate verdict is emitted from the real metadata; never edited).
- Post-run: no owned parent/child lingering; source19 `0d792e13…` and harness12
  `9611f62b…` byte-unchanged; old POC roots untouched. Production budget **1 of 1**
  (`slot-0.lock` + `imp004-prod-liveprep-20260923-o--prod-progress-1p1c`).

**Observed root cause (demonstrated only):** the single authorized parent turn
executed exactly one tool (`model_resolve`, success) and settled without submitting
the `task`; therefore the background child never started and none of the downstream
witnesses could exist. No error/exception was surfaced (the prompt resolved
normally); no speculative fix is asserted.

**Status:** **T07 not accepted**, plan **paused**, budget consumed, **no retry** and
no hidden additional model call. T06 source PASSes remain pre-live and distinct.
Next action: Manager independent read-back of the owned runtime artifacts (not a
re-run). **T08 install/delivery unauthorized.** No raw model transcripts, API keys,
or session stores are recorded here; all evidence is bounded JSON metadata under the
owned TEMP root.

## T07 offline diagnostics — observability candidate (nonce `imp004-prod-diagnostics-20260923-ac`)

Harness-only; **no model call, no RPC prompt, no source change**. The live attempt
below remains **INCONCLUSIVE with no root cause established**.

**Observability changes:**

- Every native `tool_call` decision is logged: production handlers registered via the
  guarded pi are wrapped (return/await/receiver preserved) and recorded separately
  from the wrapper's own scope guard, each with a bounded full tool-call id (≤512
  bytes), tool name, `allow`/`block`/`unknown`, and a sanitized fixed reason code —
  never raw input or secrets.
- `ParentRpc.prompt` resolves **only** on `agent_settled`; `agent_end` (including
  `willRetry:true`) is bounded metadata and never resolves; exit/timeout reject with a
  fixed reason.
- `diagnostics.json` is written with an **exclusive-create (`wx`) write** under the run
  root before exit and on error/timeout (exclusive-create, **not** crash-atomic or
  fsync-durable; only the cap reservation uses a real atomic lock): sanitized native
  timeline, guard decisions, `agent_end` metadata, bounded `get_state` model metadata
  (provider/id/effort only), registered vs active tool names, and an optional
  last-assistant reason (TEXT only, ≤2048 UTF-8, **pattern-based redaction** of
  bearer/`sk-`/`api_key` — **not an exhaustive credential guarantee**; truncation
  annotated) or `finalReasonUnavailable`. All observer data is **untrusted** and is
  never treated as instructions. The budget is reserved before any run root exists, so a
  cap rejection writes no phantom report.
- Nonces: the authorization/mission nonce `imp004-prod-LIVE-20260923-aa` is distinct
  from the reservation/scenario-binding nonce `imp004-prod-liveprep-20260923-o`; two
  separate identifiers, **not a collision defect**.

**Checks actually run:**

- Full Pi suite: **268 tests, 267 pass, 0 fail, 1 skipped**.
- Strict `tsc` (both harness tests + all `production-background-live/*.ts`): clean.
- `node packages/pi/scripts/generate-contract.mjs --check`: PASS. `verify-package`: PASS.
- REAL zero-model preflight
  `VGXNESS_PROD_PREFLIGHT=1 node --test packages/pi/test/production-live-fullentry.test.ts`:
  **12/12 pass** (native `get_state`/`get_commands` only, zero prompts).
- New diagnostic tests: blocked task recorded with sanitized `role_mismatch` and no
  task execute; resolve-only distinguished from a block; injected RPC error/timeout
  persist a report with a bounded reason; cap rejection writes no phantom report;
  `ParentRpc.prompt` does not resolve on `agent_end` (willRetry true/false).
- Source19 re-hashed: **19 rows, 0 drift**, digest `0d792e13…`.

**Harness manifest (separate, 12 rows):**

```
packages/pi/test/production-background-live/budget.ts  76d3e6a8c507fa7fedbbf6ee30322dd03708a4f9e59701a2d5b50944ac51ab4b
packages/pi/test/production-background-live/evidence.ts  81fe92acdcb3fa505c5903e33150c6d761e99672b3c235c9f116abacd8236541
packages/pi/test/production-background-live/fake-driver.ts  f8b9d0eb25049396a5e9b4d213c9ea445b8bee4a0d4059810710fc8595770878
packages/pi/test/production-background-live/fixture.ts  d145636a2f4b919cc90a2bf259f81d6c171d51b2d4e9d4ec8664ea5639b568b5
packages/pi/test/production-background-live/gate.ts  c116aaa7447e31e267a2dffd4cc30fb8ae4829a6b773a8695abc73747f90d4ac
packages/pi/test/production-background-live/guard.ts  a4e2d2644b5a308f82c817002242a0affe6f6021b1c10cc252cb281edde93603
packages/pi/test/production-background-live/prompt.ts  9daca2c60624586af5e876cb2f5a2fb7eaf75cbc740c1ddf220878005d94611e
packages/pi/test/production-background-live/rpc.ts  fffb3b60983fae90851dc6ad86b44738ee3145520446ec24c4b25fe0f754065e
packages/pi/test/production-background-live/scenario.ts  2be34410e4825f6063ebd937b2a7709c765c26cd22438132b6505870f3f1e8fb
packages/pi/test/production-background-live/wrapper.ts  100367d304809e02e093420e23e9b78ca9eab583608fd8b0385779f4354a84c1
packages/pi/test/production-live-fullentry.test.ts  5ba45058a523769396881122daa56dd0f9ef957f06b26cc1c35c30a2f82bfeb2
packages/pi/test/production-live-harness.test.ts  f3351076e922518022f7709168ac2ed119fa4004e16fb181e01f3f00296f5198
```

**Harness digest (current, after the portability correction):
`6b592e444516b9bb0e637999b1f1174d2f27ce3a5354ad16a9affc498930b3f2`.** The
verifier-`…-ad`/CARE-`…-ae`-reviewed bytes `0fa4b9cf…` and the earlier `9611f62b…`
pidfix harness are frozen history and their recorded results are not altered; the only
post-review change is the portability test correction.

**Unrecoverable old live case (recorded plainly):** the one executed live run
(`imp004-prod-LIVE-20260923-aa`) captured **stdout only** — no diagnostics file existed
then. Its gate was reconstructed from exactly **2 observation records**
(`model_resolve` start/end) and all eleven required checks are therefore **unknown**;
no model refusal, defect, or root cause is established. The new diagnostics apply only
to FUTURE attempts and cannot retroactively restore that evidence. Finishing T07 needs
a **new user-approved live budget** (not approved now). **T08 unauthorized.**

## T07 final portability correction (nonce `imp004-prod-final-portability-20260923-af`, docs + one harness test only)

The offline diagnostics candidate was reviewed by verifier `…-ad` (**PASS**) and CARE
`…-ae` (**PASS**), which found one **environment-coupled** ordinary unit test: it
asserted the real production budget root and its prior reservation by name, so the unit
suite required this machine. Corrected: that test now runs on its own `mkdtemp` root
with a synthetic nonce (`reserve → cap 1 → reject → reopen`), and **no ordinary test
reads or writes the real production root**. The actual root is touched only by an
explicit `VGXNESS_PROD_LIVE` run. Because the harness bytes changed after the `…-ad`/
`…-ae` review, this correction needs **regression evidence + read-back later**; the new
bytes are **not** claimed as already reviewed. All prior history is preserved; at that
point active plan **none**, T06 **source accepted**, T07 **blocked** (live INCONCLUSIVE
1/1, no definitive cause), T08 **unauthorized** (no install, no commit).

## T07 resume — prompt + budget-root correction (nonce `imp004-live-resume-20260923-aj`; NO model call)

Harness/docs only; **no model call, no RPC prompt, no production source change**. The
prior live attempt remains **INCONCLUSIVE** and is **not retroactively accepted**.

**Changes:**

- `prompt.ts`: `buildParentPrompt` no longer receives a literal model placeholder. STEP 1
  directs the parent to call the real native `model_resolve` once and copy
  `roles["care-reviewer"].taskModel` and `.effort` exactly, stopping/reporting
  **unavailable** if absent (no different model, no auto fallback); STEP 2 sets
  model/effort to the resolved values and marks `provider/model-id` **illustrative — DO
  NOT copy it**. No literal invalid model and no hardcoded provider. The optional resolved
  model argument is test/fake-only and is never sent to a real Pi run.
- `scenario.ts`: new pure `parseLiveCliArgs`/`normalizeBudgetRoot` require `--run
  --budget-root <ABS> --nonce <ID>`, accept only the exact approved absolute canonical
  resume root, reject any other root (incl. the old exhausted root), relative/non-canonical
  paths, duplicate flags, unknown args and candidate overrides, with **no default-root
  fallback**. Validated values feed `runProductionLive({budgetRoot, nonce})` only after
  `VGXNESS_PROD_LIVE=1`. Scenario nonce is now `imp004-prod-resume-20260923-aj`. Budget cap
  stays **1** (no `--max-scenarios`, no retries).

**Checks actually run (no live call):**

- Full Pi suite: **270 tests, 269 pass, 0 fail, 1 skipped**.
- Strict `tsc` (both harness tests + all `production-background-live/*.ts`): clean
  (exit 0). Production `tsc -p tsconfig.json`: clean (exit 0).
- `generate-contract.mjs --check`: PASS. `verify-package`: PASS.
- REAL zero-model preflight `VGXNESS_PROD_PREFLIGHT=1 node --test
  packages/pi/test/production-live-fullentry.test.ts`: **12/12 pass** (`get_state`/
  `get_commands` only, **zero prompts**).
- New pure tests: prompt has no invalid placeholder and resolves the model dynamically; a
  fake resolved model is embedded only when supplied; the CLI override allowlists the
  exact root and fails closed on every other root/arg/nonce.

**Manifests:**

- Source19 re-hashed: **19 rows, 0 drift**, byte-identical before/after,
  `0d792e13775ff8afd6ecb7646cd877bbda837e41956f51f7049bd51cb273483b`.
- Resume harness12 (new digest; the prior `6b592e44…` does **not** review these bytes).
  Changed rows:

```
packages/pi/test/production-background-live/prompt.ts  f81fe24d43a43124f95b71a0aa85989b50ade9adfe8afdc72b63924c44ad4b51
packages/pi/test/production-background-live/scenario.ts  1329a3830a957151be24f9dac08cbcfda81294c74af57c1d814239f3c9de1221
packages/pi/test/production-live-harness.test.ts  11ef074645e4c216dd0f6c32bd03f0a5639eeae6afb4fe10b2243be5f156a213
```

`RESUME_HARNESS12 = c89c72cee534bbdb7a33546146094f5cc60b9339fea90ece806b29a91c45da84` (12 rows;
`budget.ts`, `evidence.ts`, `fake-driver.ts`, `fixture.ts`, `gate.ts`, `guard.ts`, `rpc.ts`,
`wrapper.ts`, `production-live-fullentry.test.ts` byte-unchanged from `6b592e44…`).

**Budget roots:** new root `…/imp004-production-live-resume` is **absent (0 of 1)** and was
**not created**; old root `…/imp004-production-live` is **1 of 1**, byte- and
mtime-unchanged (`cap.json`, `slot-0.lock`,
`imp004-prod-liveprep-20260923-o--prod-progress-1p1c`); old POC roots untouched.

**Nonces:** writer/mission `imp004-live-resume-20260923-aj` (user authorization) is
**distinct** from scenario/binding `imp004-prod-resume-20260923-aj`; not a collision.

**Limits / honesty:** the new harness bytes are **not** independently reviewed yet and
need **regression + read-back**; the old live run's evidence cannot be restored; this
section is writer self-report, not independent verification. **T07 not accepted; T08
unauthorized.**

## T07 resume ORDERING correction (verifier `…-verify-ak` FAIL; nonce `imp004-resume-preflight-budget-20260923-am`; NO model call)

Harness/docs only; **no model call, no RPC prompt, no production source change**.

**Defect (verifier `…-verify-ak` FAIL, retained as history):** `scenario.ts` reserved the
sole new budget (`budget.reserve`) **before** the zero-model `ParentRpc.preflight`, so a
preflight error consumed the only authorized slot without any model prompt. CARE
`…-care-al` **PASS** covered a **narrow source scope** only.

**Correction — load-bearing sequence in `runProductionLive`:**

1. `assertLiveRunAuthorized` + bound installed CLI (`assertInstalledCli`) + nonce; the CLI
   entry still enforces the exact approved root via `parseLiveCliArgs`.
2. **Read-only fail-closed cap pre-check** (`precheckBudget`): reads `cap.json` + counts
   reservation directories; creates nothing, writes no reservation.
3. **Zero-model preflight + tooling capability check** in an isolated TEMP run root
   (`get_state`/`get_commands` only; requires `factoryComplete` and the five parent tools).
   On failure: stop the owned driver, remove the TEMP run root, throw — **no reservation, no
   budget consumed, no model retry**. The TEMP run root may exist during preflight (owned,
   sanitized) but holds no reservation and is not a case artifact.
4. **Only after success**: atomic `ProductionBudget.reserve` cap1, then the single parent
   `prompt`. A competing reserve blocks. On reserve rejection: stop the driver, write a
   bounded `prefailure.json` in the ephemeral run root (distinct from `diagnostics.json`),
   no model prompt, fail closed.

**Checks run (no live call):**

- Full Pi suite: **272 tests, 271 pass, 0 fail, 1 skipped**.
- Strict `tsc` (both harness tests + all `production-background-live/*.ts`) clean; production
  `tsc -p tsconfig.json` clean.
- `generate-contract.mjs --check` PASS; `verify-package` PASS.
- REAL zero-model preflight `VGXNESS_PROD_PREFLIGHT=1 node --test
  packages/pi/test/production-live-fullentry.test.ts`: **14/14 pass**, zero prompts.
- New `F5` tests: preflight error ⇒ **0 reservation / 0 prompt / empty budget root**;
  successful preflight ⇒ **cap1 reserved before the first prompt** and duplicate ⇒ no second
  prompt. The prior `rpc-error`/`rpc-timeout` test now fails closed with no reservation.

**Manifests:** source19 **`0d792e13…` byte-identical before/after** (19 rows, 0 drift);
resume harness12 **`c01c29aa30b0870d0f3b0310c675d1f081d0257026c1a17ba3e340595aed4476`**
(prior `c89c72ce…`/`6b592e44…` do not review these bytes). New root
`…/imp004-production-live-resume` absent (**0 of 1**); old root `…/imp004-production-live`
**1 of 1**, byte/mtime-unchanged.

**Limits / honesty:** the corrected bytes are **unverified** — a **fresh** independent
review is required before any resume run. This section is writer self-report. **T07 not
accepted; T08 unauthorized.**

## T07 resume live RUN + reconciliation (scenario nonce `imp004-prod-resume-20260923-aj`; mission `imp004-prod-resume-LIVE-20260923-ap`; recovery `imp004-prod-resume-recover-20260923-aq`)

Executed once; **no code/source/harness edit**; docs only after the result.

**Pre-run (verified):** HEAD `c4c6450`; source19 `0d792e13…`; harness12 `c01c29aa…`; installed
CLI `0.85.1`; no owned process; new root absent (0/1); old root 1/1 unchanged.

**Command (exact):**

```
VGXNESS_PROD_LIVE=1 node --experimental-strip-types packages/pi/test/production-background-live/scenario.ts --run --budget-root /private/var/folders/4q/nlfcvv816_l0bh4wsl846_kw0000gn/T/opencode/imp004-production-live-resume --nonce imp004-prod-resume-20260923-aj
```

**Actual result — INCONCLUSIVE:**

- Native zero-model preflight + tooling check passed: `factoryComplete=true`, registered
  `todowrite, apply_patch, model_resolve, task, task_control`; `get_state`/`get_commands` only.
- Run root `…/T/pi-prod-live-A4WFTR`; reservation at `2026-09-23T08:51:24.419Z`; settled ~17 s
  (birth 02:51:23 → diagnostics 02:51:40) — **< 180 s, no model timeout**.
- `diagnostics.json` (schema `imp004-t07-diagnostics/v1`): state INCONCLUSIVE,
  `counts {observations:6, nativeEvents:434, agentEnds:1, guardDecisions:4}`, model
  `openai-codex/gpt-5.5` effort `medium`, `agentEnds [{willRetry:false}]`, bounded
  `lastAssistantReason` present (not reproduced here).
- Guard decisions: `model_resolve` allow (production handler + scope guard); `task` **allow**
  by the production handler, **block** by the harness `scope_guard` with
  `reasonCode=candidate_mismatch`. `task` `tool_execution_end` `isError:true`. No child, no
  patch, no `todowrite`, no terminal.
- Gate (from the run's stdout report): **all 11 checks missing** (`taskHandleBound`,
  `progressNotification`, `progressBeforePlanUpdate`, `applyPatchBeforeTodo`,
  `applyPatchSucceeded`, `todoFullId`, `toolCallIdsMatchNative`, `planChangedBeforeTodo`,
  `controlRunningAtTodo`, `childAliveAtTodo`, `terminalBound`) ⇒ INCONCLUSIVE.

**Reconciliation (no new live call):** the prior invocation's transport reported an SSE read
timeout; artifacts prove the run executed and settled. No owned process remained; the new root
holds exactly 1 reservation; no `prefailure.json`. The timeout is classified as **transport**,
not model/code. Raw model transcript, auth files and host store were **not** read.

**Budget:** new root **1 of 1** (`cap.json` maxScenarios 1, `slot-0.lock`, reservation dir);
old root **1 of 1** byte/mtime-unchanged; POC roots untouched. **No retry.**

**Hashes after:** source19 `0d792e13…` (unchanged); harness12 `c01c29aa…` (unchanged).

**Limits / honesty:** the precise candidate mismatch is **not definitively established** — the
submitted task input is not persisted (bounded metadata only); the guard reason
`candidate_mismatch` is definitive that the candidate binding check failed, and the prompt's
omission of the snapshot is the likely cause. No acceptance claim; **T07 blocked, T08
unauthorized.**

## T07c — offline prompt-binding correction (nonce `imp004-prod-prompt-candidate-20260923-as`; NO model call)

Harness/docs only; **no model call, no RPC prompt, no production source change, no budget
consumed**. The prior live runs remain **INCONCLUSIVE** and are **not retroactively accepted**.

**Root cause (source-backed, offline).** `prompt.ts` `ScenarioBinding` carried only
`candidateCommit`, and `buildParentPrompt` STEP 2 quoted only `candidate commit "<commit>"`, while
the strict harness `guard.ts` requires **both** `candidate.commit` **and** `candidate.snapshot`.
`scenario.ts` already held `PRODUCTION_CANDIDATE_SNAPSHOT=0d792e13…` and passed it to the
wrapper/gate expectation, but it never reached the prompt. A live parent therefore could not supply
the snapshot and the scope guard blocked the `task` with `candidate_mismatch`. The exact submitted
input was **not persisted**, so the precise mismatch (omitted vs wrong) is **not definitively
established**; the omission is the **source-proven likely cause**. **No model blame.**

**Correction (offline, bounded).**

- `prompt.ts`: `ScenarioBinding` gains `candidateSnapshot`; STEP 2 quotes **both** the full 40-char
  commit and the full 64-char snapshot and instructs the model to copy both exactly (never
  abbreviate/infer/substitute). The dynamic `model_resolve` `care-reviewer` taskModel/effort path is
  unchanged (no placeholder).
- `scenario.ts`: `scenarioBinding` assigns the actual `PRODUCTION_CANDIDATE_SNAPSHOT`; `expected` and
  the gate read `binding.candidateSnapshot` (single source).
- **Not changed / not weakened:** `guard.ts`, `gate.ts`, the production candidate schema, and all
  production `src/**` bytes.

**Why the earlier fake full-entry test missed it.** The fake driver builds its `taskInput` **directly
from `expected`** (`fake-driver.ts` `taskInput()`), not by parsing the prompt, so it structurally
**cannot** detect a prompt omission. The new `T07c` test in
`production-live-harness.test.ts` parses the commit/snapshot out of the real `buildParentPrompt`
output, asserts they equal the expected candidate fields, requires the prompt-derived candidate to
be admitted by the strict guard, and keeps the existing omitted/incorrect-snapshot rejections.

**Checks actually run (no live call):**

| Check | Result |
| --- | --- |
| Full Pi suite (`node packages/pi/scripts/test.mjs`) | **273 tests, 272 pass, 0 fail, 1 skipped** |
| Strict `tsc` (both harness tests + all `production-background-live/*.ts`) | clean (exit 0) |
| Production `tsc -p tsconfig.json` | clean (exit 0) |
| `generate-contract.mjs --check` | PASS |
| `verify-package.mjs` | PASS |
| REAL zero-model preflight `VGXNESS_PROD_PREFLIGHT=1 node --test packages/pi/test/production-live-fullentry.test.ts` | **14/14 pass** (`get_state`/`get_commands` only, zero prompts) |

**Manifests:**

- source19 **byte-identical before/after**, **19 rows, 0 drift**,
  `0d792e13775ff8afd6ecb7646cd877bbda837e41956f51f7049bd51cb273483b`.
- New harness12 digest **`4747c5f95776b1e5aa7fcd8fb6d840a3e6dc3a3ee94bbeb4acd3d5f6b5bba4ee`** (prior
  `c01c29aa…` is history). Changed rows:

```
packages/pi/test/production-background-live/prompt.ts  9aae9b342c68e1a06bbc236091336de8053d606621637983f2775fc114ed2ce1
packages/pi/test/production-background-live/scenario.ts  f81ede810fadf59178b5a23614e2bc3b49ba4895f5840f935f8be3f8183e2160
packages/pi/test/production-live-harness.test.ts  bbe88104302e67ff5c9bebbfee21253f32b402886dc6774b473dfef0ae7f7be0
```

**Budget roots:** new root `…/imp004-production-live-resume` **1 of 1**; old root
`…/imp004-production-live` **1 of 1**; both **byte/mtime-unchanged** (no reserve, no reset, no
writes); **0 remaining**. Old POC roots untouched.

**Limits / honesty:** the new harness bytes are **not independently reviewed** and need regression
+ read-back; this section is writer self-report, not independent verification. The prior live runs'
evidence cannot be restored and are **not** retroactively accepted. **No production-live PASS is
claimed.** **T07c done (offline)**, **T07 blocked**, plan **paused**, active **none**; **T08
unauthorized**. A live T07 validation would need a **new user budget decision**.

## T07d — final live-budget preparation (nonce `imp004-final-budget-20260923-aw`; NO model call)

Harness/docs only; **no model call, no RPC prompt, no production source change, no budget
consumed**. The two prior live runs remain **INCONCLUSIVE** and are **not retroactively accepted**.

**Source change (only the approved root constant).** `scenario.ts` `APPROVED_BUDGET_ROOT` is now
`/private/var/folders/4q/nlfcvv816_l0bh4wsl846_kw0000gn/T/opencode/imp004-production-live-final`.
`normalizeBudgetRoot` remains a pure exact static allowlist (absolute + canonical + string identity;
**no symlink resolution, no env-derived root**); the gated CLI still requires `--run --budget-root
<ABS> --nonce <ID>` with no default and rejects unknown/candidate/`--max-scenarios` overrides. Both
previous exhausted roots are explicitly rejected. **Guard, gate, the production candidate schema and
all production `src/**` bytes are unchanged.**

**New root / budgets.** `…/imp004-production-live-final` is **absent (cap 1, not created)**. The two
previous roots `…/imp004-production-live` and `…/imp004-production-live-resume` remain **1 of 1,
byte/mtime-unchanged**; old POC roots untouched. **No reservation and no RUN in this mission.**

**Checks actually run (no live call):**

| Check | Result |
| --- | --- |
| Full Pi suite (`node packages/pi/scripts/test.mjs`) | **274 tests, 273 pass, 0 fail, 1 skipped** |
| Strict `tsc` (both harness tests + all `production-background-live/*.ts`) | clean (exit 0) |
| `generate-contract.mjs --check` | PASS |
| `verify-package.mjs` | PASS |
| REAL zero-model preflight `VGXNESS_PROD_PREFLIGHT=1 node --test packages/pi/test/production-live-fullentry.test.ts` | **14/14 pass** (`get_state`/`get_commands` only, zero prompts) |

**Tests added/updated.** CLI parse accepts the exact final root with nonce
`imp004-prod-final-20260923-aw` and rejects **both** previous roots plus relative/other/non-canonical
values; a fresh isolated `mkdtemp` root reserves cap 1 and blocks a second reservation (never the real
root).

**Manifests.** source19 **byte-identical**, `0d792e13…` (19 rows, 0 drift). New candidate harness12
**`f4408dc8ca908f57dd26e103008f4e2c195b6b3e9f9b4e7be25a53d79f85eea8`** (**UNRUN**). History:
`c01c29aa…` (previous live run), `4747c5f9…` (offline-corrected, superseded). Changed rows:

```
packages/pi/test/production-background-live/scenario.ts  472a9e7580858f310ed9fa4b80bcdea5734a08e28bb55b98ce6b21d0b5cfd595
packages/pi/test/production-live-harness.test.ts  6512aa461ba4c8d48ba047b7e542fde92a7a91ac0844fa4b93483a3d8b6a3b9a
```

**Supported run command (NOT executed in this mission):**

```
VGXNESS_PROD_LIVE=1 node --experimental-strip-types packages/pi/test/production-background-live/scenario.ts --run --budget-root /private/var/folders/4q/nlfcvv816_l0bh4wsl846_kw0000gn/T/opencode/imp004-production-live-final --nonce imp004-prod-final-20260923-aw
```

**Limits / honesty.** The new candidate harness12 is **unrun** and **not independently reviewed**;
this section is writer self-report. **No production-live PASS is claimed.** **T07d done (offline)**,
**T07 `in_progress`** (new final budget 1), active plan **T07**; **T08 unauthorized**.

## T07e — FINAL live production RUN (scenario nonce `imp004-prod-final-20260923-aw`; mission nonce `imp004-prod-final-LIVE-20260923-az`)

Executed **exactly once**; **no code/source/harness edit**; docs only after the outcome. Result
**FAIL**, all budgets exhausted, **no retry**.

**Pre-run (verified, read-only):** HEAD `c4c6450`; source19 `0d792e13…`; harness12 `f4408dc8…`;
installed CLI `0.85.1`; no owned process; old root **1 of 1**, resume root **1 of 1**, final root
**absent (0 of 1)**.

**Command (exact, from the workspace root):**

```
VGXNESS_PROD_LIVE=1 node --experimental-strip-types packages/pi/test/production-background-live/scenario.ts --run --budget-root /private/var/folders/4q/nlfcvv816_l0bh4wsl846_kw0000gn/T/opencode/imp004-production-live-final --nonce imp004-prod-final-20260923-aw
```

**Actual result — FAIL (exit 1):**

- Native zero-model preflight + tooling check passed: `factoryComplete=true`, registered
  `todowrite, apply_patch, model_resolve, task, task_control`; `get_state`/`get_commands` only.
- Run root `…/T/pi-prod-live-X6EMEG`; reserve then cap1 (`cap.json` createdAt
  `2026-09-23T16:43:59.763Z`); run-root birth `10:43:59` → diagnostics `10:45:06` local ⇒ **elapsed
  ≈67 s (<180 s)**. Model `openai-codex/gpt-5.5`, effort `medium`.
- `diagnostics.json` (schema `imp004-t07-diagnostics/v1`): `state=FAIL`,
  `counts {observations:37, nativeEvents:1688, agentEnds:0, guardDecisions:16}`,
  `finalReasonUnavailable=true` (no bounded last-assistant reason captured; none reproduced here).
- Guard decisions (sanitized): `model_resolve` allow; `task` **allow** (production handler + scope
  guard) — **no `candidate_mismatch`**; `task_control` allow; `apply_patch` **block** by the harness
  `scope_guard` with `reasonCode=patch_scope` (obsSeq 17 and 19) then allow (21, 25);
  `todowrite` allow (29, 30).
- Gate (11 checks): **pass** `taskHandleBound`, `progressNotification`, `progressBeforePlanUpdate`,
  `applyPatchBeforeTodo`, `todoFullId`, `terminalBound`; **fail** `applyPatchSucceeded`,
  `toolCallIdsMatchNative`, `planChangedBeforeTodo`, `controlRunningAtTodo`, `childAliveAtTodo` ⇒
  **FAIL**.

**Bounded cause (observation stream + guard decisions only).** The `task` was admitted and a real
background child ran (`bg_d170e3e3-…`), emitted a source-bound `sendMessage` progress before the
patch, and reached a **terminal `succeeded`** envelope with the exact candidate `{commit 40, snapshot
64}`. The parent's plan write did not land: two `apply_patch` attempts were blocked by the harness
`scope_guard` (`patch_scope`), the next admitted patch **errored** (`isError:true`, obsSeq 23), and a
later admitted patch **succeeded** (`isError:false`, obsSeq 27) — but the plan digest was **unchanged**
(`planState.changed:false`, `taskState:pending`, obsSeq 35), and the gate binds
`applyPatchSucceeded`/`toolCallIdsMatchNative` to the **first** apply_patch end (the errored one).
The parent's `todowrite` then occurred **after** the child had succeeded (`control.state=succeeded`,
`childAlive=false`), so `controlRunningAtTodo`/`childAliveAtTodo` fail. The hard-trace requirement
(plan persisted + projected **while the child is running**) was **not** met — a genuine **FAIL**, not
an inconclusive gap.

**Budget/cleanup.** Final root now **1 of 1** (`cap.json`, `slot-0.lock`,
`imp004-prod-final-20260923-aw--prod-progress-1p1c/reservation.json`); old and resume roots **1 of 1
byte/mtime-unchanged**; POC roots untouched. **No owned process remained after the run**
(watchdog/session cleanup normal). **No retry, no additional model command, no provider override or
auth manipulation.**

**Hashes after:** source19 `0d792e13…` **unchanged**; harness12 `f4408dc8…` **unchanged**.

**Limits / honesty.** This section is writer self-report, not independent verification. The bounded
diagnostics establish the FAIL and the ordering defect; the raw model transcript, auth files and host
store were **not** read. **No production-live PASS is claimed.** **T07e done (FAIL)**, **T07
`blocked`**, plan **paused**, active **none**, **all budgets exhausted**; **T08 unauthorized.** A
further attempt needs a **new user decision** and a code/harness correction for the
plan-write/todowrite ordering (**not** authorized here).

## T07f — offline strict full-ID native-binding correction (nonce `imp004-prod-patchgate-20260923-bc`; NO model call)

Self-reported writer evidence, **harness/docs only**. **No model call, no RPC prompt, no
production `src/**` change, no budget consumed.** The `…-aw` FAIL is **retained
unchanged**; T07f does **not** accept T07 and **no production-live PASS is claimed**.

**Root cause (offline, exploration nonce `imp004-fail-analysis-bb`).** `gate.ts` bound the
**first** native `apply_patch` `end` (`findIndex`) while the trusted wrapper's
`todoRecord.applyToolCallId` stores the **last** attempt id. On the T07e run the first apply
end errored and a later one succeeded, so the gate bound the **errored first** attempt.
Independent verifier `…-ba` had confirmed **no product defect** was demonstrated; this is a
harness binding artifact.

**Fix (bounded, fail-closed, no fallback).** The native `apply_patch` end is selected by the
**exact** wrapper-observed `observations.applyToolCallId`, requiring **exactly one** matching
native apply `end` that occurs **before** the **bound** native `todowrite` `start` (also
selected by the exact observed `todoToolCallId`, requiring exactly one matching native start).
Missing ⇒ INCONCLUSIVE; present-but-unbound or ambiguous ⇒ FAIL; the gate never falls back to
the first or the last successful patch. `applyPatchSucceeded` is evaluated **only** on the
bound end (`isError===false`). `toolCallIdsMatchNative` is an **independent** cross-layer
count: each observed id must appear exactly once among native events of its kind, lossless
(>64 chars, ≤512 UTF-8 bytes, no prefix/truncation) — not a tautology, since a wrapper id the
native stream dropped/duplicated yields count 0 or >1. `planChangedBeforeTodo` (from the
wrapper plan-file snapshot), `controlRunningAtTodo` and `childAliveAtTodo` remain **mandatory
and un-weakened**. Guard logging now emits the bounded codes `patch_unparseable` vs
`patch_scope_escape` derived from the existing `evaluateProductionGuard` strings; **no raw
patch input or text is stored**.

**Prompt/manifest unchanged.** `prompt.ts`, `guard.ts`, `scenario.ts` and the production
candidate schema are unchanged; the synthetic 20 s child health argv is unchanged (no
wall-clock gaming). Earlier patch paths are **not** retroactively identifiable (no raw input
persisted).

**Checks actually run (no live call):**

| Check | Result |
| --- | --- |
| Full Pi suite (`node packages/pi/scripts/test.mjs`) | **279 tests, 278 pass, 0 fail, 1 skipped** (274 + 5 new) |
| Strict `tsc` (both harness tests + all `production-background-live/*.ts`) | clean (exit 0) |
| Production `tsc -p tsconfig.json` | clean (exit 0) |
| `generate-contract.mjs --check` | PASS (`a890…` unchanged) |
| `verify-package.mjs` | PASS |
| REAL zero-model preflight `VGXNESS_PROD_PREFLIGHT=1 node --test packages/pi/test/production-live-fullentry.test.ts` | **16/16 pass** (`get_state`/`get_commands` only, zero prompts) |

**New tests (5).** Strict gate unit: first native apply id `A` errored then `B` succeeded,
wrapper observed `B`, plan unchanged ⇒ overall **FAIL with only `planChangedBeforeTodo`
failing** (binding `A` would additionally fail `applyPatchSucceeded`); a wrapper id with **no
unique native match** fails closed (no fallback to `A` or `B`); duplicate same-id native ends
are **ambiguous ⇒ FAIL**; bound `B` + plan changed + child alive ⇒ **PASS**, and a `todowrite`
after the child succeeded ⇒ FAIL; same-64-char-prefix different-tail ids never bind.
Full-entry through the real production wrapper callbacks: `patch-retry` (first attempt errors,
retry succeeds touching only `progress.md`, so T1 stays `pending`) ⇒ **FAIL only
`planChangedBeforeTodo`**, bound native end is the successful retry, unique match, compiled
body of 11 native events / 41 observation records; `patch-retry-pass` (retry changes plan.md)
⇒ **PASS** (no false negative). No test alters the public POC.

**Manifests.** source19 **byte-identical before/after**, **19 rows, 0 drift**,
`0d792e13775ff8afd6ecb7646cd877bbda837e41956f51f7049bd51cb273483b`. New harness12
**`f12665e01f8956472378ebbd2ec7f9e33c5b4f64edab0c18a8bcd3fd967d698f`** (the prior
`f4408dc8…` is history). Changed rows:

```
packages/pi/test/production-background-live/gate.ts  1dbb5a1e5f0318f825c2fa70818f2eb51e740b01956fe207bc09070024761b10
packages/pi/test/production-background-live/wrapper.ts  d04d68cd29bf026ae0770334f59da1135da528075a22ee159ebc00f225848615
packages/pi/test/production-background-live/fake-driver.ts  74c7a1f639839b44bcc5f00826833333131974ecf94b78034dcfec949c4a0a75
packages/pi/test/production-live-harness.test.ts  d8b17e115b559b870b323e31017e1f500f3a6e9b75f78a32090e848c81cb84b1
packages/pi/test/production-live-fullentry.test.ts  1a06e31f7961ce1a5fa55de96192c3f94cb0da0c9afa4eeed56c44a9713a5f80
```

**Budgets (unchanged, no run).** Old `…/imp004-production-live` **1 of 1**, resume
`…/imp004-production-live-resume` **1 of 1**, final `…/imp004-production-live-final` **1 of 1** —
all byte/mtime-untouched; **no reservation, no reset, no RUN**; old POC roots untouched.

**Limits / honesty.** This section is writer self-report, **not independent verification**. The
T07e FAIL and the two INCONCLUSIVE runs are **retained as history** and are **not**
retroactively accepted or repaired. The strict binding was validated **offline only** against
synthetic and fake-driver witnesses; it makes **no claim** about real model behavior on the
prior runs. **No production-live PASS is claimed.** **T07f done (offline)**, **T07 `blocked`**,
plan **paused**, active **none**; **T08 unauthorized**. Any future production-trace acceptance
requires a **new user live-budget decision** that is **not already approved**.

## T07g offline deterministic plan-write correction (nonce `imp004-planwrite-fix-20260923-bh`)

Self-reported writer evidence, **harness/docs only**. **No model call, no RPC prompt, no
production `src/**` change, no budget consumed.** The `…-aw` FAIL is **retained
unchanged**; T07g does **not** accept T07 and **no production-live PASS is claimed**.

**Scope (no root-cause claim).** The T07e FAIL showed the parent projected `todowrite`
only after the child had succeeded and the plan never changed. The exact prior patch path
is **unrecoverable** (no raw patch input was persisted), so this section does **not** claim
a proven root cause. The correction only makes the deterministic fixture guidance in the
parent prompt explicit; a real live run may still fail (for example the 20 s child health
check) and the `todowrite`-before-terminal ordering is **not guaranteed** by offline tests.

**Fix (prompt + tests only).** `prompt.ts` STEP 3 now names the exact workspace-relative
target `docs/implementations/IMP-TEST/plan.md`, the exact pre-image row
`| T1 | Fixture primary task | pending |`, the exact post-image
`| T1 | Fixture primary task | in_progress |` (status `in_progress`, **never `done`**) and
the untouched sibling `| T2 | Report target task | pending |`; it requires **ONE** native
`apply_patch` **before** the `todowrite` while the child is still running, forbids the
`*** Begin Patch` wrapper, and says the untrusted `eventId` is a detail only (it must not
choose path, status, or permission). STEP 1 (`model_resolve` dynamic), STEP 2 (candidate
commit+snapshot literal, strict guard), the single read-only child, the guard/gate and the
production candidate schema are **unchanged/not weakened**; **no new `read` tool, no
health-delay increase, no automatic production plan-writer**. **Identity reconciliation:**
the sibling row uses the **actual** fixture label `Report target task`; the mission brief's
`Fixture secondary task` does not exist in `fixture.ts` and was not introduced.

**CARE-bj INFO clarification (nonce `imp004-prompt-clarity-20260923-bk`).** An independent CARE
review (`…-bj`) raised an INFO contradiction: STEP 3 said "exactly ONE native apply_patch" **and**
offered an optional second `progress.md` patch before the `todowrite`, so the last-patch binding
could in principle track a `progress.md` write even though `plan.md` changed. The optional
`progress.md` patch is **removed** from the live diagnostic prompt: STEP 3 is now **exactly ONE
`plan.md` patch and the `todowrite` while the child is still alive**, with **no** second patch. A
`progress.md` write in a real Manager flow can be a separate later action **outside** this synthetic
hard-gate (after the child stops) and is **not** claimed as real-run progress documentation; the
`plan.md` state persistence is sufficient for this gate. The dynamic `model_resolve`, the
candidate commit+snapshot literals, the 20 s child health argv, and all guard/gate code are
**unchanged** (no timing tuning). The regression now asserts STEP 3 has one patch and **no**
`progress.md` / `optionally`.

**Tests added (3).** Harness: one test **parses the REAL `buildParentPrompt` output** to
extract the target and the pre/post rows, builds the unified diff **from those VALUES**,
and then (a) asserts `patchTargetsFromDiff` returns exactly the fixture plan target, (b)
asserts the strict guard admits it, and (c) executes the **REAL `createApplyPatchTool`** on
the fixture asserting `T1`→`in_progress`, `T2` stays `pending`, and the plan digest
changes; a second test asserts the guard rejects a `*** Begin Patch` wrapper (`unparseable
patch`) and an outside-prefix `note.txt` target (`patch escapes the fixture plan prefix`)
while the prompt-derived patch is admitted. Full entry: the fake driver now builds its plan
diff by **parsing the actual `runProductionLive` prompt** (it no longer constructs rows
from `expected`), and new scenarios `patch-unparseable`, `patch-escape`, `progress-only`,
`todo-after-child` assert INCONCLUSIVE/FAIL as appropriate, including the sanitized reason
codes `patch_unparseable`/`patch_scope_escape`.

**Checks actually run (no live call):**

| Check | Result |
| --- | --- |
| Full Pi suite (`node packages/pi/scripts/test.mjs`) | **282 tests, 281 pass, 0 fail, 1 skipped** (279 + 3 new) |
| Strict `tsc` (both harness tests + all `production-background-live/*.ts`) | clean (exit 0) |
| Production `tsc -p tsconfig.json` | clean (exit 0) |
| `generate-contract.mjs --check` | PASS (`a890…` unchanged) |
| `verify-package.mjs` | PASS |
| REAL zero-model preflight `VGXNESS_PROD_PREFLIGHT=1 node --test packages/pi/test/production-live-fullentry.test.ts` | **17/17 pass** (prior T07g evidence; **not re-run** for CARE-bj — the bound installed CLI is unchanged) |

Real `--run` was **NOT** executed.

**CARE-bj clarification re-checks (this round, nonce `imp004-prompt-clarity-20260923-bk`):** full
Pi suite **282 tests / 281 pass / 0 fail / 1 skipped**; targeted harness **26/26**, full-entry
**16 pass / 0 fail / 1 skipped**; strict `tsc` (both harness tests + all
`production-background-live/*.ts`) clean; production `tsc -p tsconfig.json` clean;
`generate-contract.mjs --check` PASS; `verify-package.mjs` PASS. No real zero-model preflight was
re-run (CLI unchanged) and no `--run` was executed.

**Manifests.** source19 **byte-identical before/after**, **19 rows, 0 drift**,
`0d792e13775ff8afd6ecb7646cd877bbda837e41956f51f7049bd51cb273483b`. New harness12
**`69ec95fb826880faf1f8af7bf422de52fcc20ff40910f2746730e8da32e5ab2a`** (the prior
`22774813…` is history). Changed rows:

```
packages/pi/test/production-background-live/prompt.ts  dc5531cef41e509a16f440af609c6eb206d47352190090381b06a8f88b028574
packages/pi/test/production-background-live/fake-driver.ts  989c36d6201f071914a3d792038d1fbd4cc3f6e7ef0ea12b764e722591ab88a4
packages/pi/test/production-live-harness.test.ts  a1f3ecca2f4c5fd533d8d6ef9f617ce2665d1c4368e209a3ae8370a5b59a2174
packages/pi/test/production-live-fullentry.test.ts  12b2032693b6936ec491ffc30e059668d36adc64bf97639c090e31ad36a27878
```

**Budgets (unchanged, no run).** Old `…/imp004-production-live` **1 of 1**, resume
`…/imp004-production-live-resume` **1 of 1**, final `…/imp004-production-live-final`
**1 of 1** — all byte/mtime-untouched; **no reservation, no reset, no RUN**; old POC roots
untouched.

**Limits / honesty.** This section is writer self-report, **not independent verification**.
The T07e FAIL and the two INCONCLUSIVE runs are **retained as history** and are **not**
retroactively accepted or repaired. The precision fix was validated **offline only** against
the fixture, the production parser and the fake driver; it makes **no claim** about real
model behavior on the prior runs and **no guarantee** about a future live ordering. **No
production-live PASS is claimed.** **T07g done (offline)**, **T07 `blocked`**, plan
**paused**, active **none**; **T08 unauthorized**. Any production-trace acceptance requires
a **new user live-budget decision** that is **not already approved**.

**CARE-bj status.** The prior CARE `…-bj` PASS applies to the **old** T07g bytes
(`2277481357…`), which are now history; the current candidate
`69ec95fb826880faf1f8af7bf422de52fcc20ff40910f2746730e8da32e5ab2a` needs a **read-only
regression readback** (not performed here; this is writer self-report). The CARE-bj
clarification was applied **only** to `prompt.ts` and `production-live-harness.test.ts`;
`fake-driver.ts` and the other harness modules are byte-unchanged from the prior T07g bytes.
The canonical `plan.md` T07g row and the `docs/implementations/README.md` T07g note were
later corrected to the then-current binding `69ec95fb…` under nonce
`imp004-current-hash-20260923-bl`; that binding is itself superseded by **T07h** below.

## T07h offline precise-budget preparation (nonce `imp004-precise-budget-20260923-bo`)

Self-reported writer evidence, **harness/docs only**. **No model call, no RPC prompt, no
production `src/**` change, no budget consumed.** The `…-aw` FAIL and both INCONCLUSIVE runs
remain **separate history** and are **not** retroactively accepted. The user **explicitly
authorized ONE further live production Pi scenario** (1 parent + 1 read-only child, ≤180 s,
existing default model/auth; no install/Git delivery); **T07 umbrella `in_progress`** under
that single slot; **T08 unauthorized**. The RUN itself is **not executed here**.

**Change.** `scenario.ts`: `APPROVED_BUDGET_ROOT` → `…/imp004-production-live-precise`;
`SCENARIO_NONCE` → `imp004-prod-precise-20260923-bo`. The pure CLI parser still requires
`--run --budget-root <exact precise> --nonce <fresh>`, has **no default fallback**, and rejects
**all three** previous exhausted roots (old/resume/final), any other/relative/non-canonical
root, candidate overrides and duplicate flags; cap **1**; the programmatic default
`runProductionLive()` (no explicit root) still targets the old exhausted `PRODUCTION_LIVE_ROOT`
and **fails closed before any prompt**. The prompt `69ec95fb…` is **unchanged** (exact
pre/post rows, one `plan.md` patch, no optional `progress.md`); the candidate
`{commit c4c6450…, snapshot 0d792e13…}`, the dynamic `model_resolve` resolver and the 20 s
child health argv are unchanged. No guard/gate/prod/POC source change.

**Checks actually run (no live call):**

| Check | Result |
| --- | --- |
| Full Pi suite (`node packages/pi/scripts/test.mjs`) | **283 tests, 282 pass, 0 fail, 1 skipped** |
| Targeted harness `production-live-harness.test.ts` | **27/27** |
| Strict `tsc` (both harness tests + all `production-background-live/*.ts`) | clean (exit 0) |
| Production `tsc -p tsconfig.json` | clean (exit 0) |
| `generate-contract.mjs --check` | PASS (`a890…` unchanged) |
| `verify-package.mjs` | PASS |
| REAL zero-model preflight `VGXNESS_PROD_PREFLIGHT=1 node --test production-live-fullentry.test.ts` | **17/17** (`get_state`/`get_commands` only, no prompt) |

Real `--run` was **NOT** executed.

**Manifests / roots.** source19 `0d792e13…` byte-identical (19 rows, 0 drift). New harness12
**`2dd035329c93a62514b5140a267f80bfeeb2d336ac1c29eb07726d954ce9e2ba`** (prior `69ec95fb…`
history). Changed rows:

```
packages/pi/test/production-background-live/scenario.ts  29ff761fe53e65d47a515f0741a16fbd51d854a49ad651186911e25d1ee042b4
packages/pi/test/production-live-harness.test.ts  509d3bf5254e15157de8f4cf580c7cb086f9d586a9ecf119749b2ce3bf96a0d5
```

Old `…/imp004-production-live`, resume `…/imp004-production-live-resume` and final
`…/imp004-production-live-final` remain **1 of 1** each, byte/mtime-unchanged (bounded
read-only readback of `cap.json`/`slot-0.lock`/`reservation.json`). The new root
`…/imp004-production-live-precise` is **absent (0/1, no `cap.json`)**.

**Limits / honesty.** Writer self-report, **not independent verification**; the current bytes
need a **read-only verifier/CARE readback** before the separate Manager RUN. Offline tests make
**no guarantee** about the live ordering or the 20 s child health check; **no source live PASS**
is claimed and **T08 install/delivery is not authorized**.

## T07i live production RUN — result **FAIL** (scenario nonce `imp004-prod-precise-20260923-bo`; mission nonce `imp004-prod-precise-LIVE-20260923-br`)

Executed **once** from the workspace with the exact authorized command; **no retry**. This is an
**observed runtime outcome**, recorded as-is (`FAIL`, exit 1); it is **not** doctored,
retroactively accepted, or upgraded. **Budget consumed** (one model prompt occurred).

```
VGXNESS_PROD_LIVE=1 node --experimental-strip-types \
  packages/pi/test/production-background-live/scenario.ts --run \
  --budget-root /private/var/folders/4q/nlfcvv816_l0bh4wsl846_kw0000gn/T/opencode/imp004-production-live-precise \
  --nonce imp004-prod-precise-20260923-bo
```

**Preflight (zero model, before reserve):** installed CLI `0.85.1`; `factoryComplete:true`;
registered `[todowrite, apply_patch, model_resolve, task, task_control]`; reserve happened only
after this PASS. Model metadata (bounded): `openai-codex` / `gpt-5.5` / effort `medium`.

**Gate verdict:** **FAIL — 8 pass / 3 fail.**

| Check | Status | Check | Status |
| --- | --- | --- | --- |
| `taskHandleBound` | pass | `applyPatchSucceeded` | **fail** |
| `progressNotification` | pass | `todoFullId` | pass |
| `progressBeforePlanUpdate` | pass | `toolCallIdsMatchNative` | pass |
| `applyPatchBeforeTodo` | pass | `planChangedBeforeTodo` | **fail** |
| `controlRunningAtTodo` | pass | `childAliveAtTodo` | **fail** |
| `terminalBound` | pass | | |

**Observed native order (full 83-char tool-call ids captured, bounded metadata only):**
`model_resolve` (allow) → `task` (allow; handle `bg_9c39cb8c-e48e-4888-9f59-9a1b235d6fb2`; plan
`IMP-TEST` / task `T1`; nonce exact) → **exactly ONE** `apply_patch` — admitted by both the
production `tool_call` handler and the harness scope guard (both `allow`), then
`tool_execution_end` `isError:true` (no second attempt) → `todowrite` (allow) →
`task_control` ×2 (allow). Counts: 693 native events, 32 wrapper observations, 12 guard
decisions (all `allow`; the bounded codes `patch_unparseable` / `patch_scope_escape` were **not**
triggered), `agentEnds:0`, `lastAssistantReason:null` (`finalReasonUnavailable:true`).

**Projection instant (wrapper obsSeq 20):** `control.state:"running"` (the background job was
still running) while `childAlive:false` from the numeric OS owned-worker snapshot; plan digest
before == after `0512ed4f…`, `taskState:"pending"`, `changed:false`; the bound
`applyToolCallId` is the **errored** apply. **Terminal (obsSeq 31):** `state:"succeeded"`, exact
nonce, candidate `{commit c4c6450b…, snapshot 0d792e13…}`. The host `sendMessage` progress
(`e1`, seq 1) and result (`e2`, seq 2) are `followUp`/`triggerTurn` **producer attempts — not
content proof**.

**Fixture after run:** `T1 | Fixture primary task | pending` and
`T2 | Report target task | pending` (byte-unchanged) — the plan write did not land. The exact
`apply_patch` failure text is **not persisted** (no raw patch input is stored), so **no cause is
attributed** and **no model refusal is claimed**.

**Measured:** wrapper observation span **≈29.8 s** (first→last `at`), well under the 180 s parent
deadline; this is a measured span, not a latency guarantee.

**Hashes / budget / cleanup:** source19 `0d792e13…` and harness12 `2dd03532…` **byte-identical
before/after**; new root `…/imp004-production-live-precise` now **1 of 1** (`cap.json`,
`slot-0.lock`, `reservation.json` nonce `imp004-prod-precise-20260923-bo`); old/resume/final
**1 of 1** byte/mtime-unchanged; **no owned process leak** (only owned groups reaped, no global
kill). No install/commit/memory/delegation.

**Limits.** `FAIL` is retained; no T07 acceptance. The `childAliveAtTodo:false`-while-`running`
observation and the unpersisted apply failure are **observation/harness limits**, not a
model-refusal claim. Plan **paused**, active **none**, T07 **blocked**; **T08 unauthorized**. Any
further attempt needs a **new user decision** — no remaining budget exists.

## T07j — offline `apply_patch` error diagnostics + liveness honesty (nonce `imp004-patch-error-offline-20260923-bu`; NO model call, NO budget consumed)

Self-reported writer evidence. **No live model call was made**; **no budget root was touched**;
**no install/commit/delivery**. This pass makes future failures diagnosable and stops the
owned-worker observer from reporting a fabricated dead child. It is **NOT retroactive** and does
**not** accept T07: the `…-br` FAIL is retained with its **exact cause still UNKNOWN** (the error
text was never retained), and the earlier `childAlive:false` reading is **not** proven to mean the
child had exited.

**Instrumentation (harness only, production source untouched).**

- **`diagnostics.ts` (new, test-only).** A closed, secret-free classifier for REAL production
  `apply_patch` errors: reason codes `patch_parse`, `path_escape`, `root_drift`, `target_drift`,
  `session_authority`, `parent_mutation_conflict`, `recovery_pending`, `fs_notfound`,
  `fs_permissions`, `other`. It matches ONLY exact known production message strings, trusted error
  `name`/`code` (`PatchRecoveryError`/`WorkerRecoveryPendingError`/`ParentMutationConflictError`),
  safe `startsWith` prefixes, and a fixed fs-code allowlist (`ENOENT`/`ENOTDIR` → `fs_notfound`;
  `EACCES`/`EPERM`/`EROFS` → `fs_permissions`). It never returns, stores, or logs the original
  message, a path, or an arbitrary `e.code`; unknown input ⇒ `other`.
- **`wrapper.ts`.** The trusted observer now wraps the ACTUAL registered `apply_patch.execute`,
  catches the thrown error **before** Pi converts it to native text, records a bounded
  `{kind:"tool_error", toolName:"apply_patch", toolCallId (≤512), reasonCode, provenance:"observer"}`
  and **rethrows the exact same error object** (native `isError` behavior unchanged; no raw patch
  input, message, or path is persisted). It also records path-free target metadata — capped
  `targetCount` ≤64 plus a SHA-256 over the normalized, sorted, workspace-relative targets — and
  never the target paths themselves (malformed diff ⇒ `targetCount:0`, code from the error).
- **`evidence.ts` / `gate.ts`.** Native `tool_execution_end` errors are classified from allowlisted
  fixed strings only (unknown ⇒ `other`), and observer/native tool errors are merged by tool call
  with the **observer preferred**, so a specific code is never duplicated contradictorily. The gate
  gained only the additive, informational `toolErrors` type; the **verdict is unchanged**.
- **Liveness honesty.** `observeOwnedWorker` returns **UNKNOWN** (omits `childAlive`) on a no-match
  or ambiguous OS snapshot instead of `false`; `childAlive:false` requires an explicit
  same-generation binding the live path does not establish. A missing OS witness is therefore
  INCONCLUSIVE, never a fabricated dead child; the strict gate still requires `childAlive === true`
  (a unique matched worker). Mandatory `planChangedBeforeTodo` / `applyPatchSucceeded` and the 20 s
  child-health argv are unchanged — no clock was gamed.

**Checks actually run (all offline):**

| Check | Result |
| --- | --- |
| Strict `tsc` (both harness tests + all `production-background-live/*.ts`) | clean (exit 0) |
| Production `tsc -p tsconfig.json` | clean (exit 0) |
| Full Pi suite (`node packages/pi/scripts/test.mjs`) | **292 tests, 291 pass, 0 fail, 1 skipped** |
| `generate-contract.mjs --check` | PASS (`a890…` unchanged) |
| `verify-package.mjs` | PASS |
| REAL zero-model preflight `VGXNESS_PROD_PREFLIGHT=1 node --test production-live-fullentry.test.ts` | **21/21** (`get_state`/`get_commands` only, no prompt) |

New tests: classifier allowlist + secret-free negatives; path-free target metadata; REAL
`createApplyPatchTool` parse-error classification and exact-identity rethrow; `ownedWorkerLiveness`
unknown/no-match; native `tool_execution_end` classification + non-contradictory merge; full-entry
`failed-patch` records `target_drift` with no raw text; helper-only / pending-plan / post-child
liveness cases stay non-PASS. Real `--run` was **NOT** executed.

**Manifests / roots.** source19 **byte-identical**, `0d792e13…` (19 rows, 0 drift). New **harness13**
**`74a2b1d7d1f645e1f1afc4a64b71c0917d77c89a99c6aabccaf7d1b00abcbcb4`** (the 12 prior rows plus the
new `diagnostics.ts`). Changed/added rows:

```
packages/pi/test/production-background-live/diagnostics.ts  61d3c889b48c94ce59f6fc163d3b8cb4f7bf52b03ff66ed9a99d3130d92ea1f4
packages/pi/test/production-background-live/evidence.ts  5d2f248f34b13677a27007771cffdceac6e3e2a2aafc4e3dae802f0f894e6cdf
packages/pi/test/production-background-live/gate.ts  62ca36fa1e49c60285b22e252f751824042e9c784b90eecd554531bf682d5e4f
packages/pi/test/production-background-live/wrapper.ts  521b8ec0fffdae2eb73b808c45cc30ab1d3404cb5d261f54f4885b91ff8e6497
packages/pi/test/production-live-fullentry.test.ts  4bbeb39dd724eb0c5812a8228bd986cb8c3cdbeccb5858281ae07b8f4919c548
packages/pi/test/production-live-harness.test.ts  0bee263c4e8abbaec690b131d1e5a0542da01ef81c2d5b7e577595269efcb9a2
```

All four budget roots — `…/imp004-production-live`, `…/imp004-production-live-resume`,
`…/imp004-production-live-final`, `…/imp004-production-live-precise` — remain **1 of 1** each,
byte/mtime-untouched (bounded read-only readback of `cap.json`/`slot-0.lock`/`reservation.json`).
No reservation, no reset, no RUN.

**Limits / honesty.** Writer self-report, **not independent verification**; the current bytes need a
read-only verifier/CARE readback. The classifier and liveness changes are **offline-only** and make
**no guarantee** about the live ordering or the 20 s child-health check. The `…-br` FAIL is retained
with its **exact cause UNKNOWN**; no source live PASS is claimed and **T08 install/delivery is not
authorized**. Any further attempt needs a **new user decision** — no implicit budget is created.
