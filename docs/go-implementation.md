# VGXNESS Go Implementation Architecture

VGXNESS is designed as a small, explicit Go control plane for adaptive agent orchestration. It will ship as a globally installed system on the user's machine. Go fits because the system needs a dependable local binary, readable storage, auditable workflow state, and testable package boundaries more than a framework-heavy stack.

OpenCode is the first preferred runtime adapter. At run start, a provider-neutral selector compares declared capabilities, versions, constraints, and policy; OpenCode is used only when eligible. The same thin boundary permits future Hermes, Claude, Codex, and other adapters without changing core contracts.

This document defines future package and interface boundaries only. The adaptive-contract change adds no Go source, provider execution, configuration mutation, or runtime migration branch.

## Quick path

1. Build and globally install a single `vgxness` binary from `cmd/vgxness`.
2. Keep orchestration, run storage, memory, provider, skill, and permission logic in focused `internal/` packages.
3. Persist operational truth as JSON snapshots plus JSONL events first.
4. Use Engram for initial semantic memory behind a replaceable memory-store interface.
5. Make a keyboard-first OpenCode setup wizard the primary installation experience while keeping it separate from normal runtime logic.
6. Keep terminal interfaces as adapters over readable files and application services.
7. Keep foreground run advancement sequential; permit only manager-owned, read-only background tasks.

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
- No database-first design for v1. Start with readable files; add SQLite only if the file model becomes insufficient.
- No CLI lock-in. Users and recovery flows should still understand the state by reading files.
- No TUI-owned business logic. Setup screens render state and collect decisions; installation, memory, and orchestration remain independent services.
- No Engram coupling in orchestration contracts. Engram is the initial adapter and will be replaced by VGXNESS's own persistence and memory system later.
- No provider execution, runtime configuration mutation, nested delegation, or owned memory implementation in the schema/documentation phase.

## Proposed package layout

```text
cmd/vgxness
internal/app
internal/config
internal/runstore
internal/memory
internal/agents
internal/orchestrator
internal/skills
internal/providers
internal/install
internal/permissions
internal/validation
internal/cli
internal/setup
internal/tui
```

| Package | Responsibility |
| --- | --- |
| `cmd/vgxness` | Binary entrypoint. Parse process startup concerns and call `internal/app`. |
| `internal/app` | Composition root. Wire config, stores, providers, orchestrator, validation, and CLI commands. |
| `internal/config` | Load defaults, user config, project config, environment overrides, and storage-root settings. |
| `internal/runstore` | Own operational truth: current run, run snapshots, JSONL events, artifact references, checkpoints, and recovery reads. |
| `internal/memory` | Define the replaceable semantic-memory contract and adapters. The initial adapter uses Engram; later adapters can use VGXNESS-owned persistence without changing consumers. |
| `internal/agents` | Define agent manifests, subagent contracts, result envelopes, capability metadata, and prompt contract references. |
| `internal/orchestrator` | Coordinate phases, context packets, delegation decisions, approval gates, and final summaries. |
| `internal/skills` | Resolve skills by explicit path, registry entry, trigger, and scoped fallback. Avoid paraphrasing skill contracts. |
| `internal/providers` | Adapter boundary for supported agent runtimes and model/provider execution. OpenCode is first; no run-store policy decisions belong here. |
| `internal/install` | Runtime-neutral installation services for backup, configuration projection, readback validation, and recovery; runtime adapters supply paths and schemas. |
| `internal/permissions` | Centralize allowed actions, approval requirements, and deny-by-default policy checks. |
| `internal/validation` | Validate schemas, readback consistency, phase results, recovery safety, and completion gates. |
| `internal/cli` | Command definitions and user-facing formatting for `status`, `run inspect`, `doctor`, and future commands. |
| `internal/setup` | Coordinate runtime-neutral detection, installation plans, progress, validation, and recovery independently of any UI; v1 is composed with the OpenCode adapter. |
| `internal/tui` | Render the keyboard-first setup wizard and translate user input into setup application-service calls. Own no installation, memory, or orchestration policy. |

