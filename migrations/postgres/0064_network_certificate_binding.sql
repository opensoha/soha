SELECT pg_catalog.set_config('search_path', '', false);

ALTER TABLE public.network_runtime_credentials
    ADD COLUMN IF NOT EXISTS certificate_authority_key_id text NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_network_runtime_credentials_certificate_binding
    ON public.network_runtime_credentials (tenant_id, workspace_id, certificate_authority_key_id, certificate_serial)
    WHERE certificate_authority_key_id <> '';
