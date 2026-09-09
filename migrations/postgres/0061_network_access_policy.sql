SELECT pg_catalog.set_config('search_path', '', false);

CREATE TABLE IF NOT EXISTS public.network_access_policies (
    id text PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    workspace_id text NOT NULL DEFAULT 'default',
    name text NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    priority integer NOT NULL CHECK (priority BETWEEN 1 AND 10000),
    effect text NOT NULL CHECK (effect IN ('allow', 'deny')),
    subjects jsonb NOT NULL CHECK (jsonb_typeof(subjects) = 'object'),
    site_ids jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(site_ids) = 'array' AND jsonb_array_length(site_ids) <= 128),
    resource_ids jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(resource_ids) = 'array' AND jsonb_array_length(resource_ids) <= 256),
    modes jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(modes) = 'array' AND jsonb_array_length(modes) <= 5),
    device_statuses jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(device_statuses) = 'array' AND jsonb_array_length(device_statuses) <= 4),
    posture_statuses jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(posture_statuses) = 'array' AND jsonb_array_length(posture_statuses) <= 3),
    access_profile text NOT NULL CHECK (access_profile IN ('onboarding', 'full', 'restricted', 'quarantine', 'deny')),
    version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, workspace_id, name),
    CHECK ((effect = 'deny' AND access_profile = 'deny') OR (effect = 'allow' AND access_profile <> 'deny'))
);

CREATE TABLE IF NOT EXISTS public.network_access_policy_snapshots (
    policy_version bigserial PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    workspace_id text NOT NULL DEFAULT 'default',
    content_hash text NOT NULL CHECK (content_hash ~ '^sha256:[a-f0-9]{64}$'),
    policies jsonb NOT NULL CHECK (jsonb_typeof(policies) = 'array'),
    protected_resource_ids jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(protected_resource_ids) = 'array'),
    policy_count integer NOT NULL CHECK (policy_count >= 0),
    protected_resource_count integer NOT NULL CHECK (protected_resource_count >= 0),
    published_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_network_access_policies_scope
    ON public.network_access_policies (tenant_id, workspace_id, enabled, priority, id);
CREATE INDEX IF NOT EXISTS idx_network_access_policy_snapshots_scope
    ON public.network_access_policy_snapshots (tenant_id, workspace_id, policy_version DESC);
