# VGXNESS Go Implementation Architecture

This document owns Go package, interface, dependency, and testing boundaries. The [VGXNESS Product Blueprint](product-blueprint.md) is the canonical product vision, taxonomy, status, and roadmap.

| Status | Current-candidate evidence and boundary |
| --- | --- |
| **Implemented** | Binary/application wiring; versioned self-install/update/rollback; storage and SQLite/FTS5 memory; runtime contract validation; Chronicle JSONL events, crash-atomic snapshot publication, terminal recovery, and task-state derivation; Registry, Gatekeeper, prompt composition, provider runner, bounded coordinator, OpenCode adapter/bridge, guided setup, hermetic setup/dispatch E2E, tests, and Go CI. |
| **Partial** | Chronicle recovery backs explicit cross-process `start`/`continue`/`finish` continuity with capsules and semantic-memory summaries. Navigator now has validated delegation request/plan/join contracts, deterministic waves, production file-backed owner/epoch authority, native planning/task children, authoritative Join, and lifecycle controls. Delivery Authority now provides content-bound receipts and four validating CLI gates. Chronicle plan events, complete SDD checkpoint/artifact lifecycle, and automatic delivery-gate rollout remain planned. |
| **Contracts-only** | Schemas for broader SDD, routing, artifact, and continuity behaviors that are not exercised by the delivered bounded runtime path. |
| **Planned** | Full SDD phase workflows and artifact backends, automatic Git/branch-protection delivery wiring, richer autonomy/approval UX, the keyboard TUI, optional adapters, and advanced semantic-memory lifecycle operations. |

VGXNESS is designed as a small, explicit Go control plane for adaptive agent orchestration. It will ship as a globally installed system on the user's machine. Go fits because the system needs a dependable local binary, readable storage, auditable workflow state, and testable package boundaries more than a framework-heavy stack.

OpenCode is the first preferred runtime adapter. At run start, a provider-neutral selector compares declared capabilities, versions, constraints, and policy; OpenCode is used only when eligible. The same thin boundary permits future Hermes, Claude, Codex, and other adapters without changing core contracts.

Capability names such as Navigator, Scout, Blueprint, Forge, Sentinel, and optional Challenger belong to the canonical product taxonomy. Names such as explore, design, apply, and verify describe SDD phase-agent operating modes that use those capabilities; they are not additional product capabilities.

The implemented control plane is deliberately narrower than the target product. It executes a bounded OpenCode path with exact Registry identities, Gatekeeper policy, contract-validated packets, crash-atomic Chronicle snapshot publication, finite coordination, a manager-facing native Navigator/wave runtime, and product-native content-bound delivery receipts. Full SDD workflows, automatic delivery integration, richer TUI, and additional providers are not yet delivered.

## Quick path

1. **Implemented:** Build a single `vgxness` binary from `cmd/vgxness`; install it behind a permanent validated launcher with immutable versions and rollback; resolve storage roots; expose status/doctor and memory save/search/get.
2. **Implemented:** Keep owned semantic memory on SQLite/FTS5; `memory` is the sole runtime backend and Engram integration is a non-goal.
3. **Implemented:** Validate runtime contracts; append and verify Chronicle events; write/read snapshots; derive legal task state; reconstruct bounded recovery state.
4. **Implemented:** Resolve exact Registry identities, evaluate Gatekeeper policy, compose frozen prompts, execute eligible providers, and coordinate finite foreground/background work.
5. **Implemented:** Install and operate the persistent OpenCode manager, strict bridge, runtime adapter, and six-step confirmation-gated setup workflow.
6. **Partial/Planned:** Harden the delivered native Navigator/adaptive wave runtime and Delivery Authority, wire gates into supported delivery workflows, broaden Chronicle plan/artifact recovery, then add the keyboard TUI and optional adapters. Native Windows runtime/distribution smoke is deferred.

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
- No Engram integration or coupling in runtime contracts. VGXNESS-owned memory is authoritative.
- No claim that schemas, the Chronicle reader, or implemented memory constitute provider execution, task-state, recovery, or orchestration behavior.

## Package delivery map

```text
cmd/vgxness
internal/app
internal/config
internal/memory
internal/cli
internal/inspection
internal/bridge
internal/chronicle
internal/contracts
internal/launcher
internal/selfinstall
internal/registry
internal/gatekeeper
internal/prompts
internal/providers
internal/providers/opencode
internal/navigator
internal/orchestrator
internal/controlplane
internal/delivery
internal/integration
internal/sensitivepaths
internal/setup

# planned addition
internal/tui
```

