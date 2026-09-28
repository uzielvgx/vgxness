# IMP-003 — VGXNESS Pi integration update (offline candidate from exact source)

- **Status:** closed (T01–T05 done; verified installation accepted by the Manager)
- **Owner:** Manager
- **Workspace:** `/Users/uzielvgx/Development/projects/vgxness`
- **Historical baseline:** `c4c6450b779cd39c95a96fc3303bb4cc67ee0291` (reported clean at plan start; source unchanged by this plan)
- **Installed package (actual):** `/Users/uzielvgx/.pi/agent/vgxness-managed/packages/pi-0.1.0-7f7f610445a24ade/package` (installer content hash `7f7f…`; preview/apply artifact digest `d02b4f6361b8f30031b25f9043ad31c3bfac530054301bc75fd6e002cccdd30b`)
- **Source contract identity:** `a890f5453c72f97c30174041b27d28d62d5b60c1bd357b89a754b7d5c60f76ce` (Pi Manager-contract `sourceDigest`)
- **Writer nonces:** `imp003-prepare-20260922-b` (build); `imp003-record-preflight-20260922-e` (records correction); `imp003-close-20260922-h` (closure)
- **Created:** 2026-09-22
- **Closed:** 2026-09-22 (verifier `imp003-verify-20260922-c` PASS; CARE `imp003-care-d`→`imp003-care-f` settled; post-verify `imp003-postverify-g` PASS)

## Authorization and scope

The user authorized an update of the VGXNESS **Pi integration**: prepare an
offline Pi package built from the exact current repository source (Pi contract
resources and tests, **no source edits**) and record the plan. The Manager
performed installation/lifecycle and authorized closure on the verified install.
Not authorized for this worker: any Pi application change, any
OpenCode/Codex/self-binary change, `commit`/`push`/PR, lifecycle execution,
durable memory, or independent verification.

- In scope: the `docs/implementations/IMP-003-pi-integration-update/` records and
  the README index; the offline candidate produced by
  `go run ./cmd/vgxness-release pi --output <dir>`; the permitted developmental
  checks.
- Out of scope: source-code edits, the Pi application, the OpenCode/Codex/self
  binaries, network/`npm ci`, Git delivery, installation (T04), durable memory,
  independent verification and CARE (T03).
- This worker performed no lifecycle, Git delivery, memory, or independent
  verification action.

## Objective

Produce and install an offline VGXNESS Pi integration candidate built from the
exact current source tree, so the installed Pi integration matches the current
Manager contract prompt/planning/todo resources, while the installer preflight
retains ownership of prior managed files and preserves authentication/model
settings and other providers.

## Scope and exclusions

- In: the IMP-003 records; the offline package and its `PROVENANCE.json` /
  `SHA256SUMS` evidence; the allowed development checks; the closed-plan history.
- Out: any source change; the Pi application runtime; OpenCode, Codex and self
  binaries; `npm ci`/network dependency fetching; `commit`/`push`/PR.

## Decisions and assumptions

Decisions are authoritative in [progress.md](progress.md). Reading summary:

- The Pi Manager-contract resource and its prompt projection are validated, not
  hand-edited.
- The offline package was built with `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`
  via the `cmd/vgxness-release pi` builder (`--output`), writing the three inner
  files and never overwriting an existing output.
- `--pi-release-dir PATH` is a real flag on the **installer** `vgxness setup pi`;
  it is absent only on the **builder** `vgxness-release pi` (uses `--output`).
- The installed content directory is `pi-0.1.0-7f7f610445a24ade`; the earlier
  `pi-0.1.0-dc4db00da451ea6d` name was a **false prediction**. `dc4db00d…` is the
  whole-repo `sourceSHA256`, not the installer content hash.

## Acceptance criteria

- **AC01** The offline Pi package was built from the exact source at `c4c6450`
  with the contract digest `a890f545…`; no source edit by this worker.
- **AC02** (Manager) Installer preflight owned the prior managed files with no
  unowned drift and left app authentication/model settings and other providers
  untouched.
- **AC03** (Manager) Candidate identity plus post-install readback show a healthy
  installed state whose actual prompt schema and `todowrite` `plan` are at most
  one, with the prior installed package retained.
- **AC04** Outcomes and limits are recorded; **no full live-model evaluation**
  claim is made, and no live model call was required.

## Tasks

Task state is authoritative in this table. Decisions are authoritative in
[progress.md](progress.md); evidence is in [validation.md](validation.md).

| ID | Task | AC | State | Dependencies | Required evidence |
| --- | --- | --- | --- | --- | --- |
| T01 | Research: scope, canonical Pi sources, contract digest, build path, no-source-edit boundary | AC01 | done | — | source map, contract digest, clean-tree check |
| T02 | Build offline package and run the permitted checks | AC01, AC04 | done | T01 | build path, digests `e5a501…`/`dc4db00d…`, check results |
| T03 | Independent verification and applicable CARE | AC01, AC04 | done | T02 | verifier `imp003-verify-20260922-c` PASS (artifact); CARE `imp003-care-f` PASS (narrowed SOURCE policy) |
| T04 | Manager installation/lifecycle | AC02, AC03 | done | T03 | `vgxness setup pi --yes` explicit root; preview digest `d02b4f63…` same on apply; `verified=true`, `changed=true` |
| T05 | Post-install verification and closure | AC03, AC04 | done | T04 | `imp003-postverify-g` PASS byte-exact 114/114; settings non-package hash unchanged; single packages pointer changed; closure recorded |

T03–T05 were Manager/verifier/CARE-owned; this worker recorded them. The plan is
**closed**.

## Open assumptions

- Host facts taken as given: Node 24.15.0; Pi 0.85.1; integration `@vgxness/pi`
  `0.1.0` local unpublished (same version, new content; no host application
  upgrade).
- Authentication was not read and no credentials were dumped.
- No live model call was made; no live-behavior guarantee is claimed.

## Closure and residual

- **Closure:** Manager local declarative acceptance of the verified install; not a
  runtime guarantee.
- **Correction retained:** the `dc4db00d…` package-path prediction was false and is
  superseded; the actual installed path is
  `.../vgxness-managed/packages/pi-0.1.0-7f7f610445a24ade/package`.
- **History retained:** the failed literal `/var` alias attempt and its canonical
  `/private/var` resolution, and the `--pi-release-dir` builder-vs-installer
  correction, are preserved (see [validation.md](validation.md)).
- **Provenance:** HEAD `c4c6450`; source unchanged; these records are local
  (uncommitted) and no commit was authorized for this plan.
- **Guides:** `installer-lifecycle` (validated against source before reading); no
  other skill body loaded.
