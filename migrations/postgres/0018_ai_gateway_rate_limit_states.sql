CREATE TABLE IF NOT EXISTS ai_gateway_rate_limit_states (
    key TEXT PRIMARY KEY,
    policy_id TEXT NOT NULL,
    scope TEXT NOT NULL,
    actor_type TEXT,
    actor_id TEXT,
    ai_client_id TEXT,
    tool_name TEXT,
    limit_value INT NOT NULL,
    burst_value INT NOT NULL DEFAULT 1,
    interval_seconds DOUBLE PRECISION NOT NULL,
    tat TIMESTAMP NOT NULL,
    metadata JSON NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ai_gateway_rate_limit_states_policy ON ai_gateway_rate_limit_states(policy_id, updated_at);
CREATE INDEX IF NOT EXISTS idx_ai_gateway_rate_limit_states_actor ON ai_gateway_rate_limit_states(actor_type, actor_id, updated_at);
CREATE INDEX IF NOT EXISTS idx_ai_gateway_rate_limit_states_client_tool ON ai_gateway_rate_limit_states(ai_client_id, tool_name, updated_at);
CREATE INDEX IF NOT EXISTS idx_ai_gateway_rate_limit_states_tat ON ai_gateway_rate_limit_states(tat);
