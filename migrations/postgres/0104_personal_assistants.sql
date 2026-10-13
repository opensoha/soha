CREATE TABLE IF NOT EXISTS ai_personal_assistants (
    id TEXT PRIMARY KEY,
    created_by TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 60),
    main_session_id TEXT NOT NULL UNIQUE REFERENCES ai_sessions(id),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
