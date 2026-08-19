# Synchronization service boundary

`vgxness-syncd` is the optional PostgreSQL-backed synchronization service. Its
HTTP listener is intentionally limited to a literal loopback address and
defaults to `127.0.0.1:8787`.

The service accepts bearer-authenticated synchronization requests. It does not
terminate TLS and must never be exposed directly on an untrusted network. A
remote deployment requires a TLS terminator or reverse proxy that accepts HTTPS
and forwards requests to the daemon through loopback on the same trusted host.
The application-side synchronization client continues to require an `https`
endpoint and does not follow credential-bearing redirects.

## Runtime configuration

The daemon reads `VGXNESS_SYNC_POSTGRES_DSN` or the mutually exclusive
`VGXNESS_SYNC_POSTGRES_DSN_FILE`, plus `VGXNESS_SYNC_OWNER_ID`, when starting
the service or managing device credentials. The file setting must name an
absolute, bounded, regular non-symlink file; exactly one final LF or CRLF is removed.
Keep the database connection string out of command arguments, logs, checked-in
files, and proxy configuration. Missing or malformed configuration fails
closed.

Optional admission-limit settings are
`VGXNESS_SYNC_AUTH_GLOBAL_PER_MINUTE`,
`VGXNESS_SYNC_AUTH_DEVICE_PER_MINUTE`, and
`VGXNESS_SYNC_AUTH_DEVICE_STATES`. When unset, they default respectively to
120, 60, and 256. Each value must be a positive base-10 integer; malformed,
zero, or negative values fail daemon startup before database setup or listener
creation. The admission window is fixed at one minute.

## Admission and audit bounds

Before PostgreSQL authentication, the daemon applies the configured admission
limits; their defaults admit at most 120 valid bearer attempts per minute
across the process and at most 60 per minute for each syntactically declared
device UUID. Excess attempts receive the normal
`429 limit_exceeded` response and do not reach PostgreSQL. The UUID fairness
state is capped at 256 entries and is process-local, so deployments with
multiple daemon processes enforce the bound independently.

Failed-authentication audit evidence converges toward a 30-day retained window
and 10,000 events for the configured owner. Cleanup runs only in a failed
authentication audit transaction, is cancellation-aware, and deletes at most
250 expired or over-cap records per audit write; a pre-existing excess can
therefore persist briefly while subsequent failures converge it. It does not
create background goroutines.

Start the local listener with:

```sh
vgxness-syncd serve
```

An explicit `--listen` value must remain a literal loopback IP with a non-zero
port. Hostnames, wildcard addresses, public or private non-loopback addresses,
and port zero are rejected before configuration or credentials are read.
The sole exception is `serve --container-network --listen 0.0.0.0:8787`, which
is intended for a private Docker network and does not itself enforce Docker
network isolation. `GET /healthz` is an unauthenticated, bounded liveness
response for container health checks; it does not disclose database state.
The retired `--development-allow-insecure-non-loopback` flag rejects `true`;
explicit `false` remains a no-op only so existing launch commands can migrate.

## Local browser administration

Run `vgxness-syncd admin` in an interactive terminal on the database host. The
command binds a random literal-loopback port and prints that run's admin URL and
ephemeral operator secret to terminal output. Open the exact printed URL, enter
the secret, and keep the terminal private. The console uses no session cookie;
its browser actions are POST-only and every response is `no-store`.

The dashboard can issue a named device credential and begin revocation of an
active device. Issuance displays the new bearer exactly once in the immediate
response. Copy it directly into `vgxness memory sync configure` standard input;
do not put it in a URL, shell argument, log, note, browser extension, or browser
storage. Leaving or refreshing the one-time page loses the display. Revocation
uses a separate, short-lived one-time confirmation page that shows the canonical
device UUID before the final POST. Returning to the dashboard after either
completed action requires POST; a GET never issues or revokes a device.
After issuance, verify that the device appears active on the dashboard before
using its bearer. Delivery can complete when commit acknowledgement is
ambiguous, so the dashboard is the source of truth.

For administration across SSH, first run the command on the remote host and
note its printed `127.0.0.1:PORT`. In a second terminal, forward the **same**
port so the browser's exact Host and Origin continue to match:

```sh
ssh -N -L 127.0.0.1:PORT:127.0.0.1:PORT operator@sync-host
```

Then open `http://127.0.0.1:PORT/` locally and use the operator secret from the
remote interactive terminal. Replace both `PORT` values with the one printed by
that admin run. Do not expose or reverse-proxy this admin listener.

Loopback and a random port narrow exposure but are not a complete browser
isolation boundary. A service worker previously registered for the exact reused
loopback origin can initiate console actions or read an in-browser bearer. This
task accepts that residual risk; the console does not claim to eliminate it.
Use a dedicated browser context where practical, close the page promptly, and
stop the admin process when the operation is complete.

## Local enrollment and status

