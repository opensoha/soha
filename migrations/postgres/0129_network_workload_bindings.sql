SELECT pg_catalog.set_config('search_path', '', false);

CREATE TABLE public.network_workload_bindings (
 id text PRIMARY KEY,
 tenant_id text NOT NULL DEFAULT 'default', workspace_id text NOT NULL DEFAULT 'default',
 service_account_id text NOT NULL REFERENCES public.service_accounts(id) ON DELETE RESTRICT,
 runtime_id text NOT NULL, device_id text NOT NULL REFERENCES public.network_access_devices(id) ON DELETE RESTRICT,
 site_id text NOT NULL REFERENCES public.network_access_sites(id) ON DELETE RESTRICT,
 network_space_id text NOT NULL REFERENCES public.network_access_spaces(id) ON DELETE RESTRICT,
 resource_ids jsonb NOT NULL CHECK(jsonb_typeof(resource_ids)='array' AND jsonb_array_length(resource_ids) BETWEEN 1 AND 256),
 enabled boolean NOT NULL, expires_at timestamptz NOT NULL, revision integer NOT NULL CHECK(revision>0),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,workspace_id,runtime_id)
);
ALTER TABLE public.network_runtime_enrollments ADD COLUMN source_token_id text;
ALTER TABLE public.network_runtime_credentials ADD COLUMN source_token_id text;
