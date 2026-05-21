ALTER TABLE virtualization_flavors
    DROP CONSTRAINT IF EXISTS virtualization_flavors_provider_connection_id_external_id_key;

ALTER TABLE virtualization_flavors
    ALTER COLUMN connection_id DROP NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_virtualization_flavors_connection_unique
    ON virtualization_flavors(provider, connection_id, external_id)
    WHERE connection_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_virtualization_flavors_global_unique
    ON virtualization_flavors(provider, external_id)
    WHERE connection_id IS NULL;

ALTER TABLE virtualization_tasks
    ADD COLUMN IF NOT EXISTS claimed_by_worker_id TEXT,
    ADD COLUMN IF NOT EXISTS attempt_count INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS max_retries INT NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS timeout_seconds INT NOT NULL DEFAULT 1800,
    ADD COLUMN IF NOT EXISTS last_heartbeat_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_virtualization_tasks_claim
    ON virtualization_tasks(status, created_at)
    WHERE status = 'queued';

CREATE INDEX IF NOT EXISTS idx_virtualization_tasks_timeout
    ON virtualization_tasks(status, last_heartbeat_at, started_at)
    WHERE status = 'running';
