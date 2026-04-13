CREATE TABLE IF NOT EXISTS ai_data_sources (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    source_type TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    credential_ref TEXT,
    scope JSON NOT NULL DEFAULT '{}',
    query_budget JSON NOT NULL DEFAULT '{}',
    redaction_policy JSON NOT NULL DEFAULT '{}',
    mcp_adapter TEXT NOT NULL,
    config JSON NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ai_analysis_profiles (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    mode TEXT NOT NULL,
    enabled_sources JSON NOT NULL DEFAULT '[]',
    enabled_playbooks JSON NOT NULL DEFAULT '[]',
    query_budgets JSON NOT NULL DEFAULT '{}',
    output_style JSON NOT NULL DEFAULT '{}',
    remediation_policy TEXT NOT NULL DEFAULT 'suggest_only',
    default_time_range_minutes INT NOT NULL DEFAULT 60,
    timeout_seconds INT NOT NULL DEFAULT 90,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ai_automation_policies (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    trigger_type TEXT NOT NULL,
    trigger_conditions JSON NOT NULL DEFAULT '{}',
    dedup_window_seconds INT NOT NULL DEFAULT 900,
    analysis_profile_id TEXT NOT NULL REFERENCES ai_analysis_profiles(id),
    remediation_policy TEXT NOT NULL DEFAULT 'suggest_only',
    approval_policy JSON NOT NULL DEFAULT '{}',
    cooldown_seconds INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

ALTER TABLE ai_root_cause_runs ADD COLUMN IF NOT EXISTS analysis_profile_id TEXT;
ALTER TABLE ai_root_cause_runs ADD COLUMN IF NOT EXISTS trigger_type TEXT NOT NULL DEFAULT 'manual';
ALTER TABLE ai_root_cause_runs ADD COLUMN IF NOT EXISTS data_source_snapshot JSON NOT NULL DEFAULT '{}';
ALTER TABLE ai_root_cause_runs ADD COLUMN IF NOT EXISTS playbook_results JSON NOT NULL DEFAULT '{}';
ALTER TABLE ai_root_cause_runs ADD COLUMN IF NOT EXISTS remediation_plan JSON NOT NULL DEFAULT '{}';
ALTER TABLE ai_root_cause_runs ADD COLUMN IF NOT EXISTS dedup_key TEXT;

CREATE INDEX IF NOT EXISTS idx_ai_data_sources_type_enabled ON ai_data_sources (source_type, enabled);
CREATE INDEX IF NOT EXISTS idx_ai_analysis_profiles_mode_enabled ON ai_analysis_profiles (mode, enabled);
CREATE INDEX IF NOT EXISTS idx_ai_automation_policies_trigger_enabled ON ai_automation_policies (trigger_type, enabled);
CREATE INDEX IF NOT EXISTS idx_ai_root_cause_runs_trigger_type_created_at ON ai_root_cause_runs (trigger_type, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_root_cause_runs_dedup_key ON ai_root_cause_runs (dedup_key);
