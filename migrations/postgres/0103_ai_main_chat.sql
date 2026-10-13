SELECT pg_catalog.set_config('search_path', '', false);

-- Existing chats remain untouched. Only the dedicated service creates a main chat.
CREATE UNIQUE INDEX idx_ai_sessions_main_owner ON public.ai_sessions (created_by)
    WHERE deleted_at IS NULL AND metadata->>'conversationKind' = 'main';
