CREATE TABLE project_roots (
    workspace_hash TEXT PRIMARY KEY,
    project_id TEXT NOT NULL UNIQUE REFERENCES projects(id)
);
