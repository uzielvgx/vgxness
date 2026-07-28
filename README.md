# VGXNESS

**Current:** This repository delivers a Go 1.26 control-plane foundation with the `vgxness` executable, a versioned self-installer with atomic activation and rollback, Chronicle events/snapshots/recovery, an owned unified SQLite/FTS5 memory store partitioned by project, Registry/Gatekeeper policy, provider-neutral bounded coordination, exact prompt composition, and a hardened OpenCode runtime adapter. Its active OpenCode projection is native-first: one `vgxness-manager`, five read-only review profiles, native skills, and optional CodeGraph MCP evidence, with no VGXNESS plugin or `vgxness_*` tools. CI validates tests, race detection, coverage, vet, formatting, module integrity, and builds with Go 1.26.3.

The read-only `status` and `doctor` commands report storage, migration, Chronicle, orchestration, ticket, and lease state. The CLI still contains the bounded orchestration, bridge, isolated-edit, validation, and Delivery Authority services as a compatibility surface, but setup no longer projects them into OpenCode. The confirmation-gated wizard installs and verifies only the permanent launcher, `vgxness-manager`, and the Risk, Readability, Reliability, Resilience, and refuter profiles. The manager works directly with OpenCode workspace tools and built-in Task delegation, loads skills by native registry name, uses one bounded `codegraph_explore` query when structural evidence is useful, and keeps exact source, diffs, and tests authoritative. Full SDD artifacts, automatic Git/branch-protection wiring, the keyboard TUI, additional runtime adapters, and final removal of the legacy CLI surface remain planned.

VGXNESS is planned as a local-first agent orchestration system that keeps human control, operational state, permissions, and validation explicit. The canonical product purpose, vocabulary, boundaries, and roadmap live in the [VGXNESS Product Blueprint](docs/product-blueprint.md).

## Documentation

| Document | Responsibility |
| --- | --- |
| [Product Blueprint — English](docs/product-blueprint.md) | Canonical version 1.0 product vision, status, taxonomy, boundaries, and roadmap. |
| [Plan maestro de producto — Español](docs/product-blueprint.es.md) | Complete version 1.0 Spanish companion; explicitly non-canonical, with English controlling conflicts. |
| [Go Implementation Architecture](docs/go-implementation.md) | Planned Go packages, interfaces, dependency rules, and testing boundaries. |
| [Orchestration Flow](docs/orchestration-flow.md) | Planned request lifecycle, gates, SDD operating modes, and recovery flow. |
| [Native Delegation and Delivery Authority Plan](docs/delegation-authority-implementation-plan.md) | Executable slices for adaptive native OpenCode subagents, wave scheduling, evidence, receipts, and delivery gates. |
| [Delivery Authority](docs/delivery-authority.md) | Operator lifecycle, manifest contract, four validating gates, invalidation, storage, and trust boundary. |
| [Operational Inspection and Retention](docs/operations.md) / [Español](docs/operations.es.md) | Deep diagnosis, inventory, dry-run retention, apply boundary, and preserved state. |
| [Versioned Self-installation](docs/self-install.md) | Permanent launcher, immutable SHA-256 versions, atomic activation, rollback, and safety behavior. |
| [Guided OpenCode Setup](docs/opencode-setup-wizard.md) | Complete explanatory wizard, confirmation boundary, verification, status, and recovery behavior. |
| [OpenCode Integration](docs/opencode-integration.md) | Current persistent manager installation, safety behavior, CLI, and remaining bridge boundary. |
| [Contract Schemas](docs/schemas/README.md) | Current normative schemas and validation guidance. |

## Direction

VGXNESS coordinates bounded capabilities through a Go control plane, Chronicle operational truth, an owned SQLite/FTS5 semantic `MemoryStore`, and runtime-neutral adapters. OpenCode is the first implemented runtime adapter and persistent native manager surface; a richer keyboard TUI remains planned. OpenCode's native skill registry is the capability discovery source for interactive work. CodeGraph is an optional read-only MCP source for structural evidence, never the authority for exact candidate changes. OpenPencil remains a preferred optional design adapter, and Engram remains an optional compatibility/import/reference adapter.

By default, semantic memory for every workspace lives in one user database at `~/.vgxness/memory.db`; a canonical workspace registry keeps same-named projects distinct while preserving an existing legacy project identity on first binding. Project scope, topic keys, sessions, and references remain isolated inside the schema. Every bounded dispatch/orchestration task retrieves up to three relevant observations, hydrates their bounded content into the child execution packet, and writes one idempotent result observation that references the evidence it used. Chronicle, native tickets, delivery receipts, and other operational files stay under `~/.vgxness/projects/<project-id>/`. On first memory access after upgrading, an existing project-level `memory.db` is imported transactionally and idempotently, while the old file is retained as an inactive recovery backup. Explicit `--storage-root` and `--project-local` modes remain isolated overrides.

**Implemented/Planned:** Navigator now plans and coordinates bounded native OpenCode read, isolated edit, and review tasks. The edit broker delivers an explicit worktree artifact; local operators can inspect, content-bind approval, integrate, retire, or discard it, while automatic review and merge remain intentionally absent. Scout, Blueprint, Forge, Sentinel, optional Challenger, and their complete SDD artifact workflows remain product capabilities under construction. Registry, Chronicle, and Gatekeeper are deterministic services; explore, design, apply, and verify are operating modes rather than a competing capability taxonomy.

**Non-goal:** VGXNESS will not copy third-party code, prompts, schemas, names, skills, or exact workflows, and it will not permit silent destructive autonomy or hide operational truth behind a CLI.

For complete definitions and status classifications, read the [Product Blueprint](docs/product-blueprint.md). Supporting documents intentionally do not duplicate its roadmap.
