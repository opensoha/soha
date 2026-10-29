-- +goose Up
CREATE TABLE IF NOT EXISTS public.network_eap_credentials (
    tenant_id text NOT NULL DEFAULT 'default',
    workspace_id text NOT NULL DEFAULT 'default',
    id text PRIMARY KEY,
    parent_credential_id text NOT NULL,
    runtime_id text NOT NULL,
    device_id text NOT NULL,
    subject_id text NOT NULL,
    request_id text NOT NULL,
    csr_hash text NOT NULL,
    generation bigint NOT NULL CHECK (generation > 0),
    certificate_pem text NOT NULL,
    ca_certificate_pem text NOT NULL,
    certificate_fingerprint text NOT NULL,
    certificate_serial text NOT NULL,
    certificate_authority_key_id text NOT NULL,
    status text NOT NULL CHECK (status IN ('pending','active','revoked')),
    not_before timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > not_before),
    created_at timestamptz NOT NULL,
    activated_at timestamptz,
    revoked_at timestamptz,
    UNIQUE (tenant_id,workspace_id,runtime_id,request_id),
    UNIQUE (tenant_id,workspace_id,runtime_id,generation),
    UNIQUE (certificate_serial,certificate_authority_key_id),
    UNIQUE (certificate_fingerprint)
);
CREATE UNIQUE INDEX IF NOT EXISTS network_eap_one_active ON public.network_eap_credentials(tenant_id,workspace_id,runtime_id) WHERE status='active';
CREATE UNIQUE INDEX IF NOT EXISTS network_eap_one_pending ON public.network_eap_credentials(tenant_id,workspace_id,runtime_id) WHERE status='pending';
CREATE INDEX IF NOT EXISTS network_eap_parent ON public.network_eap_credentials(parent_credential_id);

-- +goose Down
-- Preserve credential revocations when reverting application code.
