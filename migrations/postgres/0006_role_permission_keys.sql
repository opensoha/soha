ALTER TABLE roles ADD COLUMN IF NOT EXISTS permission_keys JSONB NOT NULL DEFAULT '[]'::jsonb;

UPDATE roles
SET permission_keys = '[]'::jsonb
WHERE permission_keys IS NULL;
