SELECT pg_catalog.set_config('search_path', '', false);

CREATE TABLE IF NOT EXISTS public.network_access_sites (
    id text PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    workspace_id text NOT NULL DEFAULT 'default',
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    location text NOT NULL DEFAULT '',
    status text NOT NULL CHECK (status IN ('active', 'disabled')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, workspace_id, name)
);

CREATE TABLE IF NOT EXISTS public.network_access_spaces (
    id text PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    workspace_id text NOT NULL DEFAULT 'default',
    site_id text NOT NULL REFERENCES public.network_access_sites(id) ON DELETE RESTRICT,
    name text NOT NULL,
    cidrs jsonb NOT NULL CHECK (jsonb_typeof(cidrs) = 'array' AND jsonb_array_length(cidrs) BETWEEN 1 AND 64),
    status text NOT NULL CHECK (status IN ('active', 'disabled')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, workspace_id, site_id, name)
);

CREATE TABLE IF NOT EXISTS public.network_access_resources (
    id text PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    workspace_id text NOT NULL DEFAULT 'default',
    space_id text NOT NULL REFERENCES public.network_access_spaces(id) ON DELETE RESTRICT,
    name text NOT NULL,
    kind text NOT NULL CHECK (kind IN ('cidr', 'ip', 'fqdn')),
    target text NOT NULL,
    protocol text NOT NULL CHECK (protocol IN ('any', 'tcp', 'udp', 'icmp')),
    ports jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(ports) = 'array' AND jsonb_array_length(ports) <= 64),
    protected boolean NOT NULL DEFAULT false,
    path_mode text NOT NULL CHECK (path_mode IN ('automatic', 'site_direct', 'wireguard', 'wireguard_ztna', 'access_proxy')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, workspace_id, space_id, name),
    CHECK (NOT protected OR path_mode NOT IN ('site_direct', 'wireguard'))
);

CREATE TABLE IF NOT EXISTS public.network_access_devices (
    id text PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    workspace_id text NOT NULL DEFAULT 'default',
    owner_user_id uuid NOT NULL REFERENCES public.users(id) ON DELETE RESTRICT,
    name text NOT NULL,
    hostname text NOT NULL DEFAULT '',
    platform text NOT NULL,
    site_id text REFERENCES public.network_access_sites(id) ON DELETE RESTRICT,
    status text NOT NULL CHECK (status IN ('pending', 'active', 'quarantined', 'revoked')),
    posture_status text NOT NULL DEFAULT 'unknown' CHECK (posture_status IN ('unknown', 'compliant', 'non_compliant')),
    credential_generation integer NOT NULL DEFAULT 1 CHECK (credential_generation >= 1),
    last_seen_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS public.network_access_gateways (
    id text PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    workspace_id text NOT NULL DEFAULT 'default',
    site_id text NOT NULL REFERENCES public.network_access_sites(id) ON DELETE RESTRICT,
    name text NOT NULL,
    status text NOT NULL DEFAULT 'offline' CHECK (status IN ('offline', 'online', 'degraded')),
    version text NOT NULL DEFAULT '',
    capabilities jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(capabilities) = 'array' AND jsonb_array_length(capabilities) <= 32),
    policy_version bigint NOT NULL DEFAULT 0 CHECK (policy_version >= 0),
    applied_at timestamptz,
    last_heartbeat_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, workspace_id, site_id, name)
);

CREATE INDEX IF NOT EXISTS idx_network_access_devices_scope
    ON public.network_access_devices (tenant_id, workspace_id, status, site_id, owner_user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_network_access_spaces_scope
    ON public.network_access_spaces (tenant_id, workspace_id, site_id, status, name);
CREATE INDEX IF NOT EXISTS idx_network_access_resources_scope
    ON public.network_access_resources (tenant_id, workspace_id, space_id, name);
CREATE INDEX IF NOT EXISTS idx_network_access_gateways_scope
    ON public.network_access_gateways (tenant_id, workspace_id, site_id, status, name);
