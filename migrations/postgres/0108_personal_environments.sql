-- Existing users remain unprovisioned. No provider calls or volume changes are
-- made by this migration. Only a matching owner can create the initial binding.
CREATE TABLE IF NOT EXISTS ai_personal_environments (
    assistant_id TEXT PRIMARY KEY REFERENCES ai_personal_assistants(id),
    created_by TEXT NOT NULL UNIQUE,
    template_id TEXT NOT NULL,
    agent_cluster_id TEXT NOT NULL,
    lease_seconds INTEGER NOT NULL CHECK (lease_seconds BETWEEN 60 AND 3600),
    lease JSONB NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('unprovisioned','pending','ready','sleeping','missing','expired','unknown','quota_exceeded')),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    data_initialized BOOLEAN NOT NULL DEFAULT FALSE,
    data_creation_attempted BOOLEAN NOT NULL DEFAULT FALSE,
    usage JSONB,
    observed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL,
    operation_id TEXT NOT NULL DEFAULT '',
    operation_until TIMESTAMPTZ,
    CHECK ((operation_id = '') = (operation_until IS NULL))
);
