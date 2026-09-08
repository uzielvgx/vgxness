CREATE TABLE portable_project_identities (
    portable_id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    workspace_hash TEXT NOT NULL UNIQUE,
    source TEXT NOT NULL,
    bound_at TEXT NOT NULL
);
CREATE INDEX portable_project_identities_project_id_idx ON portable_project_identities(project_id);
