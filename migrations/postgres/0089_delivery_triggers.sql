CREATE TABLE IF NOT EXISTS delivery_triggers (
    id TEXT PRIMARY KEY,
    revision INTEGER NOT NULL CHECK (revision > 0),
    target_kind TEXT NOT NULL CHECK (target_kind IN ('template_source', 'workflow')),
    target_id TEXT NOT NULL,
    trigger_type TEXT NOT NULL CHECK (trigger_type IN ('webhook', 'schedule', 'poll')),
    enabled BOOLEAN NOT NULL,
    definition JSONB NOT NULL,
    execution_token_id TEXT NOT NULL,
    updated_by_token_id TEXT NOT NULL DEFAULT '',
    target_digest TEXT NOT NULL,
    signing_secret_ciphertext TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS delivery_triggers_target_idx ON delivery_triggers(target_kind, target_id, created_at, id);

CREATE TABLE IF NOT EXISTS delivery_trigger_events (
    id TEXT PRIMARY KEY,
    trigger_id TEXT NOT NULL REFERENCES delivery_triggers(id),
    trigger_revision INTEGER NOT NULL CHECK (trigger_revision > 0),
    event_id TEXT NOT NULL,
    payload_digest TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('queued', 'processing', 'succeeded', 'failed', 'skipped')),
    definition JSONB NOT NULL,
    source_generation INTEGER NOT NULL DEFAULT 0,
    attempts INTEGER NOT NULL DEFAULT 0,
    lease TEXT NOT NULL DEFAULT '',
    claimed_at TIMESTAMPTZ,
    prepared_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (trigger_id, event_id)
);
CREATE INDEX IF NOT EXISTS delivery_trigger_events_claim_idx ON delivery_trigger_events(status, created_at, id);
CREATE UNIQUE INDEX IF NOT EXISTS delivery_trigger_events_active_idx ON delivery_trigger_events(trigger_id) WHERE status='processing';
CREATE INDEX IF NOT EXISTS delivery_trigger_events_history_idx ON delivery_trigger_events(trigger_id, created_at DESC, id);
