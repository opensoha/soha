CREATE TABLE IF NOT EXISTS ai_gateway_rate_limit_counters (
    key TEXT PRIMARY KEY,
    policy_id TEXT NOT NULL,
    scope TEXT NOT NULL,
    actor_type TEXT,
    actor_id TEXT,
    ai_client_id TEXT,
    tool_name TEXT,
    window_start TIMESTAMP NOT NULL,
    window_end TIMESTAMP NOT NULL,
    limit_value INT NOT NULL,
    count INT NOT NULL DEFAULT 0,
    metadata JSON NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ai_gateway_rate_limit_counters_policy_window ON ai_gateway_rate_limit_counters(policy_id, window_start, window_end);
CREATE INDEX IF NOT EXISTS idx_ai_gateway_rate_limit_counters_actor ON ai_gateway_rate_limit_counters(actor_type, actor_id, window_end);
CREATE INDEX IF NOT EXISTS idx_ai_gateway_rate_limit_counters_client_tool ON ai_gateway_rate_limit_counters(ai_client_id, tool_name, window_end);
CREATE INDEX IF NOT EXISTS idx_ai_gateway_rate_limit_counters_window_end ON ai_gateway_rate_limit_counters(window_end);
