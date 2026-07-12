SELECT pg_catalog.set_config('search_path', '', false);

CREATE TABLE IF NOT EXISTS public.directory_connections (
    id text PRIMARY KEY, name text NOT NULL, provider_type text NOT NULL,
    login_provider_id text, credential_ref text, enabled boolean NOT NULL DEFAULT true,
    capabilities jsonb NOT NULL DEFAULT '{}'::jsonb, status text NOT NULL DEFAULT 'pending',
    last_validated_at timestamptz, metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_by text NOT NULL DEFAULT '', updated_by text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS public.directory_sync_policies (
    connection_id text PRIMARY KEY REFERENCES public.directory_connections(id) ON DELETE CASCADE,
    sync_organizations boolean NOT NULL DEFAULT true CHECK (sync_organizations),
    sync_people boolean NOT NULL DEFAULT false,
    mode text NOT NULL DEFAULT 'scheduled', schedule text NOT NULL DEFAULT '',
    full_reconcile_schedule text NOT NULL DEFAULT '', provision_mode text NOT NULL DEFAULT 'review_before_link',
    trusted_email_domains jsonb NOT NULL DEFAULT '[]'::jsonb, verified_email_auto_link boolean NOT NULL DEFAULT false,
    user_disable_policy text NOT NULL DEFAULT 'managed_only', missing_object_policy text NOT NULL DEFAULT 'archive',
    field_mappings jsonb NOT NULL DEFAULT '{}'::jsonb, updated_by text NOT NULL DEFAULT '', updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS public.directory_sync_runs (
    id text PRIMARY KEY, connection_id text NOT NULL REFERENCES public.directory_connections(id) ON DELETE CASCADE,
    trigger text NOT NULL, mode text NOT NULL, include_people boolean NOT NULL DEFAULT false, status text NOT NULL,
    cursor_before text NOT NULL DEFAULT '', cursor_after text NOT NULL DEFAULT '', idempotency_key text,
    stats jsonb NOT NULL DEFAULT '{}'::jsonb, error_code text NOT NULL DEFAULT '', error_summary text NOT NULL DEFAULT '',
    requested_by text NOT NULL DEFAULT '', started_at timestamptz, heartbeat_at timestamptz, finished_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_directory_runs_idempotency ON public.directory_sync_runs(connection_id,idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_directory_runs_one_active ON public.directory_sync_runs(connection_id) WHERE status IN ('queued','running');

CREATE TABLE IF NOT EXISTS public.directory_organizations (
    id text PRIMARY KEY, connection_id text NOT NULL REFERENCES public.directory_connections(id) ON DELETE CASCADE,
    external_id text NOT NULL, external_parent_id text, local_team_id text, name text NOT NULL, path text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'active', source_version text NOT NULL DEFAULT '', raw_hash text NOT NULL DEFAULT '',
    first_seen_at timestamptz NOT NULL, last_seen_at timestamptz NOT NULL, archived_at timestamptz,
    UNIQUE(connection_id,external_id)
);

CREATE TABLE IF NOT EXISTS public.directory_people (
    id text PRIMARY KEY, connection_id text NOT NULL REFERENCES public.directory_connections(id) ON DELETE CASCADE,
    external_id text NOT NULL, provider_subject text NOT NULL DEFAULT '', local_user_id text,
    username text NOT NULL DEFAULT '', display_name text NOT NULL DEFAULT '', email text NOT NULL DEFAULT '',
    email_verified boolean NOT NULL DEFAULT false, phone text NOT NULL DEFAULT '', avatar_url text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'active', source_version text NOT NULL DEFAULT '', raw_hash text NOT NULL DEFAULT '',
    first_seen_at timestamptz NOT NULL, last_seen_at timestamptz NOT NULL, archived_at timestamptz,
    UNIQUE(connection_id,external_id)
);

CREATE TABLE IF NOT EXISTS public.directory_memberships (
    connection_id text NOT NULL REFERENCES public.directory_connections(id) ON DELETE CASCADE,
    external_person_id text NOT NULL, external_organization_id text NOT NULL,
    local_user_id text, local_team_id text, status text NOT NULL DEFAULT 'active', last_seen_at timestamptz NOT NULL,
    PRIMARY KEY(connection_id,external_person_id,external_organization_id)
);

CREATE TABLE IF NOT EXISTS public.identity_link_suppressions (
    id text PRIMARY KEY, user_id text NOT NULL, provider_type text NOT NULL, provider_id text NOT NULL,
    provider_user_id text NOT NULL, reason text NOT NULL DEFAULT '', created_by text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(), cleared_by text NOT NULL DEFAULT '', cleared_at timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_identity_link_suppressions_active
    ON public.identity_link_suppressions(user_id,provider_type,provider_id,provider_user_id) WHERE cleared_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_directory_organizations_connection_status ON public.directory_organizations(connection_id,status);
CREATE INDEX IF NOT EXISTS idx_directory_people_connection_status ON public.directory_people(connection_id,status);
