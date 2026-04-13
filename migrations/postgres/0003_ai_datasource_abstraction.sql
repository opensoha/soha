ALTER TABLE ai_data_sources ADD COLUMN IF NOT EXISTS source_kind TEXT;
ALTER TABLE ai_data_sources ADD COLUMN IF NOT EXISTS backend_type TEXT;

UPDATE ai_data_sources
SET source_kind = CASE
    WHEN source_kind IS NOT NULL AND source_kind <> '' THEN source_kind
    WHEN mcp_adapter = 'logs.v1' THEN 'logs'
    WHEN mcp_adapter = 'metrics.v1' THEN 'metrics'
    WHEN mcp_adapter = 'traces.v1' THEN 'traces'
    ELSE source_type
END
WHERE source_kind IS NULL OR source_kind = '';

UPDATE ai_data_sources
SET backend_type = CASE
    WHEN backend_type IS NOT NULL AND backend_type <> '' THEN backend_type
    WHEN source_type = 'es-logs' THEN 'es'
    WHEN source_type = 'loki-logs' THEN 'loki'
    WHEN source_type = 'clickhouse-logs' THEN 'clickhouse'
    WHEN source_type = 'prometheus' THEN 'prometheus'
    WHEN source_type = 'jaeger-traces' THEN 'jaeger'
    WHEN source_type = 'platform-native' THEN 'platform'
    WHEN source_type = 'alert-center' THEN 'platform'
    WHEN source_type = 'release-records' THEN 'platform'
    ELSE source_type
END
WHERE backend_type IS NULL OR backend_type = '';

UPDATE ai_data_sources
SET mcp_adapter = CASE
    WHEN source_kind = 'logs' THEN 'logs.v1'
    WHEN source_kind = 'metrics' THEN 'metrics.v1'
    WHEN source_kind = 'traces' THEN 'traces.v1'
    ELSE 'platform-native.v1'
END
WHERE mcp_adapter NOT IN ('logs.v1', 'metrics.v1', 'traces.v1', 'platform-native.v1');

CREATE INDEX IF NOT EXISTS idx_ai_data_sources_kind_backend_enabled ON ai_data_sources (source_kind, backend_type, enabled);
