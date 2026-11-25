SELECT pg_catalog.set_config('search_path', '', false);

CREATE TABLE IF NOT EXISTS public.app_hosting_settings (
    scope_key text PRIMARY KEY DEFAULT 'default' CHECK (scope_key = 'default'),
    enabled boolean NOT NULL DEFAULT false,
    apps_domain text NOT NULL DEFAULT '',
    public_scheme text NOT NULL DEFAULT 'https' CHECK (public_scheme IN ('https', 'http')),
    storage_integration_id text NOT NULL DEFAULT '',
    storage_prefix text NOT NULL DEFAULT 'app-hosting',
    allow_organization_visibility boolean NOT NULL DEFAULT true,
    max_bundle_compressed_bytes bigint NOT NULL DEFAULT 52428800 CHECK (max_bundle_compressed_bytes > 0),
    max_bundle_uncompressed_bytes bigint NOT NULL DEFAULT 157286400 CHECK (max_bundle_uncompressed_bytes > 0),
    max_bundle_file_count integer NOT NULL DEFAULT 2000 CHECK (max_bundle_file_count > 0),
    max_apps_per_owner integer NOT NULL DEFAULT 20 CHECK (max_apps_per_owner > 0),
    retained_revisions_per_app integer NOT NULL DEFAULT 10 CHECK (retained_revisions_per_app > 0),
    gateway_token_sha256 text NOT NULL DEFAULT '',
    gateway_token_rotated_at timestamptz,
    updated_by text NOT NULL DEFAULT '',
    updated_at timestamptz
);

CREATE TABLE IF NOT EXISTS public.hosted_apps (
    id text PRIMARY KEY,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    kind text NOT NULL DEFAULT 'static' CHECK (kind IN ('static', 'dynamic')),
    dns_label text NOT NULL UNIQUE,
    spa_fallback boolean NOT NULL DEFAULT false,
    owner_user_id text NOT NULL,
    maintainer_user_ids jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(maintainer_user_ids) = 'array'),
    visibility_mode text NOT NULL DEFAULT 'private' CHECK (visibility_mode IN ('private', 'selected', 'organization')),
    visibility_user_ids jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(visibility_user_ids) = 'array'),
    visibility_team_ids jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(visibility_team_ids) = 'array'),
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'online', 'offline', 'suspended_by_admin')),
    suspended_reason text NOT NULL DEFAULT '',
    active_revision_id text NOT NULL DEFAULT '',
    route_generation bigint NOT NULL DEFAULT 1 CHECK (route_generation >= 1),
    idempotency_key text NOT NULL DEFAULT '',
    request_fingerprint text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_hosted_apps_owner_idempotency
    ON public.hosted_apps (owner_user_id, idempotency_key)
    WHERE idempotency_key <> '';

CREATE INDEX IF NOT EXISTS idx_hosted_apps_owner_updated_at
    ON public.hosted_apps (owner_user_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_hosted_apps_status_updated_at
    ON public.hosted_apps (status, updated_at DESC);

CREATE TABLE IF NOT EXISTS public.hosted_app_revisions (
    id text PRIMARY KEY,
    app_id text NOT NULL REFERENCES public.hosted_apps(id) ON DELETE CASCADE,
    revision_number bigint NOT NULL CHECK (revision_number >= 1),
    description text NOT NULL DEFAULT '',
    kind text NOT NULL DEFAULT 'static' CHECK (kind IN ('static', 'dynamic')),
    status text NOT NULL CHECK (status IN ('uploading', 'validating', 'ready', 'failed', 'evicted')),
    bundle_sha256 text NOT NULL DEFAULT '',
    file_count integer NOT NULL DEFAULT 0 CHECK (file_count >= 0),
    total_bytes bigint NOT NULL DEFAULT 0 CHECK (total_bytes >= 0),
    spa_fallback boolean NOT NULL DEFAULT false,
    storage_integration_id text NOT NULL DEFAULT '',
    storage_prefix text NOT NULL DEFAULT '',
    manifest_json jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(manifest_json) = 'array'),
    failure_stage text NOT NULL DEFAULT '',
    failure_reason text NOT NULL DEFAULT '',
    idempotency_key text NOT NULL DEFAULT '',
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    activated_at timestamptz,
    UNIQUE (app_id, revision_number)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_hosted_app_revisions_app_idempotency
    ON public.hosted_app_revisions (app_id, idempotency_key)
    WHERE idempotency_key <> '';

CREATE INDEX IF NOT EXISTS idx_hosted_app_revisions_app_number
    ON public.hosted_app_revisions (app_id, revision_number DESC);

CREATE TABLE IF NOT EXISTS public.hosted_app_auth_tickets (
    ticket_sha256 text PRIMARY KEY CHECK (ticket_sha256 ~ '^[a-f0-9]{64}$'),
    app_id text NOT NULL REFERENCES public.hosted_apps(id) ON DELETE CASCADE,
    dns_label text NOT NULL,
    host text NOT NULL,
    route_generation bigint NOT NULL CHECK (route_generation >= 1),
    return_path text NOT NULL DEFAULT '/',
    source_session_id text NOT NULL DEFAULT '',
    gateway_state_sha256 text NOT NULL DEFAULT '',
    user_id text NOT NULL,
    username text NOT NULL,
    display_name text NOT NULL,
    email text NOT NULL DEFAULT '',
    teams_json jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(teams_json) = 'array'),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_hosted_app_auth_tickets_expires_at
    ON public.hosted_app_auth_tickets (expires_at);

CREATE TABLE IF NOT EXISTS public.hosted_app_rollback_idempotency (
    app_id text NOT NULL REFERENCES public.hosted_apps(id) ON DELETE CASCADE,
    idempotency_key text NOT NULL,
    target_revision_id text NOT NULL,
    route_generation bigint NOT NULL CHECK (route_generation >= 1),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (app_id, idempotency_key)
);
