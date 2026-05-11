ALTER TABLE application_environments
    ADD COLUMN IF NOT EXISTS strategy_profile_id TEXT,
    ADD COLUMN IF NOT EXISTS promotion_policy_id TEXT,
    ADD COLUMN IF NOT EXISTS approval_policy_id TEXT,
    ADD COLUMN IF NOT EXISTS artifact_policy_id TEXT,
    ADD COLUMN IF NOT EXISTS resource_selector JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE release_targets
    ADD COLUMN IF NOT EXISTS target_kind TEXT NOT NULL DEFAULT 'k8s_workload',
    ADD COLUMN IF NOT EXISTS executor_kind TEXT NOT NULL DEFAULT 'k8s_job_runner',
    ADD COLUMN IF NOT EXISTS group_key TEXT,
    ADD COLUMN IF NOT EXISTS wave_key TEXT,
    ADD COLUMN IF NOT EXISTS region_key TEXT,
    ADD COLUMN IF NOT EXISTS config_ref TEXT,
    ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE IF NOT EXISTS release_bundles (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    application_environment_id TEXT REFERENCES application_environments(id) ON DELETE SET NULL,
    version TEXT NOT NULL,
    source_type TEXT NOT NULL,
    status TEXT NOT NULL,
    artifact_ref TEXT,
    artifact_digest TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_release_bundles_application_id ON release_bundles(application_id);
CREATE INDEX IF NOT EXISTS idx_release_bundles_application_environment_id ON release_bundles(application_environment_id);

CREATE TABLE IF NOT EXISTS execution_tasks (
    id TEXT PRIMARY KEY,
    release_bundle_id TEXT REFERENCES release_bundles(id) ON DELETE SET NULL,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    application_environment_id TEXT REFERENCES application_environments(id) ON DELETE SET NULL,
    task_kind TEXT NOT NULL,
    provider_kind TEXT NOT NULL,
    target_kind TEXT NOT NULL DEFAULT 'k8s_workload',
    status TEXT NOT NULL,
    queue_key TEXT,
    lock_key TEXT,
    max_retries INT NOT NULL DEFAULT 0,
    attempt_count INT NOT NULL DEFAULT 0,
    timeout_seconds INT NOT NULL DEFAULT 300,
    callback_token TEXT,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    result JSONB NOT NULL DEFAULT '{}'::jsonb,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_execution_tasks_release_bundle_id ON execution_tasks(release_bundle_id);
CREATE INDEX IF NOT EXISTS idx_execution_tasks_application_id ON execution_tasks(application_id);
CREATE INDEX IF NOT EXISTS idx_execution_tasks_application_environment_id ON execution_tasks(application_environment_id);
CREATE INDEX IF NOT EXISTS idx_execution_tasks_status ON execution_tasks(status);
CREATE UNIQUE INDEX IF NOT EXISTS idx_execution_tasks_callback_token ON execution_tasks(callback_token) WHERE callback_token IS NOT NULL;

CREATE TABLE IF NOT EXISTS execution_logs (
    id TEXT PRIMARY KEY,
    execution_task_id TEXT NOT NULL REFERENCES execution_tasks(id) ON DELETE CASCADE,
    log_level TEXT NOT NULL,
    message TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_execution_logs_execution_task_id ON execution_logs(execution_task_id);

CREATE TABLE IF NOT EXISTS execution_callbacks (
    id TEXT PRIMARY KEY,
    execution_task_id TEXT NOT NULL REFERENCES execution_tasks(id) ON DELETE CASCADE,
    provider_kind TEXT NOT NULL,
    status TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_execution_callbacks_execution_task_id ON execution_callbacks(execution_task_id);

CREATE TABLE IF NOT EXISTS approval_policies (
    id TEXT PRIMARY KEY,
    policy_key TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT,
    mode TEXT NOT NULL DEFAULT 'single',
    required_approvals INT NOT NULL DEFAULT 1,
    sla_minutes INT NOT NULL DEFAULT 60,
    approver_roles JSONB NOT NULL DEFAULT '[]'::jsonb,
    change_window JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