| Package | Status | Responsibility |
| --- | --- | --- |
| `cmd/vgxness`, `internal/app` | **Implemented** | Binary entrypoint and composition for configuration, inspection, memory, setup, bridge, and bounded control-plane commands. |
| `internal/config` | **Implemented** | Resolve per-project operational roots plus the default unified user memory database and explicit isolated overrides. |
| `internal/memory` | **Implemented** | Own unified SQLite/FTS5 persistence, project isolation, migrations/imports, typed records, lifecycle fields, save/search/get, and deterministic filtering. |
| `internal/cli`, `internal/inspection`, `internal/bridge` | **Implemented** | Expose status, doctor, memory, setup, integration, and bounded dispatch with safe structured output and operational error classes. |
| `internal/launcher`, `internal/selfinstall` | **Implemented** | Validate and forward through a permanent launcher; install immutable SHA-256 versions; atomically activate updates and roll back one level without overwriting foreign content. |
| `internal/chronicle` | **Implemented/Partial** | Implement strict current-run reading, durable/verified JSONL event append, immutable SHA-256 active snapshots, atomic pointer publication, recoverable terminal finalization, task/cancellation state, and bounded recovery reconstruction. General checkpoint/artifact continuity remains planned. |
| `internal/contracts` | **Implemented** | Embed Draft 2020-12 schemas and validate current runtime packets, events, snapshots, registry records, prompts, and results; broader future schemas remain contracts-only until used. |
| `internal/registry`, `internal/gatekeeper` | **Implemented** | Resolve exact agent/skill/prompt identities and fail closed on capabilities, adapter health, operations, roots/tools, risk, leases, approvals, and task transitions. |
| `internal/prompts`, `internal/providers` | **Implemented** | Compose frozen prompt contracts, select an exact eligible provider, execute it, validate structured results, and emit bounded receipts. |
| `internal/providers/opencode` | **Implemented** | Execute the supported OpenCode provider with ephemeral fail-closed permissions, cancellation, bounded output, and strict review evidence. |
| `internal/orchestrator`, `internal/controlplane` | **Implemented/Partial** | Coordinate finite foreground/background work; persist adaptive plans, authority leases, prepared dispatches, terminals, dependency results, and joins; expose bounded lifecycle operations. Full SDD routing and artifact projection remain planned. |
| `internal/navigator` | **Implemented/Partial** | Validate advisory task decomposition, reject unsafe graphs, compute content-bound single/sequential/parallel decisions, and produce deterministic waves consumed by native planning/task sessions. Full SDD routing remains planned. |
| `internal/delivery` | **Implemented/Partial** | Build representation-independent Git target snapshots; bind context, evidence, and review identities; persist immutable receipts; and validate or explicitly invalidate the same receipt across four gates. Automatic Git/hosting integration remains planned. |
| `internal/setup` | **Implemented** | Compose self-installation, stable-path OpenCode projection, live verification, bounded rollback, and a complete explanatory plan without owning provider or installer policy. |
| `internal/tui` | **Planned** | Render the implemented setup service as a richer keyboard-first interface without moving business logic into the UI. |

## Dependency rules

Keep dependencies pointed toward stable contracts, not convenience shortcuts. These rules describe the delivered package boundaries and the remaining TUI/adaptor extensions.

| Rule | Reason |
| --- | --- |
| `cmd/vgxness` depends only on `internal/app`. | The executable should not become a second composition root. |
| `internal/app` may depend on every package for wiring. | Dependency construction belongs in one obvious place. |
| `internal/cli` calls application services; it should not own orchestration policy. | CLI is convenience, not the workflow source of truth. |
| `internal/navigator` is a pure deterministic planner over validated contracts; it does not create sessions, prepare tickets, or mutate Chronicle. | Advisory decomposition cannot become execution authority by itself. |
| `internal/orchestrator` revalidates Navigator's content-bound plan; uses a caller-owned, execution-scoped factory; claims an owner/epoch lease; and requires its file-backed authority to atomically validate confirmed prerequisites, reserve logical task slots, persist prepared replay, classify bounded native dispatch, and accept the final Join against a revocation-aware checkpoint. | Runtime safety comes from the durable authority protocol, not a process-global registry or a read-then-publish lease check. `ValidateLease` is only a freshness probe; `AcceptJoin` is the publication linearization point. Confirmed dispatches run; not-started or uncertain dispatches remain public and fail closed without automatic redispatch. |
| `internal/orchestrator` depends on small interfaces for run storage, memory, agents, skills, providers, permissions, and validation. | The orchestration core stays testable with fakes. |
| `internal/chronicle` does not depend on `internal/memory` or `internal/providers`. | Operational truth must stay deterministic and local-file readable. |
| `internal/memory` does not depend on `internal/chronicle`. | Semantic memory should not become an event log. |
| `internal/providers` does not depend on `internal/cli`. | Provider execution must work outside terminal commands. |
| `internal/tui` depends on setup application services, not installer, memory, or orchestrator implementations. | The wizard is an adapter and can evolve or be replaced without moving business logic. |
| `internal/gatekeeper` is checked before provider or command-like actions. | Risky behavior must be impossible to bypass accidentally. |
| `internal/contracts` validates inputs and outputs without mutating workflow state. | Validation proves state; it should not hide repair side effects. |

