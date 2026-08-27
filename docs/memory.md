# Native memory and structured storage

VGXNESS storage is an in-process Go subsystem backed by one owned SQLite/FTS5
database. Semantic memory exposes `Remember`, `Recall`, `Recent`, `Get`, and
`Forget`; structured SDD uses separate tables and lifecycle contracts. Neither
domain requires a daemon, second binary, embeddings, or network service.

## Strict boundary, flexible core

JSON is a trust-boundary format, not an internal service contract. The CLI keeps
schema version 1, rejects unknown and duplicate fields, accepts at most 64 KiB,
and rejects conflicting flag/payload sources. After decoding, adapters construct
native Go values (`memory.Remember`, `memory.Recall`, `memory.Recent`,
`memory.Lookup`, and `memory.Forget`). Internal application calls use those
values directly. MCP `memory_search` accepts optional `match_mode`: omitted or
`all` keeps all-term matching, while `any` enables any-term matching. Invalid
values fail before recall. Managed OpenCode and Codex managers search with `all`
first and retry with `any` only when the first results are insufficient. The CLI
continues to expose its native `matchAny` field.

`memory recent` resolves the canonical project from `--workspace`. It returns
active project-scope observations by default, ordered by most recently updated
with an ID tiebreak. Results use the same bounded preview shape as search and do
not expose full content.

`Forget` is a lifecycle operation: it atomically marks the observation archived
and removes its FTS row. The observation row and relationships remain available
to `Get` for durable history and persisted-data compatibility, while normal
`Recall` cannot return forgotten content.

## Schema v21 domains

**Implemented:** SQLite schema v21 keeps semantic observations, references,
sessions, and FTS rows isolated from structured SDD changes, artifacts,
immutable revisions, input bindings, idempotency records, and OpenSpec projection
evidence. Both domains share canonical workspace/project identity and the same
transactional database, but SDD content never appears in semantic recall and a
semantic observation is never treated as an SDD artifact.

Sync enrollment uses a bounded durable previous-credential reference marker to
finish interrupted keyring cleanup on the next enrollment; it never stores a
bearer in SQLite.

An explicit Linux/macOS `memory sync --credential-file /absolute/private/file`
mode is available for headless use. The file is revalidated on every use and is
never stored; see [sync enrollment](sync.md#local-enrollment-and-status) for
ownership, permissions, and Windows limitations. For existing project data,
run `memory sync backfill --workspace /absolute/workspace` before the first
sync. Backfill is local-only, bounded, and idempotent.

Schema v21 retains durable project transition snapshots and one per-project backup intent. It also has local-only provider-session rows and one local host-supplied draft per active session: only an SHA-256 external-ID hash and local handle are retained, never synchronized. A host end operation consumes an explicit summary or latest draft, validates it, writes one sessionless project observation, and deletes the draft atomically. The summary rejects the exact session handle and external ID; this is not generic DLP or authentication. Final provider-session summaries are immutable through both Store update APIs. For a new cloud, reset it first, then on the Mac/source device run `memory sync reseed --workspace /absolute/workspace --confirm-cloud-empty`; cloud emptiness is checked exactly. Each later Linux or Windows device runs `memory sync rejoin --workspace /absolute/workspace --confirm-merge`. These are per-project operations, never run `git pull`, and retries resume the same transition. A pending intent blocks only that project. A retry reuses a sealed SHA-256-verified backup, or may seal and reuse an extant healthy same-parent private backup only when its embedded intent exactly matches the portable project, local project, mode, intent ID, and random backup path; any different healthy database fails closed without replacement.

Foreground sync is explicitly project-scoped: use `memory sync --workspace
/absolute/workspace`. The workspace must already have a valid
`.vgxness/project-id` marker and matching local portable binding created by
`memory project init`; otherwise sync fails closed before any remote call. This
mode pushes only that project's project, session, and observation mutations,
then pulls only that portable project's history using its separate project
cursor. It never bootstraps, resolves global conflicts, or reads or advances
the owner-global cursor. Its result mode is `project_bidirectional`.

The default database is `~/.vgxness/memory.db`. Explicit `--storage-root` and
`--project-local` modes use isolated databases. The OpenCode integration exposes
both domains through the [MCP-only integration](opencode-integration.md); MCP
has no scheduler, delegation, edit, or
execution authority.

## Upgrade migration caveat

**Implemented:** A read-only database open cannot migrate an older supported schema to v21.
Immediately after upgrading the binary, `status`, `doctor`, `setup opencode --status`, or
another read operation may therefore report a migration/storage failure. Do not delete or
recreate the database; the existing data is the migration source and remains authoritative.

For a safe upgrade, first stop or quiesce other local writers and preserve an offline copy of
the database using the operator's normal backup procedure. Then run one intended write-capable
memory or SDD operation. The migrator obtains SQLite write ownership with `BEGIN IMMEDIATE`,
applies every missing forward migration and its `PRAGMA user_version` update in a transaction,
and rolls that transaction back on an error. It retries a transient SQLite busy/locked error
within the operation context; it does not downgrade a database whose schema is newer than the
binary supports. On success, rerun the read-only command to confirm the reported migration.

If the writer reports a migration failure or remains pending, preserve the database and its
error output, ensure no competing process still holds the file and that the storage location is
writable, then retry the same intentional write after correcting that local condition. Do not
reset `user_version`, replay SQL manually, or replace the database with an empty file. Escalate
with the preserved copy if the failure persists.

### Doctor scope

`doctor` deliberately has the same local inspection scope as `status`: it resolves the selected
storage paths and asks the configured storage health check for the schema version. It has no
local evidence for proxy TLS, private-network membership, remote endpoint reachability, or
fleet-wide admission limits, so it emits no guesses about those settings and makes no network
calls. Those deployment boundaries are documented in [sync](sync.md).

Older project-level `memory.db` files are a separate compatibility case. The
transactional, idempotent importer remains in the Go memory package, but normal
startup and memory operations do not invoke it automatically. Preserve those
files until a supported migration route is selected; do not treat their current
inactivity as permission to delete them.

## Runtime boundary

Engram is not part of the active architecture. Startup, `vgxness status`,
OpenCode setup, and normal memory operations do not install, probe, invoke,
import, require, or synchronize it. The owned `MemoryStore` is the sole
persistent semantic-memory authority.

## Project sync identity

During outbound project-scoped push, ordinary mappings send deterministic portable record IDs while retaining local memory IDs and outbox bytes unchanged. Adopted mappings take precedence and resend their exact inbound wire ID. The schema records that adoption provenance; project-scoped pull materializes supported project history, while reference-before-target translation and `resolve` transport remain unsupported.
