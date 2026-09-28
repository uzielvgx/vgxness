# IMP-003 — Progress

Authoritative record of decisions, blockers and the next action. Canonical task
state lives in [plan.md](plan.md) and is not duplicated here; evidence lives in
[validation.md](validation.md).

## Decisions

- **D1 — authorization boundary.** Only the VGXNESS Pi integration update was
  authorized: prepare an offline package from exact source and record the plan.
  Out of scope and not performed: source edits, the Pi application,
  OpenCode/Codex/self binaries, `commit`/`push`/PR, lifecycle/installation, durable
  memory, independent verification and CARE.
- **D2 — exact-source, no-edit build.** The candidate came from the clean tree
  (`c4c6450…`) with Pi resources and tests unchanged; no repository source edit was
  made. The only repository writes are these IMP-003 records and the README index.
- **D3 — corrected: two distinct surfaces (CARE B3).** `--pi-release-dir PATH`
  **is** a real flag on the installer `vgxness setup pi` (`internal/cli/setup.go`);
  it is absent **only** on the builder `vgxness-release pi`, which uses `--output`.
  The earlier "does not exist" claim was wrong and is corrected in `plan.md`,
  `validation.md` and here.
- **D4 — offline only.** `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`; no `npm
  pack`, no `npm ci`, no network dependency fetch.
- **D5 — model settings preserved by construction.** No Pi model flags were passed.
- **D6 — no live model call.** No full live-model evaluation claim; the feature was
  exercised by the verified install.
- **D7 — guide validated before reading.** `installer-lifecycle` hashed
  `014d9e04483d0621c2be1aa13cc135775ea0934ece1eb597ceca4999be376ab0` (exact match)
  before reading; no other skill body loaded.
- **D8 — independent artifact verification recorded (T03).** Verifier
  `imp003-verify-20260922-c` PASS: archive 114 members; exact source 113 byte-equal;
  `package.json` semantically equal; tests `174`/two Go packages good; identity
  `e5a501…`/provenance `dc4db00d…` matched.
- **D9 — CARE settled.** Initial CARE `imp003-care-d` was INCONCLUSIVE (B1 shared
  phase, B2 external-directory denial, B3 flag error). CARE then narrowed to a
  **SOURCE policy review, `imp003-care-f` PASS**. B2 is retained as a real
  environment denial (both aliases denied; do not bypass or copy); CARE claimed no
  artifact review.
- **D12 — ordering and no artifact drift.** `sourceSHA256 dc4db00d…` was computed
  over the whole-repo snapshot at build (including initial plan/progress); later
  README/validation edits are docs-excluded from package inputs, so no drift.
- **D13 — T04 install recorded (Manager).** `vgxness setup pi --yes` with the
  explicit canonical artifact root: preview digest
  `d02b4f6361b8f30031b25f9043ad31c3bfac530054301bc75fd6e002cccdd30b` was identical
  on apply; `verified=true`, `changed=true`.
- **D14 — T05 post-verify recorded (independent).** `imp003-postverify-g` PASS:
  byte-exact 114/114, the extra receipt is expected, non-package settings hash
  `813bb7e1745f5994aa7d3e10052ee8a477b6cbd9fe7daa58c576dee4489e0478` unchanged, a
  single packages pointer changed.
- **D15 — path correction (superseded prediction).** The new **actual** package
  path is
  `/Users/uzielvgx/.pi/agent/vgxness-managed/packages/pi-0.1.0-7f7f610445a24ade/package`.
  The earlier `pi-0.1.0-dc4db00da451ea6d` name was a **false prediction**; `dc4…`
  is the whole-repo provenance, **not** the installer content hash `7f7f…`. The
  actual receipt is self-consistent 114. Prior
  `.../packages/pi-0.1.0-5ad49aa8906bf490` is retained, owned, receipt verified.
- **D16 — installed health.** Contract `a890f545…`; installed schema 23; isolated
  Node probe healthy; instructions contain the "at most one" plan rule and
  `todowrite` is present. Node 24.15.0 / Pi 0.85.1 unchanged; `@vgxness/pi` `0.1.0`
  local unpublished (same version, new content; no host application upgrade).
- **D17 — closure.** All tasks done; plan closed; index returns to active **none**;
  IMP-001 and IMP-002 remain closed and preserved. HEAD `c4c6450`; source unchanged;
  these records are local (uncommitted) and no commit was authorized for this new
  plan. Self active `4057…` previous `90a9…` unchanged; skills 46 `installed`,
  `update=false` unchanged (wrapper `c55a…` is a different artifact, no defect).
  Auth not read, no credentials dumped, no model call, no live-behavior guarantee.
  Next action: none required; the user should restart Pi / reload the extension to
  consume the updated package if the app is already open — the running process is
  **not** promised to refresh.

## Findings history (preserved)

- **F1 — contract identity.** `a890f545…` is bound in
  `resources/orchestration/contract.json` and re-derived by
  `generate-contract.mjs`; `--check` confirmed no drift.
- **F2 — verifier/CARE split.** A verifier PASS never substitutes for CARE's
  narrowed SOURCE-policy review; each is preserved with its own scope and limits.
- **F3 — failed literal alias retained.** The `/var` (symlink) output failed;
  `/private/var` canonical succeeded. Retained as history, not erased.

## Blockers

- None. B1 is resolved by the completed source-lifecycle review (`imp003-care-f`);
  B2 is retained as a real environment denial; B3 is corrected. No dependency was
  reported unavailable.

## Next action

- **None required.** Optional user-side step: restart Pi / reload the extension to
  consume the updated package if the app is already open (no running-process
  refresh promised).
