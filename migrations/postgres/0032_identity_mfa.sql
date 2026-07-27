CREATE TABLE IF NOT EXISTS identity_mfa_credentials (
    id TEXT PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_type TEXT NOT NULL CHECK (credential_type IN ('totp', 'webauthn')),
    display_name TEXT NOT NULL,
    external_id TEXT,
    secret_ciphertext TEXT NOT NULL,
    sign_count BIGINT NOT NULL DEFAULT 0 CHECK (sign_count >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS identity_mfa_credentials_external_id_uq
    ON identity_mfa_credentials (credential_type, external_id)
    WHERE external_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS identity_mfa_credentials_active_totp_uq
    ON identity_mfa_credentials (user_id)
    WHERE credential_type = 'totp' AND revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS identity_mfa_credentials_user_idx
    ON identity_mfa_credentials (user_id, created_at DESC)
    WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS identity_mfa_challenges (
    id TEXT PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    challenge_type TEXT NOT NULL CHECK (challenge_type IN ('totp_enrollment', 'totp_step_up', 'webauthn_enrollment')),
    secret_ciphertext TEXT NOT NULL DEFAULT '',
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts INTEGER NOT NULL DEFAULT 5 CHECK (max_attempts > 0),
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS identity_mfa_challenges_lookup_idx
    ON identity_mfa_challenges (id, user_id, session_id);

CREATE INDEX IF NOT EXISTS identity_mfa_challenges_cleanup_idx
    ON identity_mfa_challenges (expires_at)
    WHERE consumed_at IS NULL;

CREATE TABLE IF NOT EXISTS identity_recovery_codes (
    id TEXT PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    used_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS identity_recovery_codes_user_idx
    ON identity_recovery_codes (user_id, created_at DESC)
    WHERE used_at IS NULL;
