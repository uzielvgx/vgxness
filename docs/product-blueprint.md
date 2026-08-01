# VGXNESS Product Blueprint

Current delivery policy: manager v39 loads global `stacked-pr` v3 and requires a clean pre-write gate before branch creation, source writes, or a delivery announcement. Only explicitly reauthorized, fully verified unpublished-local-slice recovery may proceed from dirt. Every first publication uses the empty-expectation create-only lease. Official setup retires exact provider v1/v2/v3 bytes before global publication; they are identity evidence only.

Delivery labels are observable only: IMPLEMENTED means intended workspace changes and developmental checks are complete, but independent verification has not occurred; VERIFIED means the exact frozen candidate passed independent verification and required review; DELIVERED means the exact commit was published and a new current-task PR was created and read back; MERGED means that PR and base containment were read back; INSTALLED additionally requires installation and handshake readback. A later state is never inferred.

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
- manager v39, 14 other model-bound agents, plugin v5, model-plan manifest, default-agent selection, restoration metadata, and the separate global 17-skill catalog;
- seven-step CLI/TUI setup with a bounded OpenCode 1.18.4+ handshake and shared portable-skill verification;
- current-only manager and agent recognition, exact storage-plugin predecessor recognition, and conservative uninstall behavior;
- deterministic release archives, checksums, and workflows.

Compatibility execution commands and subsystems are not part of the delivered product.

## Authority model

OpenCode owns engineering execution. Manager, managed `general`, and verifier have global tool permission; their prompts retain orchestration, delegated implementation, and non-mutating verification roles. The top-level manager owns route selection, synthesis, candidate acceptance, revision acceptance, and lifecycle transitions. Every installed SDD and review profile remains read-only. The plugin owns bounded memory and SDD persistence only.

The plugin exposes 18 tools: five semantic-memory operations and 13 SDD operations. SDD mutation requires trusted context for the tracked top-level manager. The plugin cannot route, invoke agents, access workspace files, run shell commands, select models, edit, delegate, or advance state independently.

## Managed artifacts

The OpenCode projection contains exactly 19 provider artifacts:

| Artifact group | Count | Contract |
| --- | ---: | --- |
| Manager v39 | 1 | Sole orchestration, lifecycle, Git, and GitHub actor. |
| Explore override | 1 | CodeGraph-first and deny-by-default read-only discovery. |
| General and verifier | 2 | Global capability with delegated implementation and independent non-mutating validation roles. |
| Reviewers | 5 | Hidden and read-only. |
| SDD profiles | 6 | Hidden, read-only, model-bound phase roles. |
| Storage plugin v5 | 1 | Memory and SDD storage only. |
| Model-plan manifest | 1 | Non-secret exact role/model and agent-digest bindings. |
| Default-agent selection | 1 | Semantic merge sets `default_agent="vgxness-manager"` in `opencode.json` while preserving unrelated JSON values; existing `opencode.jsonc` bytes remain unchanged. |
| Restoration metadata | 1 | Bounded `<config-dir>/vgxness/default-agent.json` records whether `opencode.json` existed and any prior explicit default, so uninstall can restore that default or remove a config created by setup. |

The default `medium` plan uses Luna Fast, Terra, and Sol slots. Plan or slot changes require OpenCode restart. The deprecated `--model` option remains a no-op.

The separate global portable catalog contains 41 files across 17 skills: `skills-creator`, `stacked-pr`, `cross-platform`, `installer-lifecycle`, `agent-evaluation`, `ci-triage`, `security-boundary`, `documentation-strategy`, `product-requirements`, `software-architecture-docs`, `user-documentation`, `api-documentation`, `quality-test-documentation`, `operations-runbooks`, `governance-compliance-docs`, `release-lifecycle-docs`, and `end-to-end-testing`. `vgxness skills <preview|install|status|uninstall> [--skills-dir PATH]` manages it at `~/.agents/skills` by default, and setup installs it after transactionally retiring exact provider-owned `vgxness-autonomous-stacked-pr` v1/v2/v3 bytes. Unknown or modified legacy bytes block without mutation. OpenCode uninstall never owns global skills. Exact desired/predecessor partial packs resume or uninstall safely; unknown bytes are drift. Windows uses atomic rename, readback, and backups but lacks directory fsync crash durability.

Eligible implementations default to 400 effective changed lines per slice and stack only above 800. Every stacked PR targets the same original inspected base, with immediate-parent commit ancestry and `Depends-On` metadata; merge commits preserve predecessor commits so later diffs narrow as earlier slices land. Manager v39 completes its clean pre-write gate (repository identity, intended paths, estimate/slice plan, and fresh branch) before source writes or the routine delivery announcement. After freeze, verification, and review, it may create fresh normalized branches, normal commits, first pushes, and non-draft pull requests without a second routine approval. It may merge only PRs created by the same current eligible task, in ordinal order, using the repository's allowed merge-commit method with verified `owner/repo` binding and an exact full head OID. Each slice has an expected base-tip OID from a fresh original-base readback before checks; after each predecessor merge it advances from a fresh readback, and the PR base plus live remote base must match it before checks and immediately before merge. `no merge` is transitive, while `local-only`, `no commit`, `no push`, and `no PR` also prohibit merge. Dirty state stops mutations except the exact bounded, explicitly reauthorized recovery of a verified unpublished local slice. Existing remote branches and PRs remain read-only and never gain retroactive merge or cleanup authority; only that bounded unpublished local-slice recovery is allowed. After verified merges and a clean worktree, it may fast-forward the original base from the verified remote-tracking base. Unless `no cleanup` applies, it may delete only exact current-delivery local branches proved merged with no open dependent PR; remote delivery branches are left intact. Any failed or ambiguous check, merge, host, auth, protection, topology, remote, or worktree state stops further mutation. OpenCode globs establish static policy ordering but do not prove argv semantics or external Git/GitHub behavior.

## Storage and SDD

The default database is `~/.vgxness/memory.db`. Canonical workspace bindings isolate projects, including same-named workspaces. Semantic observations and FTS rows never overlap structured SDD changes, revisions, bindings, idempotency, or projection records.

SDD advances through `explore -> proposal -> spec -> design -> tasks -> apply -> verify -> complete`. Accepted revisions are immutable and input-bound. Apply composes a hash-bound patch; the manager applies and validates it. Hybrid keeps SQLite content canonical. OpenSpec projection is bounded to `openspec/changes/<safe-change-id>/`, and divergent repository content is never imported automatically.

## Setup and health

Setup previews all changes, requires confirmation, installs the stable launcher and 19 exact OpenCode artifacts, retires exact provider v1/v2/v3 bytes, then publishes the global 41-file, 17-skill `skills-creator`, `stacked-pr`, `cross-platform`, `installer-lifecycle`, `agent-evaluation`, `ci-triage`, `security-boundary`, `documentation-strategy`, `product-requirements`, `software-architecture-docs`, `user-documentation`, `api-documentation`, `quality-test-documentation`, `operations-runbooks`, `governance-compliance-docs`, `release-lifecycle-docs`, and `end-to-end-testing` catalog. Unknown or modified legacy bytes block. OpenCode uninstall does not remove global skills.

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
