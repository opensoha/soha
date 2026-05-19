-- Consolidated PostgreSQL bootstrap migration.
-- This file contains the full current schema baseline for fresh kubecrux databases.
-- It supersedes the previously split 0002-0007 postgres migration files.

-- Schema only. The bootstrap account is seeded by backend startup from auth.dev_principal
-- and the repository baseline is admin / kubecrux with no legacy migration path.

CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    email TEXT NOT NULL UNIQUE,
    display_name TEXT,
    status TEXT NOT NULL DEFAULT 'active',
    tags JSON NOT NULL DEFAULT '[]',
    preferences JSON NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS teams (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    slug TEXT NOT NULL UNIQUE,
    metadata JSON NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS projects (
    id TEXT PRIMARY KEY,
    team_id TEXT REFERENCES teams(id),
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    environment TEXT,
    metadata JSON NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS roles (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    scope TEXT NOT NULL DEFAULT 'system',
    capabilities JSON NOT NULL DEFAULT '[]',
    permission_keys JSON NOT NULL DEFAULT '[]',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS user_role_bindings (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    scope JSON NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, role_id)
);

CREATE TABLE IF NOT EXISTS user_team_bindings (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    team_id TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, team_id)
);

CREATE TABLE IF NOT EXISTS user_project_bindings (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, project_id)
);

