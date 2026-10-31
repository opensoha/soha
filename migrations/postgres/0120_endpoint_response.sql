-- +goose Up
ALTER TABLE public.network_nas_authorizations ADD COLUMN IF NOT EXISTS credential_id text;
ALTER TABLE public.network_nas_authorizations ADD COLUMN IF NOT EXISTS station_id text;
ALTER TABLE public.network_access_nas_bindings ADD COLUMN IF NOT EXISTS eap_server_trust jsonb;
ALTER TABLE public.network_nas_session_commands ADD COLUMN IF NOT EXISTS response_event_id text;
ALTER TABLE public.network_nas_session_commands ADD COLUMN IF NOT EXISTS response_epoch bigint NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS public.network_endpoint_presence (
    tenant_id text NOT NULL DEFAULT 'default', workspace_id text NOT NULL DEFAULT 'default',
    device_id text NOT NULL, runtime_id text NOT NULL, credential_id text NOT NULL,
    boot_id text NOT NULL, sequence bigint NOT NULL CHECK (sequence > 0), payload_hash text NOT NULL,
    presence_epoch bigint NOT NULL CHECK (presence_epoch > 0), service_state text NOT NULL,
    ui_state text NOT NULL, configuration_version bigint NOT NULL, policy_version bigint NOT NULL,
    observed_at timestamptz NOT NULL, received_at timestamptz NOT NULL,
    healthy_since timestamptz, healthy_samples integer NOT NULL DEFAULT 0,
    ui_observed_at timestamptz, ui_healthy_since timestamptz, ui_healthy_samples integer NOT NULL DEFAULT 0,
    heartbeat_interval_seconds integer NOT NULL CHECK (heartbeat_interval_seconds BETWEEN 30 AND 300),
    PRIMARY KEY (tenant_id, workspace_id, device_id)
);
CREATE TABLE IF NOT EXISTS public.network_endpoint_presence_boots (
    tenant_id text NOT NULL DEFAULT 'default', workspace_id text NOT NULL DEFAULT 'default',
    credential_id text NOT NULL, boot_id text NOT NULL, retired_at timestamptz,
    PRIMARY KEY (tenant_id, workspace_id, credential_id, boot_id)
);

CREATE TABLE IF NOT EXISTS public.network_nas_session_evidence (
    tenant_id text NOT NULL DEFAULT 'default', workspace_id text NOT NULL DEFAULT 'default',
    session_id text NOT NULL, runtime_id text NOT NULL, nas_id text NOT NULL,
    credential_id text NOT NULL, subject_id text NOT NULL, device_id text NOT NULL, station_id text NOT NULL,
    accounting_session_id text NOT NULL, state text NOT NULL,
    started_at timestamptz, stopped_at timestamptz,
    observed_at timestamptz NOT NULL, received_at timestamptz NOT NULL,
    last_status text NOT NULL, last_session_seconds bigint NOT NULL,
    observed_vlan_id integer, observed_filter_id text,
    nas_port_id text,
    accounting_epoch timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, workspace_id, session_id),
    UNIQUE (tenant_id, workspace_id, runtime_id, nas_id, accounting_epoch, accounting_session_id)
);
CREATE TABLE IF NOT EXISTS public.network_nas_accounting_epochs (
    tenant_id text NOT NULL DEFAULT 'default', workspace_id text NOT NULL DEFAULT 'default',
    runtime_id text NOT NULL, nas_id text NOT NULL,
    observed_at timestamptz NOT NULL, received_at timestamptz NOT NULL, status text NOT NULL,
    PRIMARY KEY (tenant_id, workspace_id, runtime_id, nas_id)
);

CREATE TABLE IF NOT EXISTS public.network_endpoint_response_policies (
    tenant_id text NOT NULL DEFAULT 'default', workspace_id text NOT NULL DEFAULT 'default',
    id text NOT NULL, revision integer NOT NULL CHECK (revision > 0),
    published_revision integer NOT NULL DEFAULT 0,
    configuration jsonb NOT NULL, published_configuration jsonb,
    created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL, published_at timestamptz, deleted_at timestamptz,
    PRIMARY KEY (tenant_id, workspace_id, id)
);
CREATE TABLE IF NOT EXISTS public.network_endpoint_response_policy_revisions (
    tenant_id text NOT NULL DEFAULT 'default', workspace_id text NOT NULL DEFAULT 'default',
    policy_id text NOT NULL, revision integer NOT NULL, configuration jsonb NOT NULL,
    actor_id text NOT NULL, created_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, workspace_id, policy_id, revision)
);
CREATE TABLE IF NOT EXISTS public.network_endpoint_response_epochs (
    tenant_id text NOT NULL DEFAULT 'default', workspace_id text NOT NULL DEFAULT 'default',
    device_id text NOT NULL, site_id text NOT NULL, epoch bigint NOT NULL CHECK (epoch > 0),
    PRIMARY KEY (tenant_id, workspace_id, device_id, site_id)
);
CREATE TABLE IF NOT EXISTS public.network_endpoint_response_events (
    tenant_id text NOT NULL DEFAULT 'default', workspace_id text NOT NULL DEFAULT 'default',
    id text NOT NULL, policy_id text NOT NULL, policy_revision integer NOT NULL,
    device_id text NOT NULL, site_id text NOT NULL, session_id text NOT NULL,
    revision integer NOT NULL CHECK (revision > 0), status text NOT NULL,
    restriction_profile text CHECK (restriction_profile IN ('restricted', 'quarantine', 'deny')),
    event jsonb NOT NULL, notification_policy_id text,
    notification_attempts integer NOT NULL DEFAULT 0, notification_lease_until timestamptz,
    triggered_at timestamptz NOT NULL, updated_at timestamptz NOT NULL, restored_at timestamptz,
    PRIMARY KEY (tenant_id, workspace_id, id)
);
CREATE UNIQUE INDEX IF NOT EXISTS network_endpoint_response_events_current
    ON public.network_endpoint_response_events (tenant_id, workspace_id, policy_id, policy_revision, session_id)
    WHERE restored_at IS NULL;
CREATE INDEX IF NOT EXISTS network_endpoint_response_events_active
    ON public.network_endpoint_response_events (device_id, site_id, updated_at)
    WHERE restored_at IS NULL;
CREATE TABLE IF NOT EXISTS public.network_endpoint_maintenance_windows (
    tenant_id text NOT NULL DEFAULT 'default', workspace_id text NOT NULL DEFAULT 'default',
    id text NOT NULL, device_id text NOT NULL, site_id text NOT NULL,
    reason text NOT NULL, actor_id text NOT NULL, created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL, ended_at timestamptz,
    PRIMARY KEY (tenant_id, workspace_id, id)
);

-- +goose Down
-- Keep security restrictions and their epochs through an application rollback.
