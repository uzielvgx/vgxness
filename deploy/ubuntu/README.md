# Ubuntu 24.04 single-VPS deployment package

Status: **proposed operator runbook**, not observed deployment evidence. Only an
authorized root operator with DBA/maintenance approval may execute it. It does
not establish DNS/TLS or perform any action by itself. These files do not execute actions by themselves; the authorized operator executes the reviewed steps, including explicitly approved account creation where this runbook calls for it.
This native package keeps PostgreSQL and the daemon local; it does not configure
public ingress. Never publish 8787 or PostgreSQL. Do not use the native Ubuntu package as a containerized Nginx Proxy Manager upstream. Use the repository Docker deployment, where syncd joins the verified external Nginx Proxy Manager network. Mere Nginx Proxy Manager network attachment cannot reach host loopback; do not blindly open a host port or bind the daemon to 0.0.0.0 as a workaround.

## Preflight and abort

Before any write, the authorized root operator records these read-only checks:

```sh
systemctl is-active vgxness-syncd postgresql
ss -ltn '( sport = :8787 )'
getent passwd vgxness-syncd
getent group vgxness-syncd
getent passwd vgxness-syncd-backup
getent group vgxness-syncd-backup
systemd-analyze verify STAGED_UNIT_DIRECTORY/vgxness-syncd.service STAGED_UNIT_DIRECTORY/vgxness-syncd-backup.service STAGED_UNIT_DIRECTORY/vgxness-syncd-backup.timer
```

Expected observations are known recorded unit states, either no listener or a
listener only on `127.0.0.1:8787`, a known current target/checksum, and zero
validation errors for the staged candidate. On an initial install, the
installed unit/configuration/current targets are expected absent and must not
be verified as if installed; on update they must exactly match the recorded
revision. Abort on any missing tool,
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
of the prior units and env-file metadata before change;
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

Copy the reviewed unit examples, then run these loopback-only checks. This
package does not configure public ingress; its owner separately validates any
external proxy after the native daemon is healthy.

```sh
systemd-analyze verify /etc/systemd/system/vgxness-syncd.service /etc/systemd/system/vgxness-syncd-backup.service /etc/systemd/system/vgxness-syncd-backup.timer
systemctl daemon-reload
systemctl enable --now vgxness-syncd
systemctl is-active vgxness-syncd
ss -ltn '( sport = :8787 )'
test "$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' http://127.0.0.1:8787/v1/sync/capabilities)" = 401
systemctl enable --now vgxness-syncd-backup.timer
systemctl is-active vgxness-syncd vgxness-syncd-backup.timer
```

Expected smoke observations are active units, only the literal loopback
listener, and `401` for the unauthenticated local capabilities request. Do not
supply a bearer to smoke tests. Abort on any other status or public listener;
restore the recorded root-owned unit/env revision and known predecessor before
retrying.

## Immutable update and rollback

The daemon always executes `/opt/vgxness-syncd/current/vgxness-syncd`. The
approved change record must name the exact `APPROVED_RELEASE_SHA256`; it is not
a value to infer from a downloaded file. Store that reviewed binary only under
`/opt/vgxness-syncd/versions/APPROVED_RELEASE_SHA256/vgxness-syncd`; an existing
target with different bytes is an abort, never an overwrite. Before switch,
verify the staged checksum, ownership, executable mode, and recorded
predecessor target. Atomically switch `current` with a new temporary symlink
and `mv -T`, then verify the approved release, not merely that a symlink exists:

```sh
readlink -f /opt/vgxness-syncd/current
sha256sum /opt/vgxness-syncd/current/vgxness-syncd
```

Both results must exactly match the reviewed
`/opt/vgxness-syncd/versions/APPROVED_RELEASE_SHA256` path and
`APPROVED_RELEASE_SHA256`, respectively, before `systemctl restart vgxness-syncd`. Recheck all unchanged configuration target identities before
restarting the daemon.

Rollback to the verified predecessor symlink target plus preserved root-owned
unit/env revisions is allowed only when the recorded migration ledger
shows that the predecessor is compatible with the current PostgreSQL schema.
Do not roll PostgreSQL backward and never claim a predecessor will run on an
advanced schema. If the ledger is absent, advanced, or incompatible, stop
ingress and the daemon; use an approved database recovery/forward-fix path
instead. If a compatible rollback restart or smoke fails, leave the new target
inactive, restore the predecessor only if still compatible, and keep redacted
evidence for escalation.

## Legacy Caddy retirement (upgrade only)

This section applies only when inspection proves a prior VGXNESS Caddy route is installed and is never an initial-install action. For an upgrade from a prior VGXNESS route, first detect the
installed route and require exact recorded ownership/revision for the site
block and every configuration file it references. If ownership, hostname,
revision, or block boundaries are ambiguous, abort and escalate; do not edit a
shared configuration by inference.

