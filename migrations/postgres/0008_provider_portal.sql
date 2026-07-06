CREATE TABLE IF NOT EXISTS identity_applications (
    id text PRIMARY KEY,
    slug text NOT NULL UNIQUE,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    icon_url text,
    category text NOT NULL DEFAULT '',
    tags jsonb NOT NULL DEFAULT '[]'::jsonb,
    launch_url text NOT NULL DEFAULT '',
    provider_id text,
    provider_type text NOT NULL DEFAULT 'link',
    portal_visible boolean NOT NULL DEFAULT true,
    featured boolean NOT NULL DEFAULT false,
    sort_order integer NOT NULL DEFAULT 1000,
    status text NOT NULL DEFAULT 'draft',
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_by text NOT NULL DEFAULT 'system',
    updated_by text NOT NULL DEFAULT 'system',
    created_at timestamp without time zone NOT NULL DEFAULT now(),
    updated_at timestamp without time zone NOT NULL DEFAULT now(),
    CONSTRAINT identity_applications_provider_type_check CHECK (provider_type IN ('link', 'oidc', 'proxy')),
    CONSTRAINT identity_applications_status_check CHECK (status IN ('draft', 'enabled', 'disabled', 'maintenance'))
);

CREATE INDEX IF NOT EXISTS idx_identity_applications_portal_status
    ON identity_applications (portal_visible, status, featured, sort_order);

CREATE INDEX IF NOT EXISTS idx_identity_applications_category
    ON identity_applications (category);

CREATE TABLE IF NOT EXISTS identity_application_assignments (
    id text PRIMARY KEY,
    application_id text NOT NULL REFERENCES identity_applications(id) ON DELETE CASCADE,
    subject_type text NOT NULL,
    subject_id text NOT NULL,
    effect text NOT NULL DEFAULT 'allow',
    created_by text NOT NULL DEFAULT 'system',
    created_at timestamp without time zone NOT NULL DEFAULT now(),
    CONSTRAINT identity_application_assignments_subject_type_check CHECK (subject_type IN ('user', 'role', 'team', 'tag')),
    CONSTRAINT identity_application_assignments_effect_check CHECK (effect IN ('allow')),
    CONSTRAINT identity_application_assignments_unique UNIQUE (application_id, subject_type, subject_id, effect)
);

CREATE INDEX IF NOT EXISTS idx_identity_application_assignments_subject
    ON identity_application_assignments (subject_type, subject_id);

CREATE TABLE IF NOT EXISTS identity_application_favorites (
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    application_id text NOT NULL REFERENCES identity_applications(id) ON DELETE CASCADE,
    created_at timestamp without time zone NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, application_id)
);

CREATE INDEX IF NOT EXISTS idx_identity_application_favorites_application
    ON identity_application_favorites (application_id);

CREATE TABLE IF NOT EXISTS identity_application_launches (
    id text PRIMARY KEY,
    application_id text NOT NULL REFERENCES identity_applications(id) ON DELETE CASCADE,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider_id text,
    provider_type text NOT NULL DEFAULT 'link',
    result text NOT NULL,
    reason text NOT NULL DEFAULT '',
    launch_url text NOT NULL DEFAULT '',
    source_ip text NOT NULL DEFAULT '',
    user_agent text NOT NULL DEFAULT '',
    created_at timestamp without time zone NOT NULL DEFAULT now(),
    CONSTRAINT identity_application_launches_provider_type_check CHECK (provider_type IN ('link', 'oidc', 'proxy')),
    CONSTRAINT identity_application_launches_result_check CHECK (result IN ('allow', 'denied', 'error'))
);

