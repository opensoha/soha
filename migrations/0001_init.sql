-- Deprecated legacy entrypoint.
-- Canonical postgres migration moved to: migrations/postgres/0001_init.sql
-- Kept for backward compatibility with existing tooling/config.

CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    email TEXT NOT NULL UNIQUE,
    display_name TEXT,
    status TEXT NOT NULL DEFAULT 'active',
    tags JSONB NOT NULL DEFAULT '[]'::jsonb,
    preferences JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS teams (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    slug TEXT NOT NULL UNIQUE,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS projects (
    id TEXT PRIMARY KEY,
    team_id TEXT REFERENCES teams(id),
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    environment TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS roles (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    scope TEXT NOT NULL DEFAULT 'system',
    capabilities JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS user_role_bindings (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    scope JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, role_id)
);

CREATE TABLE IF NOT EXISTS user_team_bindings (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    team_id TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, team_id)
);

CREATE TABLE IF NOT EXISTS user_project_bindings (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, project_id)
);

CREATE TABLE IF NOT EXISTS user_password_credentials (
    user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    password_hash TEXT NOT NULL,
    password_updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS user_identities (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider_type TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    provider_user_id TEXT NOT NULL,
    profile JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_login_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider_type, provider_id, provider_user_id)
);

CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    refresh_token_id TEXT NOT NULL UNIQUE,
    provider_type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    expires_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNLOGGED TABLE IF NOT EXISTS auth_ephemeral_tokens (
    token TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS app_settings (
    setting_key TEXT PRIMARY KEY,
    category TEXT NOT NULL,
    value JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_by TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
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
    starts_at TIMESTAMPTZ,
    ends_at TIMESTAMPTZ,
    published_at TIMESTAMPTZ,
    created_by TEXT,
    updated_by TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS menus (
    id TEXT PRIMARY KEY,
    parent_id TEXT REFERENCES menus(id) ON DELETE CASCADE,
    path TEXT NOT NULL UNIQUE,
    label_zh TEXT NOT NULL,
    label_en TEXT NOT NULL,
    icon_key TEXT NOT NULL,
    section TEXT NOT NULL,
    sort_order INT NOT NULL DEFAULT 0,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS menu_role_bindings (
    id TEXT PRIMARY KEY,
    menu_id TEXT NOT NULL REFERENCES menus(id) ON DELETE CASCADE,
    role_id TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (menu_id, role_id)
);

CREATE TABLE IF NOT EXISTS policies (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    effect TEXT NOT NULL,
    priority INT NOT NULL DEFAULT 0,
    subjects JSONB NOT NULL DEFAULT '{}'::jsonb,
    targets JSONB NOT NULL DEFAULT '{}'::jsonb,
    actions JSONB NOT NULL DEFAULT '[]'::jsonb,
    conditions JSONB NOT NULL DEFAULT '{}'::jsonb,
    reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS policy_bindings (
    id TEXT PRIMARY KEY,
    policy_id TEXT NOT NULL REFERENCES policies(id),
    subject_type TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    scope JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS clusters (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    region TEXT,
    environment TEXT,
    labels JSONB NOT NULL DEFAULT '{}'::jsonb,
    connection_mode TEXT NOT NULL DEFAULT 'direct_kubeconfig',
    capabilities JSONB NOT NULL DEFAULT '[]'::jsonb,
    health_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE clusters ADD COLUMN IF NOT EXISTS version TEXT;
ALTER TABLE clusters ADD COLUMN IF NOT EXISTS connection_mode TEXT NOT NULL DEFAULT 'direct_kubeconfig';

CREATE TABLE IF NOT EXISTS cluster_credentials_meta (
    id TEXT PRIMARY KEY,
    cluster_id TEXT NOT NULL REFERENCES clusters(id),
    credential_type TEXT NOT NULL,
    source_type TEXT NOT NULL,
    source_ref TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS audit_logs (
    id TEXT PRIMARY KEY,
    actor_id TEXT NOT NULL,
    actor_name TEXT,
    roles JSONB NOT NULL DEFAULT '[]'::jsonb,
    teams JSONB NOT NULL DEFAULT '[]'::jsonb,
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
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'audit_logs' AND column_name = 'roles' AND udt_name = '_text'
    ) THEN
        ALTER TABLE audit_logs ALTER COLUMN roles DROP DEFAULT;
        ALTER TABLE audit_logs ALTER COLUMN roles TYPE JSONB USING to_jsonb(roles);
        ALTER TABLE audit_logs ALTER COLUMN roles SET DEFAULT '[]'::jsonb;
    END IF;
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'audit_logs' AND column_name = 'teams' AND udt_name = '_text'
    ) THEN
        ALTER TABLE audit_logs ALTER COLUMN teams DROP DEFAULT;
        ALTER TABLE audit_logs ALTER COLUMN teams TYPE JSONB USING to_jsonb(teams);
        ALTER TABLE audit_logs ALTER COLUMN teams SET DEFAULT '[]'::jsonb;
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS operation_logs (
    id TEXT PRIMARY KEY,
    actor_id TEXT NOT NULL,
    operation_type TEXT NOT NULL,
    target_scope JSONB NOT NULL DEFAULT '{}'::jsonb,
    result TEXT NOT NULL,
    summary TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS event_stream (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    category TEXT NOT NULL,
    severity TEXT NOT NULL,
    cluster_id TEXT,
    namespace TEXT,
    resource_ref JSONB NOT NULL DEFAULT '{}'::jsonb,
    summary TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    correlation_id TEXT,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS build_records (
    id TEXT PRIMARY KEY,
    project_id TEXT,
    source_system TEXT,
    status TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ai_sessions (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    created_by TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ai_messages (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES ai_sessions(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ai_inspection_tasks (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    scope_type TEXT NOT NULL DEFAULT 'platform',
    cluster_id TEXT,
    namespace TEXT,
    checks JSONB NOT NULL DEFAULT '[]'::jsonb,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    interval_minutes INT NOT NULL DEFAULT 0,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by TEXT NOT NULL,
    last_run_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ai_inspection_runs (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES ai_inspection_tasks(id) ON DELETE CASCADE,
    triggered_by TEXT NOT NULL,
    status TEXT NOT NULL,
    severity TEXT NOT NULL,
    summary TEXT NOT NULL,
    findings JSONB NOT NULL DEFAULT '[]'::jsonb,
    report JSONB NOT NULL DEFAULT '{}'::jsonb,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
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
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS deploy_records (
    id TEXT PRIMARY KEY,
    project_id TEXT,
    cluster_id TEXT,
    namespace TEXT,
    release_name TEXT,
    status TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    deployed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS notification_channels (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    channel_type TEXT NOT NULL,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
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
    labels JSONB NOT NULL DEFAULT '{}'::jsonb,
    annotations JSONB NOT NULL DEFAULT '{}'::jsonb,
    receiver TEXT,
    generator_url TEXT,
    starts_at TIMESTAMPTZ,
    ends_at TIMESTAMPTZ,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (source, fingerprint)
);

ALTER TABLE alert_instances ADD COLUMN IF NOT EXISTS owner_team TEXT;
ALTER TABLE alert_instances ADD COLUMN IF NOT EXISTS assignee TEXT;
ALTER TABLE alert_instances ADD COLUMN IF NOT EXISTS acknowledged_at TIMESTAMPTZ;
ALTER TABLE alert_instances ADD COLUMN IF NOT EXISTS acknowledged_by TEXT;
ALTER TABLE alert_instances ADD COLUMN IF NOT EXISTS acknowledged_by_name TEXT;

CREATE TABLE IF NOT EXISTS alert_routes (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    matchers JSONB NOT NULL DEFAULT '{}'::jsonb,
    channel_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS alert_delivery_logs (
    id TEXT PRIMARY KEY,
    alert_id TEXT NOT NULL,
    channel_id TEXT,
    status TEXT NOT NULL,
    summary TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS alert_silences (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    matchers JSONB NOT NULL DEFAULT '{}'::jsonb,
    reason TEXT,
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS saved_views (
    id TEXT PRIMARY KEY,
    owner_type TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    name TEXT NOT NULL,
    view_type TEXT NOT NULL,
    definition JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS workflow_runs (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL,
    workflow_name TEXT NOT NULL,
    cluster_id TEXT,
    namespace TEXT,
    deployment_name TEXT,
    status TEXT NOT NULL,
    steps JSONB NOT NULL DEFAULT '[]'::jsonb,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
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
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS business_lines (
    id TEXT PRIMARY KEY,
    business_key TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT,
    owners JSONB NOT NULL DEFAULT '[]'::jsonb,
    sort_order INT NOT NULL DEFAULT 0,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
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
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE applications ADD COLUMN IF NOT EXISTS business_line_id TEXT REFERENCES business_lines(id);

CREATE TABLE IF NOT EXISTS application_environments (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    environment_id TEXT NOT NULL REFERENCES delivery_environments(id) ON DELETE CASCADE,
    workflow_template_id TEXT,
    build_policy JSONB NOT NULL DEFAULT '{}'::jsonb,
    release_policy JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (application_id, environment_id)
);

CREATE TABLE IF NOT EXISTS workflow_templates (
    id TEXT PRIMARY KEY,
    template_key TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT,
    category TEXT,
    definition JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
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
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS scope_grants (
    id TEXT PRIMARY KEY,
    subject_type TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    business_line_id TEXT NOT NULL REFERENCES business_lines(id) ON DELETE CASCADE,
    environment_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    application_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    role TEXT NOT NULL,
    effect TEXT NOT NULL DEFAULT 'allow',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE alert_instances ADD COLUMN IF NOT EXISTS owner_team TEXT;
ALTER TABLE alert_instances ADD COLUMN IF NOT EXISTS assignee TEXT;
ALTER TABLE alert_instances ADD COLUMN IF NOT EXISTS acknowledged_at TIMESTAMPTZ;
ALTER TABLE alert_instances ADD COLUMN IF NOT EXISTS acknowledged_by TEXT;
ALTER TABLE alert_instances ADD COLUMN IF NOT EXISTS acknowledged_by_name TEXT;

CREATE TABLE IF NOT EXISTS user_preferences (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id),
    category TEXT NOT NULL,
    preferences JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, category)
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_actor_id ON audit_logs (actor_id);
CREATE INDEX IF NOT EXISTS idx_event_stream_occurred_at ON event_stream (occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_clusters_environment ON clusters (environment);
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_auth_ephemeral_tokens_kind_expires_at ON auth_ephemeral_tokens (kind, expires_at);
CREATE INDEX IF NOT EXISTS idx_alert_instances_status ON alert_instances (status);
CREATE INDEX IF NOT EXISTS idx_alert_instances_last_seen_at ON alert_instances (last_seen_at DESC);
CREATE INDEX IF NOT EXISTS idx_alert_instances_cluster_namespace ON alert_instances (cluster_id, namespace);
CREATE INDEX IF NOT EXISTS idx_alert_instances_acknowledged_at ON alert_instances (acknowledged_at DESC);
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
