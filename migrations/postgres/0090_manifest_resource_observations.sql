-- Keep the resource controller's observations separate from Soha's deployment
-- generation. NULL means an older executor or a resource without this field.
ALTER TABLE manifest_resource_inventory
    ADD COLUMN IF NOT EXISTS resource_generation BIGINT NOT NULL DEFAULT 0 CHECK (resource_generation >= 0),
    ADD COLUMN IF NOT EXISTS observed_resource_generation BIGINT CHECK (observed_resource_generation >= 0),
    ADD COLUMN IF NOT EXISTS deleting_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS finalizers JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(finalizers) = 'array');
