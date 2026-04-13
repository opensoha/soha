ALTER TABLE ai_data_sources ADD COLUMN IF NOT EXISTS validation_status TEXT;
ALTER TABLE ai_data_sources ADD COLUMN IF NOT EXISTS validation_message TEXT;
ALTER TABLE ai_data_sources ADD COLUMN IF NOT EXISTS last_validated_at TIMESTAMP;

CREATE INDEX IF NOT EXISTS idx_ai_data_sources_validation_status ON ai_data_sources (validation_status, last_validated_at DESC);
