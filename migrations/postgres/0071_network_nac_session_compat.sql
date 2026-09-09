SELECT pg_catalog.set_config('search_path', '', false);

-- Earlier development builds applied 0063 before NAC session enforcement was
-- appended to that migration. Keep the repair additive so old and fresh
-- databases converge without rewriting migration history.
ALTER TABLE public.network_runtime_sessions
    ADD COLUMN IF NOT EXISTS nas_id text,
    ADD COLUMN IF NOT EXISTS authentication_method text CHECK (authentication_method IS NULL OR authentication_method IN ('eap-tls', 'password-compatible'));

CREATE TABLE IF NOT EXISTS public.network_nas_authorizations (
    tenant_id text NOT NULL DEFAULT 'default',
    workspace_id text NOT NULL DEFAULT 'default',
    runtime_id text NOT NULL,
    request_id text NOT NULL,
    request_hash text NOT NULL CHECK (request_hash ~ '^sha256:[a-f0-9]{64}$'),
    nas_id text NOT NULL,
    subject_id text NOT NULL,
    device_id text NOT NULL,
    authentication_method text NOT NULL CHECK (authentication_method IN ('eap-tls', 'password-compatible')),
    session_id text NOT NULL,
    decision text NOT NULL CHECK (decision IN ('allow', 'deny')),
    access_profile text NOT NULL CHECK (access_profile IN ('onboarding', 'full', 'restricted', 'quarantine', 'deny')),
    policy_version bigint NOT NULL CHECK (policy_version >= 1),
    reason_code text NOT NULL,
    radius_attributes jsonb,
    valid_until timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, workspace_id, runtime_id, request_id),
    UNIQUE (tenant_id, workspace_id, session_id),
    CHECK (radius_attributes IS NULL OR jsonb_typeof(radius_attributes) = 'object'),
    CHECK (valid_until > created_at),
    CHECK ((decision = 'allow' AND access_profile <> 'deny' AND radius_attributes IS NOT NULL)
        OR (decision = 'deny' AND access_profile = 'deny' AND radius_attributes IS NULL))
);

CREATE TABLE IF NOT EXISTS public.network_nas_session_commands (
    id text PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT 'default',
    workspace_id text NOT NULL DEFAULT 'default',
    session_id text NOT NULL REFERENCES public.network_runtime_sessions(id) ON DELETE RESTRICT,
    runtime_id text NOT NULL,
    nas_id text NOT NULL,
    action text NOT NULL CHECK (action IN ('coa', 'disconnect')),
    target_access_profile text NOT NULL CHECK (target_access_profile IN ('onboarding', 'full', 'restricted', 'quarantine', 'deny')),
    policy_version bigint NOT NULL CHECK (policy_version >= 1),
    status text NOT NULL CHECK (status IN ('pending', 'delivered', 'applied', 'rejected', 'unsupported', 'timed-out', 'expired')),
    reason_code text NOT NULL,
    plan_hash text NOT NULL CHECK (plan_hash ~ '^sha256:[a-f0-9]{64}$'),
    radius_attributes jsonb,
    effective_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    delivered_at timestamptz,
    completed_at timestamptz,
    result_reason_code text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, workspace_id, plan_hash),
    CHECK (expires_at > created_at),
    CHECK ((action = 'coa' AND radius_attributes IS NOT NULL AND jsonb_typeof(radius_attributes) = 'object')
        OR (action = 'disconnect' AND radius_attributes IS NULL))
);

CREATE INDEX IF NOT EXISTS idx_network_nas_authorizations_session
    ON public.network_nas_authorizations (tenant_id, workspace_id, session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_network_nas_session_commands_delivery
    ON public.network_nas_session_commands (tenant_id, workspace_id, runtime_id, status, expires_at, created_at);
