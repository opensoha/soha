ALTER TABLE execution_tasks
    ADD COLUMN IF NOT EXISTS last_heartbeat_at TIMESTAMPTZ;

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
