# VGXNESS

**Current:** This repository delivers a Go 1.26 control-plane foundation with the `vgxness` executable, versioned self-installation, Chronicle operational state, an owned SQLite/FTS5 store with isolated semantic-memory and structured SDD domains, Registry/Gatekeeper policy, and a hardened OpenCode adapter. The installed native projection has 14 managed artifacts: 12 agents (`vgxness-manager` v29, five read-only reviewers, and six read-only SDD profiles), the storage plugin v5, and the model-plan manifest. The plugin exposes 18 storage tools: five semantic-memory tools and 13 SDD tools. CI validates tests, race detection, coverage, vet, formatting, module integrity, and builds with Go 1.26.3.

The read-only `status` and `doctor` commands report storage, migration, Chronicle, orchestration, ticket, and lease state. The CLI still contains bridge, control-plane orchestration, maintenance, isolated-edit, ticket/wave, and Delivery Authority subsystems for compatibility and maintainers; they are not the active installed OpenCode scheduler. In the active path, the manager is the sole lifecycle and workspace writer. All six SDD agents are read-only, with apply acting as a hash-bound patch composer, and all five reviewers are read-only. The storage-only plugin persists and projects data; it never executes, routes, edits, or delegates, and its SDD mutations fail closed outside the trusted top-level manager session.

VGXNESS is planned as a local-first agent orchestration system that keeps human control, operational state, permissions, and validation explicit. The canonical product purpose, vocabulary, boundaries, and roadmap live in the [VGXNESS Product Blueprint](docs/product-blueprint.md).

## Documentation

| Document | Responsibility |
| --- | --- |
| [Product Blueprint — English](docs/product-blueprint.md) | Canonical version 1.0 product vision, status, taxonomy, boundaries, and roadmap. |
| [Plan maestro de producto — Español](docs/product-blueprint.es.md) | Complete version 1.0 Spanish companion; explicitly non-canonical, with English controlling conflicts. |
| [Go Implementation Architecture](docs/go-implementation.md) | Current Go packages, active/compatibility boundaries, storage, interfaces, and testing. |
| [Orchestration Flow](docs/orchestration-flow.md) | Active native SDD lifecycle, compatibility control plane, gates, and recovery boundaries. |
| [Native Memory and Structured Storage](docs/memory.md) | SQLite schema v5 domains, isolation, memory lifecycle, and upgrade migration caveat. |
| [Compatibility Delegation and Delivery Plan](docs/delegation-authority-implementation-plan.md) | Compatibility-only control-plane waves, tickets, edit broker, and delivery rollout history. |
| [Compatibility Delivery Authority](docs/delivery-authority.md) | Maintainer-only receipt lifecycle, four validating gates, storage, and trust boundary. |
| [Compatibility Operations](docs/operations.md) / [Español](docs/operations.es.md) | Legacy CLI diagnosis, retention, and isolated-edit lifecycle. |
| [Versioned Self-installation](docs/self-install.md) | Permanent launcher, immutable SHA-256 versions, atomic activation, rollback, and safety behavior. |
| [Alpha releases](docs/release.md) | Release artifacts, support matrix, checksum verification, installation, and release rollback boundaries. |
| [Guided OpenCode Setup](docs/opencode-setup-wizard.md) | Complete explanatory wizard, confirmation boundary, verification, status, and recovery behavior. |
| [OpenCode Integration](docs/opencode-integration.md) | Current persistent manager installation, safety behavior, CLI, and remaining bridge boundary. |
| [Safe Hooks](docs/hooks.md) | Typed post-commit events, OpenCode context hooks, failure policy, delivery limits, and explicit shell/Git-hook exclusions. |
| [Contract Schemas](docs/schemas/README.md) | Current normative schemas and validation guidance. |

## Direction

VGXNESS coordinates bounded capabilities through a Go control plane, Chronicle operational truth, one owned SQLite/FTS5 database, and runtime-neutral adapters. OpenCode is the first implemented runtime and persistent native manager surface. Native SDD supports `memory`, `openspec`, and `hybrid` backends plus per-change `automatic` or `interactive` execution. Hybrid keeps memory canonical; OpenSpec projection is bounded to `openspec/changes/<safe-change-id>/`, and divergent repository content is never imported automatically. The current/default `medium` plan uses Luna Fast, Terra, and Sol slots; changing any installed plan or slot requires an OpenCode restart. See [OpenCode Integration](docs/opencode-integration.md) for the exact model IDs and [Native Memory](docs/memory.md) for storage and migration behavior.

By default, semantic memory for every workspace lives in one user database at `~/.vgxness/memory.db`; a canonical workspace registry keeps same-named projects distinct while preserving an existing legacy project identity on first binding. Project scope, topic keys, sessions, and references remain isolated inside the schema. Every bounded dispatch/orchestration task retrieves up to three relevant observations, hydrates their bounded content into the child execution packet, and writes one idempotent result observation that references the evidence it used. Chronicle, native tickets, delivery receipts, and other operational files stay under `~/.vgxness/projects/<project-id>/`. On first memory access after upgrading, an existing project-level `memory.db` is imported transactionally and idempotently, while the old file is retained as an inactive recovery backup. Explicit `--storage-root` and `--project-local` modes remain isolated overrides.

**Implemented:** The native manager chooses direct work, bounded read-only delegation, or the structured SDD lifecycle. Up to four independent read-only subworks may overlap; final synthesis, patch application, validation, artifact acceptance, projection writes, and lifecycle transitions remain sequential. **Planned:** richer Chronicle-backed interrupted-SDD recovery, provider-neutral routing/catalog probes, and automatic delivery integration. The compatibility CLI remains intentionally documented but is not the installed scheduler.

**Non-goal:** VGXNESS will not copy third-party code, prompts, schemas, names, skills, or exact workflows, and it will not permit silent destructive autonomy or hide operational truth behind a CLI.

For complete definitions and status classifications, read the [Product Blueprint](docs/product-blueprint.md). Supporting documents intentionally do not duplicate its roadmap.

## Installation and releases

On macOS or Linux, install the published alpha through the official Homebrew tap:

```sh
brew install uzielvgx/tap/vgxness
```

Homebrew verifies the formula's pinned SHA-256 digest and owns the executable in its prefix. It does not modify OpenCode or install the separate `~/.local` launcher. Use the brewed executable to preview and explicitly apply that optional setup. See the [tap documentation](https://github.com/uzielvgx/homebrew-tap) for the exact commands and ownership boundaries.

Alpha releases also provide unsigned archives for Linux, macOS, and Windows plus `SHA256SUMS`. Verify the downloaded archive before running it, then use the extracted `vgxness` or `vgxness.exe` binary to preview and perform self-installation. See [Alpha releases](docs/release.md) for acquisition, Homebrew, exact artifact names, checksums, and platform support, and [Versioned self-installation](docs/self-install.md) for launcher, status, and rollback behavior. Self-installation does not download releases or edit `PATH`.
