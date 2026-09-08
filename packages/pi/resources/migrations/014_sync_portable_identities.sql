CREATE TABLE sync_portable_identities (
    portable_project_id TEXT NOT NULL REFERENCES portable_project_identities(portable_id) ON DELETE RESTRICT,
    record_kind TEXT NOT NULL CHECK (record_kind IN ('session', 'observation')),
    local_id TEXT NOT NULL CHECK (length(CAST(local_id AS BLOB)) BETWEEN 1 AND 1024),
    portable_id TEXT NOT NULL UNIQUE CHECK (length(portable_id) = 36),
    origin_device_id TEXT NOT NULL CHECK (length(origin_device_id) = 36 AND origin_device_id = lower(origin_device_id) AND origin_device_id GLOB '????????-????-????-????-????????????' AND replace(origin_device_id,'-','') NOT GLOB '*[^0-9a-f]*' AND substr(origin_device_id,15,1) IN ('1','2','3','4','5') AND substr(origin_device_id,20,1) IN ('8','9','a','b')),
    created_at INTEGER NOT NULL CHECK (created_at > 0),
    PRIMARY KEY (portable_project_id, record_kind, local_id)
);
CREATE INDEX sync_portable_identities_inverse_idx ON sync_portable_identities(portable_project_id, record_kind, portable_id);
