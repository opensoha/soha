-- Task115 original Gateway/Native conditional mutation claims. A lost result
-- never permits replay. Payload cleanup retains actor/key tombstones.
CREATE TABLE IF NOT EXISTS platform_conditional_mutation_receipts (
    id uuid PRIMARY KEY,
    actor_id text NOT NULL,
    key_hash char(64) NOT NULL,
    status text NOT NULL CHECK (status IN ('running', 'succeeded', 'unknown')),
    created_at timestamptz NOT NULL,
    deadline_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    finished_at timestamptz,
    encrypted_payload text NOT NULL,
    UNIQUE(actor_id, key_hash)
);
CREATE INDEX IF NOT EXISTS platform_conditional_mutation_running
    ON platform_conditional_mutation_receipts(deadline_at) WHERE status = 'running';
CREATE INDEX IF NOT EXISTS platform_conditional_mutation_expiry
    ON platform_conditional_mutation_receipts(expires_at) WHERE encrypted_payload <> '';