Preserve and read back the full shared configuration, its owner/mode, and its
checksum before an authorized operator disables or removes only the exact
recorded VGXNESS site/block. Never delete a shared Caddyfile or unrelated
sites. Validate the full remaining configuration, then reload it safely only
after validation succeeds. Verify the former public hostname no longer routes to 127.0.0.1:8787; if it still does, stop and escalate rather than opening a
host port or changing the daemon listener. The following inspection/validation/controlled-reload gates execute only after exact owned site removal:

```sh
systemctl is-active caddy || true
sha256sum /etc/caddy/Caddyfile
caddy validate --config /etc/caddy/Caddyfile
systemctl reload caddy
```

Do not disable the shared service when it hosts unrelated sites. Retain the
preserved exact revision for recovery, and restore it only with the shared
configuration owner's approval.

## Backup and restore

The separate `vgxness-syncd-backup` identity has no daemon state write access.
Its timer makes one local `current.pgd`: it writes the exact temporary path,
uses `pg_restore --list`, syncs it, writes and syncs a mode-0600
`current.pgd.sha256` temporary companion whose recorded filename is the
relative `current.pgd`, then atomically publishes the dump and companion under
one lock. A missing or mismatched companion makes restore fail closed. If
publication has begun but the companion is not yet published, the pair is
intentionally invalid: stop and require operator recovery; do not claim that a
prior verified generation always survives. Pre-publication failure preserves the prior verified generation; partial publication fails closed and requires
operator recovery. Before
restore, coordinate with the timer: `systemctl disable --now vgxness-syncd-backup.timer`, wait for `vgxness-syncd-backup.service` to be
inactive, and use `flock -n /var/lib/vgxness-syncd-backup/backup.lock` as the
reviewed maintenance lock. Do not restore while the lock cannot be acquired.
Keep file descriptor 9 open for the entire operator shell, through checksum,
drop/recreate, and restore. External encrypted copy and retention are the
operator’s responsibility; this package retains no remote copy.

Restore has the blast radius of database `vgxness_sync` and every enrolled
device. With DBA/maintenance approval: stop ingress and the daemon; make and
verify an investigation dump; verify the selected dump and its mode-0600
SHA-256 companion; then only the DBA may drop/recreate exact database
`vgxness_sync`. Clear ambient libpq settings and bind every destructive command
to the reviewed local socket, port, and superuser. Record and recheck the
PostgreSQL `system_identifier`, database name, and role before drop/recreate
and restore. Use this reviewed local command prefix, never an ambient DSN or
service configuration. `env -i PATH=/usr/bin:/bin` clears every ambient libpq
variable before each PostgreSQL command. Restore only the approved local
generation in place, so the relative checksum filename is unambiguous:

