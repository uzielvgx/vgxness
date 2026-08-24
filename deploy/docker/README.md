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
Compose publishes no sync API port. Its sole host publication is dashboard port
8788 bound to the required `VGXNESS_TAILSCALE_IP`; PostgreSQL remains on the
private network only. NPM proxies only the sync API on container port 8787.
NPM is the required HTTPS/TLS terminator for public sync traffic; the daemon does
not terminate TLS and must not receive public traffic directly. Compose makes the
default admission settings explicit (120 global attempts/minute, 60 per declared
device/minute, and 256 device states), but they apply to this one `syncd` process
only. They are not a distributed rate limit; add an independently operated
upstream control before scaling to multiple daemon processes when a fleet-wide
limit is required.

## Preflight and initial deploy

From the repository root, record the source identity with `git rev-parse HEAD`
and inspect the exact staged files. Before changing anything, the authorized
operator checks `docker compose version`, `docker network inspect
nginx-proxy-manager_default`, and `docker compose -f deploy/docker/compose.yaml
config`. Abort if NPM's network is absent, the resolved configuration differs
from review, or any host port other than the exact Tailscale-IP-to-8788
dashboard mapping is published. Image tags are mutable: set
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
`VGXNESS_SYNCD_PASSWORD`, `VGXNESS_BACKUP_PASSWORD`, `VGXNESS_ADMIN_SECRET`,
and `VGXNESS_POSTGRES_INIT` before `docker compose config`; Compose has no
worktree secret defaults. Set `VGXNESS_TAILSCALE_IP` to the host's canonical
Tailscale IPv4 in `100.64.0.0/10`, never `0.0.0.0`, a LAN/public address, or a
DNS name. The database admin, app, and backup password files are root:root
`0600`. The DSN and dashboard secret files are root:65532 `0640`, allowing only
syncd runtime UID `65532:65532` to read them; `syncd.env` is root:root `0600`.
The dashboard secret payload is 32 through 4096 bytes after removing at most one final LF or CRLF.
The raw allowance is 4098 bytes solely for a 4096-byte payload plus CRLF; a larger payload or multiple newline fails.
Production `0640` and test/dev `0600` are valid; group write/execute or any other-user permission fails startup.
Never put secrets in environment values, argv, logs, or proxy configuration.

Create the dashboard secret outside the repository without exposing its value
in shell history, then verify metadata and runtime readability without printing
the value:

```sh
sudo install -o root -g 65532 -m 0640 /dev/null /absolute/private/admin-secret
openssl rand -hex 32 | sudo tee /absolute/private/admin-secret >/dev/null
sudo stat /absolute/private/admin-secret
sudo -u '#65532' test -r /absolute/private/admin-secret
export VGXNESS_ADMIN_SECRET=/absolute/private/admin-secret
```

Do not place the generated value in `syncd.env`. The application rejects an
inline `VGXNESS_SYNC_ADMIN_SECRET`, partial integrated-admin settings, unsafe
secret paths or modes, weak secrets shorter than 32 bytes, multiline secrets,
and noncanonical or non-Tailscale authorities.

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

## Persistent dashboard over Tailscale

This persistent integrated dashboard is Docker/Linux-only and fails closed on Windows; standalone loopback-only `vgxness-syncd admin` remains supported.
The same `vgxness-syncd serve` process, PostgreSQL repository/pool, image, and
container lifecycle own the sync API and dashboard. Compose binds only
`${VGXNESS_TAILSCALE_IP}:8788` on the host. From a tailnet peer, open the exact
URL `http://${VGXNESS_TAILSCALE_IP}:8788/`; Tailscale encrypts the tailnet hop.
In a private terminal on the host, read the configured value only when needed
with `sudo cat "$VGXNESS_ADMIN_SECRET"`, paste it into the login form, and
clear the terminal/clipboard according to local policy. The secret is never
printed by `serve` and is still required for peers on the Docker networks.

Never publish `0.0.0.0:8788`, bind port 8788 to a public or LAN address, add an
NPM proxy host for it, or route it through another public reverse proxy. The
dashboard is plain HTTP at the application layer because this deployment relies
on Tailscale transport encryption; it is not suitable outside the tailnet.
Verify from a non-tailnet interface that no dashboard connection is possible.

## Update and Rollback

For an update, record and retain a digest-pinned predecessor as
`VGXNESS_SYNCD_IMAGE=REGISTRY/IMAGE@sha256:PREDECESSOR`. Build only with reviewed
digest-pinned Go 1.26 builder/runtime base identities. Roll back with
`VGXNESS_SYNCD_IMAGE=REGISTRY/IMAGE@sha256:PREDECESSOR docker compose -f deploy/docker/compose.yaml up -d --no-build vgxness-syncd`,
then read back `docker compose ... ps` and repeat public-TLS `401`. Database migrations are
forward-only: do not roll PostgreSQL schema backward. If the update migrates the
database and fails, stop ingress and escalate to the DBA/maintenance owner.
The dashboard stops and restarts with the syncd container; in-memory dashboard
sessions are invalid after restart, while the protected secret file remains the
login source. Updates and rollbacks must preserve the exact Tailscale bind,
authority, and secret-file mapping. After either operation, repeat the sync API
check, log in through the exact Tailscale URL, and confirm there is still no NPM
route or non-Tailscale host publication for port 8788.
Secret replacement, removal, or permission changes require a controlled syncd container restart.
The old secret remains active until restart; changing the file alone does not revoke it from the running process.

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
