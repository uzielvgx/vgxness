ALTER TABLE local_provider_sessions ADD COLUMN lease_token TEXT NULL CHECK(lease_token IS NULL OR length(lease_token) BETWEEN 8 AND 128);
ALTER TABLE local_provider_sessions ADD COLUMN lease_until INTEGER NULL CHECK(lease_until IS NULL OR lease_until > 0);
CREATE INDEX IF NOT EXISTS local_provider_sessions_project_lease_reconcile_idx ON local_provider_sessions(project_id,state,lease_until,handle);
