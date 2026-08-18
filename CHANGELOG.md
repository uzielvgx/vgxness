# Changelog

All notable changes to this project are documented in this file.

## Unreleased

- Repairs stale, never-attempted backfill create payloads with a transactional compare-and-swap while preserving mutation identity; never-attempted requires no claim history, and claimed, attempted, retrying, malformed, or concurrently changed queue rows remain rejected.
- Aligns OpenCode manager v48 and generated Codex manager v8 on intent-triggered VGXNESS memory: search all terms first with any-term fallback only when needed, retrieve exact IDs after preview, reserve recent recall for explicit recovery/history requests, and preserve exact v47/v7 predecessor upgrades while protecting drift.
- Preserves cancellation and deadline identities during storage inspection, aligns documentation with SQLite schema v19, bounds sync authentication and failed-audit retention, and makes self-install manifest activation explicitly recoverable without overwriting concurrent content.
- Adds proposed `macos-15` source and release-artifact smoke for `darwin/arm64`; support remains preview until a tag-workflow execution provides native evidence.
- Remediates the Go toolchain and `golang.org/x/text` security dependency floor, and adds proposed tag-release artifact attestations before GitHub release publication.
- Adds a required, separately aggregated CI vulnerability lane pinned to `golang.org/x/vuln/cmd/govulncheck@v1.6.0` and an opt-in `make vuln` target while keeping the network-dependent scan out of `make verify`.
- Restricts `vgxness-syncd` HTTP serving to literal loopback addresses, retires the insecure non-loopback development override while retaining an explicit `false` compatibility no-op, and documents the required external TLS boundary for remote synchronization.
- Installs 18 managed artifacts, including a semantic merge that sets `default_agent="vgxness-manager"` in `opencode.json` while preserving unrelated JSON values; existing `opencode.jsonc` bytes remain unchanged, and bounded `default-agent.json` restoration metadata records whether `opencode.json` existed and any prior explicit default so uninstall can restore that default or remove a config created by setup.
- Updates OpenCode manager v48 and autonomous delivery skill v3: a clean pre-write delivery gate precedes implementation, while explicit reauthorization can recover only a verified unpublished local slice; remote branches and PRs remain read-only.
- Grants global OpenCode tool permission to OpenCode manager v48, managed `general` v6, and verifier v4 while retaining their distinct behavioral roles.
- Upgrades the independent managed `vgxness-autonomous-stacked-pr` skill to v3 with exact v1 and v2 predecessor migration only; modified, malformed, equal-version drifted, and newer content remain protected.
- Adds a model-bound, CodeGraph-first `explore` v2 override with deny-by-default read-only permissions.
- Removes the compatibility execution bridge, control plane, Chronicle, provider runner, compatibility commands, schemas, and related documentation, leaving the OpenCode-native manager, storage, setup, inspection, memory, SDD, launcher, and release product.
- Adds an official SHA-256-pinned Homebrew tap for macOS and Linux with explicit Homebrew and managed OpenCode ownership boundaries.
- Adds an official script-free Scoop bucket for Windows amd64 and ARM64 with native Windows installation validation.

## v0.1.0-alpha.1 - 2026-07-29

- Delivers the first bounded alpha of the local VGXNESS control plane, native OpenCode manager projection, semantic memory, structured SDD storage, and compatibility maintenance surfaces.
- Adds versioned self-installation with an immutable content-addressed application binary, a permanent launcher, status inspection, and one-level rollback.
- Adds deterministic build metadata, six unsigned cross-platform release archives, SHA-256 checksums, and tag-driven GitHub release automation.
- Establishes native artifact smoke coverage for Linux amd64 and Windows amd64 while identifying the remaining architectures as preview and compile-only.
