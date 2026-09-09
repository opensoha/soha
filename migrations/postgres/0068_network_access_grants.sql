SELECT pg_catalog.set_config('search_path', '', false);

ALTER TABLE public.ai_gateway_approval_requests
    ADD COLUMN IF NOT EXISTS actor_session_id text;

CREATE TABLE IF NOT EXISTS public.network_access_grants (
    id text PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    workspace_id text NOT NULL DEFAULT 'default',
    subject_id text NOT NULL,
    auth_session_id text NOT NULL,
    device_id text NOT NULL REFERENCES public.network_access_devices(id) ON DELETE RESTRICT,
    site_id text NOT NULL REFERENCES public.network_access_sites(id) ON DELETE RESTRICT,
    network_space_id text NOT NULL REFERENCES public.network_access_spaces(id) ON DELETE RESTRICT,
    mode text NOT NULL CHECK (mode IN ('internal_ztna', 'external_vpn_ztna', 'external_direct_ztna')),
    resource_ids jsonb NOT NULL CHECK (jsonb_typeof(resource_ids) = 'array' AND jsonb_array_length(resource_ids) BETWEEN 1 AND 256),
    policy_version bigint NOT NULL CHECK (policy_version >= 1),
    status text NOT NULL CHECK (status IN ('issued', 'consumed', 'revoked', 'expired')),
    token_hash text,
    session_id text REFERENCES public.network_runtime_sessions(id) ON DELETE RESTRICT,
    resource_lease_ids jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(resource_lease_ids) = 'array' AND jsonb_array_length(resource_lease_ids) <= 256),
    reason_code text NOT NULL,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    revoked_at timestamptz,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at >= created_at + interval '1 minute' AND expires_at <= created_at + interval '5 minutes'),
    CHECK (
        (status = 'issued' AND token_hash ~ '^sha256:[a-f0-9]{64}$' AND consumed_at IS NULL AND revoked_at IS NULL AND session_id IS NULL AND jsonb_array_length(resource_lease_ids) = 0)
        OR (status = 'consumed' AND token_hash IS NULL AND consumed_at IS NOT NULL AND revoked_at IS NULL AND session_id IS NOT NULL AND jsonb_array_length(resource_lease_ids) > 0)
        OR (status = 'revoked' AND token_hash IS NULL AND consumed_at IS NULL AND revoked_at IS NOT NULL AND session_id IS NULL AND jsonb_array_length(resource_lease_ids) = 0)
        OR (status = 'expired' AND token_hash IS NULL AND consumed_at IS NULL AND revoked_at IS NULL AND session_id IS NULL AND jsonb_array_length(resource_lease_ids) = 0)
    )
);

CREATE INDEX IF NOT EXISTS idx_network_access_grants_subject
    ON public.network_access_grants (tenant_id, workspace_id, subject_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_network_access_grants_device
    ON public.network_access_grants (tenant_id, workspace_id, device_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_network_access_grants_expiry
    ON public.network_access_grants (tenant_id, workspace_id, expires_at)
    WHERE status = 'issued';
