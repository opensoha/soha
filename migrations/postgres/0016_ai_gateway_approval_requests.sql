CREATE TABLE IF NOT EXISTS ai_gateway_approval_requests (
    id TEXT PRIMARY KEY,
    status TEXT NOT NULL,
    strategy TEXT NOT NULL,
    policy_id TEXT,
    approval_policy_ref TEXT,
    actor_type TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    actor_name TEXT,
    actor_roles JSON NOT NULL DEFAULT '[]',
    actor_teams JSON NOT NULL DEFAULT '[]',
    ai_client_id TEXT,
    ai_client_name TEXT,
    skill_id TEXT,
    tool_name TEXT NOT NULL,
    risk_level TEXT NOT NULL,
    requires_approval BOOLEAN NOT NULL DEFAULT TRUE,
    resource_scope JSON NOT NULL DEFAULT '{}',
    tool_input JSON NOT NULL DEFAULT '{}',
    related_ids JSON NOT NULL DEFAULT '{}',
    output JSON NOT NULL DEFAULT '{}',
    summary TEXT NOT NULL,
    request_id TEXT,
    source_ip TEXT,
    decided_by TEXT,
    decided_by_name TEXT,
    decided_at TIMESTAMP,
    decision_comment TEXT,
    expires_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ai_gateway_approval_requests_status ON ai_gateway_approval_requests(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_gateway_approval_requests_actor ON ai_gateway_approval_requests(actor_type, actor_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_gateway_approval_requests_client ON ai_gateway_approval_requests(ai_client_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_gateway_approval_requests_tool ON ai_gateway_approval_requests(tool_name, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_gateway_approval_requests_expires ON ai_gateway_approval_requests(status, expires_at);
