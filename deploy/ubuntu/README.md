# Ubuntu 24.04 single-VPS deployment package

Status: **proposed operator runbook**, not observed deployment evidence. Only an
authorized root operator with DBA/maintenance approval may execute it. It does
not establish DNS/TLS or perform any action by itself. These files do not execute actions by themselves; the authorized operator executes the reviewed steps, including explicitly approved account creation where this runbook calls for it.
The intended boundary is Caddy on public HTTPS to `127.0.0.1:8787`; PostgreSQL
and the daemon stay local. Never publish 8787 or PostgreSQL.

## Preflight and abort

Before any write, the authorized root operator records these read-only checks:

```sh
systemctl is-active vgxness-syncd caddy postgresql
ss -ltn '( sport = :8787 )'
getent passwd vgxness-syncd
getent group vgxness-syncd
getent passwd vgxness-syncd-backup
getent group vgxness-syncd-backup
systemd-analyze verify STAGED_UNIT_DIRECTORY/vgxness-syncd.service STAGED_UNIT_DIRECTORY/vgxness-syncd-backup.service STAGED_UNIT_DIRECTORY/vgxness-syncd-backup.timer
caddy validate --config STAGED_CADDYFILE
```

Expected observations are known recorded unit states, either no listener or a
listener only on `127.0.0.1:8787`, a known current target/checksum, and zero
validation errors for the staged candidate. On an initial install, the
installed unit/configuration/current targets are expected absent and must not
be verified as if installed; on update they must exactly match the recorded
revision. The operator selects `/etc/caddy/Caddyfile` as the primary Caddy
configuration and must confirm that choice locally. Abort on any missing tool,
validation error, public listener, unrecorded current target, or existing
unknown target in `/opt/vgxness-syncd`, `/etc/vgxness-syncd`,
`/etc/vgxness-syncd-backup`, or `/var/lib/vgxness-syncd-backup`. Do not delete
or blindly rerun over partial state.

For an initial install, additionally require successful read-only absence
checks: `test ! -e /opt/vgxness-syncd/current`, `test ! -e
/etc/systemd/system/vgxness-syncd.service`, and `test ! -e
/etc/systemd/system/vgxness-syncd-backup.service`. For an update, instead record `readlink -f
/opt/vgxness-syncd/current` and `sha256sum
/opt/vgxness-syncd/current/vgxness-syncd`, then compare both to the approved
predecessor.

For an initial install, each intended target must be absent or explicitly
approved as an exact prior revision. Preserve root-owned copies and checksums
of the prior units, Caddy configuration, and env-file metadata before change;
re-check their paths, owners, modes, and checksums immediately before use.

## Database and protected configuration

The root operator compares the four `getent` observations with the approved
identity records. If any existing account/group has a different UID, GID,
shell, or membership, abort. With explicit approval, create absent identities
fail-closed and separately; they have no shared identity and no login shell:

```sh
groupadd --system vgxness-syncd
useradd --system --gid vgxness-syncd --no-create-home --shell /usr/sbin/nologin vgxness-syncd
groupadd --system vgxness-syncd-backup
useradd --system --gid vgxness-syncd-backup --no-create-home --shell /usr/sbin/nologin vgxness-syncd-backup
install -d -o vgxness-syncd -g vgxness-syncd -m 0700 /var/lib/vgxness-syncd
install -d -o root -g root -m 0755 /opt/vgxness-syncd/versions
```

If an identity appears between preflight and creation, stop and re-run the
read-only comparison; do not modify that identity. The backup service owns its
`StateDirectory`; the daemon cannot write it.

The DBA first inspects partial state with `\du` and `\l` in local `psql`. If
either role or database already exists unexpectedly, stop: the DBA alone must
compare it with the recorded plan and explicitly resume a matching partial
state or abort. Never delete roles/databases or rerun bootstrap blindly.

Run `postgresql-bootstrap.sql.example` once through local `psql` as the DBA.
It stops on SQL error, creates unprivileged `vgxness_syncd` and separate
read-only `vgxness_syncd_backup` roles, and prompts interactively for both
passwords. Generate each password with at least `>=32` characters from exactly
`[A-Za-z0-9._~-]`; that alphabet needs no URI or pgpass escaping. Never put a
password literal in SQL, argv, shell history, or logs.

Before authorized interactive population, create all secret destinations empty:

```sh
install -d -o root -g root -m 0700 /etc/vgxness-syncd
install -d -o root -g vgxness-syncd-backup -m 0750 /etc/vgxness-syncd-backup
install -o root -g root -m 0600 /dev/null /etc/vgxness-syncd/syncd.env
install -o root -g vgxness-syncd-backup -m 0640 /dev/null /etc/vgxness-syncd-backup/pg_service.conf
install -o vgxness-syncd-backup -g vgxness-syncd-backup -m 0600 /dev/null /etc/vgxness-syncd-backup/pgpass
```

