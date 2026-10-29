SELECT pg_catalog.set_config('search_path', '', false);

CREATE TABLE public.network_site_dns_mappings (
    tenant_id text NOT NULL DEFAULT 'default',
    workspace_id text NOT NULL DEFAULT 'default',
    resource_id text NOT NULL REFERENCES public.network_access_resources(id) ON DELETE CASCADE,
    runtime_id text NOT NULL,
    credential_id text NOT NULL REFERENCES public.network_runtime_credentials(id) ON DELETE CASCADE,
    site_id text NOT NULL REFERENCES public.network_access_sites(id) ON DELETE CASCADE,
    hostname text NOT NULL,
    addresses jsonb NOT NULL CHECK (jsonb_typeof(addresses) = 'array' AND jsonb_array_length(addresses) BETWEEN 1 AND 32),
    fingerprint text NOT NULL,
    resolvers jsonb NOT NULL CHECK(jsonb_typeof(resolvers)='array' AND jsonb_array_length(resolvers) BETWEEN 1 AND 8),
    policy_version bigint NOT NULL CHECK (policy_version > 0),
    configuration_version bigint NOT NULL CHECK (configuration_version > 0),
    observed_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > observed_at AND expires_at <= observed_at + interval '5 minutes'),
    PRIMARY KEY (tenant_id, workspace_id, resource_id, runtime_id)
);
CREATE INDEX idx_network_site_dns_expiry ON public.network_site_dns_mappings(expires_at);
