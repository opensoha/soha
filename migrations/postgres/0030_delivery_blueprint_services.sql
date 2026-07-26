ALTER TABLE delivery_blueprints
ADD COLUMN IF NOT EXISTS services jsonb DEFAULT '[]'::jsonb NOT NULL;
