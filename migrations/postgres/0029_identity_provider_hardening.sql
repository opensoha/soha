SELECT pg_catalog.set_config('search_path', '', false);

ALTER TABLE public.identity_oidc_clients
    ADD COLUMN IF NOT EXISTS client_type text NOT NULL DEFAULT 'confidential',
    ADD COLUMN IF NOT EXISTS post_logout_redirect_uris jsonb NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE public.identity_oidc_clients
    DROP CONSTRAINT IF EXISTS identity_oidc_clients_client_type_check;

ALTER TABLE public.identity_oidc_clients
    ADD CONSTRAINT identity_oidc_clients_client_type_check
        CHECK (client_type IN ('public', 'confidential'));

CREATE INDEX IF NOT EXISTS idx_identity_provider_signing_keys_retained
    ON public.identity_provider_signing_keys (provider_id, created_at DESC);
