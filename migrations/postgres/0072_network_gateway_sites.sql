SELECT pg_catalog.set_config('search_path', '', false);

ALTER TABLE public.network_access_gateways
    ADD COLUMN IF NOT EXISTS hub_gateway_id text REFERENCES public.network_access_gateways(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS advertised_cidrs jsonb NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE public.network_access_gateways
    DROP CONSTRAINT IF EXISTS network_access_gateways_hub_not_self_check,
    DROP CONSTRAINT IF EXISTS network_access_gateways_advertised_cidrs_check;

ALTER TABLE public.network_access_gateways
    ADD CONSTRAINT network_access_gateways_hub_not_self_check CHECK (hub_gateway_id IS NULL OR hub_gateway_id <> id),
    ADD CONSTRAINT network_access_gateways_advertised_cidrs_check CHECK (
        jsonb_typeof(advertised_cidrs) = 'array' AND jsonb_array_length(advertised_cidrs) <= 64
    );

CREATE INDEX IF NOT EXISTS idx_network_access_gateways_hub
    ON public.network_access_gateways (tenant_id, workspace_id, hub_gateway_id)
    WHERE hub_gateway_id IS NOT NULL;
