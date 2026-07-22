SELECT pg_catalog.set_config('search_path', '', false);

CREATE TABLE IF NOT EXISTS public.system_integrations (
    id text PRIMARY KEY,
    category text NOT NULL,
    provider_type text NOT NULL,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    enabled boolean NOT NULL DEFAULT true,
    configuration jsonb NOT NULL DEFAULT '{}'::jsonb,
    health_status text NOT NULL DEFAULT 'unknown',
    last_checked_at timestamptz,
    last_error text NOT NULL DEFAULT '',
    version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_by text NOT NULL DEFAULT '',
    updated_by text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT system_integrations_health_status_check CHECK (health_status IN ('unknown', 'healthy', 'unhealthy'))
);

CREATE INDEX IF NOT EXISTS idx_system_integrations_category_provider
    ON public.system_integrations (category, provider_type, name);

CREATE TABLE IF NOT EXISTS public.system_integration_credentials (
    integration_id text NOT NULL REFERENCES public.system_integrations(id) ON DELETE CASCADE,
    credential_key text NOT NULL,
    value_encrypted text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (integration_id, credential_key)
);
