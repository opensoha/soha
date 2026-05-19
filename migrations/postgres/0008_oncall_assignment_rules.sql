CREATE TABLE IF NOT EXISTS oncall_assignment_rules (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    integration_id TEXT,
    integration_type TEXT,
    business_line_id TEXT,
    alert_category TEXT,
    alert_name TEXT,
    severity TEXT,
    service TEXT,
    role TEXT,
    matchers JSON NOT NULL DEFAULT '{}',
    target_type TEXT NOT NULL,
    target_ref TEXT NOT NULL,
    route_order INT NOT NULL DEFAULT 100,
    group_by JSON NOT NULL DEFAULT '[]',
    priority INT NOT NULL DEFAULT 100,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

ALTER TABLE oncall_assignment_rules ADD COLUMN IF NOT EXISTS integration_id TEXT;
ALTER TABLE oncall_assignment_rules ADD COLUMN IF NOT EXISTS integration_type TEXT;
ALTER TABLE oncall_assignment_rules ADD COLUMN IF NOT EXISTS route_order INT;
ALTER TABLE oncall_assignment_rules ALTER COLUMN route_order SET DEFAULT 100;
UPDATE oncall_assignment_rules SET route_order = 100 WHERE route_order IS NULL;
ALTER TABLE oncall_assignment_rules ALTER COLUMN route_order SET NOT NULL;
ALTER TABLE oncall_assignment_rules ADD COLUMN IF NOT EXISTS group_by JSON;
ALTER TABLE oncall_assignment_rules ALTER COLUMN group_by SET DEFAULT '[]';
UPDATE oncall_assignment_rules SET group_by = '[]' WHERE group_by IS NULL;
ALTER TABLE oncall_assignment_rules ALTER COLUMN group_by SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_oncall_assignment_rules_business_role
    ON oncall_assignment_rules (business_line_id, role, enabled, priority DESC);

CREATE INDEX IF NOT EXISTS idx_oncall_assignment_rules_alert_scope
    ON oncall_assignment_rules (alert_category, severity, service, enabled, priority DESC);

CREATE INDEX IF NOT EXISTS idx_oncall_assignment_rules_integration_route
    ON oncall_assignment_rules (integration_type, integration_id, enabled, route_order ASC);
