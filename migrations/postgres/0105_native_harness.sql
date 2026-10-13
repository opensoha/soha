-- Native runs remain in the existing queue; external claim never selects this owner.
ALTER TABLE ai_agent_runs
    ADD COLUMN native_request_id TEXT,
    ADD COLUMN native_request_hash TEXT,
    ADD COLUMN native_version BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN native_fence BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN native_lease_until TIMESTAMPTZ,
    ADD COLUMN native_phase TEXT NOT NULL DEFAULT '',
    ADD COLUMN native_event_sequence BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN native_checkpoint JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN native_cancel_at TIMESTAMPTZ;

CREATE UNIQUE INDEX uq_ai_native_request ON ai_agent_runs(created_by, session_id, native_request_id)
    WHERE provider_kind = 'native' AND native_request_id IS NOT NULL;
CREATE INDEX idx_ai_native_claim ON ai_agent_runs(native_lease_until, created_at)
    WHERE provider_kind = 'native' AND status IN ('queued', 'running');

CREATE TABLE ai_agent_run_steps (
    run_id TEXT NOT NULL REFERENCES ai_agent_runs(id) ON DELETE CASCADE,
    sequence BIGINT NOT NULL,
    attempt BIGINT NOT NULL,
    phase TEXT NOT NULL,
    checkpoint JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, sequence)
);
CREATE TABLE ai_agent_run_events (
    run_id TEXT NOT NULL REFERENCES ai_agent_runs(id) ON DELETE CASCADE,
    sequence BIGINT NOT NULL,
    event_id TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, sequence),
    UNIQUE (run_id, event_id)
);
