# IMP-001 — Validation

Evidence actually obtained, with its limits. Nothing is claimed before it is
observed. This file is self-reported by the writer and is not independent
evidence. History is appended, never deleted.

## Status

- Closed on the C2 reviewed candidate: HEAD
  `96a153ee91ffedd23b64389c0151de622286a3b4`, diff SHA
  `787e53eb0ae4f50a3a6047feb13bed638c9f372b35fec813f87ecbe3736db750`.
- Reviewed record digests at review time: README
  `08ca57ae59061baa91161c523a062ba07b955847`, plan
  `c4c648c39af7bc50277c4100b22462be4f468065`, progress
  `e6fd6e17149afc3ce1c7dfbf869b1961658d33be`, validation
  `753b499e3bd1b2768ae56ff6d94c239ba52fa825`.
- Self-run development checks on this candidate: **run and passing** (below).
- Independent verification (T13): **done** — verifier nonce
  `verify-plans-20260922-h`, PASS.
- CARE review (T14): **done** — CARE nonce `care-plans-20260922-i`, PASS
  AC01–AC08 with one low non-blocking F-3.
- Manager acceptance/closure (T15): **done** — local declarative acceptance
  (instructions/tests); not live, install, or delivery.
- **C2: PASS.**
- **Disclaimer:** closure metadata only. It does not alter code, contract,
  schema, tests, or resources. The digests above identify the reviewed bytes; the
  current record files are post-review metadata and are **not** claimed to be the
  reviewed bytes. A reviewer performs the closure readback.

## Findings history (preserved, no new approval)

These were held on the **previous** candidate and are superseded by corrections;
they do not approve the current candidate.

- **Verifier C1:** PASS on the previous candidate's tests.
- **CARE C1:** FAIL (technical).
  - **F-1 (medium, blocked AC01):** the plan-record schema existed only in the
    repository docs and was not delivered to an installed Manager in a new
    project. It is **not** closed by `docs/implementations/README.md` alone.
    Corrected by embedding the literal `# Implementation plan records` section
    in the shared `manager.instructions` (D10).
  - **F-2 (low, AC08):** the corpus lacked explicit closure-without-inherited-
    authorization and block/interrupt/resume scenarios. Corrected by adding
    concrete contractual clauses and per-scenario fragments (below).

## Correction pass addressing C1 findings

- Added the self-sufficient `# Implementation plan records` section to the
  shared instructions: index (`docs/implementations/README.md`) with ID, title,
  status, path and explicit active plan; unique stable `<ID-slug>/plan.md`;
  canonical plan with objective, scope/exclusions, decisions/assumptions, AC ids
  and tasks (ID, state, AC link, dependencies, required evidence); task states
  `pending, in_progress, blocked, done, cancelled`; plan states `pending, active,
  paused, closed, cancelled` with exactly one active while executing and at most
  one otherwise (zero after closure or with no work); `progress.md` decisions,
  blockers and single next action without a duplicated task table;
  `validation.md` exact candidate, check results, limits, verifier and CARE; and
  the proportional single-file allowance. The instructions state they are
  sufficient on their own in an empty project.
- Added concrete clauses: "A closed plan is not reopened: begin a new plan for
  new work and do not inherit the closed plan's authorization"; "When blocked or
  interrupted, record the blocker and the next action and stop work that depends
  on the unresolved decision; resume by reconciling the recorded next action and
  blocker against repository evidence before continuing."
- Asserted the schema in the **rendered provider and Pi prompts**, not just the
  repository README: `internal/orchestration/manager_contract_test.go`,
  `internal/providers/opencode/current_renderer_test.go`,
  `internal/providers/codex/render_test.go`,
  `internal/e2e/care_parity_test.go`, and
  `packages/pi/test/orchestration.test.ts`.
- Corpus grew 20 → 23 cases (closure A→B, block/stop-dependents, resume-blocker);
  count assertions updated in four tests.

## C2 review record

- **Verifier `verify-plans-20260922-h`:** PASS; candidate identity before/after
  exactly equal; suites 7 Go, `generate --check`, `verify-package`, typecheck,
  Pi 174 pass / 0 fail.
- **CARE `care-plans-20260922-i`:** PASS AC01–AC08.
- F-1 (shipped schema) and F-2 (corpus) confirmed closed on C2. C1 history above
  is retained without removal.
- **Residual F-3 (low, non-blocking, not corrected here):** the summary phrase
  "exactly one active per session" versus the specific rule that zero plans are
  active after closure or with no work; the specific rule controls. Registered as
  an optional improvement outside closure; no new plan is created.

## Checks (current candidate)

| Check | Command | Result | Limits |
| --- | --- | --- | --- |
| Go formatting | `gofmt -l <modified Go files>` | clean | Formatting only |
| Pi resources | `node packages/pi/scripts/generate-contract.mjs` then `--check` | generated; `--check` clean | Content equality, not runtime |
| Pi package | `node packages/pi/scripts/verify-package.mjs` | pass | Package/resource invariants |
| Orchestration + providers + e2e | `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./internal/orchestration/ ./internal/providers/opencode/ ./internal/providers/codex/ ./internal/e2e/ -count=1` | `ok` ×4 | Deterministic projection tests |
| Extra Go packages | same env, `go test ./internal/cli/ ./internal/mcp/ ./internal/config/ -count=1` | `ok` ×3 | Adjacent packages |
| Pi typecheck | `npm run typecheck --workspace packages/pi` | pass | Types only |
| Pi tests | `npm test --workspace packages/pi` | `pass 174`, `fail 0` | Includes embedded-schema assertions |

## Intentional golden updates

- `internal/providers/opencode/current_renderer_test.go`: 28 native hashes
  recomputed (all seven agents per plan) because the contract digest is embedded
  in every OpenCode agent. Manager per plan: high/ultra `48a81e4c…`, medium
  `d6d73b5d…`, low `61ec8f5d…`.
- `internal/providers/codex/current_renderer_test.go`: current `AGENTS.md` hash
  recomputed to `7ab34112…` for all plans.
- `internal/providers/codex/bootstrap_contract_test.go`: **unchanged**; the
  Codex adapter is byte-stable, so the pre-adaptive bootstrap bytes stay
  `1590647b…`.
- No install artifacts were added or removed; artifact counts are unchanged.

## Limits

- Deterministic assertions establish declared policy and native wiring only; they
  do not establish live model routing, behavioral equivalence, or any
  protected-holdout result. The corpus remains a development partition.
- No Git delivery, install, or lifecycle action was performed; the Manager owns
  Git inspection and freeze is recorded, not independently confirmed.
- The Codex native planning tool has no evidenced name and is referenced
  generically; the provider-neutral fallback is what is asserted.
- T13 verification and T14 CARE are recorded as done from the C2 reports; this
  records-only task did not re-run them. Closure is a local declarative
  acceptance, not live, install, or delivery evidence.
