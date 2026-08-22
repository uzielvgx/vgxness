# OpenCode integration

OpenCode owns 18 managed artifacts: 15 agents, a model-plan manifest, an `opencode.json` default-agent selection, and restoration metadata. The separate global 47-file, 19-skill portable catalog adds `memory-sync` and `sdd-lifecycle`; exact historical `vgxness.ts` v1-v10 plugin bytes and `vgxness-autonomous-stacked-pr` v1/v2/v3 provider-skill bytes are removable, while modified, malformed, foreign, unknown, or newer bytes block without removal.

Current delivery policy is manager v53 and global `stacked-pr` v3. Before branch creation and source writes or a routine delivery announcement, v53 requires clean untracked-inclusive porcelain, repository/base/ref identity, intended paths, an estimate and slice plan, and a deterministic fresh branch; candidate identity, developmental checks, independent verification, and review are post-implementation gates before delivery mutations. A dirty checkout stops writes except for explicitly current-task reauthorized recovery of a verified unpublished local slice. All first publications use only the empty-expectation create-only lease; existing remote branches and PRs remain read-only. Provider v1/v2/v3 bytes are retirement identities only, not activatable skills.

Delivery labels are evidence-only: IMPLEMENTED requires completed workspace changes and observed developmental checks, but not independent verification; VERIFIED requires the exact frozen candidate to pass independent verification and review; DELIVERED requires the exact commit to be published and a new current-task PR created and read back; MERGED requires that PR merge and base containment/readback; INSTALLED additionally requires installation and handshake readback. No later state is inferred.

VGXNESS installs 18 OpenCode-managed artifacts: 15 agents (`vgxness-manager` v53; managed `general` v10; `vgxness-sdd-apply` v7; verifier v6; reviewer profiles v4 except reliability v5; a read-only `explore` override; and five other hidden read-only SDD profiles), one non-secret model-plan manifest, `opencode.json` with the default-agent selection, and bounded restoration metadata. The exact v52/v9/v6 package is the immediate recognized predecessor. There is no installed plugin. The managed MCP launch command is `vgxness mcp --full`; it exposes the full five memory tools and 13 SDD tools read/write set. Read-only managed profiles receive explicit non-mutating allowlists.

MCP is local stdio for a trusted OpenCode host. It has no caller identity or session authentication: host tool allowlists, operator permissions, user authorization, and task scope are its authorization boundary. No capability token or additional authentication framework is provided.

| Mode | Discovery | Contract |
| --- | --- | --- |
| `vgxness mcp` | `memory_recent`, `memory_search` | Default-deny read-only mode: this server does not register `memory_get` or mutating tools, and rejects calls to their unregistered names. |
| `vgxness mcp --full` | 18 tools | Five memory tools and 13 SDD tools, including eight mutations: `memory_save`, `memory_forget`, `sdd_create`, `sdd_set_interaction_mode`, `sdd_transition`, `sdd_save_revision`, `sdd_accept_revision`, and `sdd_record_projection`. |

Full MCP exposes exactly 18 tools: five semantic-memory tools and 13 SDD tools.

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

The SDD tools store structured changes and immutable accepted revisions or transform supplied bytes. They do not route work, invoke agents, access the filesystem, write OpenSpec files, or advance phases on their own. OpenCode remains the execution authority for all engineering work.

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

`vgxness setup opencode` supports `--config-dir` and the model flags, then publishes global skills to the default discoverable root. Only lower-level `vgxness skills <preview|install|status|uninstall> --skills-dir PATH` supports isolated custom roots. `--model-plan low|medium|high|ultra` selects the active matrix. OpenCode supports a provider/model reference per efficient, balanced, and frontier slot. Homogeneous presets use the v1 manifest. With no model override flags, planning can retain the installed configuration or default selection. Once any slot reference or effort override is supplied, the public setup command requires all three `--model-efficient`, `--model-balanced`, and `--model-frontier` references; when those references use mixed providers, it also requires all three `--model-*-effort` values. Mixed profiles use manifest v2. Custom references report availability as `unknown`: setup does not authenticate or probe provider availability. Restart OpenCode after any artifact, plan, slot, or effort change. The deprecated singular `--model` flag remains a no-op compatibility option and never overrides the plan.

