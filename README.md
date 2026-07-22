# VGXNESS

**Current:** This repository delivers a Go 1.26 control-plane foundation with the `vgxness` executable, a versioned self-installer with atomic activation and rollback, Chronicle events/snapshots/recovery, an owned SQLite/FTS5 memory store, Registry/Gatekeeper policy, provider-neutral bounded coordination, exact prompt composition, and a hardened OpenCode runtime adapter with a persistent Node/Bun-compatible `vgxness_*` bridge. CI validates tests, race detection, coverage, vet, formatting, module integrity, and builds with Go 1.26.3.

The read-only `status` and `doctor` inspection commands report resolved storage paths, the SQLite migration version, and an active Chronicle run when present. Inspection does not create a missing database or current-run file. The CLI can install, inspect, update, and roll back its permanent launcher, then use an explanatory, confirmation-gated wizard to install and verify `vgxness-manager`, three permission-scoped native OpenCode subagents, and the `vgxness_status`/`vgxness_dispatch` plugin bridge with one explicit execution model. Dispatch now uses a durable `native child session → recovery ticket → prepared execution → accept` handoff instead of launching a nested `opencode run --pure` worker. Independent one-shot read-only dispatches may fan out across at most four native child sessions per workspace; a short workspace-wide membership guard makes shared admission and release atomic, while continuity phases and all mutating work remain exclusive. Unreadable active lease state fails closed until bounded recovery can reconcile it. Repository reviews still consume bounded, pre-collected Git evidence without shell or file access. The richer keyboard TUI, additional runtime adapters, and mutating Git or branch-protection automation remain planned.

VGXNESS is planned as a local-first agent orchestration system that keeps human control, operational state, permissions, and validation explicit. The canonical product purpose, vocabulary, boundaries, and roadmap live in the [VGXNESS Product Blueprint](docs/product-blueprint.md).

## Documentation

| Document | Responsibility |
| --- | --- |
| [Product Blueprint — English](docs/product-blueprint.md) | Canonical version 1.0 product vision, status, taxonomy, boundaries, and roadmap. |
| [Plan maestro de producto — Español](docs/product-blueprint.es.md) | Complete version 1.0 Spanish companion; explicitly non-canonical, with English controlling conflicts. |
| [Go Implementation Architecture](docs/go-implementation.md) | Planned Go packages, interfaces, dependency rules, and testing boundaries. |
| [Orchestration Flow](docs/orchestration-flow.md) | Planned request lifecycle, gates, SDD operating modes, and recovery flow. |
| [Versioned Self-installation](docs/self-install.md) | Permanent launcher, immutable SHA-256 versions, atomic activation, rollback, and safety behavior. |
| [Guided OpenCode Setup](docs/opencode-setup-wizard.md) | Complete explanatory wizard, confirmation boundary, verification, status, and recovery behavior. |
| [OpenCode Integration](docs/opencode-integration.md) | Current persistent manager installation, safety behavior, CLI, and remaining bridge boundary. |
| [Contract Schemas](docs/schemas/README.md) | Current normative schemas and validation guidance. |

## Direction

VGXNESS coordinates bounded capabilities through a Go control plane, Chronicle operational truth, an owned SQLite/FTS5 semantic `MemoryStore`, and runtime-neutral adapters. OpenCode is the first implemented runtime adapter, persistent manager surface, bounded bridge, and guided CLI setup; a richer keyboard TUI remains planned. CodeGraph and OpenPencil are preferred optional structural-intelligence and design adapters; Engram is an optional compatibility/import/reference adapter. All remain subject to capability, permission, provenance, and policy checks.

**Planned:** Navigator, Scout, Blueprint, Forge, Sentinel, and optional Challenger are product capabilities. Registry, Chronicle, and Gatekeeper are deterministic services. SDD phase agents such as explore, design, apply, and verify are operating modes rather than a competing capability taxonomy.

**Non-goal:** VGXNESS will not copy third-party code, prompts, schemas, names, skills, or exact workflows, and it will not permit silent destructive autonomy or hide operational truth behind a CLI.

For complete definitions and status classifications, read the [Product Blueprint](docs/product-blueprint.md). Supporting documents intentionally do not duplicate its roadmap.
