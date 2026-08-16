CREATE TABLE sync_portable_identity_adoptions (
    portable_project_id TEXT NOT NULL,
    record_kind TEXT NOT NULL CHECK (record_kind IN ('session', 'observation')),
    local_id TEXT NOT NULL CHECK (length(CAST(local_id AS BLOB)) BETWEEN 1 AND 1024),
    portable_id TEXT NOT NULL CHECK (length(portable_id) = 36),
    adopting_device_id TEXT NOT NULL CHECK (length(adopting_device_id) = 36 AND adopting_device_id = lower(adopting_device_id) AND adopting_device_id GLOB '????????-????-????-????-????????????' AND replace(adopting_device_id,'-','') NOT GLOB '*[^0-9a-f]*' AND substr(adopting_device_id,15,1) IN ('1','2','3','4','5') AND substr(adopting_device_id,20,1) IN ('8','9','a','b')),
    adopted_at INTEGER NOT NULL CHECK (adopted_at > 0),
    PRIMARY KEY (portable_project_id, record_kind, local_id),
    FOREIGN KEY (portable_project_id, record_kind, local_id) REFERENCES sync_portable_identities(portable_project_id, record_kind, local_id) ON DELETE RESTRICT
);
