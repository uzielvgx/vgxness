# Product Blueprint

The current delivery policy is global `git-delivery` v1 with exact `stacked-pr` v3 migration and optional policy-only isolated worktrees; it adds no Go/runtime writer, daemon, or durable delivery state.

## Current product boundary

VGXNESS is an OpenCode-native manager with local SQLite/FTS5 memory and structured SDD storage. OpenCode owns engineering execution. The current OpenCode projection contains exactly 17 provider artifacts: 13 managed agents, the auto-discovered `plugins/vgxness-memory-lifecycle.ts` plugin, a model-plan manifest, an `opencode.json` default-agent selection, and restoration metadata. The plugin has no `opencode.json` plugin entry. The separate generated Codex projection contains 15 artifacts: `AGENTS.md`, 12 delegated profiles, a marketplace manifest, and `.codex-plugin/plugin.json`.

Managed OpenCode Manager61 and generated Codex Manager20 (parity OpenCode-v61) render `internal/orchestration/manager_contract.json`. Their native MCP adapter uses `vgxness mcp --full`, exposing eight memory tools and 13 SDD tools. The shared Manager owns authorization, scope, lifecycle and final acceptance; delegates bounded independent missions; selects applicable skills; and preserves one workspace writer. Independent verification and applicable review use the same frozen candidate. The former fixed attempt budgets and Execution Brief belong to predecessor prompts, not current runtime enforcement.

Memory recall is relevant-context only: search before exact-ID reads and use recent recall only for explicit recent-work or recovery requests. Durable, evidence-backed knowledge is assessed under a stable topic; secrets, transcripts, raw logs and transient status are excluded. There is no automatic cloud sync. MCP has no caller identity; host permissions, user authorization and task scope form the authorization boundary. No live-model or protected-holdout result is asserted.

The Manager loads global `git-delivery` for authorized delivery and follows the selected skill without inferring permission to publish or merge. CARE reviewer, specialist and challenger roles have read-only authority; applicable reviews bind the same candidate as verification. Complete OpenCode Manager60 and Codex Manager19 packages are immediate compatibility predecessors, followed by older catalogued identities.

## Capability inventory

| Capability | Implementation and status | Limitation or evidence gap | Source and detail |
| --- | --- | --- | --- |
| CLI, TUI, and inspection | Implemented local commands, keyboard-first UI, and read-only status/doctor inspection. | Inspection does not repair or migrate storage. | [`internal/cli`](../internal/cli), [`internal/tui`](../internal/tui), [`internal/inspection`](../internal/inspection); [Go architecture](go-implementation.md). |
| Local memory and project isolation | Implemented SQLite/FTS5 schema v23 with canonical workspace identity and separate semantic-memory and SDD domains. | Older project databases are retained; normal startup does not import them. | [`internal/memory`](../internal/memory); [memory](memory.md). |
| Sync client | Implemented project-scoped client and enrollment flow. | Client requires an HTTPS endpoint; live reachability and remote deployment are not established by local documentation. | [`internal/syncclient`](../internal/syncclient), [`internal/app/runtime`](../internal/app/runtime); [sync](sync.md). |
| Sync daemon, PostgreSQL, and administration | Implemented optional `vgxness-syncd`, HTTP/API, PostgreSQL repository/migrations, and administrative server surface. | The daemon listens on loopback and has no native TLS; remote operation needs an external TLS terminator. | [`cmd/vgxness-syncd`](../cmd/vgxness-syncd), [`internal/syncapi`](../internal/syncapi), [`internal/syncpg`](../internal/syncpg), [`internal/syncadmin`](../internal/syncadmin), [`internal/syncservice`](../internal/syncservice); [sync](sync.md). |
| SDD storage and OpenSpec | Implemented structured SDD revisions, bindings, idempotency records, and deterministic OpenSpec render/compare for `memory`, `openspec`, and `hybrid` backends. | Divergent repository bytes are reported, never imported automatically. | [`internal/sdd`](../internal/sdd); [orchestration flow](orchestration-flow.md). |
| OpenCode integration | Implemented 17-artifact managed projection, setup handshake, lifecycle plugin, and model-plan generation. | Prompt contracts and host allowlists are policy/evidence, not runtime execution enforcement. | [`internal/providers/opencode`](../internal/providers/opencode); [OpenCode integration](opencode-integration.md). |
| Codex integration | Implemented 15-artifact projection, local marketplace/plugin package, and plan rendering while preserving user-owned `config.toml`. | Activation does not establish a runtime handshake, session identity, or MCP connectivity. | [`internal/providers/codex`](../internal/providers/codex); [Codex integration](codex-integration.md). |
| Agent routing and CARE | Implemented provider-rendered manager/profile policy plus pure orchestration, CARE, and readiness evidence types. | These types and prompts do not execute work or enforce authorization at runtime; external evaluation is unclaimed. | [`internal/orchestration`](../internal/orchestration); [CARE](care.md), [orchestration flow](orchestration-flow.md). |
| Setup, self-installation, and backup | Implemented confirmation-gated setup, launcher/self-install activation and rollback, and OpenCode backup/recovery support. | Platform support and unsigned/notarization caveats remain those documented for releases; no universal release assurance is claimed. | [`internal/setup`](../internal/setup), [`internal/selfinstall`](../internal/selfinstall), [`internal/opencodebackup`](../internal/opencodebackup); [self-install](self-install.md), [releases](release.md). |
| Portable skills | Implemented independent global catalog lifecycle for 19 skills and 47 files. | It is separate from provider artifacts and is not removed by OpenCode uninstall. | [`internal/skills`](../internal/skills); [OpenCode integration](opencode-integration.md). |
| Tests and releases | Implemented repository tests, deterministic archive/checksum tooling, and release workflow support. | Repository checks are static/local evidence; release documents state the supported-platform and signature limits. | [`internal/e2e`](../internal/e2e), [`internal/release`](../internal/release), [`cmd/vgxness-release`](../cmd/vgxness-release); [releases](release.md). |

