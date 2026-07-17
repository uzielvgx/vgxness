# VGXNESS

VGXNESS is a local-first agent orchestration system inspired by Gentle AI and Engram. Its goal is to coordinate AI agents, subagents, skills, memory, and auditable workflows without hiding critical state inside prompts.

The system should feel simple during normal use, but remain inspectable when something fails.

## Core idea

VGXNESS separates four responsibilities:

| Layer | Responsibility |
| --- | --- |
| Prompt runtime | Defines how the main agent, subagents, skills, and review agents behave. |
| Memory backend | Stores semantic memory: decisions, discoveries, summaries, bugs, and long-lived context. |
| Run store | Stores operational state: runs, phases, artifacts, events, checkpoints, and validation status. |
| Installer/configurator | Projects VGXNESS into supported agent runtimes such as OpenCode, Hermes, Codex, Claude, or future providers. |

This keeps prompts focused on behavior, memory focused on meaning, and run files focused on traceability.

## Contract schemas

Formal JSON Schema definitions for the initial control-plane and run-store files live in [`docs/schemas/README.md`](docs/schemas/README.md).

The manager/subagent workflow is defined in [`docs/orchestration-flow.md`](docs/orchestration-flow.md).

## Implementation and distribution

VGXNESS is designed for a Go implementation distributed as a globally installed system. OpenCode is the first preferred adapter, but it is selected only when its negotiated capabilities satisfy the current run; the core contracts do not require it. Go provides a dependable local binary and explicit package boundaries for orchestration, installation, storage, and runtime adapters. See [`docs/go-implementation.md`](docs/go-implementation.md) for the architecture.

The primary setup experience is a friendly, keyboard-first terminal wizard. In v1, it detects OpenCode, backs up existing OpenCode configuration, projects VGXNESS configuration into OpenCode, reads the result back for validation, and provides actionable retry, repair, or rollback steps when setup fails.

The setup TUI is an adapter over installation and validation services. It does not own installation policy, memory behavior, or orchestration business logic, and it remains conceptually separate from the normal VGXNESS runtime.

## Design principles

1. **The human leads.** VGXNESS helps coordinate work; it does not replace human approval for risky actions.
2. **Orchestrators coordinate, executors execute.** The main agent should keep a thin context and delegate complex work to scoped subagents.
3. **State must be explicit.** Important workflow state should be readable as files or retrievable from the memory backend.
4. **Memory is semantic, not a log dump.** Engram initially stores what matters long term, not every runtime event, through a replaceable memory-store contract.
5. **Runs are auditable.** Each workflow should explain what ran, which agent ran it, what artifacts were produced, and what validation passed.
6. **The CLI is convenience, not prison.** If the binary is unavailable, files and manifests should still be understandable.
7. **Skills are modular contracts.** Delegation uses exact registry-resolved identity, version, source, provenance, and allowed scope—not names, ranges, or unresolved paths.
8. **Generated technical artifacts default to English.** Conversation tone and artifact language are separate concerns.

## Expected user flow

```text
User request
  -> capability negotiation and provider selection
  -> explainable routing and selective SDD preflight
  -> structured question when required
  -> exact skill resolution and thin context packet
  -> sequential foreground agent (+ read-only background work)
  -> artifact write
  -> structured result and semantic validation
  -> event/snapshot update and continuity capsule
  -> user-language summary
```

The normal experience should stay quiet:

```bash
vgxness status
vgxness run inspect <run-id>
vgxness doctor
```

Those commands exist for inspection, recovery, and debugging. They should not become mandatory ceremony for every interaction.

## Local file layout

VGXNESS should support two storage modes:

| Mode | Location | Use case |
| --- | --- | --- |
| Project-local | `.vgxness/` inside the repo | Shared team traceability and reviewable workflow state. |
| User-global | `~/.vgxness/projects/<project-id>/` | Personal use, experiments, or projects where the repo should not be touched. |

The initial default should be user-global unless the user explicitly enables project-local state.

Recommended structure:

```text
.vgxness/
├── config.json
├── current-run.json
├── runs/
│   └── <run-id>.json
├── logs/
│   └── <run-id>.jsonl
├── artifacts/
│   └── <change-id>/
│       ├── proposal.md
│       ├── spec.md
│       ├── design.md
│       ├── tasks.md
│       └── verify-report.md
└── registry/
    ├── skills.json
    └── agents.json
```

## Run model

A run is one user-requested workflow. It may contain one or many phases.

Example `current-run.json`:

