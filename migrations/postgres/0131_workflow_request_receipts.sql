-- Additive receipt storage: retain tombstones when application/run data is removed.
-- Rollback keeps this table; older Core versions do not expose the new request paths.
CREATE TABLE IF NOT EXISTS workflow_request_receipts (
    id uuid PRIMARY KEY,
    actor_id text NOT NULL,
    request_key text NOT NULL,
    input_digest text NOT NULL,
    application_id text NOT NULL,
    binding_id text NOT NULL DEFAULT '',
    run_id text NOT NULL UNIQUE,
    status text NOT NULL DEFAULT 'unknown' CHECK (status IN ('unknown', 'accepted', 'rejected')),
    error_code text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    UNIQUE (actor_id, request_key),
    CHECK (length(request_key) BETWEEN 8 AND 128),
    CHECK (request_key ~ '^[A-Za-z0-9._:-]+$'),
    CHECK (length(input_digest) = 64)
);
