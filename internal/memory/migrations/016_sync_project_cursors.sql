CREATE TABLE IF NOT EXISTS sync_project_cursor (
    portable_project_id TEXT PRIMARY KEY REFERENCES portable_project_identities(portable_id) ON DELETE RESTRICT,
    history_id TEXT NOT NULL CHECK (length(history_id) = 36),
    position INTEGER NOT NULL CHECK (typeof(position) = 'integer' AND position >= 0),
    watermark INTEGER NOT NULL CHECK (typeof(watermark) = 'integer' AND watermark >= position),
    updated_at INTEGER NOT NULL CHECK (typeof(updated_at) = 'integer' AND updated_at > 0)
);
CREATE TABLE IF NOT EXISTS sync_project_inbox (
    portable_project_id TEXT NOT NULL REFERENCES portable_project_identities(portable_id) ON DELETE RESTRICT,
    history_id TEXT NOT NULL CHECK (length(history_id) = 36),
    seq INTEGER NOT NULL CHECK (typeof(seq) = 'integer' AND seq > 0),
    change_hash BLOB NOT NULL CHECK (typeof(change_hash) = 'blob' AND length(change_hash) = 32),
    applied_at INTEGER NOT NULL CHECK (typeof(applied_at) = 'integer' AND applied_at > 0),
    PRIMARY KEY (portable_project_id, history_id, seq)
);
CREATE INDEX IF NOT EXISTS sync_project_inbox_cursor_idx ON sync_project_inbox(portable_project_id, history_id, seq);
