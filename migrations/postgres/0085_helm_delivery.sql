ALTER TABLE release_targets ADD COLUMN IF NOT EXISTS helm_configuration JSONB;
ALTER TABLE delivery_plans ADD COLUMN IF NOT EXISTS helm_snapshots JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE delivery_plans ADD COLUMN IF NOT EXISTS helm_prepared_ciphertext TEXT NOT NULL DEFAULT '';
