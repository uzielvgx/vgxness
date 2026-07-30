# OpenCode integration

VGXNESS installs 20 managed artifacts: 15 agents (`vgxness-manager` v36, managed `general` and verifier profiles, a read-only `explore` override, five hidden read-only review profiles, and six hidden read-only SDD profiles), the bounded storage plugin v5, one non-secret model-plan manifest, `opencode.json` with the default-agent selection plus bounded restoration metadata, and the independent `vgxness-autonomous-stacked-pr` skill. The skill is not model-bound and is absent from the model-plan manifest and OpenCode configuration.

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

Fresh no-flag setup installs the medium plan with `openai/gpt-5.6-luna-fast`, `openai/gpt-5.6-terra`, and `openai/gpt-5.6-sol`. The canonical manifest is stored at `<config-dir>/vgxness/model-plan.json`; it contains no credentials and binds the resolved role assignments to exact managed agent digests. VGXNESS creates or updates `opencode.json` with `default_agent: "vgxness-manager"`, preserving every unrelated JSON value. It preserves any existing `opencode.jsonc` byte-for-byte. Bounded metadata at `<config-dir>/vgxness/default-agent.json` restores a prior explicit default during uninstall. The storage plugin does not route models.

Preview and status are read-only. The managed agent catalogue contains only manager v36 and the current 14 agent profiles. Older manager, agent, and model-plan artifacts are treated as drift and preserved byte-for-byte; they are never upgraded automatically. The v2 autonomous stacked-PR skill recognizes only its exact embedded v1 predecessor for upgrade; modified v1, malformed, foreign, equal-version drifted, and newer content are refused and preserved. Exact catalogued storage-plugin predecessors remain upgradeable. Uninstall removes only exact managed artifacts, writes recoverable hard-link backups, and refuses drift. A failed rollback or restore is returned as an explicit `recovery` failure instead of being hidden.

Changing the plan or a slot regenerates the same managed agent set only when every current byte still matches the installed current manifest. An interrupted switch containing an exact mixture of the verified source and requested target bytes resumes safely; any unrelated byte drift blocks regeneration. The change becomes active only after OpenCode restarts. Manual modification of an agent, plugin, or manifest blocks regeneration.

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

Reviewers may search and read memory as non-authoritative context. They cannot save or forget. Memory never proves a candidate diff and never overrides exact source, tests, or Git evidence.

The plugin launches the exact managed VGXNESS executable with an argument vector and `shell=false`, passes bounded JSON through stdin, limits output and runtime, supports cancellation, and inherits only the minimal home/temp environment required to locate owned storage. It does not forward credentials.

The generated plugin also uses OpenCode's `event`, `chat.message`, `experimental.chat.system.transform`, `experimental.session.compacting`, `tool.execute.before`, `tool.execute.after`, and `dispose` hooks. Session state is closure-owned and bounded. Tool observation retains only tool/session/call correlation, timing, and successful completion; it never captures arguments, output, titles, metadata, prompts, or errors and never mutates tool inputs or outputs. Hook and memory failures are fail-open for chat, compaction, and tool execution.

These OpenCode callbacks are not arbitrary shell hooks or Git hooks. VGXNESS intentionally installs neither; see [Safe hooks](hooks.md) for event semantics, exclusions, and delivery guarantees.

Engram is not part of this integration.

## Structured SDD storage and OpenSpec projection

SDD changes, artifacts, revisions, input bindings, and projection records use isolated tables in the owned SQLite database. They are not semantic memories and never appear in recent recall or memory search. The plugin resolves the project from the trusted workspace for every SDD operation. Create retries reuse a project-scoped idempotency key and must match the original normalized payload. Revision lists return metadata summaries without bodies; exact bodies require `get-revision`. Per-change automatic/interactive mode can be changed later only with an optimistic state version, and save or acceptance is valid only for the change's current phase.

The backend determines canonical content ownership. `memory` stores canonical artifact bodies in structured SDD storage. `openspec` stores only the external repository-relative location, SHA-256 digest, revision identity, and input bindings in SQLite; the canonical body remains in the repository. `hybrid` stores canonical memory content and tracks OpenSpec as a projection.

OpenSpec projection is a pure deterministic adapter. It maps accepted artifacts to bounded paths under `openspec/changes/<safe-change-id>/` and returns managed Markdown bytes with exact revision and digest metadata. Render and compare receive or return bytes through bounded JSON; neither operation reads, follows symlinks, creates directories, nor writes files. For `openspec`, the manager uses ordinary OpenCode workspace tools to write and read back the canonical file, then records bounded digest evidence. The plugin remains filesystem-free.

