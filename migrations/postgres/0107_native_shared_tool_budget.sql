-- Keep attempts outside checkpoint transactions: a provider failure or lost
-- worker must not refund an invocation that may already have reached its owner.
CREATE TABLE ai_native_tool_budget (
    run_id TEXT PRIMARY KEY REFERENCES ai_agent_runs(id) ON DELETE CASCADE,
    work_calls INTEGER NOT NULL DEFAULT 0,
    cleanup_calls INTEGER NOT NULL DEFAULT 0,
    work_limit INTEGER NOT NULL DEFAULT 8,
    cleanup_limit INTEGER NOT NULL DEFAULT 8,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (work_limit BETWEEN 1 AND 8 AND cleanup_limit BETWEEN 1 AND 8),
    CHECK (work_calls BETWEEN 0 AND work_limit),
    CHECK (cleanup_calls BETWEEN 0 AND cleanup_limit)
);

-- Earlier Native prototypes did not count child polls/retries. Preserve their
-- records and cancellation reserve, but never invent unused work allowance.
INSERT INTO ai_native_tool_budget(run_id, work_calls)
SELECT id, 8 FROM ai_agent_runs WHERE provider_kind = 'native';
