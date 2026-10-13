-- Liveness is separate from environment/data readiness. Existing bindings and
-- provider resources are unchanged; no credentials or raw challenges are stored.
CREATE UNIQUE INDEX personal_vm_single_owner ON ai_personal_environments(compute_ref) WHERE backend='vm' AND compute_generation>0;

CREATE TABLE ai_personal_guests (
    binding_id TEXT PRIMARY KEY,
    assistant_id TEXT NOT NULL REFERENCES ai_personal_environments(assistant_id),
    binding_version BIGINT NOT NULL CHECK (binding_version BETWEEN 1 AND 9007199254740991),
    binding JSONB NOT NULL,
    certificate_fingerprint TEXT NOT NULL CHECK (certificate_fingerprint ~ '^sha256:[a-f0-9]{64}$'),
    access_version BIGINT NOT NULL CHECK (access_version > 0),
    enrollment_id TEXT NOT NULL UNIQUE,
    challenge_hash TEXT NOT NULL CHECK (challenge_hash ~ '^sha256:[a-f0-9]{64}$'),
    enrollment_issued_at TIMESTAMPTZ NOT NULL,
    enrollment_expires_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending','registered','revoked')),
    last_sequence BIGINT NOT NULL DEFAULT 0 CHECK (last_sequence BETWEEN 0 AND 9007199254740991),
    last_heartbeat_at TIMESTAMPTZ,
    UNIQUE (assistant_id, binding_version),
    CHECK (binding->>'id' = binding_id AND binding->>'assistantId' = assistant_id
        AND (binding->>'version')::BIGINT = binding_version)
);
