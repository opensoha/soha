-- Runtime reports are observations; only this table establishes a signed grant.
CREATE TABLE identity_outpost_runtime_leases (
    outpost_id text PRIMARY KEY REFERENCES identity_outposts(id) ON DELETE CASCADE,
    token_hash text NOT NULL,
    configuration_version bigint NOT NULL CHECK (configuration_version > 0),
    configuration_digest text NOT NULL,
    issued_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > issued_at)
);

-- Existing v1 observations predate trusted grants and must be re-confirmed.
UPDATE identity_outposts
SET applied_configuration_version = 0, configuration_expires_at = NULL,
    runtime_status = 'unavailable', runtime_reason = 'configuration_not_applied'
WHERE protocol_version = 'v1';
