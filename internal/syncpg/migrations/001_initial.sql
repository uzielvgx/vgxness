CREATE TABLE owners (
    id uuid PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE owner_sync_state (
    owner_id uuid PRIMARY KEY REFERENCES owners(id),
    history_id uuid NOT NULL UNIQUE,
    next_seq bigint NOT NULL CHECK (next_seq > 0)
);

CREATE TABLE devices (
    id uuid PRIMARY KEY,
    owner_id uuid NOT NULL REFERENCES owners(id),
    display_name text NOT NULL CHECK (display_name <> ''),
    credential_hash bytea NOT NULL CHECK (octet_length(credential_hash) = 32),
    credential_prefix text NOT NULL,
    issued_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    last_seen_at timestamptz,
    UNIQUE (owner_id, id)
);

CREATE TABLE projects (
    owner_id uuid NOT NULL REFERENCES owners(id),
    id text NOT NULL CHECK (id <> ''),
    version bigint NOT NULL CHECK (version >= 0),
    payload jsonb NOT NULL,
    PRIMARY KEY (owner_id, id)
);

CREATE TABLE sessions (
    owner_id uuid NOT NULL REFERENCES owners(id),
    id text NOT NULL CHECK (id <> ''),
    project_id text NOT NULL,
    version bigint NOT NULL CHECK (version >= 0),
    payload jsonb NOT NULL,
    PRIMARY KEY (owner_id, id),
    UNIQUE (owner_id, id, project_id),
    FOREIGN KEY (owner_id, project_id) REFERENCES projects(owner_id, id)
);

CREATE TABLE observations (
    owner_id uuid NOT NULL REFERENCES owners(id),
    id text NOT NULL CHECK (id <> ''),
    project_id text NOT NULL,
    session_id text,
    scope text NOT NULL CHECK (scope IN ('project', 'personal')),
    type text NOT NULL CHECK (type <> ''),
    title text NOT NULL,
    content text NOT NULL,
    topic_key text,
    provenance jsonb NOT NULL DEFAULT '{}'::jsonb,
    lifecycle text NOT NULL CHECK (lifecycle IN ('active', 'archived', 'tombstoned')),
    review_state text NOT NULL CHECK (review_state IN ('clear', 'needs_review')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    review_after timestamptz,
    version bigint NOT NULL CHECK (version >= 0),
    PRIMARY KEY (owner_id, id),
    FOREIGN KEY (owner_id, project_id) REFERENCES projects(owner_id, id),
    FOREIGN KEY (owner_id, session_id, project_id) REFERENCES sessions(owner_id, id, project_id),
    CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX observations_topic_identity
    ON observations(owner_id, project_id, scope, topic_key)
    WHERE topic_key IS NOT NULL AND topic_key <> ''
      AND lifecycle NOT IN ('archived', 'tombstoned');
CREATE INDEX observations_active_idx ON observations(owner_id, project_id, updated_at DESC)
    WHERE lifecycle = 'active';

CREATE TABLE observation_references (
    owner_id uuid NOT NULL,
    observation_id text NOT NULL,
    target_observation_id text NOT NULL,
    PRIMARY KEY (owner_id, observation_id, target_observation_id),
    CHECK (observation_id <> target_observation_id),
    FOREIGN KEY (owner_id, observation_id) REFERENCES observations(owner_id, id),
    FOREIGN KEY (owner_id, target_observation_id) REFERENCES observations(owner_id, id)
);
CREATE INDEX observation_references_target_idx ON observation_references(owner_id, target_observation_id);

CREATE TABLE mutations (
    owner_id uuid NOT NULL REFERENCES owners(id),
    device_id uuid NOT NULL,
    mutation_id uuid NOT NULL,
    request_hash bytea NOT NULL CHECK (octet_length(request_hash) > 0),
    kind text NOT NULL CHECK (kind IN ('create', 'update', 'archive', 'tombstone', 'resolve')),
    record_id text NOT NULL CHECK (record_id <> ''),
    base_version bigint NOT NULL CHECK (base_version >= 0),
    disposition text NOT NULL CHECK (disposition IN ('accepted', 'previously_accepted', 'conflict', 'rejected')),
    canonical_seq bigint CHECK (canonical_seq > 0),
    canonical_version bigint CHECK (canonical_version >= 0),
    error_code text,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (owner_id, device_id, mutation_id),
    FOREIGN KEY (owner_id, device_id) REFERENCES devices(owner_id, id),
    UNIQUE (owner_id, canonical_seq),
    UNIQUE (owner_id, device_id, mutation_id, record_id),
    UNIQUE (owner_id, device_id, mutation_id, record_id, disposition),
    CHECK (disposition NOT IN ('accepted', 'conflict') OR canonical_seq IS NOT NULL)
);
CREATE INDEX mutations_lookup_idx ON mutations(owner_id, device_id, mutation_id);

CREATE TABLE record_versions (
    id bigserial PRIMARY KEY,
    owner_id uuid NOT NULL REFERENCES owners(id),
    record_kind text NOT NULL CHECK (record_kind IN ('project', 'session', 'observation')),
    record_id text NOT NULL CHECK (record_id <> ''),
    record_version bigint NOT NULL CHECK (record_version >= 0),
    source_device_id uuid NOT NULL,
    source_mutation_id uuid NOT NULL,
    base_version bigint NOT NULL CHECK (base_version >= 0),
    disposition text NOT NULL CHECK (disposition IN ('accepted', 'conflict')),
    snapshot jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner_id, id),
    UNIQUE (owner_id, id, record_kind, record_id),
    UNIQUE (owner_id, record_kind, record_id, record_version, source_mutation_id),
    FOREIGN KEY (owner_id, source_device_id, source_mutation_id, record_id, disposition)
        REFERENCES mutations(owner_id, device_id, mutation_id, record_id, disposition)
);

CREATE TABLE changes (
    owner_id uuid NOT NULL REFERENCES owners(id),
    seq bigint NOT NULL CHECK (seq > 0),
    mutation_device_id uuid NOT NULL,
    mutation_id uuid NOT NULL,
    change_kind text NOT NULL CHECK (change_kind IN ('accepted', 'conflict')),
    record_kind text NOT NULL CHECK (record_kind IN ('project', 'session', 'observation')),
    record_id text NOT NULL CHECK (record_id <> ''),
    canonical_version bigint CHECK (canonical_version >= 0),
    version_id bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (owner_id, seq),
    FOREIGN KEY (owner_id, mutation_device_id, mutation_id)
        REFERENCES mutations(owner_id, device_id, mutation_id),
    FOREIGN KEY (owner_id, mutation_device_id, mutation_id, record_id, change_kind)
        REFERENCES mutations(owner_id, device_id, mutation_id, record_id, disposition),
    FOREIGN KEY (owner_id, version_id, record_kind, record_id)
        REFERENCES record_versions(owner_id, id, record_kind, record_id)
);
ALTER TABLE changes ADD UNIQUE (owner_id, seq, record_kind, record_id, change_kind);
CREATE INDEX changes_pull_idx ON changes(owner_id, seq);

CREATE TABLE observation_conflicts (
    owner_id uuid NOT NULL REFERENCES owners(id),
    conflict_id uuid NOT NULL,
    observation_id text NOT NULL,
    canonical_version bigint NOT NULL CHECK (canonical_version >= 0),
    competing_version_id bigint NOT NULL,
    status text NOT NULL CHECK (status IN ('unresolved', 'resolved')),
    created_seq bigint NOT NULL CHECK (created_seq > 0),
    resolved_seq bigint CHECK (resolved_seq > 0),
    competing_kind text GENERATED ALWAYS AS ('observation') STORED,
    competing_id text GENERATED ALWAYS AS (observation_id) STORED,
    created_kind text GENERATED ALWAYS AS ('observation') STORED,
    created_id text GENERATED ALWAYS AS (observation_id) STORED,
    created_change_kind text GENERATED ALWAYS AS ('conflict') STORED,
    resolved_kind text GENERATED ALWAYS AS ('observation') STORED,
    resolved_id text GENERATED ALWAYS AS (observation_id) STORED,
    resolved_change_kind text GENERATED ALWAYS AS ('accepted') STORED,
    PRIMARY KEY (owner_id, conflict_id),
    FOREIGN KEY (owner_id, observation_id) REFERENCES observations(owner_id, id),
    FOREIGN KEY (owner_id, competing_version_id, competing_kind, competing_id) REFERENCES record_versions(owner_id, id, record_kind, record_id),
    FOREIGN KEY (owner_id, created_seq, created_kind, created_id, created_change_kind) REFERENCES changes(owner_id, seq, record_kind, record_id, change_kind),
    FOREIGN KEY (owner_id, resolved_seq, resolved_kind, resolved_id, resolved_change_kind) REFERENCES changes(owner_id, seq, record_kind, record_id, change_kind),
    CHECK ((status = 'unresolved' AND resolved_seq IS NULL) OR (status = 'resolved' AND resolved_seq IS NOT NULL)),
    CHECK (status <> 'resolved' OR resolved_seq >= created_seq)
);
CREATE INDEX observation_conflicts_unresolved_idx ON observation_conflicts(owner_id, created_seq)
    WHERE status = 'unresolved';

CREATE TABLE tombstones (
    owner_id uuid NOT NULL REFERENCES owners(id),
    record_kind text NOT NULL CHECK (record_kind IN ('project', 'session', 'observation')),
    record_id text NOT NULL CHECK (record_id <> ''),
    version_id bigint NOT NULL,
    mutation_device_id uuid NOT NULL,
    mutation_id uuid NOT NULL,
    deleted_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (owner_id, record_kind, record_id),
    FOREIGN KEY (owner_id, version_id, record_kind, record_id)
        REFERENCES record_versions(owner_id, id, record_kind, record_id),
    FOREIGN KEY (owner_id, mutation_device_id, mutation_id, record_id)
        REFERENCES mutations(owner_id, device_id, mutation_id, record_id)
);

CREATE TABLE audit_events (
    id bigserial PRIMARY KEY,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    owner_id uuid REFERENCES owners(id),
    device_id uuid,
    request_id uuid,
    action text NOT NULL,
    outcome text NOT NULL,
    reason_code text,
    mutation_id uuid,
    FOREIGN KEY (owner_id, device_id) REFERENCES devices(owner_id, id)
);