Enroll a local client without putting a bearer in command arguments,
environment variables, or the SQLite database. Supply the bearer only on
standard input:

```sh
vgxness memory sync configure \
  --endpoint https://sync.example.test \
  --device-id 550e8400-e29b-41d4-a716-446655440000
```

The command reads the bearer from standard input; enter or pipe it directly
without placing it in an environment variable or command argument.

On Linux and macOS, an explicitly requested current-user-owned credential file
may be used instead of the desktop keyring:

```sh
vgxness memory sync configure --credential-file /absolute/private/bearer --endpoint https://sync.example.test --device-id 550e8400-e29b-41d4-a716-446655440000
vgxness memory sync status --credential-file /absolute/private/bearer
vgxness memory sync --credential-file /absolute/private/bearer
```

The file must be an absolute regular file, not a symlink (nor beneath a
symlink), owned by the current user, with no group or other permissions, and
contain one bearer line with an optional final LF or CRLF. The path and bearer
are never persisted. Each later `status` or `sync` invocation must explicitly
provide the file again. Credential files are unsupported on Windows; omit the
flag there to use the default keyring. Same-user filesystem races cannot be
made atomically host-enforced, so the command rechecks the opened descriptor
and fails closed when its identity or metadata changes.

For pre-existing local data, queue records before the first remote sync with a
workspace-scoped, local-only operation:

```sh
vgxness memory sync backfill --workspace /absolute/workspace --limit 100 --json
vgxness memory sync --credential-file /absolute/private/bearer
```

Backfill sends no network request and reads no credential. It deterministically
queues only unsynced records for that resolved project, preserves observation
content, timestamps, and versions, skips tombstoned records, detects queue
identity collisions, and is safe to run again. If an existing create contains
an older but valid record snapshot, backfill replaces only its payload while
preserving its mutation and queue identity, and only while that exact row is
still pending with zero attempts and has no claim history. Active or expired
claims, retries, attempts, malformed or identity-changed payloads, and
concurrently changed rows fail closed. `--limit` defaults to 100 and accepts 1
through 1000; JSON reports `remaining=true` when another invocation is needed.

After a project-scoped push completes, project pull maps portable identities
back to local identities before consulting the durable push receipt. An exact
accepted or previously accepted create/update echo for a project, session, or
observation is already materialized: the local record and version are preserved
while the project inbox and cursor advance in the same transaction. Mutation
hash, record identity and kind, mutation kind, base and canonical versions,
sequence, and disposition must all match the receipt. Missing or mismatched
receipts and foreign creates still fail closed; matching conflict dispositions
continue through conflict materialization, and active reseed/rejoin transitions
retain their snapshot-specific handling.

## First-device reseed and device rejoin

Reset the cloud first. On the Mac/source device, run `vgxness memory sync reseed --workspace /absolute/workspace --confirm-cloud-empty [--json]`; it proceeds only when the cloud is exactly empty. On every subsequent Linux or Windows device, run `vgxness memory sync rejoin --workspace /absolute/workspace --confirm-merge [--json]`. Both commands require the strict project marker/binding, apply only to that project, and never run `git pull`. The confirmation is mode-specific and exact. Retries resume the durable transition; a pending intent blocks that project only. The older `repair-project` command remains a narrow local recovery for an accepted missing-project repair, not the first-device workflow.

`memory sync configure` validates the HTTPS endpoint, device ID, and bearer
locally. A context-cancellable cross-process lock serializes enrollment. It derives two
deterministic keyring slots from canonical local storage identity, stores the
bearer only in the inactive slot, then transactionally switches the SQLite
profile. Keyring and SQLite are not one atomic transaction: failed persistence
removes that inactive slot and leaves the prior active credential unchanged.
Schema v12 records only the opposite deterministic slot as a recovery marker;
on the next enrollment it compensates or completes cleanup. Cleanup failure
leaves the marker and blocks further enrollment rather than guessing. Legacy
credential references are never made markers or auto-deleted; migration leaves
them retained. No step contacts the remote service.

`vgxness memory sync status [--json]` is also local and read-only. It reports
whether a profile is configured and whether its keyring credential is
available, missing, unavailable, or invalid; it never prints the bearer or the
recovery marker, and it never contacts the remote service.

## Remote deployment boundary

For remote synchronization, the deployment owner is responsible for TLS
certificate lifecycle, proxy access controls, request-size preservation,
timeouts, logging redaction, and forwarding only to the loopback listener.
VGXNESS does not currently provide native TLS termination in `vgxness-syncd`.

For a declarative Ubuntu 24.04 single-host example, see the
[Ubuntu 24.04 single-VPS deployment package](../deploy/ubuntu/README.md).
For an additive Docker path behind an existing Nginx Proxy Manager, see the
[Docker deployment package](../deploy/docker/README.md). Neither package is
evidence of an observed VPS deployment.
