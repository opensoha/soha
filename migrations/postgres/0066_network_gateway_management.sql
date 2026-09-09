SELECT pg_catalog.set_config('search_path', '', false);

ALTER TABLE public.network_access_gateways
    ADD COLUMN IF NOT EXISTS runtime_id text,
    ADD COLUMN IF NOT EXISTS administrative_status text NOT NULL DEFAULT 'disabled',
    ADD COLUMN IF NOT EXISTS public_endpoint_host text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS public_endpoint_port integer NOT NULL DEFAULT 51820,
    ADD COLUMN IF NOT EXISTS overlay_cidr cidr,
    ADD COLUMN IF NOT EXISTS routing_mode text NOT NULL DEFAULT 'routed',
    ADD COLUMN IF NOT EXISTS mtu integer NOT NULL DEFAULT 1420,
    ADD COLUMN IF NOT EXISTS persistent_keepalive_seconds integer NOT NULL DEFAULT 25,
    ADD COLUMN IF NOT EXISTS dns_servers jsonb NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE public.network_access_gateways
    DROP CONSTRAINT IF EXISTS network_access_gateways_administrative_status_check,
    DROP CONSTRAINT IF EXISTS network_access_gateways_public_endpoint_host_check,
    DROP CONSTRAINT IF EXISTS network_access_gateways_public_endpoint_port_check,
    DROP CONSTRAINT IF EXISTS network_access_gateways_overlay_cidr_check,
    DROP CONSTRAINT IF EXISTS network_access_gateways_routing_mode_check,
    DROP CONSTRAINT IF EXISTS network_access_gateways_mtu_check,
    DROP CONSTRAINT IF EXISTS network_access_gateways_keepalive_check,
    DROP CONSTRAINT IF EXISTS network_access_gateways_dns_servers_check;

ALTER TABLE public.network_access_gateways
    ADD CONSTRAINT network_access_gateways_administrative_status_check CHECK (administrative_status IN ('active', 'disabled')),
    ADD CONSTRAINT network_access_gateways_public_endpoint_host_check CHECK (length(public_endpoint_host) <= 253),
    ADD CONSTRAINT network_access_gateways_public_endpoint_port_check CHECK (public_endpoint_port BETWEEN 1 AND 65535),
    ADD CONSTRAINT network_access_gateways_overlay_cidr_check CHECK (overlay_cidr IS NULL OR (family(overlay_cidr) = 4 AND masklen(overlay_cidr) BETWEEN 16 AND 30)),
    ADD CONSTRAINT network_access_gateways_routing_mode_check CHECK (routing_mode IN ('routed', 'snat')),
    ADD CONSTRAINT network_access_gateways_mtu_check CHECK (mtu BETWEEN 1280 AND 1500),
    ADD CONSTRAINT network_access_gateways_keepalive_check CHECK (persistent_keepalive_seconds BETWEEN 0 AND 300),
    ADD CONSTRAINT network_access_gateways_dns_servers_check CHECK (jsonb_typeof(dns_servers) = 'array' AND jsonb_array_length(dns_servers) <= 8);

CREATE UNIQUE INDEX IF NOT EXISTS idx_network_access_gateways_runtime
    ON public.network_access_gateways (tenant_id, workspace_id, runtime_id)
    WHERE runtime_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_network_access_gateways_active_site
    ON public.network_access_gateways (tenant_id, workspace_id, site_id)
    WHERE administrative_status = 'active' AND runtime_id IS NOT NULL;
