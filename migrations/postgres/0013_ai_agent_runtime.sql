CREATE TABLE IF NOT EXISTS ai_agent_runs (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    provider_kind TEXT NOT NULL,
    capability_id TEXT NOT NULL,
    skill_ids JSON NOT NULL DEFAULT '[]',
    session_id TEXT REFERENCES ai_sessions(id) ON DELETE SET NULL,
    root_cause_run_id TEXT REFERENCES ai_root_cause_runs(id) ON DELETE SET NULL,
    created_by TEXT NOT NULL,
    status TEXT NOT NULL,
    scope JSON NOT NULL DEFAULT '{}',
    toolset JSON NOT NULL DEFAULT '{}',
    tool_bindings JSON NOT NULL DEFAULT '[]',
    skill_bindings JSON NOT NULL DEFAULT '[]',
    input JSON NOT NULL DEFAULT '{}',
    output JSON NOT NULL DEFAULT '{}',
    tool_executions JSON NOT NULL DEFAULT '[]',
    analysis_artifacts JSON NOT NULL DEFAULT '[]',
    callback_token TEXT NOT NULL,
    claimed_by_agent_id TEXT,
    external_run_id TEXT,
    error_message TEXT,
    timeout_seconds INT NOT NULL DEFAULT 600,
    queued_at TIMESTAMP NOT NULL,
    started_at TIMESTAMP,
    last_heartbeat_at TIMESTAMP,
    completed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ai_agent_runs_status_created_at ON ai_agent_runs (status, created_at);
CREATE INDEX IF NOT EXISTS idx_ai_agent_runs_session_created_at ON ai_agent_runs (session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_agent_runs_root_cause_run ON ai_agent_runs (root_cause_run_id);
CREATE INDEX IF NOT EXISTS idx_ai_agent_runs_provider_status ON ai_agent_runs (provider_id, status);

ALTER TABLE ai_agent_runs ADD COLUMN IF NOT EXISTS tool_bindings JSON NOT NULL DEFAULT '[]';
ALTER TABLE ai_agent_runs ADD COLUMN IF NOT EXISTS skill_bindings JSON NOT NULL DEFAULT '[]';
