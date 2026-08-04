ALTER TABLE projects ADD COLUMN sync_version INTEGER NOT NULL DEFAULT 0 CHECK (sync_version >= 0);
ALTER TABLE sessions ADD COLUMN sync_version INTEGER NOT NULL DEFAULT 0 CHECK (sync_version >= 0);
ALTER TABLE observations ADD COLUMN sync_version INTEGER NOT NULL DEFAULT 0 CHECK (sync_version >= 0);