CREATE TABLE IF NOT EXISTS user_password_credentials (
    user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    password_hash TEXT NOT NULL,
    password_updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS user_identities (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider_type TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    provider_user_id TEXT NOT NULL,
    profile JSON NOT NULL DEFAULT '{}',
    last_login_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE (provider_type, provider_id, provider_user_id)
);

CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    refresh_token_id TEXT NOT NULL UNIQUE,
    provider_type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    expires_at TIMESTAMP NOT NULL,
    last_seen_at TIMESTAMP,
    metadata JSON NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS auth_ephemeral_tokens (
    token TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    payload JSON NOT NULL DEFAULT '{}',
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS app_settings (
    setting_key TEXT PRIMARY KEY,
    category TEXT NOT NULL,
    value JSON NOT NULL DEFAULT '{}',
    updated_by TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS announcements (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    summary TEXT,
    content TEXT NOT NULL,
    level TEXT NOT NULL DEFAULT 'info',
    status TEXT NOT NULL DEFAULT 'draft',
    audience TEXT NOT NULL DEFAULT 'all',
    sticky BOOLEAN NOT NULL DEFAULT FALSE,
    starts_at TIMESTAMP,
    ends_at TIMESTAMP,
    published_at TIMESTAMP,
    created_by TEXT,
    updated_by TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS announcement_receipts (
    id TEXT PRIMARY KEY,
    announcement_id TEXT NOT NULL REFERENCES announcements(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    read_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE (announcement_id, user_id)
);

CREATE TABLE IF NOT EXISTS menus (
    id TEXT PRIMARY KEY,
    parent_id TEXT REFERENCES menus(id) ON DELETE CASCADE,
    path TEXT NOT NULL UNIQUE,
    label_zh TEXT NOT NULL,
    label_en TEXT NOT NULL,
    icon_key TEXT NOT NULL,
    section TEXT,
    sort_order INT NOT NULL DEFAULT 0,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS menu_role_bindings (
    id TEXT PRIMARY KEY,
    menu_id TEXT NOT NULL REFERENCES menus(id) ON DELETE CASCADE,
    role_id TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE (menu_id, role_id)
);

CREATE TABLE IF NOT EXISTS policies (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    effect TEXT NOT NULL,
    priority INT NOT NULL DEFAULT 0,
    subjects JSON NOT NULL DEFAULT '{}',
    targets JSON NOT NULL DEFAULT '{}',
    actions JSON NOT NULL DEFAULT '[]',
    conditions JSON NOT NULL DEFAULT '{}',
    reason TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS policy_bindings (
    id TEXT PRIMARY KEY,
    policy_id TEXT NOT NULL REFERENCES policies(id),
    subject_type TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    scope JSON NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS clusters (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    region TEXT,
    environment TEXT,
    labels JSON NOT NULL DEFAULT '{}',
    connection_mode TEXT NOT NULL DEFAULT 'direct_kubeconfig',
    capabilities JSON NOT NULL DEFAULT '[]',
    health_snapshot JSON NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

ALTER TABLE clusters ADD COLUMN IF NOT EXISTS version TEXT;

CREATE TABLE IF NOT EXISTS cluster_credentials_meta (
    id TEXT PRIMARY KEY,
    cluster_id TEXT NOT NULL REFERENCES clusters(id),
    credential_type TEXT NOT NULL,
    source_type TEXT NOT NULL,
    source_ref TEXT,
    metadata JSON NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS audit_logs (
    id TEXT PRIMARY KEY,
    actor_id TEXT NOT NULL,
    actor_name TEXT,
    roles JSON NOT NULL DEFAULT '[]',
    teams JSON NOT NULL DEFAULT '[]',
    cluster_id TEXT,
    namespace TEXT,
    resource_kind TEXT,
    resource_name TEXT,
    action TEXT NOT NULL,
    result TEXT NOT NULL,
    summary TEXT NOT NULL,
    request_path TEXT,
    request_method TEXT,
    request_id TEXT,
    source_ip TEXT,
    metadata JSON NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'audit_logs' AND column_name = 'roles' AND udt_name = '_text'
    ) THEN
        ALTER TABLE audit_logs ALTER COLUMN roles DROP DEFAULT;
        ALTER TABLE audit_logs ALTER COLUMN roles TYPE JSON USING to_json(roles);
        ALTER TABLE audit_logs ALTER COLUMN roles SET DEFAULT '[]';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'audit_logs' AND column_name = 'teams' AND udt_name = '_text'
    ) THEN
        ALTER TABLE audit_logs ALTER COLUMN teams DROP DEFAULT;
        ALTER TABLE audit_logs ALTER COLUMN teams TYPE JSON USING to_json(teams);
        ALTER TABLE audit_logs ALTER COLUMN teams SET DEFAULT '[]';
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS operation_logs (
    id TEXT PRIMARY KEY,
    actor_id TEXT NOT NULL,
    actor_name TEXT,
    operation_type TEXT NOT NULL,
    target_scope JSON NOT NULL DEFAULT '{}',
    result TEXT NOT NULL,
    summary TEXT,
    request_path TEXT,
    request_method TEXT,
    request_id TEXT,
    source_ip TEXT,
    metadata JSON NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS event_stream (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    category TEXT NOT NULL,
    severity TEXT NOT NULL,
    cluster_id TEXT,
    namespace TEXT,
    resource_ref JSON NOT NULL DEFAULT '{}',
    summary TEXT NOT NULL,
    payload JSON NOT NULL DEFAULT '{}',
    correlation_id TEXT,
    occurred_at TIMESTAMP NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS build_records (
    id TEXT PRIMARY KEY,
    project_id TEXT,
    source_system TEXT,
    status TEXT NOT NULL,
    metadata JSON NOT NULL DEFAULT '{}',
    started_at TIMESTAMP,
    finished_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ai_sessions (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    created_by TEXT NOT NULL,
    metadata JSON NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ai_messages (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES ai_sessions(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    metadata JSON NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ai_inspection_tasks (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    scope_type TEXT NOT NULL DEFAULT 'platform',
    cluster_id TEXT,
    namespace TEXT,
    checks JSON NOT NULL DEFAULT '[]',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    interval_minutes INT NOT NULL DEFAULT 0,
    metadata JSON NOT NULL DEFAULT '{}',
    created_by TEXT NOT NULL,
    last_run_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ai_inspection_runs (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES ai_inspection_tasks(id) ON DELETE CASCADE,
    triggered_by TEXT NOT NULL,
    status TEXT NOT NULL,
    severity TEXT NOT NULL,
    summary TEXT NOT NULL,
    findings JSON NOT NULL DEFAULT '[]',
    report JSON NOT NULL DEFAULT '{}',
    started_at TIMESTAMP NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS applications (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    app_key TEXT NOT NULL UNIQUE,
    app_group TEXT NOT NULL,
    language TEXT NOT NULL,
    description TEXT,
    owner_team TEXT,
    repository_provider TEXT,
    repository_project_id TEXT,
    repository_path TEXT,
    default_branch TEXT,
    default_tag TEXT,
    build_image TEXT,
    build_context_dir TEXT,
    dockerfile_path TEXT,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    metadata JSON NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS deploy_records (
    id TEXT PRIMARY KEY,
    project_id TEXT,
    cluster_id TEXT,
    namespace TEXT,
    release_name TEXT,
    status TEXT NOT NULL,
    metadata JSON NOT NULL DEFAULT '{}',
    deployed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS notification_channels (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    channel_type TEXT NOT NULL,
    config JSON NOT NULL DEFAULT '{}',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS alert_instances (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
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
    starts_at TIMESTAMP,
    ends_at TIMESTAMP,
    last_seen_at TIMESTAMP NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE (source, fingerprint)
);

ALTER TABLE alert_instances ADD COLUMN IF NOT EXISTS owner_team TEXT;
ALTER TABLE alert_instances ADD COLUMN IF NOT EXISTS assignee TEXT;
ALTER TABLE alert_instances ADD COLUMN IF NOT EXISTS acknowledged_at TIMESTAMP;
ALTER TABLE alert_instances ADD COLUMN IF NOT EXISTS acknowledged_by TEXT;
ALTER TABLE alert_instances ADD COLUMN IF NOT EXISTS acknowledged_by_name TEXT;

CREATE TABLE IF NOT EXISTS alert_routes (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    matchers JSON NOT NULL DEFAULT '{}',
    channel_ids JSON NOT NULL DEFAULT '[]',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS alert_delivery_logs (
    id TEXT PRIMARY KEY,
    alert_id TEXT NOT NULL,
    channel_id TEXT,
    status TEXT NOT NULL,
    summary TEXT,
    metadata JSON NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS alert_silences (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    matchers JSON NOT NULL DEFAULT '{}',
    reason TEXT,
    starts_at TIMESTAMP NOT NULL,
    ends_at TIMESTAMP NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

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
    result JSON NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

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

CREATE TABLE IF NOT EXISTS oncall_assignment_rules (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    integration_id TEXT,
    integration_type TEXT,
    business_line_id TEXT,
    alert_category TEXT,
    alert_name TEXT,
    severity TEXT,
    service TEXT,
    role TEXT,
    matchers JSON NOT NULL DEFAULT '{}',
    target_type TEXT NOT NULL,
    target_ref TEXT NOT NULL,
    route_order INT NOT NULL DEFAULT 100,
    group_by JSON NOT NULL DEFAULT '[]',
    priority INT NOT NULL DEFAULT 100,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS saved_views (
    id TEXT PRIMARY KEY,
    owner_type TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    name TEXT NOT NULL,
    view_type TEXT NOT NULL,
    definition JSON NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS workflow_runs (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL,
    workflow_name TEXT NOT NULL,
    cluster_id TEXT,
    namespace TEXT,
    deployment_name TEXT,
    status TEXT NOT NULL,
    steps JSON NOT NULL DEFAULT '[]',
    metadata JSON NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS registry_connections (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    registry_type TEXT NOT NULL,
    endpoint TEXT NOT NULL,
    namespace TEXT,
    username TEXT,
    secret TEXT,
    insecure BOOLEAN NOT NULL DEFAULT FALSE,
    metadata JSON NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS business_lines (
    id TEXT PRIMARY KEY,
    business_key TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT,
    owners JSON NOT NULL DEFAULT '[]',
    sort_order INT NOT NULL DEFAULT 0,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS delivery_environments (
    id TEXT PRIMARY KEY,
    environment_key TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    tier TEXT,
    stage_level INT NOT NULL DEFAULT 0,
    sort_order INT NOT NULL DEFAULT 0,
    is_production BOOLEAN NOT NULL DEFAULT FALSE,
    requires_approval BOOLEAN NOT NULL DEFAULT FALSE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

ALTER TABLE applications ADD COLUMN IF NOT EXISTS business_line_id TEXT REFERENCES business_lines(id);

CREATE TABLE IF NOT EXISTS application_environments (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    environment_id TEXT NOT NULL REFERENCES delivery_environments(id) ON DELETE CASCADE,
    workflow_template_id TEXT,
    build_policy JSON NOT NULL DEFAULT '{}',
    release_policy JSON NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE (application_id, environment_id)
);

CREATE TABLE IF NOT EXISTS workflow_templates (
    id TEXT PRIMARY KEY,
    template_key TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT,
    category TEXT,
    definition JSON NOT NULL DEFAULT '{}',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS release_targets (
    id TEXT PRIMARY KEY,
    application_environment_id TEXT NOT NULL REFERENCES application_environments(id) ON DELETE CASCADE,
    cluster_id TEXT NOT NULL REFERENCES clusters(id),
    namespace TEXT NOT NULL,
    workload_kind TEXT NOT NULL,
    workload_name TEXT NOT NULL,
    container_name TEXT,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS scope_grants (
    id TEXT PRIMARY KEY,
    subject_type TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    business_line_id TEXT NOT NULL REFERENCES business_lines(id) ON DELETE CASCADE,
    environment_ids JSON NOT NULL DEFAULT '[]',
    application_ids JSON NOT NULL DEFAULT '[]',
    role TEXT NOT NULL,
    effect TEXT NOT NULL DEFAULT 'allow',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS user_preferences (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id),
    category TEXT NOT NULL,
    preferences JSON NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, category)
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_actor_id ON audit_logs (actor_id);
CREATE INDEX IF NOT EXISTS idx_announcement_receipts_announcement_id ON announcement_receipts (announcement_id);
CREATE INDEX IF NOT EXISTS idx_announcement_receipts_user_id ON announcement_receipts (user_id);
CREATE INDEX IF NOT EXISTS idx_event_stream_occurred_at ON event_stream (occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_clusters_environment ON clusters (environment);
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_auth_ephemeral_tokens_kind_expires_at ON auth_ephemeral_tokens (kind, expires_at);
CREATE INDEX IF NOT EXISTS idx_alert_instances_status ON alert_instances (status);
CREATE INDEX IF NOT EXISTS idx_alert_instances_last_seen_at ON alert_instances (last_seen_at DESC);
CREATE INDEX IF NOT EXISTS idx_alert_instances_cluster_namespace ON alert_instances (cluster_id, namespace);
CREATE INDEX IF NOT EXISTS idx_alert_instances_acknowledged_at ON alert_instances (acknowledged_at DESC);
CREATE INDEX IF NOT EXISTS idx_alert_rules_enabled_updated_at ON alert_rules (enabled, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_alert_events_status_last_seen_at ON alert_events (status, last_seen_at DESC);
CREATE INDEX IF NOT EXISTS idx_notification_policies_enabled_updated_at ON notification_policies (enabled, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_notification_templates_enabled_updated_at ON notification_templates (enabled, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_healing_policies_enabled_updated_at ON healing_policies (enabled, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_healing_runs_status_updated_at ON healing_runs (status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_oncall_schedules_enabled_updated_at ON oncall_schedules (enabled, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_oncall_rotations_schedule_id_updated_at ON oncall_rotations (schedule_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_oncall_escalation_policies_enabled_updated_at ON oncall_escalation_policies (enabled, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_oncall_assignment_rules_business_role ON oncall_assignment_rules (business_line_id, role, enabled, priority DESC);
CREATE INDEX IF NOT EXISTS idx_oncall_assignment_rules_alert_scope ON oncall_assignment_rules (alert_category, severity, service, enabled, priority DESC);
CREATE INDEX IF NOT EXISTS idx_oncall_assignment_rules_integration_route ON oncall_assignment_rules (integration_type, integration_id, enabled, route_order ASC);
CREATE INDEX IF NOT EXISTS idx_alert_delivery_logs_alert_id_created_at ON alert_delivery_logs (alert_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_alert_delivery_logs_status_created_at ON alert_delivery_logs (status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_alert_silences_enabled_time ON alert_silences (enabled, starts_at, ends_at);
CREATE INDEX IF NOT EXISTS idx_applications_group_enabled ON applications (app_group, enabled);
CREATE INDEX IF NOT EXISTS idx_workflow_runs_application_created_at ON workflow_runs (application_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_registry_connections_type_name ON registry_connections (registry_type, name);
CREATE INDEX IF NOT EXISTS idx_build_records_project_created_at ON build_records (project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_messages_session_created_at ON ai_messages (session_id, created_at ASC);
CREATE INDEX IF NOT EXISTS idx_ai_inspection_tasks_created_by ON ai_inspection_tasks (created_by);
CREATE INDEX IF NOT EXISTS idx_ai_inspection_tasks_enabled_last_run_at ON ai_inspection_tasks (enabled, last_run_at);
CREATE INDEX IF NOT EXISTS idx_ai_inspection_runs_task_created_at ON ai_inspection_runs (task_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_user_role_bindings_user_id ON user_role_bindings (user_id);
CREATE INDEX IF NOT EXISTS idx_business_lines_key_enabled ON business_lines (business_key, enabled);
CREATE INDEX IF NOT EXISTS idx_delivery_environments_key_enabled ON delivery_environments (environment_key, enabled);
CREATE INDEX IF NOT EXISTS idx_applications_business_line_id ON applications (business_line_id);
CREATE INDEX IF NOT EXISTS idx_application_environments_application_id ON application_environments (application_id);
CREATE INDEX IF NOT EXISTS idx_application_environments_environment_id ON application_environments (environment_id);
CREATE INDEX IF NOT EXISTS idx_release_targets_application_environment_id ON release_targets (application_environment_id);
CREATE INDEX IF NOT EXISTS idx_release_targets_cluster_namespace_workload ON release_targets (cluster_id, namespace, workload_kind, workload_name);
CREATE INDEX IF NOT EXISTS idx_scope_grants_subject ON scope_grants (subject_type, subject_id);
CREATE INDEX IF NOT EXISTS idx_scope_grants_business_line_role ON scope_grants (business_line_id, role);
CREATE INDEX IF NOT EXISTS idx_workflow_templates_key_enabled ON workflow_templates (template_key, enabled);

CREATE TABLE IF NOT EXISTS ai_root_cause_runs (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    created_by TEXT NOT NULL,
    status TEXT NOT NULL,
    severity TEXT NOT NULL,
    summary TEXT NOT NULL,
    cluster_id TEXT,
    namespace TEXT,
    workload_kind TEXT,
    workload_name TEXT,
    alert_id TEXT,
    time_range_minutes INT NOT NULL DEFAULT 60,
    question TEXT,
    evidence JSON NOT NULL DEFAULT '[]',
    hypotheses JSON NOT NULL DEFAULT '[]',
    recommendations JSON NOT NULL DEFAULT '[]',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ai_root_cause_runs_created_by_updated_at ON ai_root_cause_runs (created_by, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_root_cause_runs_cluster_namespace ON ai_root_cause_runs (cluster_id, namespace);
CREATE INDEX IF NOT EXISTS idx_ai_root_cause_runs_alert_id ON ai_root_cause_runs (alert_id);


-- Consolidated from migrations/postgres/0002_ai_control_plane.sql

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

-- Consolidated from migrations/postgres/0003_ai_datasource_abstraction.sql

ALTER TABLE ai_data_sources ADD COLUMN IF NOT EXISTS source_kind TEXT;
ALTER TABLE ai_data_sources ADD COLUMN IF NOT EXISTS backend_type TEXT;

UPDATE ai_data_sources
SET source_kind = CASE
    WHEN source_kind IS NOT NULL AND source_kind <> '' THEN source_kind
    WHEN mcp_adapter = 'logs.v1' THEN 'logs'
    WHEN mcp_adapter = 'metrics.v1' THEN 'metrics'
    WHEN mcp_adapter = 'traces.v1' THEN 'traces'
    ELSE source_type
END
WHERE source_kind IS NULL OR source_kind = '';

UPDATE ai_data_sources
SET backend_type = CASE
    WHEN backend_type IS NOT NULL AND backend_type <> '' THEN backend_type
    WHEN source_type = 'es-logs' THEN 'es'
    WHEN source_type = 'loki-logs' THEN 'loki'
    WHEN source_type = 'clickhouse-logs' THEN 'clickhouse'
    WHEN source_type = 'prometheus' THEN 'prometheus'
    WHEN source_type = 'jaeger-traces' THEN 'jaeger'
    WHEN source_type = 'platform-native' THEN 'platform'
    WHEN source_type = 'alert-center' THEN 'platform'
    WHEN source_type = 'release-records' THEN 'platform'
    ELSE source_type
END
WHERE backend_type IS NULL OR backend_type = '';

UPDATE ai_data_sources
SET mcp_adapter = CASE
    WHEN source_kind = 'logs' THEN 'logs.v1'
    WHEN source_kind = 'metrics' THEN 'metrics.v1'
    WHEN source_kind = 'traces' THEN 'traces.v1'
    ELSE 'platform-native.v1'
END
WHERE mcp_adapter NOT IN ('logs.v1', 'metrics.v1', 'traces.v1', 'platform-native.v1');

CREATE INDEX IF NOT EXISTS idx_ai_data_sources_kind_backend_enabled ON ai_data_sources (source_kind, backend_type, enabled);


-- Consolidated from migrations/postgres/0004_ai_datasource_validation.sql

ALTER TABLE ai_data_sources ADD COLUMN IF NOT EXISTS validation_status TEXT;
ALTER TABLE ai_data_sources ADD COLUMN IF NOT EXISTS validation_message TEXT;
ALTER TABLE ai_data_sources ADD COLUMN IF NOT EXISTS last_validated_at TIMESTAMP;

CREATE INDEX IF NOT EXISTS idx_ai_data_sources_validation_status ON ai_data_sources (validation_status, last_validated_at DESC);


-- Consolidated from migrations/postgres/0005_ai_workbench.sql

ALTER TABLE ai_sessions ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP;

ALTER TABLE ai_root_cause_runs ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'root_cause';
ALTER TABLE ai_root_cause_runs ADD COLUMN IF NOT EXISTS session_id TEXT;
ALTER TABLE ai_root_cause_runs ADD COLUMN IF NOT EXISTS tool_executions JSON NOT NULL DEFAULT '[]';

ALTER TABLE ai_automation_policies ADD COLUMN IF NOT EXISTS analysis_kinds JSON NOT NULL DEFAULT '["root_cause"]';

CREATE INDEX IF NOT EXISTS idx_ai_sessions_created_by_deleted_updated_at ON ai_sessions (created_by, deleted_at, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_root_cause_runs_session_id_created_at ON ai_root_cause_runs (session_id, created_at DESC);


-- Consolidated from migrations/postgres/0005_port_forward_sessions.sql

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

-- Consolidated from migrations/postgres/0007_delivery_orchestration.sql

ALTER TABLE build_records ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE TABLE IF NOT EXISTS application_build_sources (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    source_name TEXT NOT NULL,
    source_type TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    build_image TEXT,
    default_tag TEXT,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_application_build_sources_application_id ON application_build_sources(application_id);

INSERT INTO application_build_sources (
    id,
    application_id,
    source_name,
    source_type,
    enabled,
    is_default,
    build_image,
    default_tag,
    config,
    created_at,
    updated_at
)
SELECT
    'default:' || a.id,
    a.id,
    'Repository Dockerfile',
    'repo_dockerfile',
    a.enabled,
    TRUE,
    a.build_image,
    a.default_tag,
    jsonb_build_object(
        'contextDir', COALESCE(NULLIF(a.build_context_dir, ''), '.'),
        'dockerfilePath', COALESCE(NULLIF(a.dockerfile_path, ''), 'Dockerfile'),
        'builderKind', 'docker'
    ),
    a.created_at,
    a.updated_at
FROM applications a
WHERE NOT EXISTS (
    SELECT 1 FROM application_build_sources s WHERE s.application_id = a.id
);

CREATE TABLE IF NOT EXISTS build_templates (
    id TEXT PRIMARY KEY,
    template_key TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT,
    builder_kind TEXT NOT NULL DEFAULT 'custom',
    dockerfile_template TEXT,
    build_commands JSONB NOT NULL DEFAULT '[]'::jsonb,
    variable_schema JSONB NOT NULL DEFAULT '{}'::jsonb,
    default_variables JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS workflow_approvals (
    id TEXT PRIMARY KEY,
    workflow_run_id TEXT NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    node_id TEXT NOT NULL,
    action TEXT NOT NULL,
    comment TEXT,
    actor_id TEXT NOT NULL,
    actor_name TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_workflow_approvals_workflow_run_id ON workflow_approvals(workflow_run_id);

-- Consolidated from migrations/postgres/0008_delivery_blueprints.sql

CREATE TABLE IF NOT EXISTS delivery_blueprints (
    id TEXT PRIMARY KEY,
    blueprint_key TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT,
    application_draft JSONB NOT NULL DEFAULT '{}'::jsonb,
    build_sources JSONB NOT NULL DEFAULT '[]'::jsonb,
    environment_bindings JSONB NOT NULL DEFAULT '[]'::jsonb,
    file_templates JSONB NOT NULL DEFAULT '[]'::jsonb,
    execution_hints JSONB NOT NULL DEFAULT '{}'::jsonb,
    post_create_actions JSONB NOT NULL DEFAULT '[]'::jsonb,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_delivery_blueprints_enabled_updated_at ON delivery_blueprints(enabled, updated_at DESC);
