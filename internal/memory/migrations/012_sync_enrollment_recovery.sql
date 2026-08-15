ALTER TABLE sync_profiles ADD COLUMN previous_credential_ref TEXT NULL CHECK (previous_credential_ref IS NULL OR length(previous_credential_ref) BETWEEN 10 AND 512);
