-- Task115 owns 0125; 0120/0121 belong to the parallel Task114 worktree.
-- Logged durable tables are shared by all replicas. Existing sessions and v1
-- refresh credentials are intentionally left unchanged.
CREATE TABLE IF NOT EXISTS cli_device_authorizations (
 id TEXT PRIMARY KEY,
 device_hash TEXT NOT NULL UNIQUE,
 handle_hash TEXT NOT NULL UNIQUE,
 source_hash TEXT NOT NULL,
 state TEXT NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL,
 receipt_expires_at TIMESTAMPTZ,
 payload JSONB NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_cli_grant_admission ON cli_device_authorizations(state, expires_at, source_hash);
CREATE INDEX IF NOT EXISTS idx_cli_grant_expiry ON cli_device_authorizations(expires_at);
CREATE TABLE IF NOT EXISTS cli_refresh_credentials (
 hash TEXT PRIMARY KEY,
 session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 generation INTEGER NOT NULL DEFAULT 1 CHECK (generation > 0),
 replacement_hash TEXT NOT NULL DEFAULT '',
 issued_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
 consumed_at TIMESTAMPTZ,
 request_hash TEXT NOT NULL DEFAULT '',
 receipt TEXT NOT NULL DEFAULT '',
 receipt_expires_at TIMESTAMPTZ,
 expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_cli_refresh_session ON cli_refresh_credentials(session_id);
CREATE INDEX IF NOT EXISTS idx_cli_refresh_expiry ON cli_refresh_credentials(expires_at);
CREATE TABLE IF NOT EXISTS cli_auth_budgets (
 bucket TEXT PRIMARY KEY,
 count INTEGER NOT NULL CHECK (count >= 0),
 expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_cli_budget_expiry ON cli_auth_budgets(expires_at);

-- This outcome is committed with Key replacement and revocation. It never
-- contains the new plaintext value and outlives short CLI response receipts.
CREATE TABLE IF NOT EXISTS gateway_token_rotations (
 rotation_request_id uuid PRIMARY KEY,
 actor_id text NOT NULL,
 owner_id text NOT NULL,
 token_kind text NOT NULL CHECK (token_kind IN ('personal_access_token','service_account_token')),
 previous_token_id text NOT NULL,
 replacement_token_id text NOT NULL,
 rotated_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_gateway_token_rotations_owner ON gateway_token_rotations(owner_id, token_kind, rotated_at DESC);
