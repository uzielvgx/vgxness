# OpenCode integration

Structured SDD is retired: no lifecycle tools, SDD worker profiles, or active `sdd-lifecycle` skill are installed. Historical records remain inert in SQLite for data preservation; there is no SDD runtime or archive command. Model-plan schemas retain inactive legacy slots for compatibility. Use a short plan, one writer, independent verification and proportional CARE review. See [the current workflow](orchestration-flow.md).

OpenCode Manager62 owns 12 managed artifacts: seven agents (Manager, Explore, General, Verifier and three CARE roles), the memory lifecycle plugin, model-plan manifest, default-agent configuration, restoration metadata and an installation receipt. Full MCP exposes eight memory tools; read-only MCP exposes three. The global catalog contains 18 skills across 46 files.


Current identities are OpenCode Manager62 and Codex Manager21 (parity OpenCode-v62), rendered from `internal/orchestration/manager_contract.json`. Each agent has one current definition. Installed file hashes are tracked in a local installation receipt; older unreceipted agents require the bridge procedure in `docs/agent-template-migration.md`. Global `git-delivery` is the authorized delivery skill, with native Git and no Go delivery daemon.

Delivery labels are evidence-only: IMPLEMENTED requires completed workspace changes and observed developmental checks, but not independent verification; VERIFIED requires the exact frozen candidate to pass independent verification and review; DELIVERED requires the exact commit to be published and a new current-task PR created and read back; MERGED requires that PR merge and base containment/readback; INSTALLED additionally requires installation and handshake readback. No later state is inferred.

MCP is local stdio for a trusted OpenCode host. It has no caller identity or session authentication: host tool allowlists, operator permissions, user authorization, and task scope are its authorization boundary. No capability token or additional authentication framework is provided.

## Install and inspect

```sh
vgxness setup opencode --preview
vgxness setup opencode
vgxness setup opencode --status
vgxness setup all --preview
```

The lower-level lifecycle remains available:

```sh
vgxness integrate opencode preview
vgxness integrate opencode install
vgxness integrate opencode status
vgxness integrate opencode uninstall
```

`vgxness setup opencode|codex|pi|all` is the unified setup entrypoint. `--config-dir` is the OpenCode configuration root; `--codex-home` is the independent Codex home root. `vgxness setup opencode` supports `--config-dir` and the model flags, then publishes global skills to the default discoverable root. `setup all` uses the same shared preflight and reports providers in deterministic OpenCode, Codex, then Pi order; OpenCode model slot options are never passed to Codex. Only lower-level `vgxness skills <preview|install|status|uninstall> --skills-dir PATH` supports isolated custom roots. `--model-plan low|medium|high|ultra` selects the active matrix. OpenCode supports a provider/model reference per efficient, balanced, and frontier slot. Fresh model selections use per-agent manifest v3. With no model override flags, planning can retain the installed configuration or default selection. Once any slot reference or effort override is supplied, the public setup command requires all three `--model-efficient`, `--model-balanced`, and `--model-frontier` references; when those references use mixed providers, it also requires all three `--model-*-effort` values. Installed v1/v2 model configurations remain readable. Custom references report availability as `unknown`: setup does not authenticate or probe provider availability. Restart OpenCode after any artifact, plan, slot, or effort change. The deprecated singular `--model` flag remains a no-op compatibility option and never overrides the plan.

Pi needs Node and `pi` plus an offline `--pi-release-dir` or pinned `--pi-release-version`; a release binary may default to its own tag. Only applying setup acquires a package. Pi runtime then runs independently of VGXNESS; see [Pi setup](pi-typescript.md).

Self-install version cleanup is separate from this integration. `vgxness self gc preview`, `apply`, and `recover` manage only verified immutable application versions and never delete OpenCode-managed artifacts, configuration, model plans, agents, global skills, backups, or restoration metadata.

Fresh no-flag setup installs the medium plan with `openai/gpt-5.6-luna`, `openai/gpt-5.6-terra`, and `openai/gpt-5.6-sol`. The canonical manifest is stored at `<config-dir>/vgxness/model-plan.json`; it contains no credentials and binds the resolved role assignments to exact managed agent digests. VGXNESS creates or updates `opencode.json` with `default_agent: "vgxness-manager"`, preserving every unrelated JSON value. It preserves any existing `opencode.jsonc` byte-for-byte. Bounded metadata at `<config-dir>/vgxness/default-agent.json` restores a prior explicit default during uninstall. Model routing remains OpenCode-owned.


Installation stages each artifact in a private same-filesystem `0700` directory with a `0600` regular file, then publishes by no-overwrite link. Cleanup verifies the creation identity and exact expected bytes, retaining observed replacements, mutations, or extra staging entries as recovery evidence. This protects observable path replacement and content drift. POSIX provides no atomic compare-content-and-unlink operation against any external same-UID process holding a pre-opened writable descriptor, hostile or accidental; that situation is outside this supported boundary.

Changing the plan or a slot regenerates the same managed agent set only when every current byte still matches the installed current manifest. An interrupted switch containing an exact mixture of the verified source and requested target bytes resumes safely; any unrelated byte drift blocks regeneration. The change becomes active only after OpenCode restarts. Manual modification of an agent or manifest blocks regeneration; historical plugin bytes remain retirement evidence.

## Memory authority

VGXNESS's SQLite/FTS5 `MemoryStore` is the only persistent memory authority. MCP receives no caller identity; project selection and authorization are host/operator responsibilities.

The default database is `~/.vgxness/memory.db`. Records remain isolated by canonical workspace binding, project, scope, topic, type, state, session, provenance, and references.

Memory access is explicit through MCP tools. Recall is intent-triggered when the request indicates prior project context may matter:

