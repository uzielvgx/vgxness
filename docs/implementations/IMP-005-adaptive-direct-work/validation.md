# IMP-005 — Validation

Record of checks, independent verification, and CARE for the IMP-005 candidate.
Implementation evidence was self-reported by writer nonce
`imp005-impl-20260924-c`; independent verification and CARE were performed by
separate roles; closure prose was written under nonce
`imp005-close-20260924-i`. This **closure document** was not itself reviewed as
source; the verified source is the frozen candidate identified below.

## Candidate identity

- **Source revision / HEAD:** `c4c6450b779cd39c95a96fc3303bb4cc67ee0291`.
- **Accepted local source candidate — tracked diff SHA256:**
  `7c06bc6ecca6aaa6e63355b85424f7e0fab8f6331bed0231fdfdaff4e186e73b`.
- **Prior (superseded) candidate:** `diff012b749e…` — the pre-correction bytes.
  The only change from that candidate to the accepted one is the Pi test title
  in `packages/pi/test/orchestration.test.ts` and review-correction prose in this
  plan's `plan.md`; no contract, test assertion, generated resource, or golden
  changed, so the canonical digest below is stable across both.
- **Canonical contract digest (Pi `sourceDigest` / Go `ManagerContractDigest`):**
  `5ebfbe03243918ebc81778c6970de8e877be47c008efdf40809c67cf68b47e99`
  (unchanged from the accepted source; was `a890f5453c72f97c30174041b27d28d62d5b60c1bd357b89a754b7d5c60f76ce`
  before IMP-005).
- **Writer nonces:** `imp005-impl-20260924-c` (T03–T06);
  `imp005-review-correction-20260924-f` (review corrections);
  `imp005-close-20260924-i` (this closure).
- **Marker identity:** OpenCode `version: 62` and Codex `version: 21; parity:
  opencode-v62` are deliberately unchanged; the digest is the identity binding
  (see [progress.md](progress.md) D12, D16).
- **Changed paths (accepted source):**
  `internal/orchestration/manager_contract.json`,
  `internal/orchestration/manager_contract_test.go`,
  `internal/orchestration/manager_scenarios_test.go`,
  `internal/orchestration/testdata/manager-scenarios.json`,
  `internal/providers/opencode/current_renderer.go`,
  `internal/providers/opencode/current_renderer_test.go`,
  `internal/providers/opencode/shared_contract_test.go`,
  `internal/providers/codex/current_renderer_test.go`,
  `internal/providers/codex/shared_contract_test.go`,
  `packages/pi/resources/orchestration/contract.json`,
  `packages/pi/resources/prompts/manager.md`,
  `packages/pi/test/orchestration.test.ts`,
  `docs/architecture/shared-manager-contract.md`,
  `docs/manager-orchestration-skills.md`,
  `docs/opencode-integration.md`,
  `docs/orchestration-flow.md`,
  `docs/implementations/README.md`.
- **Not changed (deliberately):** `packages/pi/src/orchestration/adapter.ts`
  (no conflict), `internal/providers/codex/manager.go` (shared historical
  bootstrap adapter, no conflict), `internal/providers/opencode/current_renderer.go`
  `preAdaptive*` bytes, `internal/orchestration/manager_contract_82c7112a.json`,
  all permission maps, IMP-003/IMP-004 files, protected holdouts.
- **Working tree:** pre-existing untracked `IMP-003-pi-integration-update/` and
  `IMP-004-child-parent-progress/` preserved unchanged; IMP-005 records added.

## Checks

