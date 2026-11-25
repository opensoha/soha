SELECT pg_catalog.set_config('search_path', '', false);

-- Soft-deleted rows are tombstones: configuration and kind stay intact so a
-- site's RADIUS-resource ownership remains known after the last row is deleted.
CREATE TABLE IF NOT EXISTS public.network_access_networks (
    id text PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    workspace_id text NOT NULL DEFAULT 'default',
    site_id text NOT NULL REFERENCES public.network_access_sites(id) ON DELETE CASCADE,
    configuration jsonb NOT NULL CHECK (jsonb_typeof(configuration) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    activated boolean NOT NULL DEFAULT false,
    deleted_at timestamptz
);

CREATE INDEX IF NOT EXISTS idx_network_access_networks_scope
    ON public.network_access_networks (tenant_id, workspace_id, site_id, id);