Self-install version cleanup is separate from this integration. `vgxness self gc preview`, `apply`, and `recover` manage only verified immutable application versions and never delete OpenCode-managed artifacts, configuration, model plans, agents, global skills, backups, or restoration metadata.

Fresh no-flag setup installs the medium plan with `openai/gpt-5.6-luna`, `openai/gpt-5.6-terra`, and `openai/gpt-5.6-sol`. The canonical manifest is stored at `<config-dir>/vgxness/model-plan.json`; it contains no credentials and binds the resolved role assignments to exact managed agent digests. VGXNESS creates or updates `opencode.json` with `default_agent: "vgxness-manager"`, preserving every unrelated JSON value. It preserves any existing `opencode.jsonc` byte-for-byte. Bounded metadata at `<config-dir>/vgxness/default-agent.json` restores a prior explicit default during uninstall. Model routing remains OpenCode-owned.

Preview and status are read-only. The managed agent catalogue contains manager v53 plus 14 other profiles; `general` is v10, `vgxness-sdd-apply` is v7, verifier v6, and reviewers are v4 except reliability v5. The exact v52/v9/v6 package is the immediate predecessor; all older recognized manager, agent, model-plan, and `vgxness.ts` v1-v10 plugin artifacts remain historical predecessor identities. Modified or unknown bytes are drift and block replacement. Exact provider `vgxness-autonomous-stacked-pr` v1/v2/v3 bytes are removable; modified, malformed, foreign, unknown, or newer bytes block without removal. Uninstall removes only exact provider artifacts and never removes global skills.

Historical predecessor documentation may refer to manager v49, `general` v6, verifier v4, and reviewers v3; those identities do not describe the current generated ownership boundary.

Installation stages each artifact in a private same-filesystem `0700` directory with a `0600` regular file, then publishes by no-overwrite link. Cleanup verifies the creation identity and exact expected bytes, retaining observed replacements, mutations, or extra staging entries as recovery evidence. This protects observable path replacement and content drift. POSIX provides no atomic compare-content-and-unlink operation against any external same-UID process holding a pre-opened writable descriptor, hostile or accidental; that situation is outside this supported boundary.

Changing the plan or a slot regenerates the same managed agent set only when every current byte still matches the installed current manifest. An interrupted switch containing an exact mixture of the verified source and requested target bytes resumes safely; any unrelated byte drift blocks regeneration. The change becomes active only after OpenCode restarts. Manual modification of an agent or manifest blocks regeneration; historical plugin bytes remain retirement evidence.

## Memory authority

VGXNESS's SQLite/FTS5 `MemoryStore` is the only persistent memory authority. MCP receives no caller identity; project selection and authorization are host/operator responsibilities.

The default database is `~/.vgxness/memory.db`. Records remain isolated by canonical workspace binding, project, scope, topic, type, state, session, provenance, and references.

