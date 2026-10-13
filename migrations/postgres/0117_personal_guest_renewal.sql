-- Only public certificate material is persisted. Existing keys and sequences stay intact.
CREATE TABLE ai_personal_guest_renewals (
    request_id TEXT PRIMARY KEY,
    binding_id TEXT NOT NULL REFERENCES ai_personal_guests(binding_id),
    old_fingerprint TEXT NOT NULL,
    new_fingerprint TEXT NOT NULL,
    csr_hash TEXT NOT NULL,
    result JSONB NOT NULL,
    commit_before TIMESTAMPTZ NOT NULL,
    committed BOOLEAN NOT NULL DEFAULT FALSE,
    superseded BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE UNIQUE INDEX personal_guest_pending_renewal ON ai_personal_guest_renewals(binding_id) WHERE NOT committed AND NOT superseded;
