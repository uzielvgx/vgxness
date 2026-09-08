CREATE TABLE IF NOT EXISTS sync_project_repairs (
    portable_project_id TEXT PRIMARY KEY REFERENCES portable_project_identities(portable_id) ON DELETE RESTRICT,
    local_project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    original_mutation_id TEXT NOT NULL UNIQUE REFERENCES sync_push_results(mutation_id) ON DELETE RESTRICT,
    repair_mutation_id TEXT NOT NULL UNIQUE CHECK (length(repair_mutation_id) = 36),
    status TEXT NOT NULL CHECK (status IN ('pending','completed','rejected')),
    terminal_code TEXT NOT NULL CHECK (length(CAST(terminal_code AS BLOB)) <= 64),
    created_at INTEGER NOT NULL CHECK (typeof(created_at) = 'integer' AND created_at > 0),
    completed_at INTEGER,
    CHECK ((status = 'pending' AND terminal_code = '' AND completed_at IS NULL) OR (status = 'completed' AND terminal_code = '' AND completed_at IS NOT NULL) OR (status = 'rejected' AND terminal_code <> '' AND completed_at IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS sync_project_repairs_pending_idx ON sync_project_repairs(status, repair_mutation_id);
