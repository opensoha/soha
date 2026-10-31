-- Task115; Task114 currently owns 0120/0121. Payloads expire after seven days.
-- Key hashes remain as minimal tombstones: an expired original key cannot
-- accidentally execute a command again after payload retention cleanup.
CREATE TABLE IF NOT EXISTS platform_pod_executions (
    id uuid PRIMARY KEY,
    actor_id text NOT NULL,
    key_hash char(64) NOT NULL,
    cluster_id text NOT NULL,
    namespace text NOT NULL,
    pod_name text NOT NULL,
    expected_uid text NOT NULL,
    container text NOT NULL,
    status text NOT NULL CHECK (status IN ('running', 'succeeded', 'failed', 'timed_out', 'canceled', 'transport_lost', 'unknown')),
    created_at timestamptz NOT NULL,
    deadline_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    finished_at timestamptz,
    encrypted_payload text NOT NULL,
    UNIQUE (actor_id, key_hash)
);
CREATE INDEX IF NOT EXISTS platform_pod_executions_running
    ON platform_pod_executions(deadline_at) WHERE status = 'running';
CREATE INDEX IF NOT EXISTS platform_pod_executions_payload_expiry
    ON platform_pod_executions(expires_at) WHERE encrypted_payload <> '';
