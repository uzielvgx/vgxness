CREATE INDEX audit_events_owner_id_idx ON audit_events(owner_id, id);
CREATE INDEX audit_events_owner_occurred_at_idx ON audit_events(owner_id, occurred_at, id);
