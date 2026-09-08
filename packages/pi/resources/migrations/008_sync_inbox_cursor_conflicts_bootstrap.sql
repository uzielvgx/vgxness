CREATE TABLE sync_inbox (
    history_id TEXT NOT NULL CHECK (typeof(history_id) = 'text' AND length(CAST(history_id AS BLOB)) = 36 AND history_id GLOB '????????-????-[1-5]???-[89ab]???-????????????' AND history_id NOT GLOB '*[^0-9a-f-]*'),
    seq INTEGER NOT NULL CHECK (typeof(seq) = 'integer' AND seq > 0),
    change_hash BLOB NOT NULL CHECK (typeof(change_hash) = 'blob' AND length(change_hash) = 32),
    applied_at INTEGER NOT NULL CHECK (typeof(applied_at) = 'integer' AND applied_at > 0),
    PRIMARY KEY (history_id, seq)
);

CREATE TABLE sync_cursor (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    history_id TEXT NOT NULL CHECK (typeof(history_id) = 'text' AND length(CAST(history_id AS BLOB)) = 36 AND history_id GLOB '????????-????-[1-5]???-[89ab]???-????????????' AND history_id NOT GLOB '*[^0-9a-f-]*'),
    position INTEGER NOT NULL CHECK (typeof(position) = 'integer' AND position >= 0),
    updated_at INTEGER NOT NULL CHECK (typeof(updated_at) = 'integer' AND updated_at > 0)
);

CREATE TABLE sync_tombstones (
    history_id TEXT NOT NULL CHECK (typeof(history_id) = 'text' AND length(CAST(history_id AS BLOB)) = 36 AND history_id GLOB '????????-????-[1-5]???-[89ab]???-????????????' AND history_id NOT GLOB '*[^0-9a-f-]*'),
    seq INTEGER NOT NULL CHECK (typeof(seq) = 'integer' AND seq > 0),
    record_kind TEXT NOT NULL CHECK (record_kind IN ('project', 'session', 'observation')),
    record_id TEXT NOT NULL CHECK (typeof(record_id) = 'text' AND length(CAST(record_id AS BLOB)) BETWEEN 1 AND 1024),
    canonical_version INTEGER NOT NULL CHECK (typeof(canonical_version) = 'integer' AND canonical_version > 0),
    payload_version INTEGER NOT NULL CHECK (payload_version = 1),
    provenance BLOB NOT NULL CHECK (typeof(provenance) = 'blob' AND length(provenance) BETWEEN 1 AND 1048576 AND json_valid(CAST(provenance AS TEXT)) = 1),
    deleted_at INTEGER NOT NULL CHECK (typeof(deleted_at) = 'integer' AND deleted_at > 0),
    PRIMARY KEY (history_id, seq),
    UNIQUE (record_kind, record_id, canonical_version)
);

CREATE INDEX sync_tombstones_record_idx ON sync_tombstones(record_kind, record_id, canonical_version);

CREATE TABLE sync_conflicts (
    conflict_id TEXT NOT NULL CHECK (typeof(conflict_id) = 'text' AND length(CAST(conflict_id AS BLOB)) = 36 AND conflict_id GLOB '????????-????-[1-5]???-[89ab]???-????????????' AND conflict_id NOT GLOB '*[^0-9a-f-]*'),
    history_id TEXT NOT NULL CHECK (typeof(history_id) = 'text' AND length(CAST(history_id AS BLOB)) = 36 AND history_id GLOB '????????-????-[1-5]???-[89ab]???-????????????' AND history_id NOT GLOB '*[^0-9a-f-]*'),
    created_seq INTEGER NOT NULL CHECK (typeof(created_seq) = 'integer' AND created_seq > 0),
    record_kind TEXT NOT NULL CHECK (record_kind IN ('project', 'session', 'observation')),
    record_id TEXT NOT NULL CHECK (typeof(record_id) = 'text' AND length(CAST(record_id AS BLOB)) BETWEEN 1 AND 1024),
    canonical_version INTEGER NOT NULL CHECK (typeof(canonical_version) = 'integer' AND canonical_version > 0),
    competing_version_id TEXT NOT NULL CHECK (typeof(competing_version_id) = 'text' AND length(CAST(competing_version_id AS BLOB)) = 36 AND competing_version_id GLOB '????????-????-[1-5]???-[89ab]???-????????????' AND competing_version_id NOT GLOB '*[^0-9a-f-]*'),
    status TEXT NOT NULL CHECK (status IN ('unresolved', 'resolved')),
    resolved_seq INTEGER,
    payload_version INTEGER NOT NULL CHECK (payload_version = 1),
    snapshot BLOB NOT NULL CHECK (typeof(snapshot) = 'blob' AND length(snapshot) BETWEEN 1 AND 1048576 AND json_valid(CAST(snapshot AS TEXT)) = 1),
    created_at INTEGER NOT NULL CHECK (typeof(created_at) = 'integer' AND created_at > 0),
    updated_at INTEGER NOT NULL CHECK (typeof(updated_at) = 'integer' AND updated_at >= created_at),
    UNIQUE (conflict_id),
    UNIQUE (history_id, created_seq),
    CHECK ((status = 'unresolved' AND resolved_seq IS NULL) OR (status = 'resolved' AND resolved_seq IS NOT NULL AND typeof(resolved_seq) = 'integer' AND resolved_seq > created_seq))
);

CREATE INDEX sync_conflicts_unresolved_idx ON sync_conflicts(status, record_kind, record_id, created_seq);

CREATE TABLE sync_bootstrap (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    phase TEXT NOT NULL CHECK (phase IN ('projects', 'sessions', 'observations', 'complete')),
    payload_version INTEGER NOT NULL CHECK (payload_version = 1),
    checkpoint BLOB NOT NULL CHECK (typeof(checkpoint) = 'blob' AND length(checkpoint) BETWEEN 1 AND 1048576 AND json_valid(CAST(checkpoint AS TEXT)) = 1),
    created_at INTEGER NOT NULL CHECK (typeof(created_at) = 'integer' AND created_at > 0),
    updated_at INTEGER NOT NULL CHECK (typeof(updated_at) = 'integer' AND updated_at >= created_at)
);
