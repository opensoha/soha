ALTER TABLE delivery_workflows ADD COLUMN IF NOT EXISTS creation_key TEXT;
ALTER TABLE delivery_workflows ADD COLUMN IF NOT EXISTS creation_digest TEXT;
ALTER TABLE delivery_workflows ADD COLUMN IF NOT EXISTS creation_receipt JSONB;
CREATE UNIQUE INDEX IF NOT EXISTS delivery_workflows_actor_creation_key
    ON delivery_workflows (created_by, creation_key) WHERE creation_key IS NOT NULL;