## Storage choices

Chronicle's target operational truth remains readable local files:

- `current-run.json` for the active run pointer and current phase.
- `runs/<run-id>.<sha256>.json` for immutable active snapshots referenced by the pointer.
- `runs/<run-id>.json` for the stable terminal snapshot published when the pointer is removed.
- `logs/<run-id>.jsonl` for append-only operational events.
- `artifacts/<change-id>/...` for generated SDD or workflow artifacts when file storage is selected.
- `registry/skills.json` and `registry/agents.json` for generated registries.
- `delivery/receipts/<receipt-id>.json` for immutable content-bound review receipts and `delivery/current.json` for the atomic active/invalidated pointer.

**Implemented:** Chronicle validates JSONL append/readback and task-state replay, writes active snapshots under immutable SHA-256 names, commits an active transition with one atomic pointer replacement, and stages a terminal snapshot before atomically removing the pointer. Recovery validates the pointer's digest and completes an interrupted terminal removal. **Partial:** broader checkpoint/artifact continuity remains outside the bounded runtime. **Implemented:** one default user SQLite/FTS5 database stores owned semantic records for multiple projects with four migrations, a canonical workspace-to-project registry, project-scoped topic/session uniqueness, deterministic filters, bounded any-term lexical retrieval, and idempotent import of legacy project databases. Semantic memory does not replace Chronicle operational truth.

The dependency sequence through bounded coordination is now delivered. Further Chronicle work focuses on broader checkpoint/artifact continuity and lifecycle cleanup rather than the now-delivered crash-atomic snapshot/pointer publication.

## Interfaces

Keep interfaces small and close to the package that consumes them. Do not create broad manager interfaces just because a concrete package exists.

The following are architecture-level examples of the small interfaces used around the implemented coordinator; they are illustrative rather than exact exported APIs:

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

The implemented coordinator and provider runner follow these small-boundary rules. Provider selection records a neutral provider reference and capability evidence, never mutable provider configuration. Runtime validation occurs before provider execution and Chronicle mutations on the delivered path.

The **Implemented** `MemoryStore` is VGXNESS-owned and SQLite/FTS5-first. It stores typed observations with provenance, scope, topic, lifecycle state, references, and save/search/get behavior. Every bounded control-plane task resolves a durable project identity, retrieves and hydrates at most three relevant observations into a fixed 4,096-rune packet budget, and saves one idempotent terminal summary whose references identify the retrieved evidence. Richer approval, comparison, review, and derived-summary workflows remain **Planned**; the owned-backend contract uses `memory`, so no backend migration is pending.

## Adaptive execution boundaries

| Boundary | Status | Go responsibility |
| --- | --- | --- |
| Selection | **Implemented** | Compare run needs with exact provider capabilities, health, and policy before calling `Run`. |
| Routing | **Implemented/Partial** | Delegation contracts, stable request digests, rationales, dependency validation, safe parallel waves, native child-session identity binding, durable prerequisite-gated prepared admissions, dispatch classification, owner takeover, and authority-accepted joins are delivered. Public joins preserve `confirmed`/`not_started`/`uncertain`; full SDD routing remains planned. |
| Thin context | **Implemented** | Validate and pass one task-scoped execution packet with exact scope, tools, criteria, and return contract. |
| Skills | **Implemented** | Resolve exact identity, version, source, provenance, checksum, and allowed scope before dispatch. |
| Foreground | **Implemented** | Advance one manager-owned foreground task at a time. |
| Background | **Implemented, bounded** | Enforce read-only, non-delegating, non-advancing tasks with cancellation and finite deadlines. |
| Loops | **Implemented, bounded** | Enforce finite iteration/deadline budgets and terminal reasons in the coordinator path. |
| Continuity | **Implemented, bounded** | An opt-in dispatch can start, continue, and finish one active run across processes using immutable Chronicle snapshots, event-backed capsules, stale-continuation rejection, curated SQLite/FTS5 retrieval, and one durable memory summary per phase. Each phase is prepared as a durable ticket, executed by a native OpenCode child session, and accepted before state advances. Explicit status/cancel controls and parallel task graphs remain planned; a blocked run resumes through `continue`. |

