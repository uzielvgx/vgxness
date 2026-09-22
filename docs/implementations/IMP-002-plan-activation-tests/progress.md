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

## Blockers

- None. No dependency has been reported unavailable.

## Next action

- **T04 (Manager-owned):** freeze the exact IMP-002 candidate (HEAD + diff SHA)
  and hand the same candidate to independent verification and applicable CARE.
  This worker does not perform the freeze, verification, CARE, commit,
  installation, or closure.
