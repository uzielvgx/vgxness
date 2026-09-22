# IMP-002 — Validation

Evidence actually obtained, with its limits. Nothing is claimed before it is
observed. This file is self-reported by the writer and is not independent
evidence. History is appended, never deleted.

## Status

- Current candidate: `b0e6f266e1eec20441696672a162b6fcd5e9a093` (local commit, 32 files; source exact prior diff `909cc…`).
- Historical baseline: `96a153ee91ffedd23b64389c0151de622286a3b4` (declared baseline, not a stale current freeze).
- Generated Pi contract digest (`sourceDigest`) after regeneration:
  `a890f5453c72f97c30174041b27d28d62d5b60c1bd357b89a754b7d5c60f76ce`.
- Self-run development checks on the source candidate: **run and passing** (below).
- Independent verification (T04): **done** — `imp002-verify-20260922-e` PASS.
- CARE review (T04): **done** — `imp002-care-20260922-f` PASS (source/preflight, no blocker).
- Installation (T05): **done** — OpenCode `dde8e333…`; Codex `8768b1…`; launcher `4057b712…`.
- Live behavior probe (T06): **done** — independent liveverify
  `imp002-liveverify-20260922-o` PASS; readback self `4057b712…`, OpenCode
  `dde8e333…`, Codex `8768b1…`, all `installed` `changed=false`.
- Recovery probe (T07): **done** — one bounded OpenCode invocation (budget 6→7,
  300 s), rc 0, 99.6 s.
- Plan: **closed** (Manager local declarative acceptance; bounded scope).

## Checks (current candidate)

| Check | Command | Result | Limits |
| --- | --- | --- | --- |
| Go formatting | `gofmt -l <modified Go files>` | clean (exit 0) | Formatting only |
| Pi resources | `node packages/pi/scripts/generate-contract.mjs` then `--check` | regenerated; `--check` clean (exit 0) | Content equality, not runtime |
| Pi package | `node packages/pi/scripts/verify-package.mjs` | pass (exit 0) | Package/resource invariants |
| Orchestration + providers + e2e + cli + mcp + config | `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./internal/orchestration/ ./internal/providers/opencode/ ./internal/providers/codex/ ./internal/e2e/ ./internal/cli/ ./internal/mcp/ ./internal/config/ -count=1` | `ok` ×7 | Deterministic projection and isolated-install tests |
| Broader Go | same env, `go test -short ./...` and `go vet ./...` | all `ok` / no failures; `vet` exit 0 | Dependencies already present; no network install |
| Pi typecheck | `npm run typecheck --workspace packages/pi` | pass | Types only |
| Pi tests | `npm test --workspace packages/pi` | `pass 174`, `fail 0` | Includes embedded-schema and negative assertions |
| CLI preflight | `opencode --version`; `opencode run --help`; `codex --version`; `codex exec --help`; `codex exec resume --help`; `codex login status` | opencode `1.18.32`; codex-cli `0.153.4`; login `Logged in using ChatGPT`; help captured | Capability/help evidence only; not installed-profile or live-model proof |

## Corpus and negative assertions

- `internal/orchestration/testdata/manager-scenarios.json`: the
  `plan-single-active` fragment is now `keep at most one active plan per
  session`; the case count is still **23** (no new cases).
- Negative assertions added for the ambiguous summary in the current prompts:
  `internal/providers/opencode/current_renderer_test.go`,
  `internal/providers/codex/render_test.go`,
  `internal/e2e/care_parity_test.go`, and
  `packages/pi/test/orchestration.test.ts` assert the corrected phrase is present
  and `exactly one active plan per session` is absent.
- Docs updated to the uniform wording: `docs/implementations/README.md`,
  `docs/orchestration-flow.md`, `docs/opencode-integration.md`,
  `docs/pi-typescript.md`, `docs/architecture/shared-manager-contract.md`.
- The embedded schema clause and the blocking-decision authority are unchanged;
  the pre-adaptive/bootstrap snapshot tests still pass.

## Intentional golden updates

- `internal/providers/opencode/current_renderer_test.go`: 28 per-agent hashes
  recomputed (seven agents per plan) because the shared contract digest and
  Manager text are embedded in every OpenCode agent. Manager per plan:
  high/ultra `710e4ca0…`, low `3ebfa01c…`, medium `fbf11b15…`.
- `internal/providers/codex/current_renderer_test.go`: current `AGENTS.md` hash
  recomputed to `38cfc2ef…` for all plans.
- `internal/providers/codex/bootstrap_contract_test.go` and the OpenCode
  pre-adaptive goldens: **unchanged**; historical snapshots stay byte-intact
  (the bootstrap/pre-adaptive tests pass).

## Limits

- Deterministic assertions establish declared policy and native wiring only; they
  do not establish live model routing, behavioral equivalence, or any
  protected-holdout result. The corpus remains a development partition.
