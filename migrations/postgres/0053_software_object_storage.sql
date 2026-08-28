SELECT pg_catalog.set_config('search_path', '', false);

CREATE TABLE IF NOT EXISTS public.software_packages (
    id text PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    workspace_id text NOT NULL DEFAULT 'default',
    software_id text NOT NULL,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    publisher text NOT NULL,
    category text NOT NULL DEFAULT '',
    version text NOT NULL,
    platform text NOT NULL,
    arch text NOT NULL,
    file_name text NOT NULL,
    storage_integration_id text NOT NULL REFERENCES public.system_integrations(id) ON DELETE RESTRICT,
    object_key text NOT NULL UNIQUE,
    size_bytes bigint NOT NULL CHECK (size_bytes > 0 AND size_bytes <= 4294967296),
    sha256 text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    visibility text NOT NULL DEFAULT 'workspace' CHECK (visibility IN ('workspace', 'tenant', 'restricted')),
    status text NOT NULL DEFAULT 'ready' CHECK (status IN ('ready', 'quarantined', 'deleted')),
    created_by text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, workspace_id, software_id, version, platform, arch)
);

CREATE INDEX IF NOT EXISTS idx_software_packages_scope_created
    ON public.software_packages (tenant_id, workspace_id, status, created_at DESC, id DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_system_integrations_one_active_storage
    ON public.system_integrations (category, provider_type)
    WHERE category = 'storage' AND provider_type = 's3' AND enabled = true;
