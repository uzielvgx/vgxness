# Changelog

All notable changes to this project are documented in this file.

## Unreleased

- Installs 20 managed artifacts, including a semantic merge that sets `default_agent="vgxness-manager"` in `opencode.json` while preserving unrelated JSON values; existing `opencode.jsonc` bytes remain unchanged, and bounded `default-agent.json` restoration metadata records whether `opencode.json` existed and any prior explicit default so uninstall can restore that default or remove a config created by setup.
- Keeps manager v35 and the current 14-agent projection as the only managed agent catalogue, removing historical manager, agent, and model-plan compatibility sources.
- Grants global OpenCode tool permission to manager v35, managed `general`, and verifier while retaining their distinct behavioral roles.
- Adds the independent managed `vgxness-autonomous-stacked-pr` skill for bounded native Git/`gh` delivery after freeze, verification, and review.
- Adds a model-bound, CodeGraph-first `explore` override with deny-by-default read-only permissions.
- Removes the compatibility execution bridge, control plane, Chronicle, provider runner, compatibility commands, schemas, and related documentation, leaving the OpenCode-native manager, storage, setup, inspection, memory, SDD, launcher, and release product.
- Adds an official SHA-256-pinned Homebrew tap for macOS and Linux with explicit Homebrew and managed OpenCode ownership boundaries.
- Adds an official script-free Scoop bucket for Windows amd64 and ARM64 with native Windows installation validation.

## v0.1.0-alpha.1 - 2026-07-29

- Delivers the first bounded alpha of the local VGXNESS control plane, native OpenCode manager projection, semantic memory, structured SDD storage, and compatibility maintenance surfaces.
- Adds versioned self-installation with an immutable content-addressed application binary, a permanent launcher, status inspection, and one-level rollback.
- Adds deterministic build metadata, six unsigned cross-platform release archives, SHA-256 checksums, and tag-driven GitHub release automation.
- Establishes native artifact smoke coverage for Linux amd64 and Windows amd64 while identifying the remaining architectures as preview and compile-only.
