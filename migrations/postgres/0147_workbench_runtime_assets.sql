CREATE TABLE IF NOT EXISTS ai_workbench_runtime_asset_state (
    kind TEXT NOT NULL CHECK (kind IN ('mcp', 'skill', 'hook')),
    asset_id TEXT NOT NULL,
    version TEXT NOT NULL,
    enabled BOOLEAN NOT NULL,
    updated_by TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (kind, asset_id, version)
);
