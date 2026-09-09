SELECT pg_catalog.set_config('search_path', '', false);

ALTER TABLE public.network_runtime_credentials
    ADD COLUMN IF NOT EXISTS wireguard_public_key text;

ALTER TABLE public.network_runtime_credentials
    DROP CONSTRAINT IF EXISTS network_runtime_credentials_wireguard_public_key_check;

ALTER TABLE public.network_runtime_credentials
    ADD CONSTRAINT network_runtime_credentials_wireguard_public_key_check
    CHECK (wireguard_public_key IS NULL OR wireguard_public_key ~ '^[A-Za-z0-9+/]{43}=$');

CREATE UNIQUE INDEX IF NOT EXISTS idx_network_runtime_credentials_wireguard_public_key
    ON public.network_runtime_credentials (tenant_id, workspace_id, wireguard_public_key)
    WHERE wireguard_public_key IS NOT NULL;
