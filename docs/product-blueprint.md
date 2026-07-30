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
- manager v35, 14 other model-bound agents, plugin v5, model-plan manifest, and one independent autonomous stacked-PR skill;
- six-step CLI/TUI setup with a bounded OpenCode 1.18.4+ handshake;
- current-only manager and agent recognition, exact storage-plugin predecessor recognition, and conservative uninstall behavior;
- deterministic release archives, checksums, and workflows.

Compatibility execution commands and subsystems are not part of the delivered product.

## Authority model

OpenCode owns engineering execution. Manager, managed `general`, and verifier have global tool permission; their prompts retain orchestration, delegated implementation, and non-mutating verification roles. The top-level manager owns route selection, synthesis, candidate acceptance, revision acceptance, and lifecycle transitions. Every installed SDD and review profile remains read-only. The plugin owns bounded memory and SDD persistence only.

The plugin exposes 18 tools: five semantic-memory operations and 13 SDD operations. SDD mutation requires trusted context for the tracked top-level manager. The plugin cannot route, invoke agents, access workspace files, run shell commands, select models, edit, delegate, or advance state independently.

## Managed artifacts

The projection contains exactly 20 artifacts:

| Artifact group | Count | Contract |
| --- | ---: | --- |
| Manager v35 | 1 | Sole orchestration, lifecycle, Git, and GitHub actor. |
| Explore override | 1 | CodeGraph-first and deny-by-default read-only discovery. |
| General and verifier | 2 | Global capability with delegated implementation and independent non-mutating validation roles. |
| Reviewers | 5 | Hidden and read-only. |
| SDD profiles | 6 | Hidden, read-only, model-bound phase roles. |
| Storage plugin v5 | 1 | Memory and SDD storage only. |
| Model-plan manifest | 1 | Non-secret exact role/model and agent-digest bindings. |
| Default-agent selection | 1 | Semantic merge sets `default_agent="vgxness-manager"` in `opencode.json` while preserving unrelated JSON values; existing `opencode.jsonc` bytes remain unchanged. |
| Restoration metadata | 1 | Bounded `<config-dir>/vgxness/default-agent.json` records whether `opencode.json` existed and any prior explicit default, so uninstall can restore that default or remove a config created by setup. |
| Autonomous stacked-PR skill | 1 | Independent static native-delivery policy; not model-bound. |

The default `medium` plan uses Luna Fast, Terra, and Sol slots. Plan or slot changes require OpenCode restart. The deprecated `--model` option remains a no-op.

Eligible implementations default to 400 effective changed lines per slice and stack only above 800. After freeze, verification, and review, manager v35 may create fresh normalized branches, normal commits, first pushes, and non-draft pull requests without a second routine approval. Explicit task restrictions narrow transitively. Existing delivery state is read-only, cleanup is never automatic, and post-create mutation, worktree mutation, history rewriting, merge, force, release, credential, and configuration changes remain unsupported. OpenCode globs establish static policy ordering but do not prove argv semantics or external Git/GitHub behavior.

## Storage and SDD

The default database is `~/.vgxness/memory.db`. Canonical workspace bindings isolate projects, including same-named workspaces. Semantic observations and FTS rows never overlap structured SDD changes, revisions, bindings, idempotency, or projection records.

SDD advances through `explore -> proposal -> spec -> design -> tasks -> apply -> verify -> complete`. Accepted revisions are immutable and input-bound. Apply composes a hash-bound patch; the manager applies and validates it. Hybrid keeps SQLite content canonical. OpenSpec projection is bounded to `openspec/changes/<safe-change-id>/`, and divergent repository content is never imported automatically.

## Setup and health

Setup previews all changes, requires confirmation, installs the stable launcher and exact artifacts, reads them back, and runs a bounded `opencode --version` handshake in an absolute existing workspace. Healthy requires OpenCode major 1 at version 1.18.4 or newer. Setup never downloads packages, edits `PATH`, initializes CodeGraph, or probes model availability. It uses a semantic merge to select `vgxness-manager` through `opencode.json` while preserving unrelated JSON values; existing `opencode.jsonc` bytes remain unchanged. Bounded `<config-dir>/vgxness/default-agent.json` restoration metadata records whether `opencode.json` existed and any prior explicit default, so uninstall can restore that default or remove a config created by setup.

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
- No Go stack engine, worktree writer, delivery-state service, custom Git/GitHub tool, or network delivery test.
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