Comparison reports `synced`, `drifted`, or `missing`. In hybrid mode memory is canonical. A valid divergent projection may be inspected, replaced from a freshly rendered canonical result, or submitted explicitly through `vgxness_sdd_save_revision` as a new candidate. Compare never imports divergent content, overwrites an accepted revision, or changes lifecycle state.

## Other native capabilities

The manager uses ordinary OpenCode workspace tools, the VGXNESS-managed `explore` override and `general` profile, skills by native registry name, optional user-approved SDD, the five review profiles, and the six model-bound SDD profiles. The `explore` override is bound to the research role model and variant. Its deny-by-default permissions allow only `read`, `grep`, `glob`, `list`, `skill`, and `codegraph_explore`; it has no shell, write, network, question, or delegation access.

All six SDD profiles are read-only. Research, proposal, spec, design, and tasks return evidence or candidate artifact content; apply accepts an exact hash-bound mission and returns a bounded patch and validation plan without editing files or running commands. No SDD profile can ask questions, delegate, persist memory, save or accept revisions, record projections, or transition lifecycle state. Manager, managed `general`, and verifier declare global tool permission. Their behavioral roles remain manager orchestration and lifecycle ownership, delegated implementation by `general`, and non-mutating final verification.

Each accepted SDD change follows `explore -> proposal -> spec -> design -> tasks -> apply -> verify -> complete`. A transition requires an accepted artifact for the current phase; `openspec` and `hybrid` also require a current projection record bound to that revision and digest. The manager sends phase agents exact change, artifact, accepted-input, evidence-scope, and return contracts. It may overlap at most four independent read-only missions bound to the same inputs; final artifacts, manager-applied patches, validation, and all lifecycle writes remain sequential and single-authority.

The managed `explore` override uses `codegraph_explore` first for structural evidence and falls back narrowly to native reads and search when the index is unavailable, stale, or insufficient. When a project has a healthy `.codegraph` index, the manager and reviewers may also use one bounded query. Exact source, Git diff, and test output remain authoritative.

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

### Native autonomous stacked pull requests

For an eligible implementation task, manager v36 automatically loads `vgxness-autonomous-stacked-pr` and announces routine autonomous delivery. Sizing precedence is task override, explicit durable project memory default, then the built-in defaults: target 400 effective changed lines per slice and stack only above 800. The manager estimates before implementation and compares the result with numeric additions plus deletions from `git diff --numstat`. Delivery IDs are deterministic normalized strings of at most 48 characters; branches use `vgxness/<delivery-id>/slice-<ordinal>` in one clean checkout with linear immediate-parent topology. Each PR targets the same original inspected base while `Depends-On` records its predecessor; merge commits preserve predecessor commits so later PR diffs narrow after earlier slices land.

After the exact candidate passes freeze, independent verification, and review, a fresh branch, normal commit, first push, and non-draft `gh pr create` need no second routine approval. Current-task merge authorization may land only PRs created by that task, ordinally, with repository-bound head OID matching and the repository's allowed merge-commit method after exact PR/repository/head/base/OID, predecessor, conflict, and required-check readback. Each slice uses an expected base-tip OID: slice 1 reads it from the freshly fetched original base before checks, and each predecessor merge advances it after a fresh base readback; the PR base and live remote base must equal it before checks and immediately before merge. `no merge` is transitive; `local-only`, `no commit`, `no push`, and `no PR` also forbid merge. Any failure, drift, dirty worktree, host/auth or branch-protection ambiguity stops mutations. After verified merged readback and base containment for every slice, the manager may fast-forward the original base from its verified remote-tracking branch. Unless `no cleanup` is set, it may then delete only exact current-delivery local branches proven merged with no open dependent PR; remote delivery branches are left intact, and unrelated branches and worktrees are never touched. Existing delivery state remains read-only resumption.

Manager, managed `general`, and verifier use a single global `allow` permission rule with no contradictory static denials. This grants capability only: user authorization, task scope, role instructions, repository ownership, and external host behavior remain separate constraints. OpenCode permissions do not verify repository hooks, credentials, GitHub availability, branch protection, network success, or command semantics.

## Health contract

The integration is installed only when manager v36, all other 14 agents, storage plugin v5, the model-plan manifest, and the autonomous stacked-PR skill match their managed identities exactly. Setup health combines:

1. the permanent VGXNESS launcher is installed and verified;
2. all 20 managed artifacts are installed without drift, including the default-agent configuration and restoration metadata;
3. the bounded OpenCode handshake succeeds for the selected workspace.

Restart OpenCode Desktop after installation or a plan switch so it reloads the profiles, model bindings, variants, storage plugin, and managed skill.
