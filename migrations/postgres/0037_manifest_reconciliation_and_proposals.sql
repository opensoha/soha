ALTER TABLE manifest_deployment_status
    ADD COLUMN IF NOT EXISTS last_execution_task_id TEXT REFERENCES execution_tasks(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS drift JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE manifest_source_status
    ADD COLUMN IF NOT EXISTS last_tree_digest TEXT,
    ADD COLUMN IF NOT EXISTS last_canonical_digest TEXT;

ALTER TABLE manifest_sources
    ADD COLUMN IF NOT EXISTS auto_deploy BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE manifest_deployment_status
    ADD CONSTRAINT manifest_deployment_status_drift_object_check
    CHECK (jsonb_typeof(drift) = 'object');

CREATE TABLE manifest_operation_runs (
    id TEXT PRIMARY KEY,
    package_id TEXT NOT NULL REFERENCES manifest_packages(id) ON DELETE CASCADE,
    binding_id TEXT REFERENCES manifest_bindings(id) ON DELETE CASCADE,
    deployment_id TEXT REFERENCES manifest_deployments(id) ON DELETE CASCADE,
    generation BIGINT NOT NULL CHECK (generation >= 1),
    action TEXT NOT NULL CHECK (action IN ('preflight', 'apply', 'observe', 'repair', 'adopt', 'rollback')),
    idempotency_key TEXT NOT NULL,
    execution_task_id TEXT NOT NULL UNIQUE REFERENCES execution_tasks(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (idempotency_key)
);

CREATE INDEX idx_manifest_operation_runs_deployment
    ON manifest_operation_runs (deployment_id, generation, action, created_at DESC);

CREATE TABLE manifest_sync_runs (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL REFERENCES manifest_sources(id) ON DELETE CASCADE,
    package_id TEXT NOT NULL REFERENCES manifest_packages(id) ON DELETE CASCADE,
    execution_task_id TEXT NOT NULL UNIQUE REFERENCES execution_tasks(id) ON DELETE CASCADE,
    source_generation BIGINT NOT NULL CHECK (source_generation >= 1),
    trigger TEXT NOT NULL CHECK (trigger IN ('manual', 'webhook', 'poll')),
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'ignored')),
    idempotency_key TEXT NOT NULL UNIQUE,
    requested_commit TEXT,
    resolved_commit TEXT,
    tree_digest TEXT,
    canonical_digest TEXT,
    files JSONB NOT NULL DEFAULT '[]'::jsonb,
    revision INTEGER CHECK (revision IS NULL OR revision > 0),
    error_code TEXT,
    error_message TEXT,
    actor TEXT,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (jsonb_typeof(files) = 'array')
);

CREATE INDEX idx_manifest_sync_runs_source_created
    ON manifest_sync_runs (source_id, created_at DESC);

CREATE TABLE manifest_delivery_intents (
    id TEXT PRIMARY KEY,
    package_id TEXT NOT NULL REFERENCES manifest_packages(id) ON DELETE CASCADE,
    binding_id TEXT REFERENCES manifest_bindings(id) ON DELETE SET NULL,
    status TEXT NOT NULL CHECK (status IN ('draft', 'accepted', 'rejected')),
    files JSONB NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    prompt_template_version TEXT NOT NULL,
    request_id TEXT,
    evidence_digest TEXT NOT NULL,
    evidence_refs JSONB NOT NULL DEFAULT '[]'::jsonb,
    proposal_digest TEXT NOT NULL,
    rationale TEXT NOT NULL,
    risk TEXT NOT NULL,
    validation JSONB NOT NULL DEFAULT '{}'::jsonb,
    decision_comment TEXT,
    created_by TEXT NOT NULL,
    decided_by TEXT,
    decided_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (jsonb_typeof(files) = 'array'),
    CHECK (jsonb_typeof(evidence_refs) = 'array'),
    CHECK (jsonb_typeof(validation) = 'object')
);

CREATE INDEX idx_manifest_delivery_intents_package_created
    ON manifest_delivery_intents (package_id, created_at DESC);
