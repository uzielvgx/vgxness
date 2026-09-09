# V1 readiness audit

This source-backed audit records the local implementation and the evidence still needed for release. It does **not** declare v1 ready, independently verified, released or installed on the user's hosts. Issue status below is an audit classification; no GitHub issue is closed by this document.

## Acceptance and implementation

| Criterion | Implementation and reproducible check | Evidence still required |
| --- | --- | --- |
| U1: safe skill upgrades | Complete predecessor identities and modified/mixed package regressions in `internal/skills`; `go test ./internal/skills` | Independent review of the frozen candidate |
| U2: obsolete owned MCP | Explicit proof-bound preview/repair and conflict-preserving recovery in `internal/providers/opencode/mcp_repair.go`; CLI tests | Independent review; no automatic repair of foreign entries |
| D1: distribution | Strict portable envelope in `internal/piartifact`, bundle builder in `internal/release`, and tag workflow asset contract | Successful exact-tag workflow and native artifact evidence; no publication performed here |
| D2: provisioning | Pinned acquisition, explicit offline directory, `setup all` includes three providers; real bundle/acquire/install and idempotence fixtures | Native target runs; package health does not establish model authentication |
| Q1: reproducibility | Locked local TypeScript/SDK dependencies, full Pi test discovery and six Node/OS CI combinations | Observed remote matrix results |
| Q2: backup toolchain | Go 1.26.6 toolchain pin and focused backup regression | Preserve matching toolchain in validation; no weakening of backup checks |
| E1: behavioral evaluation | Nine development cases, normalized evidence validator and offline regressions in `tools/agent_eval` | Independent per-provider/model runs, retained traces and grading; synthetic tests are not behavioral passes |
| S1: support and trust | Current shared Manager identities, explicit platform/trust limits and individual issue reconciliation | Accountable release acceptance after required evidence is available |
| Pi C1: continuity | Expired leases reject mutations; periodic renewal, abort guards and session regressions | Independent exact-candidate verification |
| Pi W1: exploration | Mission-bound cursors, scope and budget ledger checks | Independent review of scope and replay boundaries |
| Pi R1: results/recovery | Bounded single terminal result, cancellation, reaping and retained recovery; actual SDK transport fixtures | Real selected-model behavior and target-native process evidence |
| Pi V1: views | Read-only loading/available/stale/unavailable views with bounded memory provenance and worker results | Independent review; no broad observability claim |
| Pi I1: independence/shared DB | TypeScript/Node runtime, existing SQLite migrations and memory contract; isolated package/SDK journey | No Go/VGXNESS process required by Pi runtime; cross-host checks remain separate |
| Pi P1: platforms | Temporary installation and SDK load; native platform CI configuration | Observed macOS/Windows runs; Windows workers remain unsupported |

## Open issue reconciliation

The following 14 issues were examined individually during this audit. Historical requirements are distinguished from current defects; a superseded design alone does not prove the original acceptance criteria passed.