Runtime validation is **Implemented** at packet, registry, event, snapshot, prompt, result, and readback boundaries used by the current control plane. Schemas for future SDD artifacts and continuity flows remain **Contracts-only** until those runtime paths exist.

## Setup wizard boundary

The implemented headless wizard is `vgxness setup opencode`: it explains all six phases, requires an explicit `provider/model` execution identity, detects prerequisites, previews exact destinations, requires explicit confirmation, installs the stable binary and OpenCode projections, performs readback plus a live handshake, and reports bounded recovery. The model is embedded outside the LLM-controlled tool arguments because OpenCode's custom-tool context does not expose the outer conversation model. The generated tool projection uses `node:child_process` with an argument vector and `shell: false`, so the same bounded bridge runs under the Bun-hosted CLI and the Node-hosted Desktop sidecar without relying on a runtime global. Lower-level `vgxness self` and `vgxness integrate opencode` commands remain independently testable. A future richer TUI must render the same setup service rather than duplicating its policy.

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
| Chronicle | **Implemented/Partial:** reader, JSONL append/rollback/readback, immutable snapshot publication, crash recovery, digest tampering, task transitions, corruption, symlink, locking, and cancellation are tested; broader checkpoint/artifact continuity remains open. |
| Contract semantics | **Implemented:** embedded-schema validation and semantic invariants are exercised in runtime packages; future-only schema paths remain contracts-only. |
| CLI/application | **Implemented:** output and wiring tests cover status, doctor, memory, self-install, OpenCode integration/setup, bridge status, and dispatch. |
| Registry/Gatekeeper, prompts, providers, coordinator | **Implemented:** deterministic behavior tests use fakes; OpenCode has adapter, process, permission, cancellation, and integration coverage. |
| Platform portability | **Implemented/Partial:** CI cross-builds Windows amd64/arm64 and compiles all Windows amd64 test packages; native Windows execution and distribution smoke remain open. |
| Vertical E2E | **Implemented:** an opt-in `e2e` test builds from the checkout with network-disabled nested builds, isolates home/config/data/workspace, completes the six-step setup, proves launcher independence from the source binary, dispatches through the real control plane, and verifies Chronicle evidence using a deterministic OpenCode protocol fake. |

Prefer behavior tests over implementation trivia. Each package should be testable without real providers, external network calls, package installation, or git operations.

## First version scope in Go

Continue from the delivered foundation in dependency order:

1. **Implemented:** Create the `vgxness` binary and explicit wiring.
2. **Implemented:** Resolve project-local `.vgxness/` and user-global `~/.vgxness/projects/<project-id>/` operational storage, with shared semantic memory at `~/.vgxness/memory.db`.
3. **Implemented:** Add SQLite/FTS5 memory save/search/get, lifecycle fields, migrations, status/doctor, tests, and CI.
4. **Implemented:** Validate runtime contracts and Chronicle current-run/event/task-state records.
5. **Implemented:** Append/read back Chronicle JSONL events and write/read/reconstruct snapshots and recovery state.
6. **Implemented:** Resolve Registry identities, enforce Gatekeeper decisions, compose prompts, execute providers, and coordinate bounded tasks.
7. **Implemented:** Install and operate the persistent OpenCode manager, strict bridge, runtime adapter, and guided CLI setup.
8. **Partial/Planned:** Connect native Navigator/wave execution, Delivery Authority, and broader Chronicle plan/artifact continuity; then add the keyboard TUI and optional adapters. Native Windows runtime/distribution validation remains deferred.

The first version does not include a graphical installer, multi-user sync, distributed scheduling, autonomous destructive actions, advanced embedding infrastructure, Engram integration, or runtime adapters beyond OpenCode. Its orchestration, installation, Chronicle, memory, adapter, and permission contracts must permit later runtimes without changing core authority.
