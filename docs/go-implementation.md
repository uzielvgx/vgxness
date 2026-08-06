# VGXNESS Go implementation architecture

This document describes the delivered OpenCode-native manager product. OpenCode owns engineering execution. Go owns installation, managed artifact generation, storage, inspection, memory and SDD APIs, and terminal surfaces.

## Delivered boundaries

| Area | Responsibility |
| --- | --- |
| `cmd/vgxness`, `internal/app` | Product entrypoint and dependency composition. |
| `internal/cli` | `version`, `status`, `doctor`, `memory`, `sdd`, `integrate`, `self`, and `setup` commands. |
| `internal/tui` | Keyboard-first storage, memory, and confirmation-gated setup UI. |
| `internal/config`, `internal/inspection` | Read-only storage-root, database, and schema-health inspection. |
| `internal/memory` | SQLite/FTS5 schema v5, canonical workspace identity, semantic memory, structured SDD repository, migrations, and retained legacy importer. |
| `internal/sdd` | Native SDD domain, optimistic lifecycle, immutable revisions, model plans, and deterministic OpenSpec render/compare behavior. |
| `internal/providers/opencode` | Manager v44, 14 other model-bound agents (including `general`, verifier, and reviewers v3), plugin v9, model-plan manifest, exact v1/v2/v3 retirement identities, sync plumbing, and the setup handshake. The separate 42-file, 18-skill catalog includes `sdd-lifecycle`, loaded only after explicit SDD acceptance. |
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
- The plugin remains storage-only and filesystem-free for OpenSpec projection.

## OpenCode handshake

Setup validates an absolute existing workspace, resolves `opencode`, and runs a bounded `opencode --version` in that workspace. The probe has stable `healthy`, `unavailable`, and `incompatible` statuses, honors cancellation, requires major version 1, and enforces minimum version 1.18.4. It does not run OpenCode tasks or probe model availability.

## Storage

The default database is `~/.vgxness/memory.db`. Canonical workspace bindings isolate project data in one schema-v5 SQLite database. Explicit `--storage-root` and `--project-local` options remain isolated alternatives.

Semantic observations, references, sessions, and FTS rows are separate from SDD changes, revisions, bindings, idempotency keys, and projection records. A read-only open never migrates the database. Existing older project databases are retained; normal startup does not import or delete them.

## Managed projection

The OpenCode projection contains 19 exact managed artifacts:

- manager v44 with global tool permission and a pre-write delivery gate;
- managed `general`, verifier, and reviewer profiles v3 with global tool permission and distinct implementation/verification roles;
- one CodeGraph-first, deny-by-default read-only `explore` override;
- five hidden read-only reviewers;
- six hidden read-only SDD profiles;
- storage plugin v9;
- one model-plan manifest;
- one `opencode.json` default-agent selection using a semantic merge that preserves unrelated JSON values; existing `opencode.jsonc` bytes remain unchanged;
- one bounded `<config-dir>/vgxness/default-agent.json` restoration record of whether `opencode.json` existed and any prior explicit default, so uninstall can restore that default or remove a config created by setup;

The model plan contains exactly 15 agents and does not contain the skill. Manager, agent, and model-plan recognition is current-only; older versions are preserved as drift and require explicit removal or migration outside the integration. Exact catalogued storage-plugin predecessors remain recognizable. Foreign, modified, equal-version drifted, malformed, and newer content is never overwritten. The deprecated singular `--model` flag remains accepted as a no-op; plan and slot flags own model configuration.

`internal/skills` separately owns the global 42-file, 18-skill `skills-creator`, `stacked-pr`, `cross-platform`, `installer-lifecycle`, `agent-evaluation`, `ci-triage`, `security-boundary`, `documentation-strategy`, `product-requirements`, `software-architecture-docs`, `user-documentation`, `api-documentation`, `quality-test-documentation`, `operations-runbooks`, `governance-compliance-docs`, `release-lifecycle-docs`, `end-to-end-testing`, and `sdd-lifecycle` catalog at `~/.agents/skills`, or an absolute `--skills-dir` override. Official setup retires exact provider `vgxness-autonomous-stacked-pr` v1/v2/v3 bytes before global publication; OpenCode uninstall does not remove global skills. Its selected root is descriptor-anchored with `os.Root`; exact partial packs resume or remove safely, while unknown bytes are drift. Windows retains atomic rename/readback/backups but lacks directory fsync crash durability.

## Verification

The repository validates focused packages, the full test suite, race behavior, E2E setup and native SDD lifecycle, vet, module integrity, trimmed builds, Windows amd64/arm64 builds, and diff whitespace. Tests must not require network access or package installation.
