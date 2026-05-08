CREATE TABLE IF NOT EXISTS alert_rules (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    rule_type TEXT NOT NULL,
    datasource_selector JSON NOT NULL DEFAULT '{}',
    query_spec JSON NOT NULL DEFAULT '{}',
    threshold_spec JSON NOT NULL DEFAULT '{}',
    for_seconds INT NOT NULL DEFAULT 0,
    group_by JSON NOT NULL DEFAULT '[]',
    labels JSON NOT NULL DEFAULT '{}',
    annotations JSON NOT NULL DEFAULT '{}',
    notification_policy_id TEXT,
    healing_policy_ids JSON NOT NULL DEFAULT '[]',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS alert_rule_targets (
    id TEXT PRIMARY KEY,
    rule_id TEXT NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
    datasource_id TEXT NOT NULL,
    target_kind TEXT NOT NULL,
    target_config JSON NOT NULL DEFAULT '{}',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS alert_rule_runs (
    id TEXT PRIMARY KEY,
    rule_id TEXT NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    summary TEXT,
    matched BOOLEAN NOT NULL DEFAULT FALSE,
    duration_ms INT NOT NULL DEFAULT 0,
    error TEXT,
    result JSON NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

ALTER TABLE alert_rule_runs
    ADD COLUMN IF NOT EXISTS matched BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS duration_ms INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS error TEXT;

CREATE TABLE IF NOT EXISTS alert_events (
    id TEXT PRIMARY KEY,
    rule_id TEXT,
    source_type TEXT NOT NULL,
    source_system TEXT,
    fingerprint TEXT NOT NULL,
    title TEXT NOT NULL,
    summary TEXT NOT NULL,
    severity TEXT NOT NULL,
    status TEXT NOT NULL,
    cluster_id TEXT,
    namespace TEXT,
    labels JSON NOT NULL DEFAULT '{}',
    annotations JSON NOT NULL DEFAULT '{}',
    receiver TEXT,
    generator_url TEXT,
    current_state TEXT,
    last_notification_at TIMESTAMP,
    starts_at TIMESTAMP,
    ends_at TIMESTAMP,
    last_seen_at TIMESTAMP NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS notification_policies (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    matchers JSON NOT NULL DEFAULT '{}',
    processor_chain JSON NOT NULL DEFAULT '[]',
    channel_refs JSON NOT NULL DEFAULT '[]',
    oncall_ref TEXT,
    send_resolved BOOLEAN NOT NULL DEFAULT FALSE,
    cooldown_seconds INT NOT NULL DEFAULT 0,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS notification_templates (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    template_type TEXT NOT NULL,
    content_type TEXT NOT NULL,
    body_template TEXT,
    headers JSON NOT NULL DEFAULT '{}',
    query_params JSON NOT NULL DEFAULT '{}',
    sample_payload JSON NOT NULL DEFAULT '{}',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS healing_policies (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    trigger_mode TEXT NOT NULL,
    workflow_template_id TEXT NOT NULL,
    approval_policy_ref TEXT,
    cooldown_seconds INT NOT NULL DEFAULT 0,
    concurrency_key TEXT,
    safety_window_seconds INT NOT NULL DEFAULT 0,
    definition JSON NOT NULL DEFAULT '{}',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS healing_runs (
    id TEXT PRIMARY KEY,
    policy_id TEXT NOT NULL REFERENCES healing_policies(id) ON DELETE CASCADE,
    event_id TEXT,
    status TEXT NOT NULL,
    approval_status TEXT,
    approval_comment TEXT,
    requested_by TEXT,
    approved_by TEXT,
    workflow_run_id TEXT,
    result JSON NOT NULL DEFAULT '{}',
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS oncall_schedules (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    time_zone TEXT,
    description TEXT,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS oncall_rotations (
    id TEXT PRIMARY KEY,
    schedule_id TEXT NOT NULL REFERENCES oncall_schedules(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    participants JSON NOT NULL DEFAULT '[]',
    rotation_config JSON NOT NULL DEFAULT '{}',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS oncall_escalation_policies (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    steps JSON NOT NULL DEFAULT '[]',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_alert_rules_enabled_updated_at ON alert_rules (enabled, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_alert_events_status_last_seen_at ON alert_events (status, last_seen_at DESC);
CREATE INDEX IF NOT EXISTS idx_notification_policies_enabled_updated_at ON notification_policies (enabled, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_notification_templates_enabled_updated_at ON notification_templates (enabled, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_healing_policies_enabled_updated_at ON healing_policies (enabled, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_healing_runs_status_updated_at ON healing_runs (status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_oncall_schedules_enabled_updated_at ON oncall_schedules (enabled, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_oncall_rotations_schedule_id_updated_at ON oncall_rotations (schedule_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_oncall_escalation_policies_enabled_updated_at ON oncall_escalation_policies (enabled, updated_at DESC);

INSERT INTO notification_policies (
    id,
    name,
    matchers,
    processor_chain,
    channel_refs,
    oncall_ref,
    send_resolved,
    cooldown_seconds,
    enabled,
    created_at,
    updated_at
)
SELECT
    route.id,
    route.name,
    route.matchers,
    '["webhook_update"]',
    route.channel_ids,
    NULL,
    FALSE,
    0,
    route.enabled,
    route.created_at,
    route.updated_at
FROM alert_routes AS route
WHERE NOT EXISTS (
    SELECT 1
    FROM notification_policies AS policy
    WHERE policy.id = route.id
);

ALTER TABLE execution_tasks
    ADD COLUMN IF NOT EXISTS claimed_by_agent_id TEXT,
    ADD COLUMN IF NOT EXISTS runtime_endpoint TEXT,
    ADD COLUMN IF NOT EXISTS runtime_cluster_id TEXT,
    ADD COLUMN IF NOT EXISTS stop_transport TEXT,
    ADD COLUMN IF NOT EXISTS last_runtime_seen_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS execution_artifacts (
    id TEXT PRIMARY KEY,
    execution_task_id TEXT NOT NULL REFERENCES execution_tasks(id) ON DELETE CASCADE,
    release_bundle_id TEXT REFERENCES release_bundles(id) ON DELETE SET NULL,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    application_environment_id TEXT REFERENCES application_environments(id) ON DELETE SET NULL,
    artifact_kind TEXT NOT NULL,
    name TEXT,
    ref TEXT,
    digest TEXT,
    path TEXT,
    status TEXT,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_execution_artifacts_task_id ON execution_artifacts(execution_task_id);
CREATE INDEX IF NOT EXISTS idx_execution_artifacts_bundle_id ON execution_artifacts(release_bundle_id);
CREATE INDEX IF NOT EXISTS idx_execution_artifacts_application_id ON execution_artifacts(application_id);
CREATE INDEX IF NOT EXISTS idx_execution_artifacts_application_environment_id ON execution_artifacts(application_environment_id);
