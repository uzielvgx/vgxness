# Manager orchestration: local skill registry

This document describes the local, regenerable Agent Skills registry used to
give the Manager and delegated workers bounded discovery metadata without
loading skill bodies or granting authority. It is a discovery cache, not a
provider runtime, not a schema/transport surface, and not a Go execution
broker.

## Purpose and scope

The registry enumerates every `SKILL.md` candidate under explicitly authorized
roots, extracts only declared frontmatter metadata, hashes the manifest bytes,
and records diagnostics for every candidate it could not trust. Selection is
based on metadata and hashes while resource bytes remain worker-owned: the
Manager receives metadata only, and a worker that receives a selection
validates the path and digest before reading any resource.

The registry does not synchronize, does not contact the network, does not
install, does not load skill bodies, and does not expand any role's authority.
An entry is discovery metadata, never an automatic capability grant.

## Schema

The cache is a single JSON document with `schemaVersion: 1`:

```json
{
  "schemaVersion": 1,
  "generatedAt": "2026-09-18T12:00:00Z",
  "workspace": "/absolute/canonical/workspace",
  "roots": [
    { "id": "project", "path": "/abs/workspace/.agents/skills", "scope": "project", "provenance": "project-convention", "present": true },
    { "id": "global", "path": "/home/user/.agents/skills", "scope": "global", "provenance": "global-convention", "present": true }
  ],
  "entries": [
    { "id": "security-boundary", "name": "security-boundary", "description": "…", "path": "/abs/…/security-boundary", "sha256": "…", "compatibility": "Agent Skills hosts …", "status": "ok" }
  ],
  "diagnostics": [
    { "root": "extra", "kind": "missing-root", "detail": "root directory is absent" }
  ],
  "complete": true
}
```

- `id` is unique within a scan. A duplicate name keeps the first root's entry
  as `status: "ok"` and records later ones as `status: "duplicate"` with
  `duplicateOf`.
- `description` and `compatibility` are taken verbatim from frontmatter. The
  registry never synthesizes a description.
- `status` is one of `ok`, `duplicate`, `invalid`, `symlink`, `oversized`,
  `unreadable`, `changed`.
- `diagnostics.kind` is one of `missing-root`, `root-rejected`, `symlink`,
  `oversized`, `invalid-frontmatter`, `unreadable`, `duplicate`,
  `limit-exceeded`, `changed-during-scan`.
- `complete` is false when a query truncated the entry list.

`ok` entries resolve by bare name, explicit `id`, or absolute path. A bare
duplicated name fails with `ErrAmbiguous`; an explicit `id` or absolute path can
resolve a `duplicate` entry, which is returned with its `duplicate` status and
never gains `ok` authority. Every other status is retained and reported so a
caller can never mistake it for usable.

## Roots and policy

The default policy is explicit:

1. project root `<workspace>/.agents/skills` (`scope: project`), which wins
   duplicate resolution because it is evaluated first;
2. global root `~/.agents/skills` (`scope: global`).

Additional roots are bounded configuration (`Options.Roots` in the Go API, or
repeated `--root ID=PATH` on the CLI). Supplying any root replaces the default
policy rather than augmenting it. Root count, entries per root, total entries,
manifest bytes, and search results are all bounded; a root set larger than the
bound is rejected rather than silently truncated.

Default project and global roots must stay under their trusted prefix and must
not traverse a workspace- or home-controlled symlink component (except the
documented macOS `/var` alias), so a project `.agents` symlink cannot redirect
discovery outside the workspace. Configured roots are exempt because the caller
explicitly authorized that path; that policy is explicit and tested.

A missing root is a diagnostic, not an error. A root that is a symlink, a file,
or unreadable is rejected with a diagnostic and contributes no entries. A
candidate directory with no `SKILL.md` is not a skill and is skipped; a
candidate with a symlinked directory or `SKILL.md`, unreadable bytes, an
oversized manifest, or missing/ill-formed frontmatter is recorded with its
status and a diagnostic.

## Cache location, freshness, and atomicity

The regenerable cache lives outside the versioned repository, under the
existing per-project VGXNESS user-state directory derived from the workspace
(`config.PathsFor`), as `skill-registry.json`. It is never a hand-maintained
repository file, and it is never the source of truth: the authorized roots and
their current bytes are.

- Publication is atomic: a temporary file is staged in the destination
  directory, `fsync`ed, and renamed over the destination.
- Concurrent writers serialize through an exclusive lock file
  (`skill-registry.lock`). Each acquisition records its PID and a random
  per-acquisition token and keeps an open handle to that exact file. Release
  removes the lock only when both the file identity and the recorded bytes still
  match, so a PID-only or same-PID successor, or a path recreated onto a reused
  inode or Windows FileId, is never removed. A lock is recovered only when it is
  old, the recorded owner process is provably gone (on Windows via
  `OpenProcess`/`GetExitCodeProcess`, failing closed on access-denied or unknown
  results), and the identity is unchanged; otherwise the writer waits a bounded
  interval and reports `ErrBusy` rather than deleting a possibly active lock.
  Transient Windows delete-pending or sharing-violation contention on the lock
  path is retried within the same bound; permission and other errors fail closed.
  The identity-plus-bytes check before removal is not an atomic compare-and-unlink,
  so a same-user writer that replaces the path inside the final check-to-remove
  window is outside this cooperative lock's threat model; that residual limit is
  accepted and documented rather than claimed closed.
