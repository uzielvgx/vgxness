# IMP-002 — Progress

Authoritative record of decisions, blockers and the next action. Canonical task
state lives in [plan.md](plan.md) and is not duplicated here; evidence lives in
[validation.md](validation.md).

## Decisions

- **D1 — fix the summary, keep the schema.** The embedded schema clause
  "exactly one plan is active while executing, at most one is active otherwise,
  and zero are active after closure or with no work" already encodes the correct
  rule. Only the summary sentence is corrected to "keep at most one active plan
  per session" with an explicit zero-when-no-work qualifier, so schema and
  blocking-decision authority are preserved.
- **D2 — canonical sources first, generated resources after.** The shared
  `internal/orchestration/manager_contract.json` instructions and the native
  adapters (`internal/providers/opencode/current_renderer.go`,
  `packages/pi/src/orchestration/adapter.ts`) are the maintained sources. The Pi
  `resources/orchestration/contract.json` and `resources/prompts/manager.md` are
  regenerated from them, never hand-edited.
- **D3 — uniform wording across providers and docs.** The same "at most one …
  zero when there is no work" wording is applied to the shared contract, the
  OpenCode/Pi adapters, the corpus, and the touched docs
  (`docs/implementations/README.md`, `docs/orchestration-flow.md`,
  `docs/opencode-integration.md`, `docs/pi-typescript.md`,
  `docs/architecture/shared-manager-contract.md`).
- **D4 — negative coverage.** Each current-prompt test asserts the corrected
  phrase is present and the ambiguous "exactly one active plan per session"
  summary is absent, so a positive match cannot mask the contradiction.
- **D5 — history preserved.** IMP-001 records, the pre-adaptive/bootstrap
  snapshots, the schema, the role inventory, the markers and the artifact counts
  are not modified. The corpus count stays 23.
- **D6 — intentional golden updates.** The OpenCode 28 per-agent hashes and the
  Codex current `AGENTS.md` hash change because the shared contract digest and
  text are embedded; they are recomputed, not weakened. Bootstrap goldens stay
  unchanged.

## Findings history (preserved)

- **IMP-001 F-3 (low, non-blocking, from CARE `care-plans-20260922-i`):** the
  summary phrase "exactly one active per session" could be misread as always-one
  while the specific rule determines zero active after closure or with no work.
  IMP-001 closed without correcting it; IMP-002 is the new plan that addresses
  it. IMP-001 history is not altered.

- **D7 — checks observed, not assumed.** The exact commands and results are
  recorded in [validation.md](validation.md). The 7-package Go suite, broader
  `go test -short ./...`, `go vet ./...`, Pi typecheck, and Pi 174-pass suite all
  passed; `generate-contract.mjs --check` and `verify-package.mjs` were clean.
  The OpenCode 28 per-agent hashes and the Codex current `AGENTS.md` hash were
  recomputed from actual observed values.
- **D8 — isolated installation trials, no hosts.** Isolated install/receipt/
  lifecycle tests run against temp roots inside the authorized packages
  (for example `TestIntegration_InstallReadbackStatusAndIdempotence`,
  `TestIntegration_InstallNeverOverwritesForeignOrDriftedContent`,
  `TestIntegrationV3InstallStatusChangeAndUninstall`, and the Codex
  `activation_lifecycle`/`bootstrap_integration` tests). No real host install or
  provider mutation was performed.
- **D9 — CLI preflight is capability evidence, not profile proof.** `opencode`
  1.18.32 exposes `--agent`, `--format json`, `--dir`, and (newer)
  `--continue`/`--session`/`--fork`; `codex-cli` 0.153.4 exposes `exec`,
  `exec resume`, `--json`, `-C`, and `--output-last-message`, and
  `codex login status` reports a logged-in session. The evaluation protocol is
  **not** changed to assume continuation or an installed `vgxness-manager`
  profile on the basis of help text alone.

- **D10 — T04 verification and CARE recorded.** Independent verification
  `imp002-verify-20260922-e` returned PASS (35 Go short, 7 fresh, `vet`, Pi
  `174`/typecheck/generators). CARE `imp002-care-20260922-f` returned PASS on
  source/preflight with no blocker. These are recorded as Manager/verifier
  results, not re-run here.
