CREATE TABLE IF NOT EXISTS docker_host_agent_installations (
    operation_id text PRIMARY KEY REFERENCES docker_operations(id) ON DELETE CASCADE,
    host_id text NOT NULL REFERENCES docker_hosts(id) ON DELETE CASCADE,
    download_token_hash text NOT NULL,
    download_expires_at timestamptz NOT NULL,
    downloaded_at timestamptz,
    enrollment_token_hash text,
    enrollment_expires_at timestamptz,
    enrolled_at timestamptz,
    agent_id text,
    agent_token_ciphertext text,
    runtime_token_hash text,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS docker_host_agent_installations_active_host_idx
    ON docker_host_agent_installations (host_id)
    WHERE runtime_token_hash IS NOT NULL AND revoked_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS docker_host_agent_installations_runtime_token_idx
    ON docker_host_agent_installations (runtime_token_hash)
    WHERE runtime_token_hash IS NOT NULL AND revoked_at IS NULL;
