# Portable project identity
`.vgxness/project-id` is strict UTF-8 JSON (v1 `format`, project `kind`, UUID); finals are regular non-symlinks and malformed finals fail closed. Init only publishes/binds portable identity: it never selects/rekeys local projects, sessions, observations, sync state, or normal path/root resolution.
It uses no-replace write/file-sync/close plus parent sync where supported; Windows directory durability is unsupported. Preserve, hash, and inspect a malformed final; only with operator confirmation and no conflicting prior binding may it be quarantined, then rerun init; same-UID TOCTOU is unrecovered.

## Foreground sync boundary

`memory sync --workspace <workspace>` canonicalizes the selector and requires
one existing local project with both a strict marker and its persisted portable
binding. The check precedes every remote capability, discovery, push, or pull
call. The foreground operation claims and sends only outbox rows related to
that project (project, sessions, and observations); other projects' rows and
their conflict state remain untouched. Protocol history and cursors are
owner-global, so this slice deliberately performs no pull/bootstrap and never
advances the global cursor. The result describes push-only completion.
The typed result mode is `project_push_only`; it means only this bounded local
project push was attempted, never bidirectional completion.