- **D11 — T05 installation and intermittent Codex status.** Self and provider
  installation completed. Initial Manager-observed statuses: OpenCode remained
  installed (`dde8e333…`); Codex was initially `8768b1…`, then a generic status
  **error sentinel** was observed by the verifier and the Manager, then a
  diagnostic source run showed no error, and the Manager later observed **both**
  installed — launcher and original candidate now `state=installed`,
  `changed=false`, hash `8768b1…` — **without any repair**. The intermittent root
  cause is **UNPROVEN, not fixed**, and **no rollback occurred**. `Status` reads
  are read-only calls into CLI lists; they are not a mutation.
- **D12 — candidate identity and provenance.** The resolved commit is
  `b0e6f266e1eec20441696672a162b6fcd5e9a093` (local commit, 32 files; source
  exact prior diff `909cc…`). The active launcher is `4057b712…`, previous
  `90a9aa78…`. The historical baseline `96a153ee…` remains the declared baseline;
  it is not presented as a stale current freeze. Known records append
  paths/provenance rather than rewriting prior history.
- **D13 — live probe and report v2 semantics.** The bounded live probe
  (`imp002-live-20260922-g`) used 6 invocations in 851.82 s with 2 timeouts;
  Codex exposed no native planning tool (session-scoped only), and discovery-tool
  absence limited its investigation. Independent live review
  `imp002-liveverify-20260922-i` confirmed the fixtures' source stayed clean but
  found that report summaries conflated a **tool name** with an **event
  category** (Codex actual tool name `wait` vs event item type
  `collab_tool_call`). The fix is a separate sanitized `report-v2.json` with
  distinct `event_types` / `item_types` / `tool_names` machine-parsed from the
  existing raw events (metadata keys only, no text-substring inference, no raw
  text/auth/secrets in the repo). The original report is retained as history.
- **D14 — bounded recovery probe (T07).** The OpenCode turn-3 timeout left the
  native todo projection unobserved (INCONCLUSIVE). Exactly one extra OpenCode
  invocation was authorized (budget increased once, 6 → 7; fixed 300 s timeout)
  on the existing observed session to test explicit recovery. No retry occurs
  even if this probe also times out, and   Codex receives no call.
- **D15 — closure (Manager local declarative acceptance).** Independent liveverify
  `imp002-liveverify-20260922-o` returned PASS on the current install: self
  launcher `4057b712…`, OpenCode `dde8e333…`, Codex `8768b1…`, all `installed`
  with `changed=false`; the t4 recovery run returned rc 0 in 99.6 s and its
  actual `todowrite` metadata carried 12 items mapping both the current and
  cancelled plans (native `blocked` unsupported, so T-02 is a pending item
  labelled blocked). This is bounded local closure, not a runtime guarantee.
- **D16 — retracted false inferences.** Earlier readings that a package-vs-file
  hash difference is a defect, and that `Status` performs a generic `ErrRecovery`
  rollback, are **withdrawn**: package-vs-file hashes legitimately differ, and
  `Status` is read-only (it never rolls back). The intermittent Codex status is
  real (root UNKNOWN, not fixed); its final healthy state does not erase the
  earlier observation.
- **D17 — scope, guides and totals.** Guides used: `agent-evaluation` (writer)
  and `installer-lifecycle` (Manager). Model-host calls used the existing
  authenticated login only; no additional services were installed. Live calls:
  7 total (4 OpenCode, 3 Codex); elapsed listed separately — initial 851.82 s,
  recovery 99.6 s (not summed here). Cost not measured (current subscription); no
  amount is assumed. The upcoming records commit is Manager-owned and its hash is
  not known here (not invented).

## Blockers

- None. No dependency has been reported unavailable.

## Next action

- **None required** for the accepted bounded scope. Optional future diagnostics
  (not active work): the intermittent Codex status (real, root UNKNOWN, not
  fixed); the Codex session-scoped absence of a native planning tool; the two
  original timeouts retained as INCONCLUSIVE history; and automatic/new-session
  full feature implementation (NOT tested; a future model may differ). No
  protected-holdout or every-time claim is made.