- searches with all-term matching first and retries with any-term matching only when results are insufficient;
- inspects bounded previews and reads full content only by exact ID after a relevant result;
- uses recent memory only for explicit recent-work, session, or compaction-recovery requests, never as a routine first action;
- after any route, may make durable memory assessment when supported by evidence, only for an evidence-backed, safely assessed project decision, preference, constraint, or learning;
- reuses stable topic keys for evolving subjects;
- never stores secrets, personal data, transient progress, logs, raw command output, or full transcripts, and never adds engineering ceremony or automatic cloud sync;
- forgets a memory only after an explicit user request.

Reviewers may search and read memory as non-authoritative context. They cannot save or forget. Memory never proves a candidate diff and never overrides exact source, tests, or Git evidence.

Current setup installs the exact auto-discovered lifecycle plugin only, without an `opencode.json` plugin entry. It starts top-level lifecycle records and, at the first eligible top-level system transform, its only automatic memory injection is one bounded same-project prior completed handoff as untrusted data, never instructions; it checkpoints compaction without transcript capture, and treats `memory_session_summary` as a local draft write. On explicit deletion it awaits the sole Store-backed end finalizer: a completed result must return a committed receipt with completed state, the matching handle, and a non-empty final observation ID, or deletion fails. Disposal and interrupted shutdown remain best-effort. It does not install shell or Git hooks, broadly inject recent memories or transcripts into every prompt, capture transcript content in lifecycle events, add broad observability, or add another plugin. Plugin v1–v10 material is historical retirement evidence only; see [Safe hooks](hooks.md).

Engram is not part of this integration.

## Other native capabilities

The managed `explore` override uses `codegraph_explore` first for structural evidence and falls back narrowly to native reads and search when the index is unavailable, stale, or insufficient. When a project has a healthy `.codegraph` index, the manager and reviewers may also use one bounded query. Exact source, Git diff, and test output remain authoritative.

### Adaptive workflow and interaction

Interaction mode is resolved in this order:

1. an explicit override for the current task;
2. a durable project default recalled from VGXNESS memory;
3. automatic mode as the fallback.

The primary manager has explicit access to OpenCode's native `question` tool. It asks one blocking decision at a time, presents the recommended option first, and resumes without repeating the same question. Questions do not grant permission, override a denial, or move terminal and diagnostic work to the user. Review profiles cannot ask questions.

### Adaptive TDD

For safely testable regressions and behavior changes, the manager prefers an observable RED -> GREEN -> REFACTOR cycle. It may claim TDD only when the test was run and observed failing for the expected reason before the production change. Tests added after implementation are reported as regression coverage instead.

### Native autonomous Git delivery

For explicitly authorized PR delivery, the Manager loads `git-delivery` and follows its current repository, candidate and publication gates. The skill does not grant permission to publish, merge or alter unrelated work. Local-only scope remains local; actual required checks and independent review precede verified delivery claims.

After the exact candidate passes freeze, independent verification, and review, a fresh branch, normal commit, first push, and non-draft `gh pr create` need no second routine approval. Current-task merge authorization may land only PRs created by that task, ordinally, with repository-bound head OID matching and the repository's allowed merge-commit method after exact PR/repository/head/base/OID, predecessor, conflict, and required-check readback. Each slice uses an expected base-tip OID: slice 1 reads it from the freshly fetched original base before checks, and each predecessor merge advances it after a fresh base readback; the PR base and live remote base must equal it before checks and immediately before merge. `no merge` is transitive; `local-only`, `no commit`, `no push`, and `no PR` also forbid merge. Any failure, drift, dirty worktree, host/auth or branch-protection ambiguity stops mutations, except the exact bounded, explicitly reauthorized recovery of a verified unpublished local slice. Existing remote branches and PRs remain read-only and never gain retroactive merge or cleanup authority; only that bounded unpublished local-slice recovery is allowed. After verified merged readback and base containment for every slice, the manager may fast-forward the original base from its verified remote-tracking branch. Unless `no cleanup` is set, it may then delete only exact current-delivery local branches proven merged with no open dependent PR; remote delivery branches are left intact, and unrelated branches and worktrees are never touched.

Manager, managed `general`, and verifier use a single global `allow` permission rule with no contradictory static denials. This grants capability only: user authorization, task scope, role instructions, repository ownership, and external host behavior remain separate constraints. OpenCode permissions do not verify repository hooks, credentials, GitHub availability, branch protection, network success, or command semantics.

## Health contract

Restart OpenCode Desktop after installation or a plan switch so it reloads the profiles, model bindings, variants, MCP configuration, and global portable skills.
# Shared portable skills

## CARE inventory and evaluation boundary

## Repair an obsolete owned MCP executable

Run `vgxness integrate opencode repair-mcp-preview --config-dir /absolute/config` to inspect the exact owned entry. Apply only the inspected proof with `repair-mcp --old-executable /absolute/old/vgxness --expected-mcp-sha256 <preview-digest>` and the same config directory. The repair preserves the existing full/read-only mode and unrelated MCP entries; foreign, mixed or changed entries are rejected. Concurrent changes are preserved, with retained recovery paths reported when rollback cannot safely restore ownership. Ordinary setup does not silently rewrite an obsolete executable.


Current integration contract: OpenCode Manager62 and Codex Manager21 use one workspace writer and the same frozen candidate for verification and applicable CARE review. SQLite schema v23 is preserved. OpenCode installs 12 managed artifacts and Codex installs ten; each exposes six delegated profiles. The auto-discovered `plugins/vgxness-memory-lifecycle.ts` has no `opencode.json` plugin entry; missing it is partial. `vgxness mcp --full` exposes eight memory tools. Receipt-backed prior installations are recognized by exact local bytes. During retirement, modified, malformed, foreign, unknown, or newer bytes block without removal.
