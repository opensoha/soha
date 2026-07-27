ALTER TABLE identity_mfa_challenges
    DROP CONSTRAINT IF EXISTS identity_mfa_challenges_challenge_type_check;
ALTER TABLE identity_mfa_challenges
    ADD CONSTRAINT identity_mfa_challenges_challenge_type_check
        CHECK (challenge_type IN ('totp_enrollment', 'totp_step_up', 'webauthn_enrollment', 'webauthn_authentication'));

CREATE TABLE IF NOT EXISTS identity_outpost_runtime_versions (
    outpost_id TEXT PRIMARY KEY REFERENCES identity_outposts(id) ON DELETE CASCADE,
    configuration_digest TEXT NOT NULL,
    configuration_version BIGINT NOT NULL CHECK (configuration_version > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
