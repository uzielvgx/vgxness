CREATE TABLE sync_outbox_claims (
    mutation_id TEXT NOT NULL PRIMARY KEY REFERENCES sync_outbox(mutation_id) ON DELETE CASCADE CHECK (typeof(mutation_id) = 'text' AND length(CAST(mutation_id AS BLOB)) = 36 AND mutation_id GLOB '????????-????-[1-5]???-[89ab]???-????????????' AND mutation_id NOT GLOB '*[^0-9a-f-]*'),
    first_claim_token TEXT NOT NULL CHECK (typeof(first_claim_token) = 'text' AND length(CAST(first_claim_token AS BLOB)) = 36 AND first_claim_token GLOB '????????-????-[1-5]???-[89ab]???-????????????' AND first_claim_token NOT GLOB '*[^0-9a-f-]*'),
    claim_token TEXT NOT NULL CHECK (typeof(claim_token) = 'text' AND length(CAST(claim_token AS BLOB)) = 36 AND claim_token GLOB '????????-????-[1-5]???-[89ab]???-????????????' AND claim_token NOT GLOB '*[^0-9a-f-]*'),
    first_claimed_at INTEGER NOT NULL CHECK (typeof(first_claimed_at) = 'integer' AND first_claimed_at > 0),
    claimed_at INTEGER NOT NULL CHECK (typeof(claimed_at) = 'integer' AND claimed_at > 0),
    lease_until INTEGER NOT NULL CHECK (typeof(lease_until) = 'integer' AND lease_until > 0),
    CHECK (lease_until >= claimed_at),
    CHECK (claimed_at >= first_claimed_at)
);

CREATE INDEX sync_outbox_claims_lease_idx ON sync_outbox_claims(lease_until, mutation_id);
