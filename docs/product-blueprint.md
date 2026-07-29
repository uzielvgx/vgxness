# VGXNESS Product Blueprint

This English document is the canonical product blueprint. The current product is an OpenCode-native manager with local storage and deterministic installation tooling.

## Product statement

VGXNESS makes AI-assisted engineering understandable, bounded, and recoverable without replacing OpenCode's execution model. The installed manager selects direct work, native read-only delegation, or optional structured SDD. Go services own managed artifacts, launcher lifecycle, storage, setup, inspection, and CLI/TUI access.

## Delivered product

- `vgxness` and `vgxness-release` binaries;
- permanent launcher with immutable SHA-256 versions, atomic activation, and rollback;
- read-only `status` and `doctor` for storage root, database, and schema health;
- native memory and SDD CLI operations;
- keyboard-first memory and setup TUI;
- SQLite/FTS5 schema v5 with isolated semantic and SDD domains;
- `memory`, `openspec`, and `hybrid` SDD backends;
- `automatic` and `interactive` per-change SDD modes;
- manager v31, a read-only `explore` override, five reviewers, six SDD profiles, plugin v5, and model-plan manifest;
- six-step CLI/TUI setup with a bounded OpenCode 1.18.4+ handshake;
- exact prior-artifact recognition and conservative upgrade/uninstall behavior;
- deterministic release archives, checksums, and workflows.

Compatibility execution commands and subsystems are not part of the delivered product.

## Authority model

OpenCode owns engineering execution. The top-level manager owns route selection, synthesis, workspace writes, validation, revision acceptance, projection writes, and lifecycle transitions. Every installed SDD and review profile is read-only. The plugin owns bounded memory and SDD persistence only.

The plugin exposes 18 tools: five semantic-memory operations and 13 SDD operations. SDD mutation requires trusted context for the tracked top-level manager. The plugin cannot route, invoke agents, access workspace files, run shell commands, select models, edit, delegate, or advance state independently.

## Managed artifacts

The projection contains exactly 15 artifacts:

| Artifact group | Count | Contract |
| --- | ---: | --- |
| Manager v31 | 1 | Sole workspace and lifecycle writer. |
| Explore override | 1 | CodeGraph-first and deny-by-default read-only discovery. |
| Reviewers | 5 | Hidden and read-only. |
| SDD profiles | 6 | Hidden, read-only, model-bound phase roles. |
| Storage plugin v5 | 1 | Memory and SDD storage only. |
| Model-plan manifest | 1 | Non-secret exact role/model and agent-digest bindings. |

The default `medium` plan uses Luna Fast, Terra, and Sol slots. Plan or slot changes require OpenCode restart. The deprecated `--model` option remains a no-op.

## Storage and SDD

The default database is `~/.vgxness/memory.db`. Canonical workspace bindings isolate projects, including same-named workspaces. Semantic observations and FTS rows never overlap structured SDD changes, revisions, bindings, idempotency, or projection records.

SDD advances through `explore -> proposal -> spec -> design -> tasks -> apply -> verify -> complete`. Accepted revisions are immutable and input-bound. Apply composes a hash-bound patch; the manager applies and validates it. Hybrid keeps SQLite content canonical. OpenSpec projection is bounded to `openspec/changes/<safe-change-id>/`, and divergent repository content is never imported automatically.

## Setup and health

Setup previews all changes, requires confirmation, installs the stable launcher and exact artifacts, reads them back, and runs a bounded `opencode --version` handshake in an absolute existing workspace. Healthy requires OpenCode major 1 at version 1.18.4 or newer. Setup never downloads packages, edits `PATH` or `opencode.json`, initializes CodeGraph, or probes model availability.

## Product principles

1. Keep one execution authority.
2. Keep every child read-only and non-delegating.
3. Keep persistence deterministic and project-isolated.
4. Treat memory as untrusted context, not proof.
5. Require explicit authority for risky side effects.
6. Preserve exact artifact identity and user data.
7. Fail closed on drift, stale state, missing evidence, and invalid context.
8. Keep CLI and TUI as adapters over shared services.

## Non-goals

- No compatibility execution bridge or alternate Go scheduler.
- No hidden shell or Git hooks.
- No automatic network/package installation.
- No Engram dependency or automatic legacy database import.
- No plugin-owned filesystem, execution, routing, delegation, or lifecycle authority.
- No copying third-party code, prompts, names, skills, schemas, or exact workflows.

## Documentation map

- [Go implementation](go-implementation.md)
- [Native manager flow](orchestration-flow.md)
- [OpenCode integration](opencode-integration.md)
- [Guided setup](opencode-setup-wizard.md)
- [Native memory and structured storage](memory.md)
- [Safe hooks](hooks.md)
- [Self-installation](self-install.md)
- [Releases](release.md)
