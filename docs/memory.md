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
values directly. The `memory search` boundary accepts `matchAny`; OpenCode sets
it to `true`, while native recall keeps its existing all-term default for other
callers.

`memory recent` resolves the canonical project from `--workspace`. It returns
active project-scope observations by default, ordered by most recently updated
with an ID tiebreak. Results use the same bounded preview shape as search and do
not expose full content.

`Forget` is a lifecycle operation: it atomically marks the observation archived
and removes its FTS row. The observation row and relationships remain available
to `Get` for durable history and persisted-data compatibility, while normal
`Recall` cannot return forgotten content.

## Schema v5 domains

**Implemented:** SQLite schema v5 keeps semantic observations, references,
sessions, and FTS rows isolated from structured SDD changes, artifacts,
immutable revisions, input bindings, idempotency records, and OpenSpec projection
evidence. Both domains share canonical workspace/project identity and the same
transactional database, but SDD content never appears in semantic recall and a
semantic observation is never treated as an SDD artifact.

The default database is `~/.vgxness/memory.db`. Explicit `--storage-root` and
`--project-local` modes use isolated databases. The OpenCode integration exposes
both domains through the [MCP-only integration](opencode-integration.md); MCP
has no scheduler, delegation, edit, or
execution authority.

## Upgrade migration caveat

**Implemented:** A read-only database open cannot migrate schema v4 to v5.
Immediately after upgrading the binary, `status`, `doctor`, `setup opencode --status`, or
another read operation may therefore report a migration/storage failure. Run one
write-capable memory or SDD operation to open the database and atomically apply
v5, then rerun the read-only command. Do not delete or recreate the database;
the existing data is the migration source and remains authoritative.

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
