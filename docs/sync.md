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

The daemon reads `VGXNESS_SYNC_POSTGRES_DSN` and
`VGXNESS_SYNC_OWNER_ID` when starting the service or managing device
credentials. Keep the database connection string out of command arguments,
logs, checked-in files, and proxy configuration. Missing or malformed
configuration fails closed.

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
The retired `--development-allow-insecure-non-loopback` flag rejects `true`;
explicit `false` remains a no-op only so existing launch commands can migrate.

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

The command validates the HTTPS endpoint, device ID, and bearer locally. A
context-cancellable cross-process lock serializes enrollment. It derives two
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
