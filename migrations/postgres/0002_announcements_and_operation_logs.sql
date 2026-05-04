CREATE TABLE IF NOT EXISTS announcement_receipts (
    id TEXT PRIMARY KEY,
    announcement_id TEXT NOT NULL REFERENCES announcements(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    read_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE (announcement_id, user_id)
);

ALTER TABLE operation_logs ADD COLUMN IF NOT EXISTS actor_name TEXT;
ALTER TABLE operation_logs ADD COLUMN IF NOT EXISTS request_path TEXT;
ALTER TABLE operation_logs ADD COLUMN IF NOT EXISTS request_method TEXT;
ALTER TABLE operation_logs ADD COLUMN IF NOT EXISTS request_id TEXT;
ALTER TABLE operation_logs ADD COLUMN IF NOT EXISTS source_ip TEXT;

CREATE INDEX IF NOT EXISTS idx_announcement_receipts_announcement_id ON announcement_receipts (announcement_id);
CREATE INDEX IF NOT EXISTS idx_announcement_receipts_user_id ON announcement_receipts (user_id);
