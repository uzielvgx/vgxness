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
| `internal/providers/opencode` | Manager v35, 14 other model-bound agents, independent autonomous stacked-PR skill, plugin v5, model-plan manifest, current-only agent recognition, storage-plugin predecessor recognition, sync plumbing, and the setup handshake. |
| `internal/integration`, `internal/setup` | Managed artifact lifecycle and six-step CLI/TUI setup workflow. |
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

The OpenCode projection contains 18 exact managed artifacts:

- manager v35 with global tool permission;
- managed `general` and verifier profiles with global tool permission and distinct implementation/verification roles;
- one CodeGraph-first, deny-by-default read-only `explore` override;
- five hidden read-only reviewers;
- six hidden read-only SDD profiles;
- storage plugin v5;
- one model-plan manifest;
- one independent `vgxness-autonomous-stacked-pr` skill.

The model plan contains exactly 15 agents and does not contain the skill. Manager, agent, and model-plan recognition is current-only; older versions are preserved as drift and require explicit removal or migration outside the integration. Exact catalogued storage-plugin predecessors remain recognizable. Foreign, modified, equal-version drifted, malformed, and newer content is never overwritten. The deprecated singular `--model` flag remains accepted as a no-op; plan and slot flags own model configuration.

## Verification

The repository validates focused packages, the full test suite, race behavior, E2E setup and native SDD lifecycle, vet, module integrity, trimmed builds, Windows amd64/arm64 builds, and diff whitespace. Tests must not require network access or package installation.
