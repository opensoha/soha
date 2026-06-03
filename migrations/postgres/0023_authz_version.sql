ALTER TABLE users
    ADD COLUMN IF NOT EXISTS authz_version BIGINT NOT NULL DEFAULT 1;

ALTER TABLE sessions
    ADD COLUMN IF NOT EXISTS authz_version BIGINT NOT NULL DEFAULT 1;

UPDATE users
SET authz_version = 1
WHERE authz_version IS NULL OR authz_version < 1;

UPDATE sessions
SET authz_version = 1
WHERE authz_version IS NULL OR authz_version < 1;