<!-- schema: https://vgxness.dev/schemas/current-run.schema.json -->
```json
{
  "schemaVersion": "1",
  "id": "2026-07-07T01-30-00Z-add-auth",
  "project": "my-app",
  "goal": "Add authentication",
  "status": "running",
  "phase": "design",
  "selectionId": "selection-1",
  "decisionId": "route-1",
  "preflightId": "preflight-1",
  "taskId": "task-design-1",
  "lastEventId": "event-4",
  "artifactIds": ["obs-spec-123"],
  "storageMode": "user-global",
  "startedAt": "2026-07-07T01:30:00Z",
  "updatedAt": "2026-07-07T01:42:10Z"
}
```

Canonical memory artifact reference (a filesystem `path` is optional):

<!-- schema: https://vgxness.dev/schemas/common.schema.json#/$defs/artifactReference -->
```json
{
  "kind": "artifact.reference",
  "schemaVersion": "1",
  "provider": "engram",
  "id": "obs-spec-123",
  "artifactType": "spec",
  "provenance": {
    "producer": "vgxness-requirements",
    "createdAt": "2026-07-07T01:34:00Z",
    "runId": "2026-07-07T01-30-00Z-add-auth",
    "phase": "spec"
  }
}
```

The full run snapshot composes provider selection, routing, SDD preflight, tasks, cancellations, results, capsules, artifacts, and validation records through `run.schema.json`.

Operational events should be append-only JSONL:

<!-- schema: https://vgxness.dev/schemas/run-event.schema.json -->
```jsonl
{"schemaVersion":"1","eventId":"event-1","type":"run.started","runId":"2026-07-07T01-30-00Z-add-auth","at":"2026-07-07T01:30:00Z"}
{"schemaVersion":"1","eventId":"event-2","type":"routing.decided","runId":"2026-07-07T01-30-00Z-add-auth","decisionId":"route-1","at":"2026-07-07T01:30:03Z"}
{"schemaVersion":"1","eventId":"event-3","type":"task.started","runId":"2026-07-07T01-30-00Z-add-auth","taskId":"task-design-1","phase":"design","agent":"vgxness-design","at":"2026-07-07T01:30:05Z"}
{"schemaVersion":"1","eventId":"event-4","type":"artifact.written","runId":"2026-07-07T01-30-00Z-add-auth","artifact":{"kind":"artifact.reference","schemaVersion":"1","provider":"engram","id":"obs-spec-123","artifactType":"spec","provenance":{"producer":"vgxness-requirements","createdAt":"2026-07-07T01:34:00Z","runId":"2026-07-07T01-30-00Z-add-auth","phase":"spec"}},"at":"2026-07-07T01:34:00Z"}
```

JSONL is preferred for logs because it survives partial writes better than rewriting one large JSON document after every event.

## Memory model

The first version uses Engram as its semantic-memory backend. Engram is a temporary implementation choice, not part of the orchestration contract. All memory operations must pass through a replaceable memory-store interface so VGXNESS can later migrate to its own persistence and memory system without changing orchestration behavior or agent contracts.

Use memory for:

- Architecture decisions.
- Bug fixes and root causes.
- Non-obvious discoveries.
- User preferences.
- Session summaries.
- SDD artifacts when the selected artifact backend is memory.

Do not use memory for:

- Every phase event.
- Raw tool logs.
- Temporary command output.
- Data that belongs in deterministic run files.

Recommended topic key pattern:

```text
vgxness/{project}/runs/{run-id}/summary
vgxness/{project}/changes/{change-id}/proposal
vgxness/{project}/changes/{change-id}/spec
vgxness/{project}/changes/{change-id}/design
vgxness/{project}/changes/{change-id}/tasks
vgxness/{project}/changes/{change-id}/verify-report
vgxness/{project}/decisions/{topic}
```

## Agent model

VGXNESS should use a configurable orchestrator plus hidden capability agents. At run start, the manager compares run needs with provider capabilities, versions, constraints, and policy. OpenCode is preferred only when eligible; otherwise an eligible adapter is selected or the run returns a structured `unsupported` result before execution.

| Agent | Role |
| --- | --- |
| `vgxness-manager` | Primary orchestrator. Talks to the user, resolves intent, delegates work, and summarizes outcomes. |
| `vgxness-explore` | Investigates code, context, requirements, and unknowns. |
| `vgxness-propose` | Produces change proposals and scope boundaries. |
| `vgxness-requirements` | Turns proposals into requirements and acceptance scenarios. |
| `vgxness-design` | Produces technical design and tradeoffs. |
| `vgxness-plan` | Breaks work into implementation tasks and review slices. |
| `vgxness-apply` | Implements scoped tasks. |
| `vgxness-verify` | Validates implementation against requirements, design, and tasks. |
| `vgxness-archive` | Closes the change and persists final state. |

