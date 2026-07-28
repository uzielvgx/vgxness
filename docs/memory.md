# Native memory

VGXNESS memory is an in-process Go subsystem backed by the existing SQLite and
FTS5 database. Its core API is `Remember`, `Recall`, `Get`, and `Forget`; it does
not require a daemon, a second binary, embeddings, or a network service.

## Strict boundary, flexible core

JSON is a trust-boundary format, not an internal service contract. The CLI keeps
schema version 1, rejects unknown and duplicate fields, accepts at most 64 KiB,
and rejects conflicting flag/payload sources. After decoding, adapters construct
native Go values (`memory.Remember`, `memory.Recall`, `memory.Lookup`, and
`memory.Forget`). Internal application calls use those values directly.

The SQLite schema and migrations remain unchanged. `Forget` is a lifecycle
operation: it atomically marks the observation archived and removes its FTS row.
The observation row and relationships remain available to `Get` for durable
history and persisted-data compatibility, while normal `Recall` cannot return
forgotten content.

## Legacy compatibility

Engram data can still be imported explicitly through the legacy import API for
operators who need migration compatibility. Opening, reading, remembering,
recalling, getting, forgetting, resolving a project, and continuity processing
do not probe, require, or automatically import an Engram database.
