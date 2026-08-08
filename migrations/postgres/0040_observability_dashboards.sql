CREATE TABLE IF NOT EXISTS observability_dashboards (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    source TEXT NOT NULL CHECK (source IN ('grafana')),
    source_schema_version INTEGER NOT NULL DEFAULT 0,
    data_source_id TEXT NOT NULL DEFAULT '',
    tags JSONB NOT NULL DEFAULT '[]'::jsonb,
    panels JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_observability_dashboards_updated_at
    ON observability_dashboards(updated_at DESC);
