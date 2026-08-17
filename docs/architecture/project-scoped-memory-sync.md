# Portable project identity
`.vgxness/project-id` is strict UTF-8 JSON (v1 `format`, project `kind`, UUID); finals are regular non-symlinks and malformed finals fail closed. Init only publishes/binds portable identity: it never selects/rekeys local projects, sessions, observations, sync state, or normal path/root resolution.
It uses no-replace write/file-sync/close plus parent sync where supported; Windows directory durability is unsupported. Preserve, hash, and inspect a malformed final; only with operator confirmation and no conflicting prior binding may it be quarantined, then rerun init; same-UID TOCTOU is unrecovered.

## Foreground sync boundary

A pending operator-confirmed project-create repair blocks foreground remote
work and every pull-apply transaction before local mutation; repair is local,
then ordinary foreground sync sends it. Configure, sync, and repair share the
storage-root lock; pending repair makes global claim/pull conservative, while
separately invoked exact-bound project flows proceed serially under that lock.

`memory sync --workspace <workspace>` canonicalizes the selector and requires
one existing local project with both a strict marker and its persisted portable
binding. The check precedes every remote capability, discovery, push, or pull
call. The foreground operation claims and sends only outbox rows related to
that project (project, sessions, and observations); other projects' rows and
their conflict state remain untouched. After its successful (including empty)
push phase, it discovers the owner history, reads only that portable project's
durable cursor, and pulls bounded sparse pages using the exact portable
selector. Each page is applied atomically before the next cursor is requested,
so a retry resumes at the last committed page. Push rejections and conflicts
block the pull phase.

Project cursors and inbox rows are separate from owner-global pull state: this
flow never calls global pull/bootstrap or reads or advances `sync_cursor`.
The typed result mode is `project_bidirectional`; it means this bounded local
project push and pull were attempted without synchronizing other projects.

## Portable wire identities

Project-scoped push translates a claimed mutation only at the outbound transport boundary. The stored outbox payload, local record IDs, mutation IDs, and base versions remain unchanged. Ordinary mappings derive outbound portable session and observation UUIDs from the portable project marker plus the local kind and ID; tombstones require the retained observation mapping. An adopted mapping takes precedence and reuses its exact received inbound wire UUID, with adopting-device/time provenance. Project pull materializes records through those portable identity mappings. It does not support reference-before-target dependencies, global bootstrap, or global cursor advancement. `resolve` mutations are unsupported and fail before any remote call.
