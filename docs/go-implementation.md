# VGXNESS Go implementation architecture

This document describes the delivered OpenCode-native manager product. OpenCode owns engineering execution. Go owns installation, managed artifact generation, storage, inspection, memory and SDD APIs, and terminal surfaces.

## Delivered boundaries

| Area | Responsibility |
| --- | --- |
| `cmd/vgxness`, `internal/app` | Product entrypoint and dependency composition. |
| `internal/cli` | `version`, `status`, `doctor`, `memory`, `sdd`, `integrate`, `self`, and `setup` commands. |
| `internal/tui` | Keyboard-first storage, memory, and confirmation-gated setup UI. |
| `internal/config`, `internal/inspection` | Read-only storage-root, database, and schema-health inspection. |
| `internal/memory` | SQLite/FTS5 schema v19, canonical workspace identity, explicit portable-to-local provenance, semantic memory, structured SDD repository, migrations, and retained legacy importer. Portable metadata is not normal resolution or sync selection. |
| `internal/hooks` | Internal-only best-effort lifecycle observation. Completed memory synchronization can emit synchronous invocation-correlation events for the effective canonical invocation workspace (empty project directory means current working directory; explicit paths are absolute, clean, symlink-resolved, and case-normalized); listeners can block, and global single-flight drops concurrent or reentrant events. There is no queue, retry, replay, persistence, or crash durability. |
| `internal/sdd` | Native SDD domain, optimistic lifecycle, immutable revisions, model plans, and deterministic OpenSpec render/compare behavior. |
| `internal/providers/opencode` | Manager v48, 14 other model-bound agents (including `general` v6, verifier v4, and reviewers v3), model-plan manifest, exact v47 and older managed predecessors, historical plugin v1–v10 and provider-skill v1/v2/v3 retirement identities, sync plumbing, and the setup handshake. The separate 47-file, 19-skill catalog includes `memory-sync` and `sdd-lifecycle`, the latter loaded only after explicit SDD acceptance. |
| `internal/providers/codex` | Standalone Codex manager v8 lifecycle for `AGENTS.md` and 14 delegated profiles, with exact v7 and older package predecessors and exact `low`, `medium`, `high`, and `ultra` model-plan projections while preserving user-owned `config.toml`. |
| `internal/integration`, `internal/setup`, `internal/skills` | Managed OpenCode lifecycle, independent global portable-skill lifecycle, and seven-step CLI/TUI setup workflow. |
| `internal/launcher`, `internal/selfinstall` | Permanent launcher, immutable SHA-256 application versions, atomic activation, and one-level rollback. |
| `internal/release`, `cmd/vgxness-release` | Deterministic archives, checksums, release metadata, and workflow support. |

Compatibility execution packages and commands are not delivered. There is no Go provider runner, execution adapter, bridge, control plane, Chronicle, Gatekeeper, registry, prompt composer, coordinator, stack engine, worktree writer, delivery-state service, custom Git/GitHub tool, or runtime contract-schema layer. Native delivery policy lives only in the installed manager and skill.

## Dependency rules

- `cmd/vgxness` depends on `internal/app`; it is not a second composition root.
- The CLI and TUI depend on narrow consumer-owned interfaces.
- Storage and SDD code do not depend on terminal presentation.
- OpenCode integration generation does not execute engineering work.
- Setup composes self-installation, integration, and a narrow OpenCode prober.
- Read-only inspection never creates, migrates, or repairs storage.
- MCP operations are filesystem-free for OpenSpec projection. Lifecycle observation does not expand MCP or provider boundaries, and it does not expose synchronization data scope.

## OpenCode handshake

Setup validates an absolute existing workspace, resolves `opencode`, and runs a bounded `opencode --version` in that workspace. The probe has stable `healthy`, `unavailable`, and `incompatible` statuses, honors cancellation, requires major version 1, and enforces minimum version 1.18.4. It does not run OpenCode tasks or probe model availability.

## Storage

The default database is `~/.vgxness/memory.db`. Canonical workspace bindings isolate project data in one schema-v19 SQLite database. Explicit `--storage-root` and `--project-local` options remain isolated alternatives.

Semantic observations, references, sessions, and FTS rows are separate from SDD changes, revisions, bindings, idempotency keys, and projection records. A read-only open never migrates the database. Existing older project databases are retained; normal startup does not import or delete them.

## Managed projection

The OpenCode projection contains 18 exact managed artifacts:

- manager v48 with global tool permission and a pre-write delivery gate;
- managed `general` v6, verifier v4, and reviewer profiles v3 with global tool permission and distinct implementation/verification roles;
- one CodeGraph-first, deny-by-default read-only `explore` override;
- five hidden read-only reviewers;
- six hidden read-only SDD profiles;
- MCP configuration using `vgxness mcp --full` (no installed plugin).
- one model-plan manifest;
- one `opencode.json` default-agent selection using a semantic merge that preserves unrelated JSON values; existing `opencode.jsonc` bytes remain unchanged;
- one bounded `<config-dir>/vgxness/default-agent.json` restoration record of whether `opencode.json` existed and any prior explicit default, so uninstall can restore that default or remove a config created by setup;

The model plan contains exactly 15 agents and does not contain the skill. Exact manager v47 and older catalogued manager, agent, and model-plan predecessors can be upgraded; exact Codex v7 and older package identities are likewise recognized. Unknown, foreign, modified, equal-version drifted, malformed, and newer content is never overwritten. Exact catalogued storage-plugin predecessors remain recognizable. The deprecated singular `--model` flag remains accepted as a no-op; plan and slot flags own model configuration.

`internal/skills` separately owns the global 47-file, 19-skill `skills-creator`, `stacked-pr`, `cross-platform`, `installer-lifecycle`, `agent-evaluation`, `ci-triage`, `security-boundary`, `documentation-strategy`, `product-requirements`, `software-architecture-docs`, `user-documentation`, `api-documentation`, `quality-test-documentation`, `operations-runbooks`, `governance-compliance-docs`, `release-lifecycle-docs`, `end-to-end-testing`, `memory-sync`, and `sdd-lifecycle` catalog at `~/.agents/skills`, or an absolute `--skills-dir` override. Official setup retires only exact `vgxness.ts` v1-v10 plugin bytes and provider `vgxness-autonomous-stacked-pr` v1/v2/v3 bytes before global publication; modified, malformed, foreign, unknown, or newer bytes block without removal. OpenCode uninstall does not remove global skills. Its selected root is descriptor-anchored with `os.Root`; exact partial packs resume or remove safely, while unknown bytes are drift. Windows retains atomic rename/readback/backups but lacks directory fsync crash durability.

## Verification

The repository validates focused packages, the full test suite, race behavior, E2E setup and native SDD lifecycle, vet, module integrity, trimmed builds, Windows amd64/arm64 builds, and diff whitespace. Tests must not require network access or package installation.
