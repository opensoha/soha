ALTER TABLE delivery_drafts ADD COLUMN IF NOT EXISTS creation_key TEXT;
ALTER TABLE delivery_drafts ADD COLUMN IF NOT EXISTS creation_digest TEXT;
ALTER TABLE delivery_drafts ADD COLUMN IF NOT EXISTS creation_receipt JSONB;
ALTER TABLE delivery_drafts ADD COLUMN IF NOT EXISTS confirmation_receipt JSONB;
CREATE UNIQUE INDEX IF NOT EXISTS idx_delivery_drafts_actor_creation_key
    ON delivery_drafts (created_by, creation_key) WHERE creation_key IS NOT NULL;
