CREATE TABLE IF NOT EXISTS port_forward_sessions (
    session_id TEXT PRIMARY KEY,
    cluster_id TEXT NOT NULL,
    namespace TEXT NOT NULL,
    target_kind TEXT NOT NULL,
    target_name TEXT NOT NULL,
    local_port INTEGER NOT NULL,
    remote_port INTEGER NOT NULL,
    status TEXT NOT NULL,
    connection_mode TEXT NOT NULL DEFAULT 'direct',
    last_error TEXT,
    created_by TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_port_forward_sessions_cluster ON port_forward_sessions(cluster_id);
