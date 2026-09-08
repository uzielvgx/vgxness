CREATE TABLE IF NOT EXISTS sync_project_backup_intents (
    portable_project_id TEXT PRIMARY KEY,
    local_project_id TEXT NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN ('reseed_source','rejoin_merge')),
    intent_id TEXT NOT NULL UNIQUE,
    backup_path TEXT NOT NULL,
    backup_sha256 BLOB NULL CHECK (backup_sha256 IS NULL OR length(backup_sha256)=32),
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS sync_project_backup_intents_local_idx ON sync_project_backup_intents(local_project_id, portable_project_id);
