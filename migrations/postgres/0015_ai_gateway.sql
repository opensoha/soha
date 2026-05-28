CREATE TABLE IF NOT EXISTS personal_access_tokens (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    token_hash TEXT NOT NULL,
    token_prefix TEXT NOT NULL,
    scopes JSON NOT NULL DEFAULT '[]',
    permission_keys JSON NOT NULL DEFAULT '[]',
    metadata JSON NOT NULL DEFAULT '{}',
    expires_at TIMESTAMP,
    last_used_at TIMESTAMP,
    revoked_at TIMESTAMP,
    created_by TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_personal_access_tokens_token_hash ON personal_access_tokens(token_hash);
CREATE INDEX IF NOT EXISTS idx_personal_access_tokens_user_id ON personal_access_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_personal_access_tokens_active ON personal_access_tokens(user_id, expires_at) WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS service_accounts (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    status TEXT NOT NULL DEFAULT 'active',
    owner_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
    role_ids JSON NOT NULL DEFAULT '[]',
    team_ids JSON NOT NULL DEFAULT '[]',
    scope_grant_ids JSON NOT NULL DEFAULT '[]',
    metadata JSON NOT NULL DEFAULT '{}',
    created_by TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_service_accounts_status ON service_accounts(status);
CREATE INDEX IF NOT EXISTS idx_service_accounts_owner ON service_accounts(owner_user_id);

CREATE TABLE IF NOT EXISTS service_account_tokens (
    id TEXT PRIMARY KEY,
    service_account_id TEXT NOT NULL REFERENCES service_accounts(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    token_hash TEXT NOT NULL,
    token_prefix TEXT NOT NULL,
    scopes JSON NOT NULL DEFAULT '[]',
    permission_keys JSON NOT NULL DEFAULT '[]',
    metadata JSON NOT NULL DEFAULT '{}',
    expires_at TIMESTAMP,
    last_used_at TIMESTAMP,
    revoked_at TIMESTAMP,
    created_by TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_service_account_tokens_token_hash ON service_account_tokens(token_hash);
CREATE INDEX IF NOT EXISTS idx_service_account_tokens_account ON service_account_tokens(service_account_id);
CREATE INDEX IF NOT EXISTS idx_service_account_tokens_active ON service_account_tokens(service_account_id, expires_at) WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS ai_clients (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    redirect_uris JSON NOT NULL DEFAULT '[]',
    allowed_origins JSON NOT NULL DEFAULT '[]',
    metadata JSON NOT NULL DEFAULT '{}',
    created_by TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ai_clients_kind_status ON ai_clients(kind, status);

CREATE TABLE IF NOT EXISTS ai_access_policies (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    subject_type TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    ai_client_id TEXT REFERENCES ai_clients(id) ON DELETE CASCADE,
    effect TEXT NOT NULL DEFAULT 'allow',
    tool_patterns JSON NOT NULL DEFAULT '[]',
    skill_ids JSON NOT NULL DEFAULT '[]',
    resource_scopes JSON NOT NULL DEFAULT '{}',
    risk_levels JSON NOT NULL DEFAULT '[]',
    approval_policy JSON NOT NULL DEFAULT '{}',
    conditions JSON NOT NULL DEFAULT '{}',
    created_by TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ai_access_policies_subject ON ai_access_policies(subject_type, subject_id);
CREATE INDEX IF NOT EXISTS idx_ai_access_policies_client ON ai_access_policies(ai_client_id);

CREATE TABLE IF NOT EXISTS mcp_tool_grants (
    id TEXT PRIMARY KEY,
    subject_type TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    ai_client_id TEXT REFERENCES ai_clients(id) ON DELETE CASCADE,
    tool_name TEXT NOT NULL,
    effect TEXT NOT NULL DEFAULT 'allow',
    risk_level TEXT NOT NULL DEFAULT 'read',
    permission_keys JSON NOT NULL DEFAULT '[]',
    resource_scopes JSON NOT NULL DEFAULT '{}',
    requires_approval BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at TIMESTAMP,
    created_by TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_mcp_tool_grants_subject ON mcp_tool_grants(subject_type, subject_id);
CREATE INDEX IF NOT EXISTS idx_mcp_tool_grants_client_tool ON mcp_tool_grants(ai_client_id, tool_name);

CREATE TABLE IF NOT EXISTS ai_gateway_skill_bindings (
    id TEXT PRIMARY KEY,
    subject_type TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    ai_client_id TEXT REFERENCES ai_clients(id) ON DELETE CASCADE,
    skill_id TEXT NOT NULL,
    capability_refs JSON NOT NULL DEFAULT '[]',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    metadata JSON NOT NULL DEFAULT '{}',
    created_by TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ai_gateway_skill_bindings_subject ON ai_gateway_skill_bindings(subject_type, subject_id);
CREATE INDEX IF NOT EXISTS idx_ai_gateway_skill_bindings_client_skill ON ai_gateway_skill_bindings(ai_client_id, skill_id);

CREATE TABLE IF NOT EXISTS ai_gateway_audit_logs (
    id TEXT PRIMARY KEY,
    actor_type TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    actor_name TEXT,
    ai_client_id TEXT,
    ai_client_name TEXT,
    skill_id TEXT,
    tool_name TEXT,
    risk_level TEXT,
    resource_scope JSON NOT NULL DEFAULT '{}',
    action TEXT NOT NULL,
    result TEXT NOT NULL,
    summary TEXT NOT NULL,
    request_id TEXT,
    source_ip TEXT,
    metadata JSON NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ai_gateway_audit_logs_actor ON ai_gateway_audit_logs(actor_type, actor_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_gateway_audit_logs_client ON ai_gateway_audit_logs(ai_client_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_gateway_audit_logs_tool ON ai_gateway_audit_logs(tool_name, created_at DESC);