- Cache reads use a rooted directory handle and a bounded regular-file read
  (`O_NOFOLLOW`/`O_NONBLOCK` on Unix), so a symlink, FIFO, or oversized file is
  rejected without unbounded or blocking I/O. A symlinked state directory, cache
  file, or lock file is rejected. Publication refuses to write a document larger
  than the read bound, so the writer can never publish bytes its own reader would
  reject.
- A missing, malformed, oversized, or schema-mismatched cache is treated as
  absent and rebuilt. A non-`schemaVersion: 1` document is never read as
  current.
- `Load` binds a cache to the requested normalized workspace, the exact
  configured root set, and a validated timestamp: a cache for another workspace,
  another root set, a future timestamp, or an age beyond `MaxAge` is reported
  present but not fresh/useful. Entries that reference paths outside the
  caller's currently authorized roots are rejected, so an edited or foreign
  document cannot legitimize arbitrary paths.
- `Ensure` performs a bounded rescan of the roots and reuses the cache only
  when its content digest, workspace, and root set still match and its
  timestamp is not in the future. Otherwise it rebuilds. This detects
  additions, removals, root changes, and hash changes without trusting the
  cache age alone.
- `Fresh` requires a workspace match and rejects a future timestamp.
- Immediately before publication, each winner's manifest identity is re-read
  and re-hashed. A mismatch, or an entry outside the authorized roots,
  downgrades the entry to `changed` with a `changed-during-scan` diagnostic
  instead of publishing it as fresh.

## CLI and API

```
vgxness skills registry status  [--workspace PATH] [--home PATH] [--cache-path PATH] [--max-age DURATION] [--root ID=PATH] [--json]
vgxness skills registry ensure  [--workspace PATH] [...]      # bounded startup summary
vgxness skills registry refresh [--workspace PATH] [...]
vgxness skills registry search  --query Q [--limit N] [...]
vgxness skills registry resolve --name ID|PATH [...]
vgxness skills registry unlock  [--cache-path PATH] [...]   # dead-owner stale lock only
```

`status` never rebuilds and never claims freshness it cannot verify: it reports
`cache_state=present|absent` and a separate `fresh=true|false`, so a present but
stale or unbound cache is visible rather than mistaken for a fresh one. `unlock`
removes a stale lock only when the owner is provably gone and the identity is
unchanged. `ensure` rescans and publishes
only when needed, printing a bounded summary (schema version, generation time,
root and entry counts, diagnostics, `complete`). `refresh` always rebuilds and
publishes. `search` and `resolve` call `Ensure`. All output is bounded metadata:
no skill body is ever printed.

`status` freshness reflects only the cache timestamp, the normalized workspace,
and the configured root set; it does not re-read skill content. `ensure`,
`search`, and `resolve` rescan the authorized roots (bounded) and are
authoritative for current content, and a selection they return is revalidated
against source bytes before it is handed back.

`resolve` revalidates the selected entry against its source bytes. A bare
duplicated name is `ErrAmbiguous`, never a silent winner pick; an explicit `id`
or absolute path resolves either the winner or a duplicate entry, and a
duplicate keeps `status: duplicate` rather than being promoted to `ok`. `search`
retains discovery diagnostics and marks truncation with `complete: false` plus a
`limit-exceeded` diagnostic rather than hiding it.

The Go API in `internal/skillregistry` exposes `Scan`, `Load`, `Ensure`,
`Refresh`, `Search`, `Resolve`, `Unlock`, and `Fresh` with the same bounds and
fail-closed freshness rules. It is source-compatible: `RunSkills` keeps its existing
callers and gains an optional registry runtime.

## Manager and worker contract

The shared contract (`internal/orchestration/manager_contract.json`, rendered
by every provider adapter) requires the Manager to **delegate all project
exploration to `explore`, including status checks, reviews, and read-only
diagnosis; there is no simple exception**. The Manager inspects only exact
evidence supplied for a decision, does not browse the project itself, and fails
closed with a missing-dependency report when `explore` is unavailable.

For skills, the Manager:

- selects by bounded metadata (`id`, `name`, `description`, `status`, `sha256`);
- loads a skill body only for its own action, never for a worker and never as a
  broad pre-task load; there is no conflicting "load before any task" rule;
- passes only the selected name, sha256, and resource paths to the worker, which
  reads and validates the body against source;
- never injects whole catalogs or skill bodies into Manager context;
- treats automatic registry startup and worker transport as provider-specific
  and claims them only where implemented.

Registry metadata is the Manager's own orchestration query, not project
exploration. In OpenCode the Manager runs the installed absolute launcher already
bound as the managed `vgxness` MCP server command (`mcp.vgxness.command[0]`) with
`skills registry search|resolve ... --json`; it never searches PATH and reports
the dependency unavailable rather than browsing the repository. In Pi the same
bounded metadata is obtained through `vgx_skill list` selectors, and Pi has no
Go registry command. Each role loads only the skills for its own action.

