CREATE TABLE IF NOT EXISTS secrets (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    scope_type TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    status TEXT NOT NULL,
    current_version INTEGER NOT NULL,
    bindings JSON NOT NULL DEFAULT '[]'::json,
    created_by TEXT NOT NULL,
    created_at TIMESTAMP WITHOUT TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITHOUT TIME ZONE NOT NULL,
    CONSTRAINT secrets_scope_type_check CHECK (scope_type IN ('workspace', 'project', 'environment')),
    CONSTRAINT secrets_status_check CHECK (status IN ('active', 'disabled')),
    CONSTRAINT secrets_current_version_check CHECK (current_version > 0),
    CONSTRAINT secrets_scope_name_key UNIQUE (scope_type, scope_id, name)
);

CREATE TABLE IF NOT EXISTS secret_versions (
    secret_id TEXT NOT NULL REFERENCES secrets(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    ciphertext TEXT NOT NULL,
    status TEXT NOT NULL,
    created_by TEXT NOT NULL,
    created_at TIMESTAMP WITHOUT TIME ZONE NOT NULL,
    revoked_at TIMESTAMP WITHOUT TIME ZONE,
    PRIMARY KEY (secret_id, version),
    CONSTRAINT secret_versions_version_check CHECK (version > 0),
    CONSTRAINT secret_versions_status_check CHECK (status IN ('active', 'revoked'))
);

ALTER TABLE ai_gateway_approval_requests ADD COLUMN IF NOT EXISTS secret_refs JSON NOT NULL DEFAULT '{}'::json;
ALTER TABLE execution_tasks ADD COLUMN IF NOT EXISTS secret_refs JSON NOT NULL DEFAULT '[]'::json;
ALTER TABLE execution_tasks ADD COLUMN IF NOT EXISTS secret_principal JSON NOT NULL DEFAULT '{}'::json;
ALTER TABLE execution_tasks ADD COLUMN IF NOT EXISTS secret_target JSON NOT NULL DEFAULT '{}'::json;
ALTER TABLE ai_agent_runs ADD COLUMN IF NOT EXISTS secret_refs JSON NOT NULL DEFAULT '[]'::json;
ALTER TABLE ai_agent_runs ADD COLUMN IF NOT EXISTS secret_principal JSON NOT NULL DEFAULT '{}'::json;
ALTER TABLE ai_agent_runs ADD COLUMN IF NOT EXISTS secret_target JSON NOT NULL DEFAULT '{}'::json;

CREATE TABLE IF NOT EXISTS secret_leases (
    id TEXT PRIMARY KEY,
    token_hash TEXT NOT NULL UNIQUE,
    agent_id TEXT NOT NULL,
    subject_type TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_ref TEXT NOT NULL,
    secret_refs JSON NOT NULL,
    principal JSON NOT NULL,
    expires_at TIMESTAMP WITHOUT TIME ZONE NOT NULL,
    redeemed_at TIMESTAMP WITHOUT TIME ZONE,
    revoked_at TIMESTAMP WITHOUT TIME ZONE,
    created_at TIMESTAMP WITHOUT TIME ZONE NOT NULL,
    CONSTRAINT secret_leases_subject_type_check CHECK (subject_type IN ('execution_task', 'agent_run'))
);

CREATE INDEX IF NOT EXISTS idx_secret_leases_subject ON secret_leases(subject_type, subject_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_secret_leases_expiry ON secret_leases(expires_at) WHERE redeemed_at IS NULL AND revoked_at IS NULL;
