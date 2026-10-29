CREATE TABLE IF NOT EXISTS public.network_vpn_revocations (
    tenant_id text NOT NULL DEFAULT 'default',
    workspace_id text NOT NULL DEFAULT 'default',
    subject_id text NOT NULL,
    device_id text NOT NULL,
    site_id text NOT NULL,
    session_id text NOT NULL,
    epoch bigint NOT NULL CHECK (epoch > 0),
    active boolean NOT NULL,
    reason_code text NOT NULL,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL,
    released_by text,
    released_at timestamptz,
    executions jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(executions) = 'array'),
    PRIMARY KEY (tenant_id, workspace_id, subject_id, device_id, site_id)
);