Review agents should be capability-specific instead of generic:

| Review lens | Focus |
| --- | --- |
| Risk | Security, permissions, sensitive data, destructive actions. |
| Reliability | Behavior, tests, determinism, regressions. |
| Resilience | Fallbacks, operational failure, recovery, observability. |
| Readability | Maintainability, naming, complexity, review burden. |

## Skill model

Skills are versioned behavior modules.

Each skill should include:

- Name.
- Trigger conditions.
- Scope boundaries.
- Required workflow.
- Examples or checklists.
- Optional references.

The manager should not summarize skills from memory when delegating. It must pass an exact registry-resolved skill reference containing identity, exact version, source, provenance, and allowed scope. `user` is a valid scope; unresolved and out-of-scope skills fail closed.

## Installer model

VGXNESS is installed globally on the user's machine. Its installer/configurator projects the canonical VGXNESS model into supported runtimes.

OpenCode is the first officially supported runtime. The v1 wizard therefore presents a focused OpenCode setup flow rather than a runtime-selection step.

The primary setup interface is a keyboard-first terminal wizard that:

1. Detects VGXNESS prerequisites and the global OpenCode installation and configuration paths.
2. Shows the OpenCode configuration changes that VGXNESS plans to make.
3. Calls installation services to back up existing OpenCode configuration and project the canonical VGXNESS model safely.
4. Reports stable, readable progress during setup.
5. Reads generated configuration back and validates OpenCode readiness.
6. Preserves backup and failure context and offers specific retry, repair, or rollback guidance.

The wizard renders state and collects user decisions. Installation services own prerequisite checks, backups, configuration writes, validation, and recovery actions. Memory adapters own persistence integration, and the orchestrator owns workflow policy. This separation allows setup to evolve without coupling the normal runtime to a TUI framework.

For each runtime adapter, define:

| Adapter concern | Example |
| --- | --- |
| Config path | `~/.config/opencode/opencode.json` |
| Global instruction path | `~/.config/opencode/AGENTS.md` |
| Agent projection | Runtime-specific agent/subagent config. |
| Skill projection | Runtime-specific skill folder. |
| MCP projection | Memory and VGXNESS MCP server config. |
| Backup strategy | Snapshot before writes. |
| Doctor checks | Validate generated config and runtime readiness. |

Adapters should be thin. They should know paths and runtime schema, not business behavior. Core orchestration, installation services, run storage, memory, and permission contracts remain runtime-neutral behind this boundary so Hermes, Claude, Codex, and other runtimes can be added later.

## Recovery model

VGXNESS should be able to recover from interruption by reading:

1. `current-run.json`.
2. The latest event in `logs/<run-id>.jsonl`.
3. The run snapshot in `runs/<run-id>.json`.
4. Artifact existence.
5. Memory summaries when needed.

If these disagree, the system should stop and report the inconsistency instead of guessing.

## First version scope

The first version should be intentionally small:

- Ship VGXNESS as a globally installed Go system.
- Provide the keyboard-first OpenCode setup wizard for detection, backup, configuration projection, progress, readback validation, and actionable recovery.
- Define canonical agent manifest.
- Define run JSON and event JSONL schemas.
- Create local/global storage resolver.
- Add `status`, `doctor`, and `run inspect` commands.
- Add Engram-backed memory save, search, and session-summary integration behind a replaceable memory-store interface.
- Project manager/subagents into OpenCode through the first runtime adapter.
- Keep orchestration, installation services, run storage, memory, and permissions runtime-neutral so later runtime adapters do not change core contracts.

Out of scope for the first version:

- Web dashboard.
- Multi-user sync.
- Complex distributed scheduling.
- Automatic destructive actions without human approval.
- A graphical or web-based setup experience.
- Making the setup TUI the owner of installation, memory, or orchestration logic.
- Building VGXNESS's long-term persistence and memory system in v1.
- Replacing Engram before the memory contract is stable.

This contract revision is documentation and schema only. It does not add Go implementation, execute providers, mutate runtime configuration, permit nested delegation, create VGXNESS-owned memory, or introduce OpenSpec/project state outside the documented schema surface.

## Open questions

- Should project-local state be opt-in per project or globally configurable?
- Should artifact markdown live next to run files or only in the selected artifact backend?
- What level of prompt/template versioning is required before public release?
