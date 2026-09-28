# IMP-003 — Validation

Self-reported evidence by the worker (build `imp003-prepare-20260922-b`; records
correction `imp003-record-preflight-20260922-e`; closure `imp003-close-20260922-h`).
This file is the writer's record; independent verification and CARE results are
owned by the verifier/reviewer and recorded here, not re-run.

## Candidate identity

- **Source revision:** `c4c6450b779cd39c95a96fc3303bb4cc67ee0291` (branch `main`); no source edit by this plan.
- **Pi Manager-contract `sourceDigest`:** `a890f5453c72f97c30174041b27d28d62d5b60c1bd357b89a754b7d5c60f76ce`.
- **Build artifact provenance `sourceSHA256`:** `dc4db00da451ea6d8bc488b66ff7bd8cb06d6030bd6651887bd9c1f91f39f742` (whole-repo snapshot).
- **Build/preview artifact digest:** `d02b4f6361b8f30031b25f9043ad31c3bfac530054301bc75fd6e002cccdd30b`.
- **Installer content hash:** `7f7f610445a24ade` (installed directory name).
- **Installed package (actual path):** `/Users/uzielvgx/.pi/agent/vgxness-managed/packages/pi-0.1.0-7f7f610445a24ade/package`
- **Prior package (retained):** `/Users/uzielvgx/.pi/agent/vgxness-managed/packages/pi-0.1.0-5ad49aa8906bf490` (owned; receipt verified).
- **Package version:** `@vgxness/pi` `0.1.0`, local unpublished (same version, new content; no host application upgrade).

## Build evidence

- **Build command:** `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go run ./cmd/vgxness-release pi --output <dir>` — exit `0`.
- **Logical path:** `/var/folders/4q/nlfcvv816_l0bh4wsl846_kw0000gn/T/opencode/imp003-pi-release`
- **Canonical path (actually written):** `/private/var/folders/4q/nlfcvv816_l0bh4wsl846_kw0000gn/T/opencode/imp003-pi-release` (same physical directory; see V1).
- Inner files: `vgxness-pi-0.1.0.tgz` `e5a50115849ba237427fa5110ee35232794c8a8a9cf5b1985ade96ef35bdc06c` (152845 B); `SHA256SUMS` `5eb427eeb7e6ff6e55a99338d1124c25f8deae53fa28b4cab663d6a7c8788f28`; `PROVENANCE.json` `5bcb086c06017667e2975b5d21c2bbe5e5ae27d6c2a8fe4a816b8fd212e4c0b2`.

## Checks (worker, T02)

| Check | Command | Result |
| --- | --- | --- |
| Environment | `node --version`; `go version`; `npm --version` | Node `v24.15.0`; go `1.26.6 darwin/arm64`; npm `11.12.1` |
| Contract drift | `node packages/pi/scripts/generate-contract.mjs --check` | PASS (no drift) |
| Package metadata | `node packages/pi/scripts/verify-package.mjs` | PASS |
| Pi typecheck | `npm run typecheck --workspace packages/pi` | exit `0` |
| Pi tests | `npm test --workspace packages/pi` | `tests 174`, `pass 174`, `fail 0` |
| Go suites | `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./internal/providers/pi ./internal/release -count=1` | `ok` both, exit `0` |
| Artifact digests | `shasum -a 256 *` | see above |

No `npm pack`, `npm ci`, pack-check, or network fetch was run.

## Independent verification and CARE

- **Artifact verifier (T03) — `imp003-verify-20260922-c`: PASS.** Archive 114
  members; exact source 113 members byte-equal; `package.json` semantically equal;
  tests `174` and two Go packages good; identity `e5a501…` / provenance
  `dc4db00d…` matched. Verifier-owned.
- **CARE (T03) — narrowed SOURCE policy `imp003-care-f`: PASS.** Initial
  `imp003-care-d` was INCONCLUSIVE. The narrowed review is a **SOURCE-policy**
  pass and claims **no artifact review**. **B2 retained:** the
  `external_directory` denial is a real environment denial — both aliases are
  denied and must not be bypassed or copied. **B3 corrected** (`--pi-release-dir`).
- **CARE limit stated plainly:** the PASS covers the narrowed source policy only;
  it is not a general artifact or runtime guarantee.

## Install and post-install (T04/T05)

- **T04 (Manager):** `vgxness setup pi --yes` with the explicit canonical artifact
  root. Preview digest `d02b4f63…` was identical on apply; `verified=true`,
  `changed=true`.
- **T05 (independent) — `imp003-postverify-g`: PASS.** Byte-exact `114/114`; the
  extra receipt is expected; non-package settings hash
  `813bb7e1745f5994aa7d3e10052ee8a477b6cbd9fe7daa58c576dee4489e0478` unchanged
  (single packages pointer changed).
- **Installed health:** contract `a890f545…`; installed schema 23; isolated Node
  probe healthy; instructions carry the "at most one" plan rule and `todowrite` is
  present.

## Source-tree status (read-only)

HEAD `c4c6450b779cd39c95a96fc3303bb4cc67ee0291`. Only
`docs/implementations/IMP-003-pi-integration-update/` (new) and
`docs/implementations/README.md` are changed. No file under `packages/pi/`,
`internal/`, or any other source path was modified. These records are **local
(uncommitted)**; no commit was authorized for this plan. No Git-clean claim is
made.

## Findings

- **V1 — symlink-ancestor guard vs. macOS `/var`.** The literal `/var/folders/...`
  output failed (`linked path ancestor: /var`; `/var` → `private/var`). The same
  physical directory was written via canonical `/private/var/...`; disclosed, not
  substituted. Retained as history; no staging left behind.
- **V2 — `--pi-release-dir` (CORRECTED, CARE B3).** Real on the installer
  `vgxness setup pi`; absent only on the builder `vgxness-release pi` (uses
  `--output`).
- **V3 — path prediction superseded.** The earlier claimed
  `pi-0.1.0-dc4db00da451ea6d` package path was a **false prediction**; `dc4db00d…`
  is the whole-repo `sourceSHA256`, not the installer content hash. The actual
  installed path is `.../packages/pi-0.1.0-7f7f610445a24ade/package`, and the
  actual receipt is self-consistent 114. Correction recorded, not hidden.

## Limits and closure

- No live model call; no live-behavior guarantee.
- Authentication was not read; no credentials were dumped.
- No source, archive, install, Git, or memory change by this records-only closure.
- Plan **closed**; index returns to active **none**; IMP-001 and IMP-002 remain
  closed. Next action: none required. Optional user step: restart Pi / reload the
  extension to consume the updated package if the app is already open — the running
  process is **not** promised to refresh.
