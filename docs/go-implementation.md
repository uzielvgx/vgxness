# VGXNESS Go Implementation Architecture

This document owns Go package, interface, dependency, and testing boundaries. The [VGXNESS Product Blueprint](product-blueprint.md) is the canonical product vision, taxonomy, status, and roadmap.

| Status | Merged-main evidence and boundary |
| --- | --- |
| **Implemented** | `cmd/vgxness`, application wiring, storage configuration, status/doctor inspection, owned SQLite/FTS5 memory with two migrations, memory save/search/get CLI operations, tests, and Go CI. |
| **Partial** | `internal/chronicle` strictly reads `current-run.json`; it does not append events, write snapshots, replay, or recover runs. |
| **Contracts-only** | Draft 2020-12 schemas and semantic backend/reference validation define shapes and rules without providing orchestration. |
| **Planned** | Contract validation in runtime paths, Chronicle events, snapshots/recovery, task state machine, Gatekeeper/Registry, providers, bounded coordination, setup, and adapters. |

VGXNESS is designed as a small, explicit Go control plane for adaptive agent orchestration. It will ship as a globally installed system on the user's machine. Go fits because the system needs a dependable local binary, readable storage, auditable workflow state, and testable package boundaries more than a framework-heavy stack.

OpenCode is the first preferred runtime adapter. At run start, a provider-neutral selector compares declared capabilities, versions, constraints, and policy; OpenCode is used only when eligible. The same thin boundary permits future Hermes, Claude, Codex, and other adapters without changing core contracts.

Capability names such as Navigator, Scout, Blueprint, Forge, Sentinel, and optional Challenger belong to the canonical product taxonomy. Names such as explore, design, apply, and verify describe SDD phase-agent operating modes that use those capabilities; they are not additional product capabilities.

The implemented foundation is deliberately narrower than the target architecture. No provider execution, task state machine, Gatekeeper/Registry service, bounded coordination, setup workflow, or runtime configuration mutation is present.

## Quick path

1. **Implemented:** Build a single `vgxness` binary from `cmd/vgxness`; resolve storage roots; expose status/doctor and memory save/search/get.
2. **Implemented:** Keep owned semantic memory on SQLite/FTS5; `memory` is the owned-backend contract and `engram` is only an external-provider reference.
3. **Partial:** Strictly read the Chronicle current-run pointer for inspection.
4. **Contracts-only:** Maintain schemas and semantic validation without treating them as a runtime.
5. **Planned:** Deliver in this dependency order: **contract validation → Chronicle events → snapshots/recovery → task state machine → Gatekeeper/Registry → providers → bounded coordination**.
6. **Planned:** Add keyboard-first setup and optional adapters over application services without moving policy into terminal UI.

## Why Go fits VGXNESS

| Need | Go fit |
| --- | --- |
| Local-first distribution | Go produces a single binary that is easy to install, copy, and run in project or user-global contexts. |
| Auditable failure handling | Explicit `error` returns make failure paths visible instead of hidden behind framework magic. |
| CLI and server surfaces | Go works well for command-line tools today and can expose local HTTP/MCP-style services later without changing the core model. |
| Local files | The standard library handles JSON, JSONL-style file I/O, paths, atomic-ish replacement patterns, and permissions clearly. |
| Long-running work | `context.Context` gives provider calls, agent runs, file operations, and cancellation a shared lifecycle. |
| Testability | Small packages, small interfaces, fakes, and the standard `testing` package are enough for the first implementation. |

## Non-goals