| Check | Command | Result | Limits |
| --- | --- | --- | --- |
| Baseline HEAD | `git rev-parse HEAD` | `c4c6450b…ee0291` | read-only |
| Go formatting | `gofmt -l <changed .go>` | clean | changed files only |
| Pi generator | `node packages/pi/scripts/generate-contract.mjs` | wrote `contract.json` + `manager.md` | local Node only |
| Pi generator check | `node packages/pi/scripts/generate-contract.mjs --check` | pass | deterministic content |
| Pi package check | `node packages/pi/scripts/verify-package.mjs` | pass | offline |
| Orchestration | `… go test ./internal/orchestration/ -count=1` | ok | 38-case corpus incl. `expect: absent` |
| OpenCode provider | `… go test ./internal/providers/opencode/ -count=1` | ok | 28-hash golden refreshed |
| Codex provider | `… go test ./internal/providers/codex/ -count=1` | ok | `AGENTS.md` golden refreshed |
| e2e parity | `… go test ./internal/e2e/ -count=1` | ok | projection parity |
| Pi typecheck | `npm run typecheck --workspace packages/pi` | pass | local `tsc` |
| Pi tests | `npm test --workspace packages/pi` | 174 pass / 0 fail | local Node |
| Broader Go | `… go test ./internal/cli/ ./internal/mcp/ ./internal/config/ -count=1` | ok | relevant surface only |
| Correction Pi tests | `npm test --workspace packages/pi` (nonce `imp005-review-correction-20260924-f`) | 174 pass / 0 fail | title-only test edit |
| Correction typecheck / generator check / package check | `npm run typecheck --workspace packages/pi`; `generate-contract.mjs --check`; `verify-package.mjs` | pass | Go unchanged |
| Whitespace | `git diff --check` | clean | tracked diff |

The correction changed only a test title and plan prose; Go files were unchanged
from the verified build, so the prior full Go PASS context still applies and Go
was not re-run for the title/prose-only change.

## Scenarios covered

- **Positive:** routine bounded direct read; trivial task with no mandatory
  delegation/plan/task list/skill load/CARE; capability-honest unavailable
  report; dev-scope reset guard; chosen delegation for broad scope; delegation
  never a permission elevation; read-only bounded `explore` mission; no native
  deny bypass (incl. Python); direct evidence; trivial-task plan exemption.
- **Negative (`expect: absent`):** blanket "Delegate all project code
  exploration"; "with no simple exception"; "does not browse project itself";
  blanket formal plan; delegation as permission elevation.

These are deterministic contract-projection assertions (substring/digest), not
observed live model behavior or behavioral equivalence. No expected failure (RED)
was observed before the change: the corpus and assertions are regression
coverage, not observed TDD.

## Independent verification and CARE

- **Verifier `imp005-verify-d`: PASS** on the accepted local source candidate —
  full Go 7 packages (`orchestration`, `providers/opencode`,
  `providers/codex`, `e2e`, `cli`, `mcp`, `config`) plus Pi 174, typecheck,
  generator, and `verify-package`.
- **CARE `imp005-care-e`: PASS** on AC01–AC07, with two low findings: **F1** stale
  Pi test title and **F2** present-tense plan motivation. Both were corrected
  under `imp005-review-correction-20260924-f`.
- **Correction verifier `imp005-verify-correction-g`: PASS** (Pi 174, typecheck,
  generator `--check`, `verify-package`); Go files unchanged, so the prior Go PASS
  applies.
- **Correction CARE `imp005-care-correction-h`: PASS** with F1/F2 closures; the
  remaining **F3 info** finding — that the limitations assertions check only
  `evidenceKind`/`partition` — was recorded as non-blocking.
- **Not observed:** no live model run, no dev app start, no database reset, no
  installed activation, receipt, or readback, and no `commit`/`push`. Prior
  findings and closures are retained; a source change invalidates them.

## Limits

Local, offline developmental checks only: no install, Git commit/push/PR, memory,
network, or live model run. The destructive-reset guard is contract text; no
database was reset. Marker version labels were not bumped; the canonical digest
is the binding. The corpus proves declared projection consistency, not runtime
enforcement or routing. Closure prose (including this file) was not reviewed as
source; the reviewed artifact is the frozen source candidate identified above,
and `closed` does not mean delivered or installed.
