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

## Remote deployment boundary

For remote synchronization, the deployment owner is responsible for TLS
certificate lifecycle, proxy access controls, request-size preservation,
timeouts, logging redaction, and forwarding only to the loopback listener.
VGXNESS does not currently provide native TLS termination in `vgxness-syncd`.
