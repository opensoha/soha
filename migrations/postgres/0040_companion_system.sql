BEGIN;

CREATE TABLE IF NOT EXISTS public.companion_artifacts (
    plugin_id text NOT NULL,
    version text NOT NULL,
    sha256 text NOT NULL,
    size_bytes bigint NOT NULL,
    checksum_status text NOT NULL,
    signature_status text NOT NULL,
    provenance_status text NOT NULL,
    storage_digest text NOT NULL,
    assets jsonb DEFAULT '[]'::jsonb NOT NULL,
    pack_manifest jsonb DEFAULT '{}'::jsonb NOT NULL,
    active boolean DEFAULT false NOT NULL,
    installed_at timestamp without time zone NOT NULL,
    retired_at timestamp without time zone,
    CONSTRAINT companion_artifacts_pkey PRIMARY KEY (plugin_id, version),
    CONSTRAINT companion_artifacts_size_positive CHECK (size_bytes > 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_companion_artifacts_active
    ON public.companion_artifacts (plugin_id)
    WHERE active = true AND retired_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_companion_artifacts_digest
    ON public.companion_artifacts (storage_digest);

CREATE TABLE IF NOT EXISTS public.companion_profiles (
    owner_id text PRIMARY KEY,
    id text NOT NULL,
    active_plugin_id text NOT NULL,
    active_version text NOT NULL,
    level integer DEFAULT 1 NOT NULL,
    xp integer DEFAULT 0 NOT NULL,
    affinity integer DEFAULT 0 NOT NULL,
    unlocked_ids jsonb DEFAULT '[]'::jsonb NOT NULL,
    revision bigint DEFAULT 1 NOT NULL,
    last_interaction_at timestamp without time zone,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL,
    CONSTRAINT companion_profiles_level_range CHECK (level BETWEEN 1 AND 100),
    CONSTRAINT companion_profiles_xp_nonnegative CHECK (xp >= 0),
    CONSTRAINT companion_profiles_affinity_nonnegative CHECK (affinity >= 0),
    CONSTRAINT companion_profiles_revision_positive CHECK (revision > 0)
);

CREATE TABLE IF NOT EXISTS public.companion_idempotency_receipts (
    owner_id text NOT NULL,
    idempotency_key text NOT NULL,
    operation_kind text NOT NULL,
    input_hash text NOT NULL,
    response jsonb NOT NULL,
    created_at timestamp without time zone NOT NULL,
    CONSTRAINT companion_idempotency_receipts_pkey PRIMARY KEY (owner_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_companion_receipts_created_at
    ON public.companion_idempotency_receipts (created_at);

CREATE TABLE IF NOT EXISTS public.companion_interaction_states (
    owner_id text NOT NULL,
    plugin_id text NOT NULL,
    interaction_id text NOT NULL,
    last_interaction_at timestamp without time zone NOT NULL,
    CONSTRAINT companion_interaction_states_pkey PRIMARY KEY (owner_id, plugin_id, interaction_id)
);

COMMIT;
