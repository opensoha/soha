SELECT pg_catalog.set_config('search_path', '', false);

CREATE TABLE IF NOT EXISTS public.network_runtime_enrollments (
    id text PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    workspace_id text NOT NULL DEFAULT 'default',
    challenge_id text NOT NULL,
    challenge_hash text NOT NULL CHECK (challenge_hash ~ '^sha256:[a-f0-9]{64}$'),
    runtime_id text NOT NULL,
    runtime_kind text NOT NULL CHECK (runtime_kind IN ('endpoint', 'gateway', 'nas')),
    device_id text NOT NULL,
    subject_id text NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'consumed', 'revoked')),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    revoked_at timestamptz,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, workspace_id, challenge_id)
);

CREATE TABLE IF NOT EXISTS public.network_runtime_credentials (
    id text PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    workspace_id text NOT NULL DEFAULT 'default',
    enrollment_id text NOT NULL REFERENCES public.network_runtime_enrollments(id),
    runtime_id text NOT NULL,
    runtime_kind text NOT NULL CHECK (runtime_kind IN ('endpoint', 'gateway', 'nas')),
    device_id text NOT NULL,
    subject_id text NOT NULL,
    certificate_fingerprint text NOT NULL CHECK (certificate_fingerprint ~ '^sha256:[a-f0-9]{64}$'),
    public_key_fingerprint text NOT NULL CHECK (public_key_fingerprint ~ '^sha256:[a-f0-9]{64}$'),
    certificate_serial text NOT NULL,
    generation integer NOT NULL CHECK (generation >= 1),
    capabilities jsonb NOT NULL CHECK (jsonb_typeof(capabilities) = 'array' AND jsonb_array_length(capabilities) BETWEEN 1 AND 32),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked', 'expired')),
    not_before timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    last_control_seen_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, workspace_id, runtime_id, generation),
    UNIQUE (tenant_id, workspace_id, certificate_fingerprint),
    CHECK (expires_at > not_before)
);

CREATE TABLE IF NOT EXISTS public.network_runtime_sessions (
    id text PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    workspace_id text NOT NULL DEFAULT 'default',
    runtime_id text NOT NULL,
    subject_id text NOT NULL,
    device_id text NOT NULL,
    site_id text,
    mode text NOT NULL CHECK (mode IN ('internal_direct', 'internal_ztna', 'external_vpn', 'external_vpn_ztna', 'external_direct_ztna')),
    access_profile text NOT NULL CHECK (access_profile IN ('onboarding', 'full', 'restricted', 'quarantine', 'deny')),
    status text NOT NULL CHECK (status IN ('pending', 'active', 'restricted', 'quarantine', 'revoked', 'expired')),
    policy_version bigint NOT NULL CHECK (policy_version >= 1),
    configuration_version bigint NOT NULL DEFAULT 0 CHECK (configuration_version >= 0),
    posture_version bigint NOT NULL DEFAULT 0 CHECK (posture_version >= 0),
    valid_until timestamptz NOT NULL,
    last_control_seen_at timestamptz,
    revoked_at timestamptz,
    revoke_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS public.network_runtime_leases (
    id text PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    workspace_id text NOT NULL DEFAULT 'default',
    session_id text NOT NULL REFERENCES public.network_runtime_sessions(id),
    lease_kind text NOT NULL CHECK (lease_kind IN ('network', 'resource')),
    subject_id text NOT NULL,
    device_id text NOT NULL,
    network_space_id text,
    cidrs jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(cidrs) = 'array' AND jsonb_array_length(cidrs) <= 128),
    resource_ids jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(resource_ids) = 'array' AND jsonb_array_length(resource_ids) <= 256),
    policy_version bigint NOT NULL CHECK (policy_version >= 1),
    status text NOT NULL CHECK (status IN ('issued', 'active', 'revoking', 'revoked', 'expired')),
    issued_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    revoke_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at > issued_at),
    CHECK ((lease_kind = 'network' AND network_space_id IS NOT NULL AND jsonb_array_length(cidrs) > 0 AND jsonb_array_length(resource_ids) = 0)
        OR (lease_kind = 'resource' AND network_space_id IS NULL AND jsonb_array_length(cidrs) = 0 AND jsonb_array_length(resource_ids) > 0))
);

CREATE TABLE IF NOT EXISTS public.network_runtime_configurations (
    tenant_id text NOT NULL DEFAULT 'default',
    workspace_id text NOT NULL DEFAULT 'default',
    runtime_id text NOT NULL,
    configuration_version bigint NOT NULL CHECK (configuration_version >= 1),
    policy_version bigint NOT NULL CHECK (policy_version >= 1),
    desired_payload jsonb NOT NULL CHECK (jsonb_typeof(desired_payload) = 'object'),
    desired_hash text NOT NULL CHECK (desired_hash ~ '^sha256:[a-f0-9]{64}$'),
    valid_until timestamptz NOT NULL,
    apply_status text NOT NULL DEFAULT 'pending' CHECK (apply_status IN ('pending', 'applied', 'rejected', 'rolled-back')),
    readback_hash text CHECK (readback_hash IS NULL OR readback_hash ~ '^sha256:[a-f0-9]{64}$'),
    reason_code text,
    applied_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, workspace_id, runtime_id, configuration_version)
);

CREATE INDEX IF NOT EXISTS idx_network_runtime_enrollments_runtime
    ON public.network_runtime_enrollments (tenant_id, workspace_id, runtime_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_network_runtime_credentials_active
    ON public.network_runtime_credentials (tenant_id, workspace_id, runtime_id, generation DESC)
    WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_network_runtime_sessions_active
    ON public.network_runtime_sessions (tenant_id, workspace_id, runtime_id, status, valid_until);
CREATE INDEX IF NOT EXISTS idx_network_runtime_leases_session
    ON public.network_runtime_leases (tenant_id, workspace_id, session_id, status, expires_at);
CREATE INDEX IF NOT EXISTS idx_network_runtime_configurations_latest
    ON public.network_runtime_configurations (tenant_id, workspace_id, runtime_id, configuration_version DESC);
