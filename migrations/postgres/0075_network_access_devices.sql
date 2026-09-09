SELECT pg_catalog.set_config('search_path', '', false);

ALTER TABLE public.network_access_nas_bindings
    ADD COLUMN IF NOT EXISTS access_medium text,
    ADD COLUMN IF NOT EXISTS device_type text,
    ADD COLUMN IF NOT EXISTS ssid text,
    ADD COLUMN IF NOT EXISTS management_address text;

ALTER TABLE public.network_access_nas_bindings
    ADD CONSTRAINT network_access_nas_bindings_access_medium_check
        CHECK (access_medium IS NULL OR access_medium IN ('wifi', 'wired')),
    ADD CONSTRAINT network_access_nas_bindings_device_type_check
        CHECK (device_type IS NULL OR device_type IN ('wireless_controller', 'access_point', 'switch', 'other')),
    ADD CONSTRAINT network_access_nas_bindings_ssid_check
        CHECK (ssid IS NULL OR (access_medium = 'wifi' AND octet_length(ssid) BETWEEN 1 AND 32)),
    ADD CONSTRAINT network_access_nas_bindings_management_address_check
        CHECK (management_address IS NULL OR char_length(management_address) BETWEEN 1 AND 255);

CREATE INDEX IF NOT EXISTS idx_network_access_nas_bindings_medium
    ON public.network_access_nas_bindings (tenant_id, workspace_id, access_medium, site_id, status);
