-- All Core workers must be upgraded before using the new request/dispatch
-- fence. Keep receipts and these columns on rollback; unknown is not replayable.
ALTER TABLE healing_runs ADD COLUMN IF NOT EXISTS execution_generation bigint NOT NULL DEFAULT 1 CHECK (execution_generation >= 1);
ALTER TABLE healing_runs ADD COLUMN IF NOT EXISTS submission_status text NOT NULL DEFAULT 'not_submitted' CHECK (submission_status IN ('not_submitted','unknown','accepted'));
ALTER TABLE healing_runs ADD COLUMN IF NOT EXISTS intent_digest text NOT NULL DEFAULT '' CHECK (intent_digest = '' OR intent_digest ~ '^sha256:[a-f0-9]{64}$');

CREATE TABLE IF NOT EXISTS healing_request_receipts (
    id uuid PRIMARY KEY,
    actor_id text NOT NULL,
    credential_hash text NOT NULL CHECK (credential_hash ~ '^sha256:[a-f0-9]{64}$'),
    request_key uuid NOT NULL CHECK (request_key <> '00000000-0000-0000-0000-000000000000'::uuid),
    event_id text NOT NULL,
    policy_id text NOT NULL,
    input_digest text NOT NULL CHECK (input_digest ~ '^[a-f0-9]{64}$'),
    run_id text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    UNIQUE (actor_id, request_key)
);
