-- No commands or personal environments are created by this migration.
ALTER TABLE ai_personal_environments ADD COLUMN IF NOT EXISTS active_task_id TEXT NOT NULL DEFAULT '';
ALTER TABLE ai_personal_environments DROP CONSTRAINT IF EXISTS ai_personal_environments_state_check;
ALTER TABLE ai_personal_environments ADD CONSTRAINT ai_personal_environments_state_check
    CHECK (state IN ('unprovisioned','pending','ready','sleeping','missing','expired','unknown','quota_exceeded','deleting','deletion_unknown','deleted'));
CREATE TABLE IF NOT EXISTS ai_personal_commands (
    id TEXT PRIMARY KEY,
    assistant_id TEXT NOT NULL REFERENCES ai_personal_environments(assistant_id),
    created_by TEXT NOT NULL,
    request_hash TEXT NOT NULL CHECK (request_hash ~ '^sha256:[a-f0-9]{64}$'),
    task_id TEXT NOT NULL,
    lease JSONB NOT NULL,
    env_operation_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('dispatching','succeeded','unknown','not_dispatched','cancelling','canceled','cancellation_unknown')),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    output TEXT NOT NULL DEFAULT '',
    output_digest TEXT NOT NULL DEFAULT '',
    observed_at TIMESTAMPTZ,
    operation_until TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS ai_personal_commands_owner ON ai_personal_commands(created_by,assistant_id,updated_at DESC);
