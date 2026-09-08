CREATE TABLE IF NOT EXISTS local_provider_sessions (
    handle TEXT PRIMARY KEY CHECK(length(handle) BETWEEN 8 AND 128),
    project_id TEXT NOT NULL REFERENCES projects(id),
    provider TEXT NOT NULL CHECK(length(provider) BETWEEN 1 AND 128),
    external_id_hash BLOB NOT NULL CHECK(length(external_id_hash)=32),
    state TEXT NOT NULL CHECK(state IN ('active','completed','interrupted','cancelled')),
    checkpointed INTEGER NOT NULL DEFAULT 0 CHECK(checkpointed IN (0,1)),
    final_observation_id TEXT NULL REFERENCES observations(id),
    created_at INTEGER NOT NULL CHECK(created_at > 0),
    updated_at INTEGER NOT NULL CHECK(updated_at >= created_at),
    completed_at INTEGER NULL CHECK(completed_at IS NULL OR completed_at >= created_at),
    UNIQUE(project_id,provider,external_id_hash),
    CHECK((state='completed') = (final_observation_id IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS local_provider_sessions_project_state_updated_idx ON local_provider_sessions(project_id,state,updated_at DESC,handle);
