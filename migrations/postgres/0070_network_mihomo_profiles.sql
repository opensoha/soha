SELECT pg_catalog.set_config('search_path', '', false);

CREATE TABLE IF NOT EXISTS public.network_mihomo_profiles (
    id text PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    workspace_id text NOT NULL DEFAULT 'default',
    device_id text NOT NULL REFERENCES public.network_access_devices(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    mode text NOT NULL CHECK (mode IN ('managed_follow', 'app_subscription')),
    status text NOT NULL CHECK (status IN ('active', 'disabled')),
    subscription_url_ciphertext text,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision >= 1),
    mixed_port integer NOT NULL CHECK (mixed_port BETWEEN 1 AND 65535),
    controller_port integer NOT NULL CHECK (controller_port BETWEEN 1 AND 65535),
    dns_mode text NOT NULL CHECK (dns_mode IN ('disabled', 'fake_ip')),
    fake_ip_range cidr,
    selector_group text NOT NULL CHECK (length(selector_group) BETWEEN 1 AND 128),
    selected_proxy text,
    bypass_cidrs jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(bypass_cidrs) = 'array'),
    bypass_hosts jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(bypass_hosts) = 'array'),
    fail_closed boolean NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (mixed_port <> controller_port),
    CHECK ((dns_mode = 'fake_ip' AND fake_ip_range IS NOT NULL) OR (dns_mode = 'disabled' AND fake_ip_range IS NULL)),
    CHECK ((mode = 'managed_follow' AND subscription_url_ciphertext IS NOT NULL AND selected_proxy IS NOT NULL AND fail_closed)
        OR (mode = 'app_subscription' AND subscription_url_ciphertext IS NULL AND selected_proxy IS NULL))
);

CREATE UNIQUE INDEX IF NOT EXISTS network_mihomo_profiles_device_active_uidx
    ON public.network_mihomo_profiles (tenant_id, workspace_id, device_id)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS network_mihomo_profiles_device_idx
    ON public.network_mihomo_profiles (tenant_id, workspace_id, device_id, status);
