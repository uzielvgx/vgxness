# IMP-001 — Progress

Authoritative record of decisions, blockers and the next action. Canonical task
state lives in [plan.md](plan.md) and is not duplicated here; evidence lives in
[validation.md](validation.md).

## Decisions

- **D1 — no version-marker bump.** The OpenCode manager marker stays `version:
  62` and the Codex marker stays `version: 21; parity: opencode-v62`. This is
  acceptable because the canonical content digest is the real identity binding
  (`vgxness-orchestration/v1` plus `sourceDigest`), the receipt records that
  digest, and the historical pre-adaptive snapshots stay untouched. The design
  and tests rely on digest identity, not the marker string, so no bump is needed
  to keep current installs distinguishable.
- **D2 — provider-neutral core, adapter-specific projection.** The shared
  contract names no provider tool. OpenCode names `todowrite` (confirmed
  available this session); Codex stays generic; Pi names `todowrite`.
- **D3 — regression coverage, not observed TDD.** No expected failure was
  observed before the edits, so the added tests are documented as regression
  coverage rather than TDD.
- **D4 — `docs/implementations/` is a documentation convention**, not a runtime
  service. No synchronizer, watcher, or atomic cross-tool state is promised.
- **D5 — Codex adapter kept byte-stable.** The Codex adapter string is shared by
  the current and pre-adaptive bootstrap renderers; editing it would shift the
  historical bootstrap `AGENTS.md` bytes and weaken receiptless legacy
  recognition. Codex therefore carries the planning policy provider-neutrally
  from the shared contract. OpenCode has a separate pre-adaptive adapter, so only
  its current adapter changed.
- **D6 — necessary test updates.** `internal/providers/{opencode,codex}/
  shared_contract_test.go` (scenario count) and `internal/providers/codex/
  current_renderer_test.go` (current `AGENTS.md` golden) were updated because
  the authorized contract/adapter change altered them; the Codex bootstrap
  golden was intentionally left unchanged (D5).
- **D7 — F1 correction (critical).** The unsafe clause "do not block authorized
  work on a pending decision—record an explicit assumption and continue" was
  removed from the shared contract, docs and plan. The contract now requires
  resolving discoverable facts by inspection, asking consequential blocking
  decisions *before* closing the plan or implementing any dependent part,
  continuing only independent authorized work, and restricting explicit
  assumptions to minor reversible defaults. Negative assertions were added so a
  positive match cannot mask the contradiction.
- **D8 — F2 usable schema.** `docs/implementations/README.md` now documents the
  index and states, the canonical `plan.md` sections, `progress.md` without a
  duplicated task list, and `validation.md` including verification/CARE status,
  so the mechanism works in a new project without assuming this repository.
- **D9 — F3 faithful records.** The close-out was split into explicit pending
  independent tasks T13 (verification), T14 (CARE) and T15 (Manager freeze and
  closure); T12 is the correction pass/handoff. Decisions are now recorded here
  only, and the reachable stale Git/exploration-exception sentence in
  `docs/manager-orchestration-skills.md` was corrected within the already-edited
  scope.

- **D10 — C1 correction (embedded schema).** CARE C1 found that the plan-record
  schema existed only in repository docs and was never delivered to an installed
  Manager in a new project. A literal `# Implementation plan records` section is
  now part of the shared `manager.instructions` with the full minimum (index,
  ID/status/path, `<ID-slug>/plan.md` sections, task and plan states, the
  exactly-one-active-while-executing rule, progress/validation contents, and the
  proportional single-file allowance). Provider and Pi tests assert the schema
  in the rendered prompts, not only in the repository README. F-1 is therefore
  addressed by embedding, not by the README.
- **D11 — closure.** The Manager accepted the local declarative
  instructions/tests (not live, install, or delivery) and closed IMP-001 on the
  C2 reviewed candidate (diff SHA `787e53eb0ae4f50a3a6047feb13bed638c9f372b35fec813f87ecbe3736db750`).
  C1 history is retained; the C2 evidence refers to the previously frozen source.
  These post-review records do not re-approve changed documentation bytes, and
  closure metadata does not alter code, contract, schema, tests, or resources.

## Findings history (preserved)

Held on the **previous** candidate and superseded by the corrections; they are
not approval of the current candidate.

- **Verifier C1:** PASS on the tests of the previous candidate.
- **CARE C1:** FAIL (technical). F-1 medium blocked AC01 (schema not delivered);
  F-2 low on AC08 (missing explicit closure-without-inherited-authorization and
  block/interrupt/resume scenarios). Both were corrected rather than retried
  unchanged.
- **Verifier C2 (`verify-plans-20260922-h`):** PASS on the C2 frozen candidate;
  candidate identity before/after exactly equal; suites: 7 Go, `generate
  --check`, `verify-package`, typecheck, Pi 174 pass / 0 fail.
- **CARE C2 (`care-plans-20260922-i`):** PASS AC01–AC08. F-1 (shipped schema) and
  F-2 (corpus) confirmed closed. One low, non-blocking finding **F-3**: the
  summary phrase "exactly one active per session" can be misread as always-one,
  while the specific rule determines zero active after closure or with no work.
  The specific rule controls; recorded as an optional residual, not corrected
  here.

## Blockers

- None. No dependency was reported unavailable.

## Next action

- **None pending.** IMP-001 is closed. T13 verification
  (`verify-plans-20260922-h`, PASS) and T14 CARE (`care-plans-20260922-i`, PASS
  AC01–AC08) reviewed the C2 frozen candidate (diff SHA `787e53eb…`); T15
  recorded the Manager's local declarative acceptance. The only remainder is the
  optional F-3 wording improvement; it is not an active plan and no new plan is
  created automatically.
