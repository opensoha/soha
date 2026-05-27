ALTER TABLE ai_automation_policies ADD COLUMN IF NOT EXISTS agent_provider_id TEXT NOT NULL DEFAULT 'internal';
