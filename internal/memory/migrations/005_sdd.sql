CREATE TABLE sdd_changes (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    title TEXT NOT NULL,
    backend TEXT NOT NULL CHECK (backend IN ('openspec', 'memory', 'hybrid')),
    interaction_mode TEXT NOT NULL CHECK (interaction_mode IN ('automatic', 'interactive')),
    model_plan TEXT NOT NULL CHECK (model_plan IN ('low', 'medium', 'high')),
    phase TEXT NOT NULL CHECK (phase IN ('explore', 'proposal', 'spec', 'design', 'tasks', 'apply', 'verify', 'complete')),
    status TEXT NOT NULL CHECK (status IN ('active', 'completed', 'cancelled')),
    state_version INTEGER NOT NULL CHECK (state_version > 0),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE (project_id, id),
    UNIQUE (project_id, idempotency_key),
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);

CREATE INDEX sdd_changes_project_updated_idx ON sdd_changes(project_id, updated_at DESC, id);

CREATE TABLE sdd_artifacts (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    change_id TEXT NOT NULL,
    phase TEXT NOT NULL CHECK (phase IN ('explore', 'proposal', 'spec', 'design', 'tasks', 'apply', 'verify')),
    status TEXT NOT NULL CHECK (status IN ('draft', 'accepted', 'stale')),
    current_revision_id TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE (project_id, change_id, phase),
    UNIQUE (project_id, change_id, id),
    FOREIGN KEY (project_id, change_id) REFERENCES sdd_changes(project_id, id) ON DELETE CASCADE
);

CREATE TABLE sdd_revisions (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    change_id TEXT NOT NULL,
    artifact_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('candidate', 'accepted')),
    content BLOB,
    external_location TEXT,
    content_digest TEXT NOT NULL CHECK (length(content_digest) = 64),
    input_digest TEXT NOT NULL CHECK (length(input_digest) = 64),
    created_at INTEGER NOT NULL,
    accepted_at INTEGER,
    CHECK ((content IS NOT NULL AND external_location IS NULL) OR (content IS NULL AND external_location IS NOT NULL)),
    UNIQUE (project_id, change_id, id),
    FOREIGN KEY (project_id, change_id, artifact_id) REFERENCES sdd_artifacts(project_id, change_id, id) ON DELETE CASCADE
);

CREATE INDEX sdd_revisions_artifact_created_idx ON sdd_revisions(project_id, change_id, artifact_id, created_at DESC, id);

CREATE TABLE sdd_revision_links (
    project_id TEXT NOT NULL,
    change_id TEXT NOT NULL,
    revision_id TEXT NOT NULL,
    input_artifact_id TEXT NOT NULL,
    input_revision_id TEXT NOT NULL,
    input_digest TEXT NOT NULL CHECK (length(input_digest) = 64),
    PRIMARY KEY (revision_id, input_artifact_id),
    FOREIGN KEY (project_id, change_id, revision_id) REFERENCES sdd_revisions(project_id, change_id, id) ON DELETE CASCADE,
    FOREIGN KEY (project_id, change_id, input_artifact_id) REFERENCES sdd_artifacts(project_id, change_id, id),
    FOREIGN KEY (project_id, change_id, input_revision_id) REFERENCES sdd_revisions(project_id, change_id, id)
);

CREATE TABLE sdd_projections (
    project_id TEXT NOT NULL,
    change_id TEXT NOT NULL,
    artifact_id TEXT NOT NULL,
    revision_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('current', 'stale', 'drift', 'failed')),
    digest TEXT NOT NULL CHECK (length(digest) = 64),
    location TEXT NOT NULL,
    recorded_at INTEGER NOT NULL,
    PRIMARY KEY (artifact_id),
    FOREIGN KEY (project_id, change_id, artifact_id) REFERENCES sdd_artifacts(project_id, change_id, id) ON DELETE CASCADE,
    FOREIGN KEY (project_id, change_id, revision_id) REFERENCES sdd_revisions(project_id, change_id, id)
);
