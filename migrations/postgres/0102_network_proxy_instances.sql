SELECT pg_catalog.set_config('search_path', '', false);

ALTER TABLE public.network_runtime_enrollments DROP CONSTRAINT IF EXISTS network_runtime_enrollments_runtime_kind_check;
ALTER TABLE public.network_runtime_enrollments ADD CONSTRAINT network_runtime_enrollments_runtime_kind_check CHECK (runtime_kind IN ('endpoint', 'gateway', 'nas', 'proxy'));
ALTER TABLE public.network_runtime_credentials DROP CONSTRAINT IF EXISTS network_runtime_credentials_runtime_kind_check;
ALTER TABLE public.network_runtime_credentials ADD CONSTRAINT network_runtime_credentials_runtime_kind_check CHECK (runtime_kind IN ('endpoint', 'gateway', 'nas', 'proxy'));

CREATE TABLE public.network_proxy_instances (
    id text PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    workspace_id text NOT NULL DEFAULT 'default',
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 128),
    engine text NOT NULL CHECK (engine IN ('mihomo', 'sing-box', 'v2ray')),
    host text NOT NULL DEFAULT '',
    enabled boolean NOT NULL DEFAULT true,
    desired_revision bigint NOT NULL DEFAULT 0 CHECK (desired_revision >= 0),
    desired_content_encrypted text NOT NULL DEFAULT '',
    desired_hash text NOT NULL DEFAULT '',
    observed_revision bigint NOT NULL DEFAULT 0 CHECK (observed_revision >= 0),
    engine_version text NOT NULL DEFAULT '',
    capabilities jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(capabilities) = 'array'),
    health text NOT NULL DEFAULT '' CHECK (health IN ('', 'healthy', 'degraded', 'unhealthy')),
    reason_code text NOT NULL DEFAULT '',
    last_seen_at timestamptz,
    last_sample_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (desired_revision = 0 OR (desired_content_encrypted <> '' AND desired_hash ~ '^sha256:[a-f0-9]{64}$'))
);

CREATE TABLE public.network_proxy_samples (
    instance_id text NOT NULL REFERENCES public.network_proxy_instances(id) ON DELETE CASCADE,
    observed_at timestamptz NOT NULL,
    uptime_seconds bigint NOT NULL CHECK (uptime_seconds >= 0),
    upload_total bigint NOT NULL CHECK (upload_total >= 0),
    download_total bigint NOT NULL CHECK (download_total >= 0),
    active_connections integer CHECK (active_connections >= 0),
    PRIMARY KEY (instance_id, observed_at)
);
CREATE INDEX idx_network_proxy_samples_retention ON public.network_proxy_samples (observed_at);

CREATE TABLE public.network_proxy_connections (
    instance_id text PRIMARY KEY REFERENCES public.network_proxy_instances(id) ON DELETE CASCADE,
    observed_at timestamptz NOT NULL,
    connections jsonb NOT NULL CHECK (jsonb_typeof(connections) = 'array' AND jsonb_array_length(connections) <= 200)
);

CREATE TABLE public.network_proxy_close_commands (
    id text PRIMARY KEY,
    instance_id text NOT NULL REFERENCES public.network_proxy_instances(id) ON DELETE CASCADE,
    connection_id text NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'closed', 'not-found', 'failed', 'expired')),
    reason_code text NOT NULL DEFAULT '',
    expires_at timestamptz NOT NULL,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz
);
CREATE INDEX idx_network_proxy_close_pending ON public.network_proxy_close_commands (instance_id, created_at) WHERE status = 'pending';
