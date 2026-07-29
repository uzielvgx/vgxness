# OpenCode integration

VGXNESS installs 14 managed artifacts: 12 agents (`vgxness-manager` v28, five hidden read-only review profiles, and six hidden read-only SDD profiles), the bounded storage plugin v5, and one non-secret model-plan manifest.

The plugin exposes exactly 18 tools: five semantic-memory tools and 13 SDD tools.

- `vgxness_memory_search`
- `vgxness_memory_recent`
- `vgxness_memory_get`
- `vgxness_memory_save`
- `vgxness_memory_forget`
- `vgxness_sdd_create`
- `vgxness_sdd_list`
- `vgxness_sdd_get`
- `vgxness_sdd_set_interaction_mode`
- `vgxness_sdd_save_revision`
- `vgxness_sdd_get_revision`
- `vgxness_sdd_list_revisions`
- `vgxness_sdd_accept_revision`
- `vgxness_sdd_transition`
- `vgxness_sdd_projection_status`
- `vgxness_sdd_record_projection`
- `vgxness_sdd_render_projection`
- `vgxness_sdd_compare_projection`

The SDD tools store structured changes and immutable accepted revisions or transform supplied bytes. Read operations remain available to the bounded SDD profiles, but every SDD mutation fails closed unless it comes from a tracked top-level `vgxness-manager` session. They do not route work, invoke agents, access the filesystem, write OpenSpec files, or advance phases on their own. The plugin does not expose orchestration, control-plane status, dispatch, tickets, editing, validation, CodeGraph, shell execution, model selection, or subagent creation. OpenCode remains the execution authority for all engineering work.

## Install and inspect

```sh
vgxness setup opencode --preview
vgxness setup opencode
vgxness setup opencode --status
```

The lower-level lifecycle remains available:

```sh
vgxness integrate opencode preview
vgxness integrate opencode install
vgxness integrate opencode status
vgxness integrate opencode uninstall
```

`--config-dir` can select a non-default OpenCode configuration directory. `--model-plan low|medium|high` selects the active matrix; `--model-efficient`, `--model-balanced`, and `--model-frontier` replace individual exact provider/model slots. Flags overlay the verified installed manifest, so omitted plan and slot values are preserved rather than reset. All slots must use one provider. The deprecated singular `--model` flag remains a no-op compatibility option and never overrides the plan.

Fresh no-flag setup installs the medium plan with `openai/gpt-5.6-luna-fast`, `openai/gpt-5.6-terra`, and `openai/gpt-5.6-sol`. The canonical manifest is stored at `<config-dir>/vgxness/model-plan.json`; it contains no credentials and binds the resolved role assignments to exact managed agent digests. VGXNESS does not create or modify `opencode.json` and the storage plugin does not route models.

Preview and status are read-only. Installation creates absent managed artifacts and atomically upgrades only exact catalogued older VGXNESS versions with matching artifact identities. The current catalogue installs manager v28 and storage plugin v5 and recognizes the exact prior manager v27/plugin v4 set for upgrade. It refuses foreign, modified, malformed, equal-version drifted, or newer content. Uninstall removes only exact managed artifacts, writes recoverable hard-link backups, and refuses drift. A failed rollback or restore is returned as an explicit `recovery` failure instead of being hidden.

Changing the plan or a slot regenerates the same managed agent set only when every current byte still matches the installed current or exact prior manifest. An interrupted switch containing an exact mixture of those old and new bytes resumes safely; any unrelated byte drift blocks regeneration. The change becomes active only after OpenCode restarts. Manual modification of an agent, plugin, or manifest blocks regeneration.

## Memory authority

VGXNESS's SQLite/FTS5 `MemoryStore` is the only persistent memory authority. The plugin resolves project identity from OpenCode's trusted workspace directory, so agents cannot select another project by supplying an arbitrary project ID.

The default database is `~/.vgxness/memory.db`. Records remain isolated by canonical workspace binding, project, scope, topic, type, state, session, provenance, and references.

