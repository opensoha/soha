CREATE TABLE IF NOT EXISTS alert_integrations (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    integration_type TEXT NOT NULL,
    description TEXT,
    token TEXT NOT NULL,
    label_mapping JSON NOT NULL DEFAULT '{}',
    dedupe_config JSON NOT NULL DEFAULT '{}',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    status TEXT NOT NULL DEFAULT 'pending',
    last_error TEXT,
    last_received_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_alert_integrations_type_enabled ON alert_integrations (integration_type, enabled, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_alert_integrations_status_updated_at ON alert_integrations (status, updated_at DESC);