## Dependency rules

Keep dependencies pointed toward stable contracts, not convenience shortcuts.

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

Start with readable local files:

- `current-run.json` for the active run pointer and current phase.
- `runs/<run-id>.json` for the full run snapshot.
- `logs/<run-id>.jsonl` for append-only operational events.
- `artifacts/<change-id>/...` for generated SDD or workflow artifacts when file storage is selected.
- `registry/skills.json` and `registry/agents.json` for generated registries.

Use JSON snapshots plus JSONL events first because they are easy to inspect, diff, validate, back up, and recover after interruption. SQLite can be added later only if queries, concurrency, or file-size pressure become real problems. If SQLite is added, keep the JSON/JSONL export or readback story so the CLI remains convenience, not lock-in.

## Interfaces

Keep interfaces small and close to the package that consumes them. Do not create broad manager interfaces just because a concrete package exists.

Example consumer-side interfaces for `internal/orchestrator`:

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

The orchestrator consumes these small interfaces. Provider selection records the neutral provider reference and capability evidence, never mutable provider configuration. `ContractValidator` returns `contract.invalid` details with schema URI and JSON Pointer before delegation or state mutation.

The first `MemoryStore` implementation delegates to Engram. Engram-specific identifiers, request types, and lifecycle behavior must be translated inside that adapter rather than exposed through orchestrator or agent contracts. Memory artifacts use immutable provider/id references and do not require filesystem paths.

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

Schema validation runs at registry/config ingestion, before delegation, before event append or snapshot write, and during readback. Semantic validation follows it for capability satisfaction, reference resolution, legal state transitions, background restrictions, ID consistency, and loop terminality. A failed check preserves the last valid state.

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
| Config loading | Table-driven tests for defaults, overrides, invalid files, and storage-root resolution. |
| Run store | Temporary directories, JSON snapshot readback, JSONL append/read behavior, interruption edge cases. |
| Memory | Fakes for semantic save/search behavior and topic-key handling. |
| Orchestrator | Fakes for stores, skills, agents, permissions, providers, and validation gates. Test phase decisions and blocked states. |
| Permissions | Table-driven allow/deny cases for git, install, external, destructive, secret, and permission-expansion actions. |
| Validation | Schema/readback consistency tests and snapshot/event mismatch cases. |
| CLI | Output-focused tests against app service fakes; avoid coupling tests to terminal trivia. |

Prefer behavior tests over implementation trivia. Each package should be testable without real providers, real Engram, external network calls, package installation, or git operations.

## First version scope in Go

Build the smallest useful product slice:

1. Create the globally installed `vgxness` binary and explicit dependency wiring.
2. Add a keyboard-first OpenCode setup wizard backed by UI-independent, runtime-neutral setup services.
3. Detect OpenCode, back up its existing configuration, project VGXNESS through the OpenCode adapter, report progress, read the result back for validation, and expose actionable retry, repair, or rollback recovery.
4. Add `internal/config` storage-root resolution for project-local `.vgxness/` and user-global `~/.vgxness/projects/<project-id>/`.
5. Add `internal/runstore` support for `current-run.json`, `runs/<run-id>.json`, and `logs/<run-id>.jsonl` using the existing schemas.
6. Add Engram as the initial `MemoryStore` adapter for semantic save, search, and session summaries.
7. Add `internal/validation` readback checks and minimal `status`, `run inspect <run-id>`, and `doctor` commands.

The first version does not include a graphical installer, multi-user sync, distributed scheduling, autonomous destructive actions, VGXNESS's eventual proprietary memory engine, or runtime adapters beyond OpenCode. Its orchestration, installation services, run-store, memory, and permission contracts must already permit additional runtimes and replacement of Engram without changing core contracts.