The same schema-v5 database contains separate structured SDD tables. Immediately after a binary upgrade from schema v4, read-only opens cannot migrate: `status`, `doctor`, `setup opencode --status`, and read tools may report a storage/migration failure until one write-capable memory or SDD operation opens the database and atomically applies v5. Do not delete the database. Run the write-capable operation and rerun status; see [Native memory](memory.md#upgrade-migration-caveat).

The manager and plugin:

- automatically recall recent active project memories once on the first top-level manager turn, append them as bounded untrusted reference data, and preserve that context across later model calls and compaction;
- fall back to the explicit `vgxness_memory_recent` tool only when the automatic bounded context block is absent or unavailable;
- searches memory with any-term matching when prior project decisions, fixes, discoveries, or conventions may matter;
- reads full content only after a relevant search result;
- saves durable evidence-backed knowledge immediately;
- reuses stable topic keys for evolving subjects;
- never stores secrets, personal data, transient progress, raw command output, or full transcripts;
- forgets a memory only after an explicit user request.

Reviewers may search and read memory as non-authoritative context. They cannot save or forget. Memory never proves a candidate diff and never overrides exact source, tests, Git evidence, or Chronicle operational truth.

The plugin launches the exact managed VGXNESS executable with an argument vector and `shell=false`, passes bounded JSON through stdin, limits output and runtime, supports cancellation, and inherits only the minimal home/temp environment required to locate owned storage. It does not forward credentials.

The generated plugin also uses OpenCode's `event`, `chat.message`, `experimental.chat.system.transform`, `experimental.session.compacting`, `tool.execute.before`, `tool.execute.after`, and `dispose` hooks. Session state is closure-owned and bounded. Tool observation retains only tool/session/call correlation, timing, and successful completion; it never captures arguments, output, titles, metadata, prompts, or errors and never mutates tool inputs or outputs. Hook and memory failures are fail-open for chat, compaction, and tool execution.

These generated OpenCode callbacks are the shipped active hook surface. The typed internal Go dispatcher remains available only to in-process callers that explicitly register and inject handlers; the application registers no production handlers.

These OpenCode callbacks are not arbitrary shell hooks or Git hooks. VGXNESS intentionally installs neither; see [Safe hooks](hooks.md) for event semantics, exclusions, and delivery guarantees.

Engram is not part of this integration.

## Structured SDD storage and OpenSpec projection

SDD changes, artifacts, revisions, input bindings, and projection records use isolated tables in the owned SQLite database. They are not semantic memories and never appear in recent recall or memory search. The plugin resolves the project from the trusted workspace for every SDD operation. Create retries reuse a project-scoped idempotency key and must match the original normalized payload. Revision lists return metadata summaries without bodies; exact bodies require `get-revision`. Per-change automatic/interactive mode can be changed later only with an optimistic state version, and save or acceptance is valid only for the change's current phase.

The backend determines canonical content ownership. `memory` stores canonical artifact bodies in structured SDD storage. `openspec` stores only the external repository-relative location, SHA-256 digest, revision identity, and input bindings in SQLite; the canonical body remains in the repository. `hybrid` stores canonical memory content and tracks OpenSpec as a projection.

OpenSpec projection is a pure deterministic adapter. It maps accepted artifacts to bounded paths under `openspec/changes/<safe-change-id>/` and returns managed Markdown bytes with exact revision and digest metadata. Render and compare receive or return bytes through bounded JSON; neither operation reads, follows symlinks, creates directories, nor writes files. For `openspec`, the manager uses ordinary OpenCode workspace tools to write and read back the canonical file, then records bounded digest evidence. The plugin remains filesystem-free.

Comparison reports `synced`, `drifted`, or `missing`. In hybrid mode memory is canonical. A valid divergent projection may be inspected, replaced from a freshly rendered canonical result, or submitted explicitly through `vgxness_sdd_save_revision` as a new candidate. Compare never imports divergent content, overwrites an accepted revision, or changes lifecycle state.

## Other native capabilities

The manager uses ordinary OpenCode workspace tools, built-in `explore` and `general` Task workers, skills by native registry name, optional user-approved SDD, the five review profiles, and the six model-bound SDD profiles. VGXNESS does not override the built-in `explore` or `general` definitions.

All six SDD profiles are read-only. Research, proposal, spec, design, and tasks return evidence or candidate artifact content; apply accepts an exact hash-bound mission and returns a bounded patch and validation plan without editing files or running commands. No SDD profile can ask questions, delegate, persist memory, save or accept revisions, record projections, or transition lifecycle state. The manager is the sole workspace writer, test runner, lifecycle authority, and authorized caller for SDD persistence mutations.

Each accepted SDD change follows `explore -> proposal -> spec -> design -> tasks -> apply -> verify -> complete`. A transition requires an accepted artifact for the current phase; `openspec` and `hybrid` also require a current projection record bound to that revision and digest. The manager sends phase agents exact change, artifact, accepted-input, evidence-scope, and return contracts. It may overlap at most four independent read-only missions bound to the same inputs; final artifacts, manager-applied patches, validation, and all lifecycle writes remain sequential and single-authority.

When a project has a healthy `.codegraph` index, the manager and reviewers may use one bounded `codegraph_explore` query for structural evidence. Exact source, Git diff, and test output remain authoritative.

### Adaptive workflow and interaction

The manager chooses the smallest capable native route: direct inline work, read-only exploration, plan-only work, bounded delegated implementation, or optional SDD. File count influences inline versus delegated topology; it does not force ceremony or SDD. TDD, spikes, vertical slices, review, and validation are composable practices within those routes.

Interaction mode is resolved in this order:

1. an explicit override for the current task;
2. a durable project default recalled from VGXNESS memory;
3. automatic mode as the fallback.

Automatic mode resolves reversible choices from repository evidence and asks only for required authorization, irreversible or high-consequence ambiguity, unavailable prerequisites, or acceptance before SDD. Interactive mode asks about consequential route, architecture, behavior, scope, or testing tradeoffs while still deriving routine facts from the repository. A task override is never persisted; an explicitly requested project default may be retained as a durable decision.

After SDD is accepted, the manager separately asks whether that change uses automatic or interactive phase execution. Automatic SDD advances validated gates without routine pauses but still stops for hard decisions, authorization, unavailable evidence, and drift. Interactive SDD pauses at every validated candidate boundary for approve, revise, or cancel. This choice is stored on the SDD change and does not alter the manager's general project default.

The primary manager has explicit access to OpenCode's native `question` tool. It asks one blocking decision at a time, presents the recommended option first, and resumes without repeating the same question. Questions do not grant permission, override a denial, or move terminal and diagnostic work to the user. Review profiles cannot ask questions.

### Adaptive TDD

For safely testable regressions and behavior changes, the manager prefers an observable RED -> GREEN -> REFACTOR cycle. It may claim TDD only when the test was run and observed failing for the expected reason before the production change. Tests added after implementation are reported as regression coverage instead.

TDD is not a universal gate. Documentation, passive assets, generated code, disposable spikes, and changes for which a safe failing test cannot be expressed use proportional validation with an explicit rationale. SDD defines requirements and design; TDD may guide implementation and does not replace SDD.

## Health contract

The integration is installed only when manager v28, all five reviewers, all six SDD profiles, storage plugin v5, and the model-plan manifest match their managed identities exactly. Setup health combines:

1. the permanent VGXNESS launcher is installed and verified;
2. all 14 managed artifacts are installed without drift;
3. the OpenCode adapter handshake succeeds for the selected workspace.

The old execution bridge and any modification to `opencode.json` are not readiness requirements.

Restart OpenCode Desktop after installation or a plan switch so it reloads the profiles, model bindings, variants, and storage plugin.
