SELECT pg_catalog.set_config('search_path', '', false);

ALTER TABLE public.network_runtime_sessions
    ADD COLUMN IF NOT EXISTS gateway_id text REFERENCES public.network_access_gateways(id) ON DELETE RESTRICT;

CREATE TABLE IF NOT EXISTS public.network_wireguard_peers (
    id text PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    workspace_id text NOT NULL DEFAULT 'default',
    gateway_id text NOT NULL REFERENCES public.network_access_gateways(id) ON DELETE RESTRICT,
    runtime_id text NOT NULL,
    device_id text NOT NULL,
    session_id text NOT NULL REFERENCES public.network_runtime_sessions(id) ON DELETE RESTRICT,
    public_key text NOT NULL CHECK (public_key ~ '^[A-Za-z0-9+/]{43}=$'),
    overlay_address inet NOT NULL CHECK (family(overlay_address) = 4 AND masklen(overlay_address) = 32),
    status text NOT NULL CHECK (status IN ('active', 'revoked', 'expired')),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    revoke_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, workspace_id, session_id),
    CHECK (expires_at > created_at)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_network_wireguard_peers_active_address
    ON public.network_wireguard_peers (tenant_id, workspace_id, gateway_id, overlay_address)
    WHERE status = 'active';
CREATE UNIQUE INDEX IF NOT EXISTS uq_network_wireguard_peers_active_runtime
    ON public.network_wireguard_peers (tenant_id, workspace_id, runtime_id)
    WHERE status = 'active';
CREATE UNIQUE INDEX IF NOT EXISTS uq_network_wireguard_peers_active_key
    ON public.network_wireguard_peers (tenant_id, workspace_id, public_key)
    WHERE status = 'active';

CREATE TABLE IF NOT EXISTS public.network_vpn_connection_requests (
    tenant_id text NOT NULL DEFAULT 'default',
    workspace_id text NOT NULL DEFAULT 'default',
    runtime_id text NOT NULL,
    request_id text NOT NULL,
    request_hash text NOT NULL CHECK (request_hash ~ '^sha256:[a-f0-9]{64}$'),
    decision text NOT NULL CHECK (decision IN ('allow', 'deny')),
    session_id text REFERENCES public.network_runtime_sessions(id) ON DELETE RESTRICT,
    gateway_id text REFERENCES public.network_access_gateways(id) ON DELETE RESTRICT,
    result_payload jsonb NOT NULL CHECK (jsonb_typeof(result_payload) = 'object'),
    valid_until timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, workspace_id, runtime_id, request_id),
    CHECK (valid_until > created_at),
    CHECK ((decision = 'allow' AND session_id IS NOT NULL AND gateway_id IS NOT NULL)
        OR (decision = 'deny' AND session_id IS NULL AND gateway_id IS NULL))
);

CREATE INDEX IF NOT EXISTS idx_network_wireguard_peers_gateway
    ON public.network_wireguard_peers (tenant_id, workspace_id, gateway_id, status, expires_at);
CREATE INDEX IF NOT EXISTS idx_network_vpn_requests_session
    ON public.network_vpn_connection_requests (tenant_id, workspace_id, session_id, created_at DESC);
