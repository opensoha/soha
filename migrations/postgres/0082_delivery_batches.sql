ALTER TABLE workflow_runs
    ADD COLUMN scope TEXT NOT NULL DEFAULT 'application' CHECK (scope IN ('application', 'delivery_batch')),
    ADD COLUMN delivery_batch_id TEXT,
    ADD COLUMN version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    ADD COLUMN lease_owner TEXT NOT NULL DEFAULT '',
    ADD COLUMN lease_until TIMESTAMPTZ,
    ADD COLUMN fencing_token BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN stop_reason TEXT NOT NULL DEFAULT '' CHECK (stop_reason IN ('', 'user', 'failure')),
    ADD COLUMN stop_summary TEXT NOT NULL DEFAULT '',
    ADD CONSTRAINT workflow_run_scope CHECK (
        (scope = 'application' AND delivery_batch_id IS NULL) OR
        (scope = 'delivery_batch' AND application_id = '' AND delivery_batch_id IS NOT NULL)
    );

CREATE UNIQUE INDEX workflow_runs_delivery_batch ON workflow_runs (delivery_batch_id) WHERE delivery_batch_id IS NOT NULL;
CREATE INDEX workflow_runs_delivery_dispatch ON workflow_runs (lease_until, updated_at)
    WHERE scope = 'delivery_batch' AND status NOT IN ('completed', 'partially_completed', 'failed', 'canceled');

CREATE TABLE delivery_workflows (
    id TEXT PRIMARY KEY,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    definition JSONB NOT NULL,
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE delivery_batches (
    id TEXT PRIMARY KEY,
    root_run_id TEXT NOT NULL UNIQUE REFERENCES workflow_runs(id),
    created_by TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_digest TEXT NOT NULL CHECK (request_digest ~ '^sha256:[a-f0-9]{64}$'),
    snapshot JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (created_by, idempotency_key)
);
CREATE INDEX delivery_batches_created ON delivery_batches (created_at DESC);
CREATE INDEX delivery_batches_targets ON delivery_batches USING GIN ((snapshot->'targets'));
