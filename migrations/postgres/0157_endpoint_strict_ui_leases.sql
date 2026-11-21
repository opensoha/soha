SELECT pg_catalog.set_config('search_path', '', false);

ALTER TABLE public.network_endpoint_presence
    ADD COLUMN ui_lease_subject_id text NOT NULL DEFAULT '',
    ADD COLUMN ui_lease_epoch text NOT NULL DEFAULT '',
    ADD COLUMN ui_lease_until timestamptz,
    ADD CONSTRAINT network_endpoint_presence_ui_lease_complete CHECK (
        (ui_lease_subject_id = '' AND ui_lease_epoch = '' AND ui_lease_until IS NULL)
        OR (ui_lease_subject_id <> '' AND ui_lease_epoch ~ '^[0-9a-f]{64}$' AND ui_lease_until IS NOT NULL)
    );

ALTER TABLE public.network_access_nas_bindings
    ADD COLUMN strict_online_qualified boolean NOT NULL DEFAULT false;

ALTER TABLE public.network_nas_authorizations
    ADD COLUMN strict_ui_epoch text NOT NULL DEFAULT '',
    ADD CONSTRAINT network_nas_authorizations_strict_ui_epoch_format CHECK (
        strict_ui_epoch = '' OR strict_ui_epoch ~ '^[0-9a-f]{64}$'
    );

CREATE INDEX idx_network_nas_authorizations_station_order
    ON public.network_nas_authorizations (tenant_id, workspace_id, runtime_id, nas_id, station_id, created_at DESC, session_id DESC)
    WHERE decision = 'allow' AND station_id IS NOT NULL;
