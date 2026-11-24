SELECT pg_catalog.set_config('search_path', '', false);

CREATE INDEX IF NOT EXISTS idx_network_nas_authorizations_app_readback
    ON public.network_nas_authorizations (tenant_id, workspace_id, subject_id, device_id, access_network_id, created_at DESC, session_id DESC)
    WHERE access_network_id <> '' AND authentication_method = 'eap-tls' AND decision = 'allow';
