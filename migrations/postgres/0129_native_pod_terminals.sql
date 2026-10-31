-- Task115 owns this native lease; Task114 currently ends at 0121.
-- Tickets are one use, short lived and bound to the original credential.
-- Closed/expired rows are removed after 24 hours; no stream data is persisted.
CREATE TABLE IF NOT EXISTS platform_pod_terminals (
    id uuid PRIMARY KEY,
    actor_id text NOT NULL,
    credential_hash char(64) NOT NULL,
    ticket_hash text NOT NULL,
    status text NOT NULL CHECK (status IN ('pending', 'active', 'closing', 'closed', 'expired')),
    created_at timestamptz NOT NULL,
    ticket_expires_at timestamptz NOT NULL,
    deadline_at timestamptz NOT NULL,
    closed_at timestamptz,
    encrypted_payload text NOT NULL
);
CREATE INDEX IF NOT EXISTS platform_pod_terminals_actor ON platform_pod_terminals(actor_id, id);
CREATE INDEX IF NOT EXISTS platform_pod_terminals_admission ON platform_pod_terminals(deadline_at)
    WHERE status IN ('pending', 'active', 'closing');
CREATE INDEX IF NOT EXISTS platform_pod_terminals_cleanup ON platform_pod_terminals(closed_at)
    WHERE status IN ('closed', 'expired');
