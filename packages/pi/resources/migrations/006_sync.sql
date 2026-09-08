CREATE TABLE sync_profiles (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    endpoint TEXT NOT NULL CHECK (length(endpoint) BETWEEN 1 AND 2048),
    device_id TEXT NOT NULL CHECK (length(device_id) = 36),
    credential_ref TEXT NOT NULL CHECK (length(credential_ref) BETWEEN 10 AND 512),
    created_at INTEGER NOT NULL CHECK (created_at > 0),
    updated_at INTEGER NOT NULL CHECK (updated_at >= created_at)
);

CREATE TABLE sync_outbox (
    id INTEGER PRIMARY KEY,
    mutation_id TEXT NOT NULL UNIQUE CHECK (length(mutation_id) = 36),
    record_kind TEXT NOT NULL CHECK (record_kind IN ('project', 'session', 'observation')),
    record_id TEXT NOT NULL CHECK (length(CAST(record_id AS BLOB)) BETWEEN 1 AND 1024),
    mutation_kind TEXT NOT NULL CHECK (mutation_kind IN ('create', 'update', 'archive', 'tombstone', 'resolve')),
    base_version INTEGER NOT NULL CHECK (base_version >= 0),
    payload_version INTEGER NOT NULL CHECK (payload_version = 1),
    payload BLOB NOT NULL CHECK (length(payload) BETWEEN 1 AND 1048576),
    state TEXT NOT NULL CHECK (state IN ('pending', 'retry')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at INTEGER NOT NULL CHECK (next_attempt_at > 0),
    last_error_code TEXT NOT NULL DEFAULT '' CHECK (length(last_error_code) <= 64),
    created_at INTEGER NOT NULL CHECK (created_at > 0),
    updated_at INTEGER NOT NULL CHECK (updated_at >= created_at)
);

CREATE INDEX sync_outbox_due_idx ON sync_outbox(next_attempt_at, created_at, id);