```sh
# Run this block as root in one POSIX shell.
set -eu
umask 077
EXPECTED_SYSTEM_IDENTIFIER="$(/usr/sbin/runuser --user postgres -- /usr/bin/env -i PATH=/usr/bin:/bin /usr/bin/psql --host=/var/run/postgresql --port=5432 --username=postgres --dbname=postgres --tuples-only --no-align -c "SELECT system_identifier FROM pg_control_system();")"
test -n "$EXPECTED_SYSTEM_IDENTIFIER"
EXPECTED_DATABASE_OWNER="$(/usr/sbin/runuser --user postgres -- /usr/bin/env -i PATH=/usr/bin:/bin /usr/bin/psql --host=/var/run/postgresql --port=5432 --username=postgres --dbname=postgres --tuples-only --no-align -c "SELECT datdba::regrole FROM pg_database WHERE datname = 'vgxness_sync';")"
test "$EXPECTED_DATABASE_OWNER" = "vgxness_syncd"
EXPECTED_ROLE="$(/usr/sbin/runuser --user postgres -- /usr/bin/env -i PATH=/usr/bin:/bin /usr/bin/psql --host=/var/run/postgresql --port=5432 --username=postgres --dbname=postgres --tuples-only --no-align -c "SELECT rolname FROM pg_roles WHERE rolname = 'vgxness_syncd';")"
test "$EXPECTED_ROLE" = "vgxness_syncd"
exec 9>/var/lib/vgxness-syncd-backup/backup.lock
flock -n 9
/usr/sbin/runuser --user postgres -- /usr/bin/env -i PATH=/usr/bin:/bin /usr/bin/pg_dump --host=/var/run/postgresql --port=5432 --username=postgres --dbname=vgxness_sync --format=custom --file=- > INVESTIGATION_VERIFIED.pgd
/usr/bin/sha256sum INVESTIGATION_VERIFIED.pgd
/usr/bin/pg_restore --list INVESTIGATION_VERIFIED.pgd
cd /var/lib/vgxness-syncd-backup
test "$(stat -c %a current.pgd.sha256)" = 600
/usr/bin/sha256sum --check current.pgd.sha256
/usr/bin/pg_restore --list current.pgd
CURRENT_SYSTEM_IDENTIFIER="$(/usr/sbin/runuser --user postgres -- /usr/bin/env -i PATH=/usr/bin:/bin /usr/bin/psql --host=/var/run/postgresql --port=5432 --username=postgres --dbname=postgres --tuples-only --no-align -c "SELECT system_identifier FROM pg_control_system();")"
test "$CURRENT_SYSTEM_IDENTIFIER" = "$EXPECTED_SYSTEM_IDENTIFIER"
test "$(/usr/sbin/runuser --user postgres -- /usr/bin/env -i PATH=/usr/bin:/bin /usr/bin/psql --host=/var/run/postgresql --port=5432 --username=postgres --dbname=postgres --tuples-only --no-align -c "SELECT datdba::regrole FROM pg_database WHERE datname = 'vgxness_sync';")" = "$EXPECTED_DATABASE_OWNER"
/usr/sbin/runuser --user postgres -- /usr/bin/env -i PATH=/usr/bin:/bin /usr/bin/dropdb --host=/var/run/postgresql --port=5432 --username=postgres --force vgxness_sync
CURRENT_SYSTEM_IDENTIFIER="$(/usr/sbin/runuser --user postgres -- /usr/bin/env -i PATH=/usr/bin:/bin /usr/bin/psql --host=/var/run/postgresql --port=5432 --username=postgres --dbname=postgres --tuples-only --no-align -c "SELECT system_identifier FROM pg_control_system();")"
test "$CURRENT_SYSTEM_IDENTIFIER" = "$EXPECTED_SYSTEM_IDENTIFIER"
test "$(/usr/sbin/runuser --user postgres -- /usr/bin/env -i PATH=/usr/bin:/bin /usr/bin/psql --host=/var/run/postgresql --port=5432 --username=postgres --dbname=postgres --tuples-only --no-align -c "SELECT rolname FROM pg_roles WHERE rolname = 'vgxness_syncd';")" = "$EXPECTED_ROLE"
/usr/sbin/runuser --user postgres -- /usr/bin/env -i PATH=/usr/bin:/bin /usr/bin/createdb --host=/var/run/postgresql --port=5432 --username=postgres --owner=vgxness_syncd vgxness_sync
CURRENT_SYSTEM_IDENTIFIER="$(/usr/sbin/runuser --user postgres -- /usr/bin/env -i PATH=/usr/bin:/bin /usr/bin/psql --host=/var/run/postgresql --port=5432 --username=postgres --dbname=postgres --tuples-only --no-align -c "SELECT system_identifier FROM pg_control_system();")"
test "$CURRENT_SYSTEM_IDENTIFIER" = "$EXPECTED_SYSTEM_IDENTIFIER"
test "$(/usr/sbin/runuser --user postgres -- /usr/bin/env -i PATH=/usr/bin:/bin /usr/bin/psql --host=/var/run/postgresql --port=5432 --username=postgres --dbname=postgres --tuples-only --no-align -c "SELECT datdba::regrole FROM pg_database WHERE datname = 'vgxness_sync';")" = "vgxness_syncd"
/usr/sbin/runuser --user postgres -- /usr/bin/env -i PATH=/usr/bin:/bin /usr/bin/pg_restore --host=/var/run/postgresql --port=5432 --username=postgres --exit-on-error --single-transaction --no-owner --role=vgxness_syncd --dbname=vgxness_sync < current.pgd
systemctl restart vgxness-syncd
systemctl is-active vgxness-syncd
test "$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' http://127.0.0.1:8787/v1/sync/capabilities)" = 401
systemctl enable --now vgxness-syncd-backup.timer
```

The DBA records the exact `system_identifier`, `vgxness_sync` database, and
`vgxness_syncd` owner/role observations immediately before destructive action,
and rechecks them before `dropdb`, `createdb`, and `pg_restore`. Verify
schema/owner/readback before restarting the daemon; its local unauthenticated
401 gate on `127.0.0.1:8787` must pass before the backup timer is re-enabled.
No proxy is activated by this restore shell; external ingress must remain stopped until separately validated by its owner. On any restore failure, keep
external ingress, daemon, and backup timer stopped: the timer remains stopped
until DBA recovery uses the same reviewed local prefix to
restore the verified investigation dump. Abort rather than guessing if either
dump, hash, identity, target, maintenance lock, or explicit approval is absent.

## Device credentials, Blast radius, and Escalation

`device issue --name AUTHORIZED_DEVICE_NAME` and `device revoke CANONICAL_UUID`
are local, separately authorized maintenance actions. Issue requires an
authorized local TTY and protected service environment; never use a pipe,
redirect, systemd output capture, argv bearer, or log for its returned bearer.
Revoke carries only the canonical device UUID. Escalate DNS/certificate/firewall
issues to the host owner, database partial/restore issues to the DBA, and any
credential exposure to the security owner for rotation. Abort on uncertainty.
