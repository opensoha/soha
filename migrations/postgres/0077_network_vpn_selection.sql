SELECT pg_catalog.set_config('search_path', '', false);

ALTER TABLE public.network_access_gateways
    ADD COLUMN region text NOT NULL DEFAULT '',
    ADD COLUMN provider_code text NOT NULL DEFAULT '',
    ADD COLUMN provider_name text NOT NULL DEFAULT '',
    ADD COLUMN selection_priority integer NOT NULL DEFAULT 100 CHECK (selection_priority BETWEEN 0 AND 10000),
    ADD COLUMN accept_new_connections boolean NOT NULL DEFAULT true,
    ADD COLUMN max_sessions integer NOT NULL DEFAULT 0 CHECK (max_sessions BETWEEN 0 AND 1000000),
    ADD COLUMN probe_url text NOT NULL DEFAULT '';

-- Two typed configuration resources share the same immutable revision lifecycle.
CREATE TABLE public.network_vpn_documents (
    id text PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    workspace_id text NOT NULL DEFAULT 'default',
    kind text NOT NULL CHECK (kind IN ('profile', 'selection-policy')),
    revision integer NOT NULL CHECK (revision >= 1),
    published_revision integer NOT NULL DEFAULT 0 CHECK (published_revision >= 0),
    configuration jsonb NOT NULL CHECK (jsonb_typeof(configuration) = 'object'),
    published_configuration jsonb CHECK (jsonb_typeof(published_configuration) = 'object'),
    deleted_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (published_revision <= revision),
    CHECK ((published_revision = 0 AND published_configuration IS NULL)
        OR (published_revision > 0 AND published_configuration IS NOT NULL))
);
CREATE INDEX idx_network_vpn_documents_kind
    ON public.network_vpn_documents (tenant_id, workspace_id, kind, id) WHERE deleted_at IS NULL;

CREATE TABLE public.network_vpn_document_revisions (
    document_id text NOT NULL REFERENCES public.network_vpn_documents(id) ON DELETE RESTRICT,
    revision integer NOT NULL CHECK (revision >= 1),
    configuration jsonb NOT NULL CHECK (jsonb_typeof(configuration) = 'object'),
    created_at timestamptz NOT NULL,
    created_by text NOT NULL,
    PRIMARY KEY (document_id, revision)
);

CREATE TABLE public.network_vpn_connection_intents (
    id text PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    workspace_id text NOT NULL DEFAULT 'default',
    runtime_id text NOT NULL,
    credential_id text NOT NULL REFERENCES public.network_runtime_credentials(id) ON DELETE RESTRICT,
    subject_id text NOT NULL,
    device_id text NOT NULL REFERENCES public.network_access_devices(id) ON DELETE RESTRICT,
    auth_session_id text NOT NULL,
    profile_id text NOT NULL REFERENCES public.network_vpn_documents(id) ON DELETE RESTRICT,
    profile_revision integer NOT NULL CHECK (profile_revision >= 1),
    selection_policy_id text NOT NULL REFERENCES public.network_vpn_documents(id) ON DELETE RESTRICT,
    selection_policy_revision integer NOT NULL CHECK (selection_policy_revision >= 1),
    selection text NOT NULL CHECK (selection IN ('auto', 'manual')),
    requested_gateway_id text REFERENCES public.network_access_gateways(id) ON DELETE RESTRICT,
    token_hash text,
    status text NOT NULL CHECK (status IN ('issued', 'consumed', 'rejected', 'expired')),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    consumed_at timestamptz,
    session_id text REFERENCES public.network_runtime_sessions(id) ON DELETE RESTRICT,
    request_id text,
    request_hash text,
    managed_result jsonb CHECK (jsonb_typeof(managed_result) = 'object'),
    CHECK (expires_at > created_at AND expires_at <= created_at + interval '5 minutes'),
    CHECK ((selection = 'auto' AND requested_gateway_id IS NULL) OR (selection = 'manual' AND requested_gateway_id IS NOT NULL)),
    CHECK ((status = 'issued' AND token_hash ~ '^sha256:[a-f0-9]{64}$' AND consumed_at IS NULL AND session_id IS NULL)
        OR (status = 'consumed' AND token_hash IS NULL AND consumed_at IS NOT NULL AND session_id IS NOT NULL)
        OR (status IN ('rejected', 'expired') AND token_hash IS NULL AND session_id IS NULL))
);
CREATE INDEX idx_network_vpn_intents_owner
    ON public.network_vpn_connection_intents (tenant_id, workspace_id, runtime_id, expires_at);

CREATE TABLE public.network_vpn_decisions (
    id text PRIMARY KEY REFERENCES public.network_vpn_connection_intents(id) ON DELETE RESTRICT,
    tenant_id text NOT NULL DEFAULT 'default',
    workspace_id text NOT NULL DEFAULT 'default',
    profile_id text NOT NULL REFERENCES public.network_vpn_documents(id) ON DELETE RESTRICT,
    site_id text NOT NULL REFERENCES public.network_access_sites(id) ON DELETE RESTRICT,
    network_space_id text NOT NULL REFERENCES public.network_access_spaces(id) ON DELETE RESTRICT,
    subject_id text NOT NULL,
    device_id text NOT NULL,
    gateway_id text REFERENCES public.network_access_gateways(id) ON DELETE RESTRICT,
    session_id text REFERENCES public.network_runtime_sessions(id) ON DELETE RESTRICT,
    state text NOT NULL CHECK (state IN ('selected', 'connecting', 'connected', 'failed', 'disconnected')),
    established_at timestamptz,
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
CREATE INDEX idx_network_vpn_decisions_scope
    ON public.network_vpn_decisions (tenant_id, workspace_id, site_id, profile_id, created_at DESC);
CREATE INDEX idx_network_vpn_decisions_session ON public.network_vpn_decisions(session_id);
CREATE INDEX idx_network_vpn_decisions_device ON public.network_vpn_decisions(device_id, created_at DESC);

ALTER TABLE public.network_runtime_sessions
    ADD COLUMN vpn_profile_id text REFERENCES public.network_vpn_documents(id) ON DELETE RESTRICT,
    ADD COLUMN vpn_profile_revision integer,
    ADD COLUMN vpn_decision_id text,
    ADD COLUMN vpn_selection text CHECK (vpn_selection IN ('auto', 'manual'));
