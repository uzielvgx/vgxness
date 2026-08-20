# Product Blueprint

## Current product boundary

VGXNESS is an OpenCode-native manager with local SQLite/FTS5 memory and structured SDD storage. OpenCode owns engineering execution. The current MCP-only projection contains exactly 18 provider artifacts: 15 managed agents, a model-plan manifest, an `opencode.json` default-agent selection, and restoration metadata. No plugin is installed.

Managed OpenCode v52 and generated Codex v12 use `vgxness mcp --full`, exposing five memory tools and 13 SDD tools. Their shared prompt contract silently selects the least-cost route without classification tools: no-effect conversation, writing, translation, summarization, brainstorming, and planning use a zero-execution-tool fast path; bounded exact reads allow at most three attempts without delegation or todos; complex evidence research may use one read-only delegation. Attempts include failures and retries and stop before budget exhaustion. This is prompt policy, not runtime enforcement.

Recall remains intent-triggered: search all terms first, retry any-term only when needed, retrieve exact IDs after preview, and use recent recall only for explicit recent-work, session, or compaction-recovery requests. Orthogonally, after any route the prompt permits at most one autonomous save only for durable, evidence-backed, safely assessed project decisions, preferences, constraints, or learnings; it excludes transient state, logs, secrets, and personal data, adds no engineering ceremony, and performs no automatic cloud sync. MCP has no caller identity; host/operator permissions, user authorization, and task scope own authorization. No external, NLP, or holdout result is claimed.

Current delivery policy is manager v52 with global `stacked-pr` v3. The manager requires its clean pre-write gate before branch creation, source writes, or routine delivery announcements. The managed `general` v9, `sdd-apply` v6, verifier v6, and reviewer profiles v4 except reliability v5 are current.

## Managed projection

| Artifact | Count | Responsibility |
| --- | ---: | --- |
| Manager v52 | 1 | Adaptive general-purpose routing plus SDD lifecycle ownership when activated; it is not the SDD workspace writer. |
| General v9, SDD apply v6, and verifier v6 | 3 | General implements ordinary authorized non-SDD work; SDD apply exclusively writes accepted SDD workspace/projections; verifier is non-mutating. |
| Review profiles v4 except reliability v5 | 5 | Read-only specialist review. |
| Explore and five read-only SDD phase profiles | 6 | Read-only research plus research, proposal, spec, design, and tasks SDD roles. |
| Model-plan manifest | 1 | Resolved model bindings. |
| Default-agent selection | 1 | OpenCode default manager selection. |
| Restoration metadata | 1 | Prior default-agent restoration state. |

Historical predecessor documentation may refer to Manager v49, `general` v6, General v6 and verifier v4, or Review profiles v3; those identities do not describe the current generated ownership boundary.

The separate global portable catalog contains 47 files across 19 skills, including `memory-sync` and `sdd-lifecycle`; it is not an OpenCode artifact or uninstall target.

## Memory and SDD

The owned SQLite database isolates semantic memory from structured SDD changes, artifacts, revisions, bindings, idempotency, and projection records. SDD supports `memory`, `openspec`, and `hybrid` backends with `automatic` or `interactive` per-change modes. OpenSpec projection maps accepted content to bounded paths and never imports divergent repository bytes automatically.

MCP operations do not route work, invoke agents, access workspace files, run shell commands, select models, edit, delegate, or advance a lifecycle independently. Memory is untrusted context, never candidate proof.

## Setup and retirement

Setup previews changes, requires confirmation, installs the launcher and 18 exact OpenCode artifacts, configures `vgxness mcp --full`, and publishes the global catalog. Exact OpenCode Manager v48 and Codex manager v8 artifacts remain recognized historical predecessors alongside older supported identities. Exact historical plugin `vgxness.ts` v1-v10 bytes and provider-skill `vgxness-autonomous-stacked-pr` v1/v2/v3 bytes are removable; modified, malformed, foreign, unknown, or newer bytes block without removal. OpenCode uninstall does not remove global skills.

## Non-goals

- No installed plugin, hook surface, automatic memory injection, compaction, observability, or plugin session identity.
- No shell or Git hooks.
- No MCP-owned filesystem, execution, routing, delegation, or lifecycle authority.
- No automatic network/package installation or legacy database import.
