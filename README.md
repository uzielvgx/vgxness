# VGXNESS

**Current:** This repository contains product documentation and Draft 2020-12 contract schemas only. These are reviewable contracts, not a claim of complete release validation or runtime enforcement. The repository does not contain Go source, an executable, an installer, bundled skills, runtime adapters, persistence, Git automation, or configuration mutation.

VGXNESS is planned as a local-first agent orchestration system that keeps human control, operational state, permissions, and validation explicit. The canonical product purpose, vocabulary, boundaries, and roadmap live in the [VGXNESS Product Blueprint](docs/product-blueprint.md).

## Documentation

| Document | Responsibility |
| --- | --- |
| [Product Blueprint — English](docs/product-blueprint.md) | Canonical version 1.0 product vision, status, taxonomy, boundaries, and roadmap. |
| [Plan maestro de producto — Español](docs/product-blueprint.es.md) | Complete version 1.0 Spanish companion; explicitly non-canonical, with English controlling conflicts. |
| [Go Implementation Architecture](docs/go-implementation.md) | Planned Go packages, interfaces, dependency rules, and testing boundaries. |
| [Orchestration Flow](docs/orchestration-flow.md) | Planned request lifecycle, gates, SDD operating modes, and recovery flow. |
| [Contract Schemas](docs/schemas/README.md) | Current normative schemas and validation guidance. |

## Direction

**Planned:** VGXNESS will coordinate bounded capabilities through a globally installed Go control plane, a keyboard-first setup experience, Chronicle operational truth, an owned SQLite/FTS5 semantic `MemoryStore`, and runtime-neutral adapters. OpenCode is the first preferred runtime adapter; CodeGraph and OpenPencil are preferred optional structural-intelligence and design adapters; Engram is an optional compatibility/import/reference adapter. All remain subject to capability, permission, provenance, and policy checks.

**Planned:** Navigator, Scout, Blueprint, Forge, Sentinel, and optional Challenger are product capabilities. Registry, Chronicle, and Gatekeeper are deterministic services. SDD phase agents such as explore, design, apply, and verify are operating modes rather than a competing capability taxonomy.

**Non-goal:** VGXNESS will not copy third-party code, prompts, schemas, names, skills, or exact workflows, and it will not permit silent destructive autonomy or hide operational truth behind a CLI.

For complete definitions and status classifications, read the [Product Blueprint](docs/product-blueprint.md). Supporting documents intentionally do not duplicate its roadmap.
