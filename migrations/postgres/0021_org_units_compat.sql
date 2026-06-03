ALTER TABLE teams ADD COLUMN IF NOT EXISTS parent_id TEXT REFERENCES teams(id) ON DELETE SET NULL;
ALTER TABLE teams ADD COLUMN IF NOT EXISTS org_path TEXT;
ALTER TABLE teams ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'local';
ALTER TABLE teams ADD COLUMN IF NOT EXISTS external_id TEXT;

UPDATE teams
SET org_path = '/' || slug
WHERE org_path IS NULL OR org_path = '';

CREATE INDEX IF NOT EXISTS idx_teams_parent_id ON teams(parent_id);
CREATE INDEX IF NOT EXISTS idx_teams_org_path ON teams(org_path);
CREATE INDEX IF NOT EXISTS idx_teams_source_external_id ON teams(source, external_id);
