CREATE TABLE IF NOT EXISTS ai_workbench_runtime_profile_revisions (
    profile_id TEXT NOT NULL,
    revision BIGINT NOT NULL CHECK (revision > 0),
    scope TEXT NOT NULL CHECK (scope IN ('chat', 'heya')),
    enabled BOOLEAN NOT NULL,
    name TEXT NOT NULL,
    assets JSONB NOT NULL CHECK (jsonb_typeof(assets) = 'array'),
    model_preferences JSONB NOT NULL CHECK (jsonb_typeof(model_preferences) = 'object'),
    max_output_tokens INTEGER NOT NULL CHECK (max_output_tokens BETWEEN 0 AND 4096),
    published_by TEXT NOT NULL,
    published_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (profile_id, revision)
);

CREATE INDEX IF NOT EXISTS ai_workbench_runtime_profile_revocations
    ON ai_workbench_runtime_profile_revisions (profile_id, revision) WHERE NOT enabled;
