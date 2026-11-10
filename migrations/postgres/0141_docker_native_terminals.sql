-- Independent Docker PTY leases, never shared with Pod terminal authorization.
-- Single-use tickets and encrypted actor/credential/target snapshots. No stream
-- data is stored. Keep this table on rollback for original outcome queries.
CREATE TABLE IF NOT EXISTS docker_native_terminals (
    id uuid PRIMARY KEY,
    actor_id text NOT NULL,
    credential_hash char(64) NOT NULL CHECK (credential_hash ~ '^[a-f0-9]{64}$'),
    ticket_hash text NOT NULL CHECK (ticket_hash = '' OR ticket_hash ~ '^[a-f0-9]{64}$'),
    status text NOT NULL CHECK (status IN ('pending','active','closing','closed','expired')),
    created_at timestamptz NOT NULL,
    ticket_expires_at timestamptz NOT NULL,
    deadline_at timestamptz NOT NULL,
    closed_at timestamptz,
    encrypted_payload text NOT NULL
);
CREATE INDEX IF NOT EXISTS docker_native_terminals_actor ON docker_native_terminals(actor_id,id);
CREATE INDEX IF NOT EXISTS docker_native_terminals_admission ON docker_native_terminals(deadline_at) WHERE status IN ('pending','active','closing');
CREATE INDEX IF NOT EXISTS docker_native_terminals_cleanup ON docker_native_terminals(closed_at) WHERE status IN ('closed','expired');
