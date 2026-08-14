ALTER TABLE observability_dashboards
    ADD COLUMN IF NOT EXISTS source_format TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS variables JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS data_source_bindings JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS import_warnings JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS raw_json JSONB;
