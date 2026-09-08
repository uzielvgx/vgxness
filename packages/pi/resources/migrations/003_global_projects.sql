DROP TRIGGER observations_boundary_insert;
DROP TRIGGER observations_boundary_update;
DROP INDEX observations_topic_key;

CREATE TABLE sessions_v3 (
    id TEXT NOT NULL,
    project_id TEXT NOT NULL REFERENCES projects(id),
    PRIMARY KEY(id, project_id)
);
INSERT INTO sessions_v3(id, project_id)
SELECT id, project_id FROM sessions;

CREATE TABLE observations_v3 (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    session_id TEXT,
    scope TEXT NOT NULL CHECK(scope IN ('project','personal')),
    type TEXT NOT NULL,
    content TEXT NOT NULL,
    topic_key TEXT,
    producer TEXT NOT NULL,
    source_provider TEXT NOT NULL DEFAULT '',
    source_id TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL CHECK(state IN ('active','needs_review','archived')),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    review_after INTEGER,
    title TEXT NOT NULL DEFAULT '',
    FOREIGN KEY(session_id, project_id) REFERENCES sessions_v3(id, project_id)
);
INSERT INTO observations_v3(
    id, project_id, session_id, scope, type, content, topic_key, producer,
    source_provider, source_id, state, created_at, updated_at, review_after, title
)
SELECT
    id, project_id, session_id, scope, type, content, topic_key, producer,
    source_provider, source_id, state, created_at, updated_at, review_after, title
FROM observations;

CREATE TABLE observation_refs_v3 (
    observation_id TEXT NOT NULL REFERENCES observations_v3(id) ON DELETE CASCADE,
    target_id TEXT NOT NULL REFERENCES observations_v3(id),
    PRIMARY KEY(observation_id, target_id)
);
INSERT INTO observation_refs_v3(observation_id, target_id)
SELECT observation_id, target_id FROM observation_refs;

DROP TABLE observation_refs;
DROP TABLE observations;
DROP TABLE sessions;

ALTER TABLE sessions_v3 RENAME TO sessions;
ALTER TABLE observations_v3 RENAME TO observations;
ALTER TABLE observation_refs_v3 RENAME TO observation_refs;

CREATE TRIGGER observations_boundary_insert BEFORE INSERT ON observations BEGIN
    INSERT OR IGNORE INTO projects(id) VALUES(NEW.project_id);
    INSERT OR IGNORE INTO sessions(id,project_id)
    SELECT NEW.session_id,NEW.project_id WHERE NEW.session_id IS NOT NULL;
END;
CREATE TRIGGER observations_boundary_update BEFORE UPDATE OF session_id ON observations BEGIN
    INSERT OR IGNORE INTO sessions(id,project_id)
    SELECT NEW.session_id,NEW.project_id WHERE NEW.session_id IS NOT NULL;
END;

CREATE UNIQUE INDEX observations_topic_key
ON observations(project_id, scope, topic_key)
WHERE topic_key IS NOT NULL;

CREATE TABLE legacy_imports (
    source_id TEXT PRIMARY KEY,
    imported_at INTEGER NOT NULL
);
