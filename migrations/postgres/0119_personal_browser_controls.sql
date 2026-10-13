-- Browser identity is user/workspace-owned and survives foreground sessions.
CREATE TABLE ai_personal_browser_controls (
 assistant_id TEXT PRIMARY KEY REFERENCES ai_personal_assistants(id),
 owner_id TEXT NOT NULL,
 control_id TEXT NOT NULL,
 generation BIGINT NOT NULL CHECK (generation BETWEEN 1 AND 9007199254740991),
 owner_mode TEXT NOT NULL CHECK (owner_mode IN ('user','agent','released')),
 task_id TEXT NOT NULL DEFAULT '',
 instance_id TEXT NOT NULL DEFAULT '',
 expires_at TIMESTAMPTZ NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