Populate `syncd.env` only with the reviewed daemon DSN and owner UUID; root
owns it with mode 0600. Populate `pg_service.conf` from its non-secret example;
it is `root:vgxness-syncd-backup` and mode `0640`, so the backup user can
traverse/read it without daemon access. Populate `pgpass` from its example for
the backup role; it is `vgxness-syncd-backup:vgxness-syncd-backup` mode 0600.
Do not put a DSN, password, or bearer in a unit, argv, proxy configuration, or
diagnostic log.

## Install, validation, and smoke

Copy the reviewed unit examples and the Caddy example to the approved primary
configuration, replace only `SYNC_PUBLIC_HOSTNAME`, then run:

```sh
systemd-analyze verify /etc/systemd/system/vgxness-syncd.service /etc/systemd/system/vgxness-syncd-backup.service /etc/systemd/system/vgxness-syncd-backup.timer
caddy validate --config /etc/caddy/Caddyfile
systemctl daemon-reload
systemctl reload caddy
systemctl restart vgxness-syncd
systemctl enable --now vgxness-syncd-backup.timer
systemctl is-active vgxness-syncd caddy vgxness-syncd-backup.timer
ss -ltn '( sport = :8787 )'
curl -i http://127.0.0.1:8787/v1/sync/capabilities
curl -i https://SYNC_PUBLIC_HOSTNAME/v1/sync/capabilities
```

Expected smoke observations are active units, only the literal loopback
listener, and `401` for both unauthenticated HTTP requests. Do not supply a
bearer to smoke tests. Abort on any other status, any public listener, or a
failed validation; restore the recorded root-owned configuration revision and
known predecessor before retrying.

## Immutable update and rollback

The daemon always executes `/opt/vgxness-syncd/current/vgxness-syncd`. Store a
reviewed binary under a checksum-named, content-addressed directory such as
`/opt/vgxness-syncd/versions/SHA256/vgxness-syncd`; an existing target with
different bytes is an abort, never an overwrite. Verify the staged checksum,
ownership, and executable mode, retain and verify the predecessor target, then
atomically switch `current` with a new temporary symlink and `mv -T`. Recheck
all unchanged configuration target identities before restarting the daemon.

Rollback is only the verified predecessor symlink target plus the preserved
root-owned unit/Caddy/env revisions, followed by `systemctl daemon-reload`,
`systemctl reload caddy`, and `systemctl restart vgxness-syncd`. Do not roll
PostgreSQL backward: its schema migration path is forward-only. If restart or
smoke fails, leave the new target inactive, restore the predecessor, and keep
redacted evidence for escalation.

## Backup and restore

The separate `vgxness-syncd-backup` identity has no daemon state write access.
Its timer makes one local `current.pgd`: it writes the exact temporary path,
uses `pg_restore --list`, syncs it, records a `sha256sum`, atomically `mv -T`s
it to `current.pgd`, then syncs the published file. A failure leaves the prior
current backup unchanged. External encrypted copy and retention are the
operator’s responsibility; this package retains no remote copy.

Restore has the blast radius of database `vgxness_sync` and every enrolled
device. With DBA/maintenance approval: stop ingress and the daemon; make and
verify an investigation dump; verify the selected dump and hash; then only the
DBA may drop/recreate exact database `vgxness_sync`. Restore with:

```sh
pg_dump --format=custom --file=INVESTIGATION_VERIFIED.pgd vgxness_sync
sha256sum INVESTIGATION_VERIFIED.pgd
pg_restore --list INVESTIGATION_VERIFIED.pgd
sha256sum SELECTED_VERIFIED_DUMP.pgd
pg_restore --list SELECTED_VERIFIED_DUMP.pgd
dropdb --force vgxness_sync
createdb --owner=vgxness_syncd vgxness_sync
pg_restore --exit-on-error --single-transaction --no-owner --role=vgxness_syncd --dbname=vgxness_sync SELECTED_VERIFIED_DUMP.pgd
```

Verify schema/owner/readback before restarting Caddy and the daemon. On any
restore failure, keep ingress and daemon stopped and restore the verified
investigation dump with `pg_restore --exit-on-error --single-transaction
--no-owner --role=vgxness_syncd --dbname=vgxness_sync INVESTIGATION_VERIFIED.pgd`.
Abort rather than guessing if either dump, hash, identity, target, or explicit
approval is absent.

## Device credentials, Blast radius, and Escalation

`device issue --name AUTHORIZED_DEVICE_NAME` and `device revoke CANONICAL_UUID`
are local, separately authorized maintenance actions. Issue requires an
authorized local TTY and protected service environment; never use a pipe,
redirect, systemd output capture, argv bearer, or log for its returned bearer.
Revoke carries only the canonical device UUID. Escalate DNS/certificate/firewall
issues to the host owner, database partial/restore issues to the DBA, and any
credential exposure to the security owner for rotation. Abort on uncertainty.