- No clever framework-heavy architecture. VGXNESS should be boring, inspectable Go.
- No hidden global mutable state. Dependencies should be passed through constructors or explicit wiring.
- No prompt behavior hardcoded into random packages. Prompt contracts belong in agent, skill, provider, or orchestration boundaries where they can be reviewed.
- No database replacement for operational truth. Chronicle starts with readable JSON/JSONL files; SQLite/FTS5 is intentionally limited to the initial semantic `MemoryStore`.
- No CLI lock-in. Users and recovery flows should still understand the state by reading files.
- No TUI-owned business logic. Setup screens render state and collect decisions; installation, memory, and orchestration remain independent services.
- No Engram coupling in orchestration contracts. VGXNESS-owned memory is authoritative; Engram is an optional compatibility/import/reference adapter.
- No claim that schemas, the Chronicle reader, or implemented memory constitute provider execution, task-state, recovery, or orchestration behavior.

## Package delivery map

```text
cmd/vgxness
internal/app
internal/config
internal/memory
internal/cli
internal/inspection
internal/chronicle
internal/contracts

# planned additions
internal/runstore
internal/taskstate
internal/registry
internal/gatekeeper
internal/providers
internal/orchestrator
internal/install
internal/setup
internal/tui
```

| Package | Status | Responsibility |
| --- | --- | --- |
| `cmd/vgxness`, `internal/app` | **Implemented** | Binary entrypoint and composition for current configuration, inspection, Chronicle reader, and memory commands. |
| `internal/config` | **Implemented** | Resolve defaults and explicit project-local or user-global storage roots. |
| `internal/memory` | **Implemented** | Own SQLite/FTS5 persistence, migrations, typed records, lifecycle fields, save/search/get, and deterministic filtering. |
| `internal/cli`, `internal/inspection` | **Implemented** | Expose status, doctor, and memory operations with safe output and operational error classes. |
| `internal/chronicle` | **Partial** | Strictly read and validate `current-run.json`; no event or snapshot writes. |
| `internal/contracts` | **Contracts-only** | Enforce owned `memory` versus external `engram` reference semantics; schema shapes remain contracts. |
| `internal/runstore`, `internal/taskstate` | **Planned** | Add Chronicle events and snapshots/recovery before legal task transitions. |
| `internal/registry`, `internal/gatekeeper` | **Planned** | Resolve exact identities and enforce permissions, approvals, capabilities, and transitions after task state exists. |
| `internal/providers` | **Planned** | Execute eligible runtime/model providers only after Registry/Gatekeeper decisions. |
| `internal/orchestrator` | **Planned** | Coordinate bounded phases only after all preceding operational dependencies exist. |
| `internal/install`, `internal/setup`, `internal/tui` | **Planned** | Provide runtime-neutral setup services and a keyboard-first adapter without owning orchestration policy. |

## Dependency rules

Keep dependencies pointed toward stable contracts, not convenience shortcuts. These are **Contracts-only** architecture rules until the named planned packages exist.

| Rule | Reason |
| --- | --- |
| `cmd/vgxness` depends only on `internal/app`. | The executable should not become a second composition root. |
| `internal/app` may depend on every package for wiring. | Dependency construction belongs in one obvious place. |
| `internal/cli` calls application services; it should not own orchestration policy. | CLI is convenience, not the workflow source of truth. |
| `internal/orchestrator` depends on small interfaces for run storage, memory, agents, skills, providers, permissions, and validation. | The orchestration core stays testable with fakes. |
| `internal/runstore` does not depend on `internal/memory` or `internal/providers`. | Operational truth must stay deterministic and local-file readable. |
| `internal/memory` does not depend on `internal/runstore`. | Semantic memory should not become an event log. |
| `internal/providers` does not depend on `internal/cli`. | Provider execution must work outside terminal commands. |
| `internal/tui` depends on setup application services, not installer, memory, or orchestrator implementations. | The wizard is an adapter and can evolve or be replaced without moving business logic. |
| `internal/permissions` is checked before provider, installer, filesystem, or command-like actions. | Risky behavior must be impossible to bypass accidentally. |
| `internal/validation` may read schemas and outputs, but should not mutate workflow state directly. | Validation proves state; it should not hide repair side effects. |

## Storage choices

