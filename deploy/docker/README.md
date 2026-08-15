# Docker deployment with Nginx Proxy Manager

Status: proposed operator runbook, not observed VPS deployment evidence. This
package is additive to the Ubuntu host package; choose one deployment path.
This deployment package does not execute deployment actions by itself.
Docker runtime is unverified until an authorized VPS test records its result.

Nginx Proxy Manager (NPM) continues to own public ports 80/443. It must proxy
by container DNS to `vgxness-syncd:8787`; do not alter the existing NPM
container. The observed VPS network name is `nginx-proxy-manager_default`.
Set `VGXNESS_PROXY_NETWORK` only if local inspection shows a different NPM
network. Docker network attachment is not an application isolation guarantee:
the daemon's explicit `--container-network` flag permits only `0.0.0.0:8787`.
Compose publishes no host ports, and PostgreSQL is on the private network only.

## Preflight and initial deploy

From the repository root, record the source identity with `git rev-parse HEAD`
and inspect the exact staged files. Before changing anything, the authorized
operator checks `docker compose version`, `docker network inspect
nginx-proxy-manager_default`, and `docker compose -f deploy/docker/compose.yaml
config`. Abort if NPM's network is absent, any host port is published, or the
resolved configuration differs from review. Image tags are mutable: set
`POSTGRES_IMAGE` to an operator-verified digest-pinned image reference. Record
the source commit and the resulting local syncd image ID; do not describe a tag
as immutable.
Set `COMPOSE_PROJECT_NAME=vgxness_sync`, `POSTGRES_IMAGE=postgres@sha256:...`,
`VGXNESS_GO_BUILDER_IMAGE=golang:1.26-alpine@sha256:...`,
`VGXNESS_RUNTIME_IMAGE=RUNTIME@sha256:...`, `VGXNESS_SYNCD_IMAGE` to a fresh
unused commit-scoped local tag after verifying it absent, and `VGXNESS_PROXY_NETWORK`.
Record the resulting image ID and never overwrite that local tag.
For a first local build, `docker image inspect "$VGXNESS_SYNCD_IMAGE"` must be
absent. Before first initialization, `docker volume inspect vgxness_sync_postgres-data vgxness_sync_backup-data` must also be absent; abort
on either unexpected object rather than overwriting or deleting it.

Create a root-owned private directory outside the repository. Set absolute paths
for `VGXNESS_SYNCD_ENV`, `VGXNESS_SYNCD_DSN`, `VGXNESS_POSTGRES_ADMIN_PASSWORD`,
`VGXNESS_SYNCD_PASSWORD`, `VGXNESS_BACKUP_PASSWORD`, and `VGXNESS_POSTGRES_INIT`
before `docker compose config`; Compose has no worktree secret defaults. The
admin, app, and backup password files are root:root `0600`. The DSN file is
root:65532 `0640`, allowing only syncd runtime UID `65532:65532` to read it;
`syncd.env` is root:root `0600`. The DSN accepts exactly one final LF or CRLF.
Never put secrets in argv, logs, or proxy configuration.

Generate the PostgreSQL password with at least 32 characters from exactly
`[A-Za-z0-9._~-]`. This alphabet is safe both as a PostgreSQL password-file
line and in the supplied DSN placeholder without URI escaping. Do not change
the placeholder format or use a password containing an unescaped DSN delimiter.
Use distinct admin, syncd, and backup passwords. The first-volume init script
rejects any other form, creates non-superuser app and read-only backup roles,
and grants future-table SELECT explicitly.

Run `docker compose -f deploy/docker/compose.yaml up -d --build`, then inspect
`docker compose -f deploy/docker/compose.yaml ps` until PostgreSQL and syncd
are healthy. In NPM, create the HTTPS proxy host targeting `vgxness-syncd` port
`8787` on its existing Docker network. A public TLS request to
`/v1/sync/capabilities` without a bearer must return `401`; a direct host-port
request must not be possible. Abort and escalate to the host owner on any other
result, unknown network attachment, secret exposure, or unexpected port.

## Update and Rollback

For an update, record and retain a digest-pinned predecessor as
`VGXNESS_SYNCD_IMAGE=REGISTRY/IMAGE@sha256:PREDECESSOR`. Build only with reviewed
digest-pinned Go 1.26 builder/runtime base identities. Roll back with
`VGXNESS_SYNCD_IMAGE=REGISTRY/IMAGE@sha256:PREDECESSOR docker compose -f deploy/docker/compose.yaml up -d --no-build vgxness-syncd`,
then read back `docker compose ... ps` and repeat public-TLS `401`. Database migrations are
forward-only: do not roll PostgreSQL schema backward. If the update migrates the
database and fails, stop ingress and escalate to the DBA/maintenance owner.

## Backup and restore

`backup.sh` is an explicitly invoked helper, not a scheduled action. The
Compose `backup` service is opt-in through the `maintenance` profile; it joins
only the private network, has no ports, mounts the protected `backup-data`
volume at `/backups`, reads the existing PostgreSQL password Docker secret, and
runs read-only with all capabilities dropped. Invoke it exactly with:

```sh
docker compose -f deploy/docker/compose.yaml --profile maintenance run --rm backup
docker compose -f deploy/docker/compose.yaml --profile maintenance run --rm --entrypoint sha256sum backup /backups/current.pgd
docker compose -f deploy/docker/compose.yaml --profile maintenance run --rm --entrypoint pg_restore backup --list /backups/current.pgd
```

The helper creates a custom-format dump, verifies it with `pg_restore --list`,
fsyncs it, prints its SHA-256 as execution evidence, then atomically publishes
only `current.pgd` and fsyncs the published artifact. The second command above
is the authoritative later hash readback. It has no wildcard cleanup: a failure
before `mv -T` preserves the prior dump except for a bounded temporary path;
a failure after `mv -T` leaves the new artifact published and requires hash and
`pg_restore --list` readback plus escalation before relying on it. The helper
assumes the selected PostgreSQL image provides
POSIX `sh`, `pg_dump`, `pg_restore`, `sha256sum`, `sync -f`, and `mv -T`; verify
these tools against the digest-pinned image before use.
The lock directory is acquired before any temporary artifact; a pre-existing
lock or fixed temporary path, including a symlink, aborts. After a confirmed
interruption inspect dump/list/hash evidence; only an authorized operator may
remove a stale lock. Do not delete a volume to recover.

The official PostgreSQL entrypoint may need to initialize and change ownership
of a new data volume. Therefore the `postgres` service intentionally does not
drop all Linux capabilities; its least-feasible controls are its private-only
network, no published ports, Docker secret password file, and
`no-new-privileges`. Do not add `cap_drop: [ALL]` until the exact pinned image
and first-volume initialization have been tested.

Restore is destructive and requires DBA and maintenance approval: stop NPM
ingress and syncd, make and verify an investigation dump, verify the selected
dump and SHA-256 hash, then have the DBA restore to the exact database. Verify
schema and authenticated application behavior before reopening ingress. Abort
on any missing verification, uncertain database identity, or restore error.
On first deploy require `docker volume inspect` to show intended volumes absent.
On recovery record exact identities; never delete a volume to guess. Password
authentication failure can indicate an initialized volume with different
credentials: preserve evidence and have the DBA reconcile interactively without
secret command arguments or logs.
With DBA approval, run `docker compose -f deploy/docker/compose.yaml exec postgres psql --username postgres --dbname vgxness_sync`, then run
`\password vgxness_syncd` interactively. Update the protected DSN file, restart
syncd, and read back `docker compose -f deploy/docker/compose.yaml ps`; never
delete a volume during this recovery.
