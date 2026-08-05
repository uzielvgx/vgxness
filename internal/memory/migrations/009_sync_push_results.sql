CREATE TABLE sync_push_results (
    mutation_id TEXT PRIMARY KEY CHECK (typeof(mutation_id) = 'text' AND length(CAST(mutation_id AS BLOB)) = 36 AND mutation_id GLOB '????????-????-[1-5]???-[89ab]???-????????????' AND mutation_id NOT GLOB '*[^0-9a-f-]*'),
    disposition TEXT NOT NULL CHECK (disposition IN ('accepted', 'previously_accepted', 'conflict', 'rejected')),
    retryable INTEGER NOT NULL CHECK (retryable = 0),
    code TEXT NOT NULL CHECK (typeof(code) = 'text' AND length(CAST(code AS BLOB)) <= 64),
    sequence INTEGER UNIQUE,
    canonical_version INTEGER NOT NULL CHECK (typeof(canonical_version) = 'integer' AND canonical_version >= 0),
    record_kind TEXT NOT NULL CHECK (record_kind IN ('project', 'session', 'observation')),
    record_id TEXT NOT NULL CHECK (typeof(record_id) = 'text' AND length(CAST(record_id AS BLOB)) BETWEEN 1 AND 1024),
    mutation_kind TEXT NOT NULL CHECK (mutation_kind IN ('create', 'update', 'archive', 'tombstone', 'resolve')),
    base_version INTEGER NOT NULL CHECK (typeof(base_version) = 'integer' AND base_version >= 0),
    mutation_hash BLOB NOT NULL CHECK (typeof(mutation_hash) = 'blob' AND length(mutation_hash) = 32),
    completed_at INTEGER NOT NULL CHECK (typeof(completed_at) = 'integer' AND completed_at > 0),
    CHECK ((disposition IN ('accepted', 'previously_accepted', 'conflict') AND retryable = 0 AND code = '' AND typeof(sequence) = 'integer' AND sequence > 0 AND canonical_version > 0) OR (disposition = 'rejected' AND retryable = 0 AND length(CAST(code AS BLOB)) BETWEEN 1 AND 64 AND sequence IS NULL AND canonical_version = 0))
);