The General role validates every selected skill path and sha256 against its
source before reading a resource; drift blocks that dependency and is never
substituted silently. The Verifier may not edit files, delegate tasks, run
workflows, or perform durable mutations; shell access is not a read-only
guarantee, so checks are constrained to non-mutating commands, candidate
identity is inspected before and after, and the isolation limit is reported
explicitly rather than claimed as hard isolation. The OpenCode verifier header
sets `edit: deny` and `task: deny` explicitly, and the three CARE headers use
the current `codegraph_codegraph_explore` override; Explore and the three CARE
roles use current Codegraph tooling and headers as read-only evidence when
available and never edit, delegate, or mutate durable state. Native prompt or
permission restrictions are not a hard sandbox.

The Pi native adapter adds the same bounded rule: registry listings return
metadata only and a worker revalidates each selected path and hash against
source. Existing Pi discovery and selection
(`packages/pi/src/skills/catalog.ts`, `workers/context.ts`) is reused; the
registry does not replace it or break its API consumers. OpenCode is the
primary provider with an implemented automatic registry initialization; Pi
registry startup is not claimed.

## OpenCode startup integration

The managed plugin `plugins/vgxness-memory-lifecycle.ts` (artifact
`opencode-plugin/vgxness-memory-lifecycle`, version 2) performs one bounded
registry initialization on construction:

```
skills registry ensure --workspace <directory>
```

(no `--json`, so only the bounded summary is emitted rather than the full
index) via `spawn` with an argv array (`shell: false`), an 8 KiB stdout/stderr bound,
a 5 s timeout, and a minimal environment. It is independent of the memory
lifecycle: it never blocks session acquisition or system injection, and failure
is explicit and non-fatal. It does not read the full index into any prompt and
does not inject registry content into the system transform. `ensure` performs a
bounded check so startup overhead stays proportional to the authorized roots.

An event-only hook would fire on the first session/message rather than on
project open, so the constructor initialization is used instead; no unsupported
project-open hook is claimed. Registry availability remains independent of
memory success.

## Tests

Offline tests cover discovery, verbatim metadata and hashes, explicit
duplicates, ambiguous-name resolution, missing and rejected roots, symlinks,
invalid frontmatter, oversized manifests, atomic publication outside the
workspace, freshness reuse, content-change detection without clock advance,
future-timestamp rejection, outside-root cache rejection, symlinked state-path
rejection, corruption and schema
rebuild, stale rebuild, concurrent refresh validity, workspace mismatch,
pre-publication identity change, source revalidation, retained diagnostics and
truncation, bounded search/resolve, FIFO/oversized cache rejection, identity-aware
lock release, dead-owner-only stale recovery, Load workspace/root/age/future
binding, excess-root rejection, workspace-controlled symlink rejection,
explicit duplicate path/ID resolution, delegated-exploration and
verifier-denial and Codegraph header assertions, bounded skill-listing selector,
and the mocked plugin registry initialization before any session/message:

```
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test ./internal/skillregistry/ ./internal/cli/ ./internal/providers/opencode/
```

The provider-native agent goldens for OpenCode and Codex were regenerated from
the updated contract and remain reproducible.

## Migration

An existing receipt-backed installation upgrades through its receipt, not a
fabricated constant: the receipt binds the previously installed bytes, the
inspector exposes them as regenerations, and the current bundle is re-rendered
from the managed manifest. The plugin artifact is version 2 and the receipt
still contains nine files. Unreceipted pre-adaptive (Manager61-era) installs are
recognized by the preserved `manager_contract_82c7112a` snapshot and historical
native bytes; those historical golden bytes were not edited. An artifact that is
neither exact nor a recognized predecessor fails closed as drift and is never
overwritten. This workspace cannot reconstruct the exact pre-change current
contract/plugin bytes as a fixed predecessor (Git and generated copies are
unavailable), so no such constant was invented.

## Limits

- Pi registry startup is not implemented; Pi consumes bounded registry metadata
  only through existing selection APIs.
- Native prompt and permission denials constrain behavior but are not a hard
  sandbox; a shell is not a read-only guarantee.
- The lock identity check and removal have a small residual TOCTOU window on
  filesystems without atomic replace semantics. The cache document itself stays
  atomic, so a reader always sees a complete prior or next document. Off Unix,
  owner liveness cannot be proven and stale-lock recovery fails closed. Lock
  removal is therefore not claimed race-free.
- Default roots reject workspace- or home-controlled symlink components before
  scanning, but that check and the later rooted open are not one atomic
  operation, so a hostile process that swaps a component between them is a
  low-severity residual race. Content reads additionally re-check file identity,
  size, and mtime, and the macOS `/var` alias is the only permitted platform
  symlink.
- Windows atomic replace semantics were not exercised; publication relies on
  `os.Rename` replacing an existing file.
- The registry does not verify that a resolved skill is safe or authorized; it
  reports identity and diagnostics only.
- The Pi installer tests that enumerate Git source identity are blocked in this
  environment by the pending Xcode license (git exit 69); they do not exercise
  registry behavior.
