CREATE TABLE IF NOT EXISTS ai_provider_rollouts (
    id VARCHAR(128) PRIMARY KEY, status VARCHAR(32) NOT NULL, payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ai_provider_rollouts_status ON ai_provider_rollouts(status, updated_at);

CREATE TABLE IF NOT EXISTS ai_provider_conformance_runs (
    id VARCHAR(128) PRIMARY KEY, status VARCHAR(32) NOT NULL, payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS ai_environment_templates (
    id VARCHAR(128) PRIMARY KEY, status VARCHAR(32) NOT NULL, payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS ai_environment_leases (
    id VARCHAR(128) PRIMARY KEY, template_id VARCHAR(128) NOT NULL, status VARCHAR(32) NOT NULL,
    expires_at TIMESTAMPTZ, payload JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ai_environment_leases_gc ON ai_environment_leases(status, expires_at);

CREATE TABLE IF NOT EXISTS ai_production_operations (
    id VARCHAR(128) PRIMARY KEY, kind VARCHAR(32) NOT NULL, category VARCHAR(32) NOT NULL,
    status VARCHAR(32) NOT NULL, payload JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS ai_runbook_evidence (
    id VARCHAR(128) PRIMARY KEY, runbook_id VARCHAR(128) NOT NULL, operation_id VARCHAR(128) NOT NULL,
    status VARCHAR(32) NOT NULL, payload JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ai_runbook_evidence_runbook ON ai_runbook_evidence(runbook_id, created_at);