CREATE INDEX IF NOT EXISTS idx_identity_application_launches_user_created
    ON identity_application_launches (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_identity_application_launches_application_created
    ON identity_application_launches (application_id, created_at DESC);

CREATE TABLE IF NOT EXISTS identity_providers (
    id text PRIMARY KEY,
    application_id text NOT NULL REFERENCES identity_applications(id) ON DELETE CASCADE,
    name text NOT NULL,
    type text NOT NULL,
    enabled boolean NOT NULL DEFAULT false,
    config jsonb NOT NULL DEFAULT '{}'::jsonb,
    secret_refs jsonb NOT NULL DEFAULT '{}'::jsonb,
    status text NOT NULL DEFAULT 'disabled',
    created_by text NOT NULL DEFAULT 'system',
    updated_by text NOT NULL DEFAULT 'system',
    created_at timestamp without time zone NOT NULL DEFAULT now(),
    updated_at timestamp without time zone NOT NULL DEFAULT now(),
    CONSTRAINT identity_providers_type_check CHECK (type IN ('oidc', 'proxy')),
    CONSTRAINT identity_providers_status_check CHECK (status IN ('enabled', 'disabled'))
);

CREATE INDEX IF NOT EXISTS idx_identity_providers_application
    ON identity_providers (application_id);

CREATE INDEX IF NOT EXISTS idx_identity_providers_type_status
    ON identity_providers (type, status, enabled);

CREATE TABLE IF NOT EXISTS identity_outposts (
    id text PRIMARY KEY,
    name text NOT NULL,
    mode text NOT NULL DEFAULT 'embedded',
    endpoint text,
    token_hash text,
    status text NOT NULL DEFAULT 'offline',
    version text,
    last_seen_at timestamp without time zone,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_by text NOT NULL DEFAULT 'system',
    updated_by text NOT NULL DEFAULT 'system',
    created_at timestamp without time zone NOT NULL DEFAULT now(),
    updated_at timestamp without time zone NOT NULL DEFAULT now(),
    CONSTRAINT identity_outposts_mode_check CHECK (mode IN ('embedded', 'agent', 'kubernetes', 'external')),
    CONSTRAINT identity_outposts_status_check CHECK (status IN ('online', 'offline', 'degraded'))
);

CREATE INDEX IF NOT EXISTS idx_identity_outposts_mode_status
    ON identity_outposts (mode, status);

CREATE INDEX IF NOT EXISTS idx_identity_outposts_last_seen
    ON identity_outposts (last_seen_at DESC);

CREATE TABLE IF NOT EXISTS identity_oidc_clients (
    id text PRIMARY KEY,
    provider_id text NOT NULL REFERENCES identity_providers(id) ON DELETE CASCADE,
    client_id text NOT NULL UNIQUE,
    client_secret_hash text NOT NULL DEFAULT '',
    redirect_uris jsonb NOT NULL DEFAULT '[]'::jsonb,
    allowed_scopes jsonb NOT NULL DEFAULT '["openid", "profile", "email"]'::jsonb,
    allowed_grant_types jsonb NOT NULL DEFAULT '["authorization_code"]'::jsonb,
    require_pkce boolean NOT NULL DEFAULT true,
    access_token_ttl_seconds integer NOT NULL DEFAULT 3600,
    id_token_ttl_seconds integer NOT NULL DEFAULT 300,
    refresh_token_ttl_seconds integer NOT NULL DEFAULT 0,
    status text NOT NULL DEFAULT 'enabled',
    created_at timestamp without time zone NOT NULL DEFAULT now(),
    updated_at timestamp without time zone NOT NULL DEFAULT now(),
    CONSTRAINT identity_oidc_clients_status_check CHECK (status IN ('enabled', 'disabled')),
    CONSTRAINT identity_oidc_clients_access_ttl_check CHECK (access_token_ttl_seconds > 0),
    CONSTRAINT identity_oidc_clients_id_ttl_check CHECK (id_token_ttl_seconds > 0)
);

CREATE INDEX IF NOT EXISTS idx_identity_oidc_clients_provider
    ON identity_oidc_clients (provider_id);

CREATE TABLE IF NOT EXISTS identity_provider_signing_keys (
    id text PRIMARY KEY,
    provider_id text NOT NULL REFERENCES identity_providers(id) ON DELETE CASCADE,
    key_id text NOT NULL UNIQUE,
    algorithm text NOT NULL DEFAULT 'ES256',
    encrypted_private_key text NOT NULL,
    public_jwk jsonb NOT NULL DEFAULT '{}'::jsonb,
    active boolean NOT NULL DEFAULT true,
    created_at timestamp without time zone NOT NULL DEFAULT now(),
    rotated_at timestamp without time zone,
    CONSTRAINT identity_provider_signing_keys_algorithm_check CHECK (algorithm IN ('ES256'))
);

CREATE INDEX IF NOT EXISTS idx_identity_provider_signing_keys_provider_active
    ON identity_provider_signing_keys (provider_id, active, created_at DESC);

CREATE TABLE IF NOT EXISTS identity_authorization_codes (
    id text PRIMARY KEY,
    provider_id text NOT NULL REFERENCES identity_providers(id) ON DELETE CASCADE,
    client_id text NOT NULL REFERENCES identity_oidc_clients(client_id) ON DELETE CASCADE,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash text NOT NULL UNIQUE,
    redirect_uri text NOT NULL,
    scopes jsonb NOT NULL DEFAULT '[]'::jsonb,
    nonce text,
    code_challenge text,
    code_challenge_method text,
    expires_at timestamp without time zone NOT NULL,
    consumed_at timestamp without time zone,
    created_at timestamp without time zone NOT NULL DEFAULT now(),
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT identity_authorization_codes_challenge_method_check CHECK (code_challenge_method IS NULL OR code_challenge_method IN ('S256'))
);

CREATE INDEX IF NOT EXISTS idx_identity_authorization_codes_client_created
    ON identity_authorization_codes (client_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_identity_authorization_codes_expiry
    ON identity_authorization_codes (expires_at, consumed_at);

INSERT INTO identity_applications (
    id, slug, name, description, icon_url, category, tags, launch_url, provider_id,
    provider_type, portal_visible, featured, sort_order, status, metadata,
    created_by, updated_by, created_at, updated_at
)
VALUES (
    'soha-console',
    'soha-console',
    'Soha Console',
    'Open the existing Soha operations console.',
    '',
    'Platform',
    '["platform", "console"]'::jsonb,
    '/',
    NULL,
    'link',
    TRUE,
    TRUE,
    0,
    'enabled',
    '{"builtIn": true}'::jsonb,
    'system',
    'system',
    now(),
    now()
)
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    description = EXCLUDED.description,
    category = EXCLUDED.category,
    tags = EXCLUDED.tags,
    launch_url = EXCLUDED.launch_url,
    provider_type = EXCLUDED.provider_type,
    portal_visible = EXCLUDED.portal_visible,
    featured = EXCLUDED.featured,
    sort_order = EXCLUDED.sort_order,
    status = EXCLUDED.status,
    metadata = identity_applications.metadata || EXCLUDED.metadata,
    updated_by = 'system',
    updated_at = now();
