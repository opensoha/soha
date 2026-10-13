CREATE TABLE IF NOT EXISTS ai_personal_feed_settings (
 owner TEXT PRIMARY KEY,
 instructions TEXT NOT NULL,
 search_tool TEXT NOT NULL DEFAULT '',
 auto_update BOOLEAN NOT NULL DEFAULT FALSE,
 state TEXT NOT NULL DEFAULT 'idle' CHECK (state IN ('idle','queued','running','failed')),
 error TEXT NOT NULL DEFAULT '',
 job_id TEXT NOT NULL DEFAULT '',
 lease_until TIMESTAMPTZ,
 last_attempt_at TIMESTAMPTZ,
 last_updated_at TIMESTAMPTZ,
 next_update_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS ai_personal_feed_due ON ai_personal_feed_settings (next_update_at) WHERE auto_update;
CREATE TABLE IF NOT EXISTS ai_personal_feed_items (
 id TEXT PRIMARY KEY,
 owner TEXT NOT NULL REFERENCES ai_personal_feed_settings(owner),
 title TEXT NOT NULL,
 body TEXT NOT NULL,
 reason TEXT NOT NULL,
 sources JSONB NOT NULL,
 source_key TEXT NOT NULL,
 liked BOOLEAN NOT NULL DEFAULT FALSE,
 hidden BOOLEAN NOT NULL DEFAULT FALSE,
 created_at TIMESTAMPTZ NOT NULL,
 UNIQUE(owner, source_key)
);
CREATE INDEX IF NOT EXISTS ai_personal_feed_history ON ai_personal_feed_items(owner, created_at DESC, id DESC) WHERE NOT hidden;
CREATE INDEX IF NOT EXISTS ai_message_search_order ON ai_messages(created_at DESC, id DESC);
