-- Runtime observations are owned by authenticated node callbacks, never by
-- the management form or its legacy metadata/status/version fields.
ALTER TABLE identity_outposts
    ADD COLUMN IF NOT EXISTS forward_auth_url TEXT,
    ADD COLUMN IF NOT EXISTS claimed_agent_id TEXT,
    ADD COLUMN IF NOT EXISTS protocol_version TEXT,
    ADD COLUMN IF NOT EXISTS runtime_version TEXT,
    ADD COLUMN IF NOT EXISTS applied_configuration_version BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS configuration_expires_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_heartbeat_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS runtime_status TEXT NOT NULL DEFAULT 'unavailable',
    ADD COLUMN IF NOT EXISTS runtime_reason TEXT NOT NULL DEFAULT '';