- No Git delivery, install, lifecycle, memory, independent verification or CARE
  action is performed by this worker. The Manager owns T05/T06.
- The Codex native planning tool has no evidenced name and is referenced
  generically; the provider-neutral fallback is what is asserted.

## T04 — independent verification and CARE (recorded)

- Verifier `imp002-verify-20260922-e`: PASS (35 Go short, 7 fresh, `vet`, Pi
  `174`/typecheck/generators).
- CARE `imp002-care-20260922-f`: PASS (source/preflight), no blocker.

## T05 — installation and configuration (recorded)

- OpenCode: installed, artifact `dde8e333…`.
- Codex: initially `8768b1…`; a generic status **error sentinel** was observed by
  the verifier and Manager; a diagnostic source run showed no error; the Manager
  later observed **both** installed (launcher and original candidate) as
  `state=installed`, `changed=false`, hash `8768b1…`, **without repair**.
- Intermittent root cause: **UNPROVEN, not fixed**; **no rollback occurred**.
  `Status` reads only call CLI lists (read-only).
- Active launcher `4057b712…`, previous `90a9aa78…`.

## T06 — live behavior probe (nonce `imp002-live-20260922-g`)

- 6 invocations, 851.82 s total, 2 timeouts (OpenCode turn 3, Codex turn 2).
- Codex exposed no native planning/task tool (session-scoped only); discovery-tool
  absence limited its investigation.
- Independent live review `imp002-liveverify-20260922-i`: fixtures' source stayed
  clean; the report summaries conflated a tool name with an event category.
- Limits: per-turn "no write outside fixtures" is an observation, not a sandbox
  guarantee.

## Report v2 — semantic labels

- `results/report-v2.json` (temp only) separates `event_types`, `item_types` and
  `tool_names` for all seven invocations (six initial plus the recovery probe),
  machine-parsed from the existing raw events by metadata keys only. No tool call
  is inferred from text substrings; no raw text, auth, or secrets are stored in
  the repo. `report.json` is retained as history.

## T07 — bounded recovery probe

- Authorization: budget increased explicitly once, 6 → 7; fixed 300 s timeout;
  OpenCode only; existing observed session `ses_f37a7b735ffe5JA7G5wHQK3jCE`.
- Outcome: **PASS** — rc 0, 99.6 s, no timeout; tools `task`, `apply_patch`,
  `todowrite` (no new tool names beyond the previously observed set).
- Native projection (`todowrite`): both plans' task IDs and states were projected
  — `IMP-001-json-notes` T-01 `completed`, T-02…T-06 `cancelled`;
  `IMP-002-title-search` T-01 `completed`, T-02 pending labelled blocked,
  T-03…T-06 pending.
- Fixture: index keeps `IMP-002-title-search` active and `IMP-001-json-notes`
  cancelled with history preserved; canonical ≤1 active; no code or scope change;
  repo source unchanged.
- History: the original OpenCode turn-3 timeout remains **INCONCLUSIVE**; this
  explicit recovery is **not** a retroactive PASS of the automatic run.
- Cost: not measured (current subscription); no amount is assumed.

## Closure (nonce `imp002-close-20260922-p`)

- **Manager accepted LOCAL closure** with bounded evidence; this is **not** a
  runtime guarantee.
- Independent liveverify `imp002-liveverify-20260922-o`: PASS — self launcher
  `4057b712…`, OpenCode `dde8e333…`, Codex `8768b1…`, all `installed`
  `changed=false`. Independent t4 recovery: rc 0, 99.6 s; actual `todowrite`
  metadata 12 items mapping current + cancelled plans; native `blocked`
  unsupported, so T-02 is a pending item labelled blocked. Source verify+CARE as
  previously recorded.
- **Retractions:** the earlier inferences that a package-vs-file hash difference
  is a defect, and that `Status` performs a generic `ErrRecovery` rollback, are
  **withdrawn** (package-vs-file hashes legitimately differ; `Status` is
  read-only and never rolls back). The intermittent Codex status is real with an
  **unknown root, not fixed**; the final healthy state does not erase the earlier
  observation.
- **Scope of evidence:** 7 live model-host calls (4 OpenCode, 3 Codex) using the
  existing authenticated login only; no additional services installed. Elapsed
  listed separately: initial 851.82 s; recovery 99.6 s. Cost not measured.
- **Not tested / residual:** automatic/new-session full feature implementation;
  protected holdout; any every-time behavior; a future model may differ.
- Source and repo: 32-file commit `b0e6f26`; no source/test/runtime change in this
  record. The upcoming records commit is Manager-owned; its hash is not known
  here and is not invented.
- Guides: `agent-evaluation` (writer) and `installer-lifecycle` (Manager).