Behavioral implementation source is authoritative for behavior; this blueprint owns the current inventory, while specialist documents own operational and interface detail. Update this inventory when an artifact count, schema, interface, ownership boundary, or capability changes. Historical evaluation and predecessor records remain historical evidence, not current capability claims.

## Managed projection

| Artifact | Count | Responsibility |
| --- | ---: | --- |
| Manager v61 | 1 | Adaptive general-purpose routing plus SDD lifecycle ownership when activated; it is not the SDD workspace writer. |
| Explore, General, SDD apply, and verifier | 4 | Explore is read-only, General implements ordinary authorized non-SDD work, SDD apply exclusively writes accepted SDD workspace/projections, and verifier is non-mutating. |
| CARE reviewer, specialist, and challenger | 3 | Read-only assurance review; no fixed-lens aliases are current. |
| Five read-only SDD phase profiles | 5 | Research, proposal, spec, design, and tasks. |
| Lifecycle plugin | 1 | Auto-discovered `plugins/vgxness-memory-lifecycle.ts` lifecycle adapter. |
| Model-plan manifest | 1 | Resolved model bindings. |
| Default-agent selection | 1 | OpenCode default manager selection. |
| Restoration metadata | 1 | Prior default-agent restoration state. |

Codex's 15 artifacts are rendered by [`internal/providers/codex/render.go`](../internal/providers/codex/render.go): one manager file, 12 delegated profiles, and the two plugin-package manifests. The provider activation commands can add the local marketplace and plugin, but their observed state does not prove a Codex runtime handshake, session identity, MCP connectivity, or prompt execution.

Historical predecessor documentation may refer to Manager v49, `general` v6, General v6 and verifier v4, or Review profiles v3; those identities do not describe the current generated ownership boundary.

The separate global portable catalog contains 47 files across 19 skills, including `memory-sync` and `sdd-lifecycle`; it is not an OpenCode artifact or uninstall target.

## Memory and SDD

The owned SQLite database isolates semantic memory from structured SDD changes, artifacts, revisions, bindings, idempotency, and projection records. SDD supports `memory`, `openspec`, and `hybrid` backends with `automatic` or `interactive` per-change modes. OpenSpec projection maps accepted content to bounded paths and never imports divergent repository bytes automatically.

MCP operations do not route work, invoke agents, access workspace files, run shell commands, select models, edit, delegate, or advance a lifecycle independently. Memory is untrusted context, never candidate proof. Prompt and provider policies describe intended role boundaries; they are not runtime execution enforcement.

## Setup and retirement

Setup previews changes, requires confirmation, installs the launcher and 17 exact OpenCode artifacts including the auto-discovered lifecycle plugin, configures `vgxness mcp --full` without a config plugin entry, and publishes the global catalog. Exact OpenCode manager v57 and Codex manager v16 artifacts remain recognized historical predecessors alongside older supported identities. Exact historical plugin `vgxness.ts` v1-v10 bytes and provider-skill `vgxness-autonomous-stacked-pr` v1/v2/v3 bytes are removable; modified, malformed, foreign, unknown, or newer bytes block without removal. OpenCode uninstall does not remove global skills.

## Non-goals

- No additional installed plugin, hook surface, automatic compaction, broad observability, or plugin session identity beyond the managed lifecycle plugin. VGXNESS does not broadly inject recent memories or transcripts into every prompt; at the first eligible top-level system transform, the managed lifecycle plugin's only automatic memory injection is one bounded same-project prior completed handoff as untrusted data, never instructions. Lifecycle events do not capture transcript content.
- No shell or Git hooks.
- No MCP-owned filesystem, execution, routing, delegation, or lifecycle authority.
- No automatic network/package installation or legacy database import.