| Issue | Audit status | Finding and source |
| --- | --- | --- |
| [#268](https://github.com/uzielvgx/vgxness/issues/268) | pending enhancement | Current native git-delivery replaced historical stacked-pr. No current v1 evidence proves all requested large-dirty snapshot/reconstruction criteria. Source: [`internal/skills/pack/git-delivery/SKILL.md`](../internal/skills/pack/git-delivery/SKILL.md). |
| [#204](https://github.com/uzielvgx/vgxness/issues/204) | implemented locally; verification pending | Explicit owned-MCP preview/apply proof and conflict-preserving repair implemented in this candidate. Source: [`internal/providers/opencode/mcp_repair.go`](../internal/providers/opencode/mcp_repair.go). |
| [#78](https://github.com/uzielvgx/vgxness/issues/78) | pending behavioral evidence | Deterministic contract tests and completed-ungraded development transport do not satisfy real independently graded trace criteria. Source: [`tools/agent_eval/runner.py`](../tools/agent_eval/runner.py). |
| [#77](https://github.com/uzielvgx/vgxness/issues/77) | historical schema superseded; behavioral evidence pending | Shared CARE role instructions replace frozen legacy reviewer mode schema. No current first-mission behavioral trace proves closure. Source: [`internal/orchestration/manager_contract.json`](../internal/orchestration/manager_contract.json). |
| [#76](https://github.com/uzielvgx/vgxness/issues/76) | historical routing superseded; evidence pending | Current Manager can inspect command evidence; explore remains read-only. Historical no-Manager-command policy is not current source. Never fabricate child capability. Source: [`internal/orchestration/manager_contract.json`](../internal/orchestration/manager_contract.json). |
| [#75](https://github.com/uzielvgx/vgxness/issues/75) | pending behavioral evidence | Current freeze/readback rules are present; repository-required validation omission needs an actual trace regression on current candidate. Source: [`internal/orchestration/manager_contract.json`](../internal/orchestration/manager_contract.json). |
| [#74](https://github.com/uzielvgx/vgxness/issues/74) | historical policy superseded; evidence pending | Current contract preserves unrelated work and binds candidate. Historical absolute dirty-start stop is not an enforced current runtime gate. No proof of original criteria closure. Source: [`internal/orchestration/manager_contract.json`](../internal/orchestration/manager_contract.json). |
| [#72](https://github.com/uzielvgx/vgxness/issues/72) | historical schema superseded; evidence pending | Current shared SDD research role returns read-only evidence, without contradictory artifact research schema. Existing lifecycle canonical explore remains. Source: [`internal/orchestration/manager_contract.json`](../internal/orchestration/manager_contract.json). |
| [#71](https://github.com/uzielvgx/vgxness/issues/71) | pending refactor | Reinstall transaction still exists with ownership/recovery tests; maintainability request is not a newly reproduced defect. Source: [`internal/providers/opencode/integration.go`](../internal/providers/opencode/integration.go). |
| [#70](https://github.com/uzielvgx/vgxness/issues/70) | historical plugin retired | Current setup retires exact legacy vgxness.ts bytes and uses native MCP plus lifecycle-only plugin. Do not refactor retired implementation into active runtime. Source: [`internal/setup/setup.go`](../internal/setup/setup.go). |
| [#69](https://github.com/uzielvgx/vgxness/issues/69) | pending refactor | Plan and Status retain distinct methods and shared components; full requested state-table/refactor closure not established. Source: [`internal/setup/setup.go`](../internal/setup/setup.go). |
| [#67](https://github.com/uzielvgx/vgxness/issues/67) | existing implementation; final verification pending | Rooted manifest recovery and concurrent substitution/post-publication cancellation regression tests already exist in baseline. Source: [`internal/selfinstall/selfinstall_test.go`](../internal/selfinstall/selfinstall_test.go). |
| [#65](https://github.com/uzielvgx/vgxness/issues/65) | partially addressed; residual boundary open | Rooted ancestor walk rejects symlinks and binds identity, but existing ancestry owner/ACL/write-permission checks and verified-to-exec identity guarantee not established. Source: [`internal/selfinstall/selfinstall.go`](../internal/selfinstall/selfinstall.go). |
| [#64](https://github.com/uzielvgx/vgxness/issues/64) | unsupported threat model; open | Prompt permissions and argv/target controls do not provide OS isolation for untrusted repository code; v1 must state trusted-repository prerequisite. Source: [`internal/orchestration/manager_contract.json`](../internal/orchestration/manager_contract.json). |

## Trust and evidence limits

Repositories, hosts and release publishers must be trusted. Prompt instructions, exact argv, role allowlists, hashes and target checks do not provide OS isolation for arbitrary repository code. Issue #64 remains an unsupported threat model. For #65, rooted identity and symlink checks exist, but complete ownership/ACL/write-permission checks of existing ancestry and a verification-to-execution identity guarantee are not established. Attacker-writable custom ancestry remains a residual boundary, not a demonstrated compromise of every default installation.

The downloader uses the fixed GitHub release origin, TLS, bounded parsing and checksums from that origin. This provides integrity relative to the trusted publisher, not an independent signature. Automatic attestation verification is not implemented. Acquisition cleanup removes only identified files through the held directory root and never recursively removes a mutable path. It preserves observed replacements, changed files and extra entries, reporting retained recovery state. Portable filesystems do not provide atomic compare-identity/content-and-unlink against another same-UID process; a replacement in that final per-file check/unlink window (or an empty-directory replacement before final nonrecursive removal) remains outside this trusted-host boundary.

Local development checks have been exercised on Linux ARM64. The Node/OS matrix and release-native workflows are configured, not evidence of a successful run of this candidate on every platform. Windows worker process ownership is explicitly unavailable. A portable archive or cross-build does not promote native support. Previous model evaluations on other candidates cannot certify this one; protected holdouts remain with their independent owner.

## SDD and release gates

The accepted umbrella is `change-1c725f7c8d4ef034196aa8629325ccd0`; its Pi dependency is `change-45acf811fa4602b0d46f5c077f8003b1`. The Pi dependency must supply accepted verification before umbrella completion. Current canonical phase/revision identities live in SDD memory; this document does not replace those records.

Freeze one candidate with HEAD, full tracked/untracked file digests and diff scope. Run repository-required validation, independent verification and applicable reviews against that identity. Record any unsupported native transport, unavailable platform or missing model/grade evidence as pending. A source change invalidates earlier candidate evidence. Publication, merge, live installation and cloud synchronization are separate authorized operations and are excluded from this implementation scope.
