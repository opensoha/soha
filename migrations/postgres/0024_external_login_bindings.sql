ALTER TABLE user_role_bindings ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'local';
ALTER TABLE user_role_bindings ADD COLUMN IF NOT EXISTS provider_id TEXT;
ALTER TABLE user_team_bindings ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'local';
ALTER TABLE user_team_bindings ADD COLUMN IF NOT EXISTS provider_id TEXT;

CREATE INDEX IF NOT EXISTS idx_user_role_bindings_user_source_provider ON user_role_bindings(user_id, source, provider_id);
CREATE INDEX IF NOT EXISTS idx_user_team_bindings_user_source_provider ON user_team_bindings(user_id, source, provider_id);