The current SQLite schema v19 contains separate structured SDD tables and per-project sync backup intents; it does not make SDD content semantic memory or turn OpenSpec projections into canonical SQLite content. Immediately after a binary upgrade from an older supported schema, read-only opens cannot migrate: `status`, `doctor`, `setup opencode --status`, and read tools may report a storage/migration failure until one write-capable memory or SDD operation opens the database and atomically applies v19. Do not delete the database. Run the write-capable operation and rerun status; see [Native memory](memory.md#upgrade-migration-caveat).

Memory access is explicit through MCP tools. Recall is intent-triggered when the request indicates prior project context may matter:

- searches with all-term matching first and retries with any-term matching only when results are insufficient;
- inspects bounded previews and reads full content only by exact ID after a relevant result;
- uses recent memory only for explicit recent-work, session, or compaction-recovery requests, never as a routine first action;
- after any route, may autonomously make at most one save only for a durable, evidence-backed, safely assessed project decision, preference, constraint, or learning;
- reuses stable topic keys for evolving subjects;
- never stores secrets, personal data, transient progress, logs, raw command output, or full transcripts, and never adds engineering ceremony or automatic cloud sync;
- forgets a memory only after an explicit user request.

Reviewers may search and read memory as non-authoritative context. They cannot save or forget. Memory never proves a candidate diff and never overrides exact source, tests, or Git evidence.

Current setup installs no plugin, callbacks, automatic memory injection, compaction, observability, or session correlation. Plugin v1–v10 material is historical retirement evidence only; see [Safe hooks](hooks.md).

Engram is not part of this integration.

## Structured SDD storage and OpenSpec projection

SDD changes, artifacts, revisions, input bindings, and projection records use isolated tables in the owned SQLite database. They are not semantic memories and never appear in memory search. MCP exposes the operations but supplies no caller identity. Create retries reuse a project-scoped idempotency key and must match the original normalized payload. Revision lists return metadata summaries without bodies; exact bodies require `get-revision`. Per-change automatic/interactive mode can be changed later only with an optimistic state version, and save or acceptance is valid only for the change's current phase.

The backend determines canonical content ownership. `memory` stores canonical artifact bodies in structured SDD storage. `openspec` stores only the external repository-relative location, SHA-256 digest, revision identity, and input bindings in SQLite; the canonical body remains in the repository. `hybrid` stores canonical memory content and tracks OpenSpec as a projection.

OpenSpec projection is a pure deterministic adapter. It maps accepted artifacts to bounded paths under `openspec/changes/<safe-change-id>/` and returns managed Markdown bytes with exact revision and digest metadata. Render and compare receive or return bytes through bounded JSON; neither operation reads, follows symlinks, creates directories, nor writes files. For `openspec`, `vgxness-sdd-apply` uses ordinary OpenCode workspace tools to write and read back the canonical file; Manager records bounded digest evidence.

Comparison reports `synced`, `drifted`, or `missing`. In hybrid mode memory is canonical. A valid divergent projection may be inspected, replaced from a freshly rendered canonical result, or submitted explicitly through `vgxness_sdd_save_revision` as a new candidate. Compare never imports divergent content, overwrites an accepted revision, or changes lifecycle state.

## Other native capabilities

The manager uses ordinary OpenCode workspace tools, the VGXNESS-managed `explore` override and `general` profile, skills by native registry name, optional user-approved SDD, the five review profiles, and the six model-bound SDD profiles. The `explore` override is bound to the research role model and variant. Its deny-by-default permissions allow only `read`, `grep`, `glob`, `list`, `skill`, and `codegraph_explore`; it has no shell, write, network, question, or delegation access.

Research, proposal, spec, design, and tasks SDD profiles are read-only. `vgxness-sdd-apply` v7 alone has workspace-write authority for an explicitly accepted, hash-bound SDD apply or OpenSpec/hybrid projection; it cannot ask questions, delegate, persist memory, save or accept revisions, record projections, or transition lifecycle state. `general` v10 may write only ordinary authorized non-SDD repository implementation and rejects SDD missions. Manager owns lifecycle state, projections, and transitions but is not the workspace writer; verifier remains non-mutating.

Each accepted SDD change follows `explore -> proposal -> spec -> design -> tasks -> apply -> verify -> complete`. A transition requires an accepted artifact for the current phase; `openspec` and `hybrid` also require a current projection record bound to that revision and digest. The manager sends phase agents exact change, artifact, accepted-input, evidence-scope, and return contracts. It may overlap at most four independent read-only missions bound to the same inputs; final artifacts, accepted-SDD workspace changes by `vgxness-sdd-apply`, validation, and all Manager lifecycle writes remain sequential and single-authority.

The managed `explore` override uses `codegraph_explore` first for structural evidence and falls back narrowly to native reads and search when the index is unavailable, stale, or insufficient. When a project has a healthy `.codegraph` index, the manager and reviewers may also use one bounded query. Exact source, Git diff, and test output remain authoritative.

### Adaptive workflow and interaction

Manager v53 silently classifies domain, operation, side effect, complexity, and risk without tools or delegation, then chooses the least-cost route. Conversation, writing, translation, summarization, brainstorming, and no-effect planning use zero execution tools, skills, todos, delegation, or review. Bounded simple exact reads allow at most three tool attempts and no delegation or todo; complex evidence research may use at most one read-only delegation. Reversible actions preserve authorization and readback; repository engineering preserves General, Explore, TDD, and developmental checks; irreversible or high-risk work preserves freeze, verifier, review, and delivery assurance. Every tool or delegation attempt—including failures and retries—counts, and the manager halts before a further attempt would exceed the selected budget. This is static prompt policy, not runtime enforcement, and no external, NLP, or holdout result is claimed.

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

For an eligible implementation task, manager v53 automatically loads global `stacked-pr`. Its clean pre-write gate—untracked-inclusive porcelain, repository/base/ref identity, intended paths, estimate and slice plan, and a fresh branch—precedes source writes and the routine delivery announcement. The global policy keeps the same sizing, deterministic branch, original-base, and `Depends-On` boundaries.

After the exact candidate passes freeze, independent verification, and review, a fresh branch, normal commit, first push, and non-draft `gh pr create` need no second routine approval. Current-task merge authorization may land only PRs created by that task, ordinally, with repository-bound head OID matching and the repository's allowed merge-commit method after exact PR/repository/head/base/OID, predecessor, conflict, and required-check readback. Each slice uses an expected base-tip OID: slice 1 reads it from the freshly fetched original base before checks, and each predecessor merge advances it after a fresh base readback; the PR base and live remote base must equal it before checks and immediately before merge. `no merge` is transitive; `local-only`, `no commit`, `no push`, and `no PR` also forbid merge. Any failure, drift, dirty worktree, host/auth or branch-protection ambiguity stops mutations, except the exact bounded, explicitly reauthorized recovery of a verified unpublished local slice. Existing remote branches and PRs remain read-only and never gain retroactive merge or cleanup authority; only that bounded unpublished local-slice recovery is allowed. After verified merged readback and base containment for every slice, the manager may fast-forward the original base from its verified remote-tracking branch. Unless `no cleanup` is set, it may then delete only exact current-delivery local branches proven merged with no open dependent PR; remote delivery branches are left intact, and unrelated branches and worktrees are never touched.

Manager, managed `general`, and verifier use a single global `allow` permission rule with no contradictory static denials. This grants capability only: user authorization, task scope, role instructions, repository ownership, and external host behavior remain separate constraints. OpenCode permissions do not verify repository hooks, credentials, GitHub availability, branch protection, network success, or command semantics.

## Health contract

The integration is installed only when manager v53, all other 14 agents (including `general` v10, `vgxness-sdd-apply` v7, verifier v6, and reviewers v4 except reliability v5), the model-plan manifest, default-agent selection, and restoration metadata match their provider identities exactly. Setup health combines:

1. the permanent VGXNESS launcher is installed and verified;
2. all 18 OpenCode-managed artifacts are installed without drift: 15 agents, model-plan manifest, default-agent selection, and restoration metadata;
3. the separate global 47-file, 19-skill `skills-creator`, `stacked-pr`, `cross-platform`, `installer-lifecycle`, `agent-evaluation`, `ci-triage`, `security-boundary`, `documentation-strategy`, `product-requirements`, `software-architecture-docs`, `user-documentation`, `api-documentation`, `quality-test-documentation`, `operations-runbooks`, `governance-compliance-docs`, `release-lifecycle-docs`, `end-to-end-testing`, `memory-sync`, and `sdd-lifecycle` catalog is installed without drift;
4. the bounded OpenCode handshake succeeds for the selected workspace.

Restart OpenCode Desktop after installation or a plan switch so it reloads the profiles, model bindings, variants, MCP configuration, and global portable skills.
# Shared portable skills

The global 47-file, 19-skill `skills-creator`, `stacked-pr`, `cross-platform`, `installer-lifecycle`, `agent-evaluation`, `ci-triage`, `security-boundary`, `documentation-strategy`, `product-requirements`, `software-architecture-docs`, `user-documentation`, `api-documentation`, `quality-test-documentation`, `operations-runbooks`, `governance-compliance-docs`, `release-lifecycle-docs`, `end-to-end-testing`, `memory-sync`, and `sdd-lifecycle` catalog is installed automatically by setup and can be managed independently with `vgxness skills <preview|install|status|uninstall>` into `~/.agents/skills` (or an absolute `--skills-dir` override). Only exact `vgxness.ts` v1-v10 plugin bytes and provider `vgxness-autonomous-stacked-pr` v1/v2/v3 bytes are removable before global publication; modified, malformed, foreign, unknown, or newer bytes block without removal. `integrate opencode uninstall` is scoped to its 18 provider artifacts and never removes global skills.
