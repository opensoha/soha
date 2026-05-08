CREATE TABLE IF NOT EXISTS notification_policies (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    matchers JSON NOT NULL DEFAULT '{}',
    processor_chain JSON NOT NULL DEFAULT '[]',
    channel_refs JSON NOT NULL DEFAULT '[]',
    oncall_ref TEXT,
    send_resolved BOOLEAN NOT NULL DEFAULT FALSE,
    cooldown_seconds INT NOT NULL DEFAULT 0,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notification_policies_enabled_updated_at
    ON notification_policies (enabled, updated_at DESC);

INSERT INTO notification_policies (
    id,
    name,
    matchers,
    processor_chain,
    channel_refs,
    oncall_ref,
    send_resolved,
    cooldown_seconds,
    enabled,
    created_at,
    updated_at
)
SELECT
    route.id,
    route.name,
    route.matchers,
    '["webhook_update"]',
    route.channel_ids,
    NULL,
    FALSE,
    0,
    route.enabled,
    route.created_at,
    route.updated_at
FROM alert_routes AS route
WHERE NOT EXISTS (
    SELECT 1
    FROM notification_policies AS policy
    WHERE policy.id = route.id
);
