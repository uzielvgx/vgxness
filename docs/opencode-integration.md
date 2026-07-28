# OpenCode integration

VGXNESS installs one persistent primary profile, `vgxness-manager`, five hidden read-only review profiles, and one bounded memory-only plugin.

The plugin exposes exactly these tools:

- `vgxness_memory_search`
- `vgxness_memory_recent`
- `vgxness_memory_get`
- `vgxness_memory_save`
- `vgxness_memory_forget`

It does not expose orchestration, status, dispatch, tickets, filesystem access, editing, validation, CodeGraph, shell execution, model selection, or subagent creation. OpenCode remains the execution authority for all engineering work.

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

`--config-dir` can select a non-default OpenCode configuration directory. The deprecated `--model` flag is accepted temporarily for command compatibility but has no effect.

Preview and status are read-only. Installation creates absent managed artifacts and atomically upgrades only exact catalogued older VGXNESS versions with matching artifact identities. It refuses foreign, modified, malformed, equal-version drifted, or newer content. Uninstall removes only exact managed artifacts, writes recoverable hard-link backups, and refuses drift.

## Memory authority

VGXNESS's SQLite/FTS5 `MemoryStore` is the only persistent memory authority. The plugin resolves project identity from OpenCode's trusted workspace directory, so agents cannot select another project by supplying an arbitrary project ID.

The default database is `~/.vgxness/memory.db`. Records remain isolated by canonical workspace binding, project, scope, topic, type, state, session, provenance, and references.

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

## Other native capabilities

The manager uses ordinary OpenCode workspace tools, built-in `explore` and `general` Task workers, skills by native registry name, optional user-approved SDD, and the five review profiles.

When a project has a healthy `.codegraph` index, the manager and reviewers may use one bounded `codegraph_explore` query for structural evidence. Exact source, Git diff, and test output remain authoritative.

## Health contract

The integration is installed only when the manager, all five reviewers, and the memory-only plugin match their managed identities exactly. Setup health combines:

1. the permanent VGXNESS launcher is installed and verified;
2. all seven managed artifacts are installed without drift;
3. the OpenCode adapter handshake succeeds for the selected workspace.

A child execution model and the old execution bridge are not readiness requirements.

Restart OpenCode Desktop after installation so it reloads the profiles and memory plugin.
