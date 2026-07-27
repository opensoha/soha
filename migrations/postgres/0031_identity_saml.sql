SELECT pg_catalog.set_config('search_path', '', false);

ALTER TABLE public.identity_applications
    DROP CONSTRAINT IF EXISTS identity_applications_provider_type_check;
ALTER TABLE public.identity_applications
    ADD CONSTRAINT identity_applications_provider_type_check
        CHECK (provider_type IN ('link', 'oidc', 'proxy', 'saml'));

ALTER TABLE public.identity_application_launches
    DROP CONSTRAINT IF EXISTS identity_application_launches_provider_type_check;
ALTER TABLE public.identity_application_launches
    ADD CONSTRAINT identity_application_launches_provider_type_check
        CHECK (provider_type IN ('link', 'oidc', 'proxy', 'saml'));

ALTER TABLE public.identity_providers
    DROP CONSTRAINT IF EXISTS identity_providers_type_check;
ALTER TABLE public.identity_providers
    ADD CONSTRAINT identity_providers_type_check
        CHECK (type IN ('oidc', 'proxy', 'saml'));

CREATE TABLE IF NOT EXISTS public.identity_saml_service_providers (
    provider_id text PRIMARY KEY REFERENCES public.identity_providers(id) ON DELETE CASCADE,
    entity_id text NOT NULL,
    assertion_consumer_service_urls jsonb NOT NULL DEFAULT '[]'::jsonb,
    name_id_format text NOT NULL,
    want_authn_requests_signed boolean NOT NULL DEFAULT false,
    want_assertions_signed boolean NOT NULL DEFAULT true,
    signing_certificate_pem text NOT NULL DEFAULT '',
    attribute_mappings jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT identity_saml_service_providers_entity_id_key UNIQUE (entity_id)
);

CREATE TABLE IF NOT EXISTS public.identity_saml_signing_keys (
    id text PRIMARY KEY,
    provider_id text NOT NULL REFERENCES public.identity_providers(id) ON DELETE CASCADE,
    encrypted_private_key text NOT NULL,
    certificate_pem text NOT NULL,
    fingerprint_sha256 text NOT NULL,
    active boolean NOT NULL DEFAULT true,
    not_before timestamptz NOT NULL,
    not_after timestamptz NOT NULL,
    retire_after timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_identity_saml_signing_keys_active
    ON public.identity_saml_signing_keys (provider_id) WHERE active;
CREATE INDEX IF NOT EXISTS idx_identity_saml_signing_keys_validation_window
    ON public.identity_saml_signing_keys (provider_id, not_after DESC);

CREATE TABLE IF NOT EXISTS public.identity_saml_replay_keys (
    replay_key text PRIMARY KEY,
    provider_id text NOT NULL,
    kind text NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT identity_saml_replay_keys_kind_check CHECK (kind IN ('request', 'assertion'))
);

CREATE INDEX IF NOT EXISTS idx_identity_saml_replay_keys_expires_at
    ON public.identity_saml_replay_keys (expires_at);

CREATE TABLE IF NOT EXISTS public.identity_saml_pending_requests (
    token text PRIMARY KEY,
    provider_id text NOT NULL REFERENCES public.identity_providers(id) ON DELETE CASCADE,
    method text NOT NULL,
    encoded_request text NOT NULL,
    relay_state text NOT NULL DEFAULT '',
    raw_query text NOT NULL DEFAULT '',
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_identity_saml_pending_requests_expires_at
    ON public.identity_saml_pending_requests (expires_at);
