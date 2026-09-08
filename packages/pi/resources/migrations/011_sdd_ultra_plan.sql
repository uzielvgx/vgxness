CREATE TABLE sdd_changes_v11 (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    title TEXT NOT NULL,
    backend TEXT NOT NULL CHECK (backend IN ('openspec', 'memory', 'hybrid')),
    interaction_mode TEXT NOT NULL CHECK (interaction_mode IN ('automatic', 'interactive')),
    model_plan TEXT NOT NULL CHECK (model_plan IN ('low', 'medium', 'high', 'ultra')),
    phase TEXT NOT NULL CHECK (phase IN ('explore', 'proposal', 'spec', 'design', 'tasks', 'apply', 'verify', 'complete')),
    status TEXT NOT NULL CHECK (status IN ('active', 'completed', 'cancelled')),
    state_version INTEGER NOT NULL CHECK (state_version > 0),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE (project_id, id),
    UNIQUE (project_id, idempotency_key),
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);

INSERT INTO sdd_changes_v11(id, project_id, idempotency_key, title, backend, interaction_mode, model_plan, phase, status, state_version, created_at, updated_at)
SELECT id, project_id, idempotency_key, title, backend, interaction_mode, model_plan, phase, status, state_version, created_at, updated_at
FROM sdd_changes;

DROP TABLE sdd_changes;
ALTER TABLE sdd_changes_v11 RENAME TO sdd_changes;
CREATE INDEX sdd_changes_project_updated_idx ON sdd_changes(project_id, updated_at DESC, id);
