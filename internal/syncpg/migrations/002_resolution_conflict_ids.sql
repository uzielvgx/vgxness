DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM mutations WHERE kind = 'resolve') THEN
        RAISE EXCEPTION 'unrecoverable v1 resolve history';
    END IF;
END $$;

ALTER TABLE mutations ADD COLUMN resolution_conflict_ids uuid[];
ALTER TABLE mutations ADD CONSTRAINT mutations_resolution_conflict_ids CHECK (
    resolution_conflict_ids IS NULL OR (
        kind = 'resolve' AND cardinality(resolution_conflict_ids) BETWEEN 1 AND 128
        AND array_position(resolution_conflict_ids, NULL::uuid) IS NULL
    )
);
