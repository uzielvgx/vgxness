CREATE TABLE IF NOT EXISTS sync_project_transitions (
    portable_project_id TEXT PRIMARY KEY REFERENCES portable_project_identities(portable_id) ON DELETE RESTRICT,
    local_project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    mode TEXT NOT NULL CHECK (mode IN ('reseed_source','rejoin_merge')),
    status TEXT NOT NULL CHECK (status IN ('pulling','publishing','completed')),
    created_at INTEGER NOT NULL CHECK (typeof(created_at) = 'integer' AND created_at > 0),
    completed_at INTEGER,
    CHECK ((mode = 'reseed_source' AND status <> 'pulling') OR mode = 'rejoin_merge'),
    CHECK ((status = 'completed' AND completed_at IS NOT NULL) OR (status <> 'completed' AND completed_at IS NULL))
);
CREATE TABLE IF NOT EXISTS sync_project_transition_records (
    portable_project_id TEXT NOT NULL REFERENCES sync_project_transitions(portable_project_id) ON DELETE CASCADE,
    record_kind TEXT NOT NULL CHECK (record_kind IN ('project','session','observation')),
    local_id TEXT NOT NULL CHECK (length(CAST(local_id AS BLOB)) BETWEEN 1 AND 1024),
    payload_hash BLOB NOT NULL CHECK (typeof(payload_hash) = 'blob' AND length(payload_hash) = 32),
    seen_remote INTEGER NOT NULL DEFAULT 0 CHECK (seen_remote IN (0,1)),
    PRIMARY KEY (portable_project_id, record_kind, local_id)
);
CREATE INDEX IF NOT EXISTS sync_project_transitions_status_idx ON sync_project_transitions(status, portable_project_id);