Chronicle's target operational truth remains readable local files:

- `current-run.json` for the active run pointer and current phase.
- `runs/<run-id>.json` for the full run snapshot.
- `logs/<run-id>.jsonl` for append-only operational events.
- `artifacts/<change-id>/...` for generated SDD or workflow artifacts when file storage is selected.
- `registry/skills.json` and `registry/agents.json` for generated registries.

**Partial:** The current implementation only reads and validates `current-run.json`; snapshot and JSONL writers do not exist. **Implemented:** SQLite/FTS5 stores owned semantic records with two migrations, deterministic filters, and lexical retrieval. The stores may eventually cross-reference IDs, but implemented semantic memory does not resolve operational state.

Chronicle delivery must proceed from contract validation to event append, then snapshots/recovery. Task transitions cannot depend on operational state until those layers are verifiable.

## Interfaces

Keep interfaces small and close to the package that consumes them. Do not create broad manager interfaces just because a concrete package exists.

The following are **Contracts-only** examples for the planned `internal/orchestrator`; no orchestrator currently consumes them:

```go
type RunStore interface {
    Current(ctx context.Context) (RunRef, error)
    AppendEvent(ctx context.Context, event RunEvent) error
    SaveSnapshot(ctx context.Context, run RunSnapshot) error
}

type MemoryStore interface {
    Save(ctx context.Context, entry MemoryEntry) (MemoryRef, error)
    Search(ctx context.Context, query MemoryQuery) ([]MemoryRef, error)
}

type AgentRunner interface {
    RunAgent(ctx context.Context, req AgentRequest) (AgentResult, error)
}

type OrchestratorProvider interface {
    Reference() ProviderRef
    Capabilities(ctx context.Context) ([]CapabilityDeclaration, error)
    Run(ctx context.Context, packet ExecutionPacket) (AgentResult, error)
}

type ContractValidator interface {
    Validate(ctx context.Context, schemaURI string, value any) error
}
```

If a caller only needs `AppendEvent`, define an even smaller interface in that caller. Interfaces should describe what the consumer needs, not every method the provider can perform.

The planned orchestrator will consume these small interfaces. Provider selection will record a neutral provider reference and capability evidence, never mutable provider configuration. Runtime `ContractValidator` behavior must be delivered before delegation or state mutation.

The **Implemented** `MemoryStore` is VGXNESS-owned and SQLite/FTS5-first. It stores typed observations with provenance, scope, topic, lifecycle state, references, and save/search/get behavior. Richer approval, summary, capsule, comparison, review, and optional Engram import workflows remain **Planned**; the owned-backend contract already uses `memory`, so no backend migration is pending.

## Adaptive execution boundaries

| Boundary | Go responsibility |
| --- | --- |
| Selection | Compare run needs with provider capabilities and policy before calling `Run`. |
| Routing | Persist route inputs, candidates, rationale, policy version, SDD decision, and attributed override. |
| Thin context | Pass one task-scoped `ExecutionPacket`; fetch durable artifacts by immutable reference. |
| Skills | Resolve exact identity, version, source, provenance, and allowed scope before dispatch. |
| Foreground | Advance one manager-owned foreground task at a time. |
| Background | Enforce read-only, non-delegating, non-advancing tasks with independent cancellation. |
| Loops | Enforce finite iteration/deadline budgets and explicit terminal reasons. |
| Continuity | Persist decisions, state references, provenance, and next actions in a capsule rather than a transcript. |

**Contracts-only:** Schemas and semantic rules define validation points and legal records. **Planned:** Runtime validation will run at ingestion, before Chronicle event/snapshot writes, before delegation, and during readback; a failed check will preserve the last valid state.

## Setup wizard boundary

The primary setup experience is a friendly, keyboard-first terminal wizard for OpenCode. In v1, it must detect the global OpenCode installation and configuration paths, show the proposed changes, back up existing configuration, project VGXNESS configuration through the OpenCode adapter, read the result back for validation, and provide actionable retry, repair, or rollback steps when a step fails.

