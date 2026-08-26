# Changelog

All notable changes to this project are documented in this file.

## Unreleased

- Replaces active global `stacked-pr` v3 with `git-delivery` v1, retaining exact migration and policy-only isolated-worktree gates. OpenCode advances to CARE-v2 Manager59 (immediate Manager58); Codex advances to Manager18 (immediate Manager17).

- Earlier in this unreleased cycle, upgrades OpenCode CARE reviewer, specialist, and challenger prompts to v2 while retaining fixed exact CARE-v1 snapshots. OpenCode current was CARE v2 with Manager58; OpenCode immediate predecessor was CARE-v1 with Manager58; OpenCode next predecessor was CARE-v1 with Manager57; OpenCode v56 is deeper. Codex current was Manager17, with immediate Manager16 and deeper Manager15/v14 identities separate.
- Recognizes exact accepted or previously accepted project-pull echoes from durable push receipts after portable-ID mapping, preserving local project, session, and observation create/update state while atomically advancing the project inbox and cursor; receipt mismatches, foreign creates, conflicts, and active transitions retain fail-closed handling.
- Repairs stale, never-attempted backfill create payloads with a transactional compare-and-swap while preserving mutation identity; never-attempted requires no claim history, and claimed, attempted, retrying, malformed, or concurrently changed queue rows remain rejected.
- Projects the shared adaptive route and attempt-budget contract into OpenCode's 16 managed artifacts: manager v58, `general` v10, `explore` v4, and verifier v7; Codex manager v17 provides the equivalent provider-native contract without renaming the default agent. The exact OpenCode v57 and Codex v16 manager artifacts are immediate predecessors. The current CARE matrix includes reviewer, specialist, and challenger roles. Direct no-effect work avoids execution ceremony, bounded reads and research have explicit ceilings, action/engineering/assured routes preserve authorization and assurance, and memory remains orthogonal. Exact immediately preceding manager artifacts retain intent-triggered recall semantics; external holdout, live-provider, and NLP evaluations remain pending.
- Preserves cancellation and deadline identities during storage inspection, aligns documentation with SQLite schema v19, bounds sync authentication and failed-audit retention, and makes self-install manifest activation explicitly recoverable without overwriting concurrent content.
- Adds proposed `macos-15` source and release-artifact smoke for `darwin/arm64`; support remains preview until a tag-workflow execution provides native evidence.
- Remediates the Go toolchain and `golang.org/x/text` security dependency floor, and adds proposed tag-release artifact attestations before GitHub release publication.
- Adds a required, separately aggregated CI vulnerability lane pinned to `golang.org/x/vuln/cmd/govulncheck@v1.6.0` and an opt-in `make vuln` target while keeping the network-dependent scan out of `make verify`.
- Restricts `vgxness-syncd` HTTP serving to literal loopback addresses, retires the insecure non-loopback development override while retaining an explicit `false` compatibility no-op, and documents the required external TLS boundary for remote synchronization.
- Earlier in this unreleased cycle, installed 18 managed artifacts, including a semantic merge that sets `default_agent="vgxness-manager"` in `opencode.json` while preserving unrelated JSON values; existing `opencode.jsonc` bytes remain unchanged, and bounded `default-agent.json` restoration metadata records whether `opencode.json` existed and any prior explicit default so uninstall can restore that default or remove a config created by setup.
- Earlier in this unreleased cycle, updated OpenCode manager v49 and autonomous delivery skill v3: a clean pre-write delivery gate preceded implementation, while explicit reauthorization could recover only a verified unpublished local slice; remote branches and PRs remained read-only.
- Earlier in this unreleased cycle, granted global OpenCode tool permission to OpenCode manager v49, managed `general` v6, and verifier v4 while retaining their distinct behavioral roles.
- Upgrades the independent managed `vgxness-autonomous-stacked-pr` skill to v3 with exact v1 and v2 predecessor migration only; modified, malformed, equal-version drifted, and newer content remain protected.
- Earlier in this unreleased cycle, added a model-bound, CodeGraph-first `explore` v2 override with deny-by-default read-only permissions.
- Removes the compatibility execution bridge, control plane, Chronicle, provider runner, compatibility commands, schemas, and related documentation, leaving the OpenCode-native manager, storage, setup, inspection, memory, SDD, launcher, and release product.
- Adds an official SHA-256-pinned Homebrew tap for macOS and Linux with explicit Homebrew and managed OpenCode ownership boundaries.
- Adds an official script-free Scoop bucket for Windows amd64 and ARM64 with native Windows installation validation.

## v0.1.0-alpha.1 - 2026-07-29

- Delivers the first bounded alpha of the local VGXNESS control plane, native OpenCode manager projection, semantic memory, structured SDD storage, and compatibility maintenance surfaces.
- Adds versioned self-installation with an immutable content-addressed application binary, a permanent launcher, status inspection, and one-level rollback.
- Adds deterministic build metadata, six unsigned cross-platform release archives, SHA-256 checksums, and tag-driven GitHub release automation.
- Establishes native artifact smoke coverage for Linux amd64 and Windows amd64 while identifying the remaining architectures as preview and compile-only.
