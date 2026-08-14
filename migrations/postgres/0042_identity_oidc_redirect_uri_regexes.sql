SELECT pg_catalog.set_config('search_path', '', false);

ALTER TABLE public.identity_oidc_clients
    ADD COLUMN IF NOT EXISTS redirect_uri_regexes jsonb NOT NULL DEFAULT '[]'::jsonb;
