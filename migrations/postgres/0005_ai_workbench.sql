ALTER TABLE ai_sessions ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP;

ALTER TABLE ai_root_cause_runs ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'root_cause';
ALTER TABLE ai_root_cause_runs ADD COLUMN IF NOT EXISTS session_id TEXT;
ALTER TABLE ai_root_cause_runs ADD COLUMN IF NOT EXISTS tool_executions JSON NOT NULL DEFAULT '[]';

ALTER TABLE ai_automation_policies ADD COLUMN IF NOT EXISTS analysis_kinds JSON NOT NULL DEFAULT '["root_cause"]';

CREATE INDEX IF NOT EXISTS idx_ai_sessions_created_by_deleted_updated_at ON ai_sessions (created_by, deleted_at, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_root_cause_runs_session_id_created_at ON ai_root_cause_runs (session_id, created_at DESC);