Keep the wizard and normal runtime as separate entry flows over shared application services:

```text
setup TUI -> setup service -> prerequisite/install/validation adapters
normal CLI/runtime -> orchestration services -> run store and MemoryStore
```

The TUI owns focus, navigation, presentation state, and user input. The setup service owns runtime-neutral workflow state and decisions. The OpenCode adapter owns OpenCode-specific detection, paths, schema projection, and readback translation. This makes the same setup behavior testable without a terminal, prevents the UI from becoming the system architecture, and leaves the adapter boundary ready for later runtimes.

## Error handling and logging

- Return errors for normal failures; panic only for impossible programmer mistakes during startup or tests.
- Wrap errors with operation context: `fmt.Errorf("load run snapshot: %w", err)`.
- Use typed errors only when callers must branch, such as `ErrRunNotFound`, `ErrApprovalRequired`, or `ErrValidationFailed`.
- Make user-facing errors actionable and safe to display.
- Never include secrets, tokens, raw prompts with credentials, or provider payloads in logs by default.
- Logs should help debug run IDs, phase names, package operations, and validation failures without becoming a second event store.
- The run store records operational events; logs explain process behavior around those events.

## Context and cancellation

- Accept `context.Context` as the first parameter for provider calls, agent runs, run-store reads/writes, memory operations, installation steps, validation, and long-running CLI commands.
- Do not store context in structs unless the struct represents a clear lifecycle boundary.
- Cancellation should stop provider streams, subagent execution, validation, and pending writes cleanly.
- When cancellation happens, write a safe failure or checkpoint event only if the caller still permits a bounded cleanup write.
- Deadlines and cancellation should flow from `internal/app` or the active command/request into lower packages.

## Testing strategy

Use Go's standard `testing` package first.

| Area | Test style |
| --- | --- |
| Config loading | **Implemented:** table-driven coverage for storage-root resolution and invalid input. |
| Memory | **Implemented:** service and SQLite tests for save/search/get, validation, FTS5, migrations, and error handling. |
| Chronicle reader | **Partial:** strict current-run parsing, malformed input, symlink, and cancellation cases. Event/snapshot tests await writers. |
| Contract semantics | **Contracts-only:** tests verify owned `memory`, external `engram` references, and selected schema invariants. |
| CLI/application | **Implemented:** output and wiring tests for status, doctor, and memory commands. |
| Run store, task state, Registry/Gatekeeper, providers, orchestrator | **Planned:** add behavior tests with fakes as each dependency layer is delivered. |

Prefer behavior tests over implementation trivia. Each package should be testable without real providers, real Engram, external network calls, package installation, or git operations.

## First version scope in Go

Continue from the delivered foundation in dependency order:

1. **Implemented:** Create the `vgxness` binary and explicit wiring.
2. **Implemented:** Resolve project-local `.vgxness/` and user-global `~/.vgxness/projects/<project-id>/` storage.
3. **Implemented:** Add SQLite/FTS5 memory save/search/get, lifecycle fields, migrations, status/doctor, tests, and CI.
4. **Partial:** Read and validate `current-run.json` for inspection.
5. **Planned:** Add runtime contract validation, then Chronicle event append.
6. **Planned:** Add snapshots/recovery, then a legal task state machine.
7. **Planned:** Add Gatekeeper/Registry, then provider execution, then bounded coordination.
8. **Planned:** Add keyboard-first OpenCode setup and optional adapters after the core boundaries are enforceable.

The first version does not include a graphical installer, multi-user sync, distributed scheduling, autonomous destructive actions, advanced embedding infrastructure, or runtime adapters beyond OpenCode. Its orchestration, installation, Chronicle, memory, adapter, and permission contracts must permit later runtimes and optional Engram interoperability without changing core authority.
