CREATE TABLE IF NOT EXISTS ai_personal_agent_access (
    user_id TEXT PRIMARY KEY,
    status TEXT NOT NULL CHECK (status IN ('pending', 'approved', 'rejected', 'revoked')),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    requested_at TIMESTAMPTZ NOT NULL,
    decided_at TIMESTAMPTZ,
    decided_by TEXT NOT NULL DEFAULT ''
);

-- Legacy main chats stay ordinary; Heya has a separate server-owned main chat.
CREATE UNIQUE INDEX IF NOT EXISTS idx_ai_sessions_personal_main_owner
ON ai_sessions (created_by)
WHERE deleted_at IS NULL AND metadata->>'conversationKind' = 'personal_main';
