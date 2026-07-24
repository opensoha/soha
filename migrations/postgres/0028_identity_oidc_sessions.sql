SELECT pg_catalog.set_config('search_path', '', false);

ALTER TABLE public.identity_oidc_clients
    DROP CONSTRAINT IF EXISTS identity_oidc_clients_grant_types_reserved_check,
    DROP CONSTRAINT IF EXISTS identity_oidc_clients_refresh_reserved_check;

ALTER TABLE public.identity_oidc_clients
    ADD CONSTRAINT identity_oidc_clients_refresh_ttl_check
        CHECK (refresh_token_ttl_seconds >= 0);

CREATE TABLE IF NOT EXISTS public.identity_oidc_sessions (
    id text PRIMARY KEY,
    provider_id text NOT NULL REFERENCES public.identity_providers(id) ON DELETE CASCADE,
    client_id text NOT NULL REFERENCES public.identity_oidc_clients(client_id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    platform_session_id text REFERENCES public.sessions(id) ON DELETE SET NULL,
    scopes jsonb NOT NULL DEFAULT '[]'::jsonb,
    auth_time timestamp without time zone NOT NULL,
    expires_at timestamp without time zone NOT NULL,
    last_seen_at timestamp without time zone NOT NULL,
    revoked_at timestamp without time zone,
    created_at timestamp without time zone NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_identity_oidc_sessions_platform_session
    ON public.identity_oidc_sessions (platform_session_id)
    WHERE revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_identity_oidc_sessions_client_user
    ON public.identity_oidc_sessions (client_id, user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS public.identity_oidc_refresh_tokens (
    id text PRIMARY KEY,
    session_id text NOT NULL REFERENCES public.identity_oidc_sessions(id) ON DELETE CASCADE,
    family_id text NOT NULL,
    token_hash text NOT NULL UNIQUE,
    parent_id text REFERENCES public.identity_oidc_refresh_tokens(id) ON DELETE SET NULL,
    expires_at timestamp without time zone NOT NULL,
    consumed_at timestamp without time zone,
    revoked_at timestamp without time zone,
    created_at timestamp without time zone NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_identity_oidc_refresh_tokens_family
    ON public.identity_oidc_refresh_tokens (family_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_identity_oidc_refresh_tokens_session
    ON public.identity_oidc_refresh_tokens (session_id, created_at DESC);
