-- Work Pulse reuses the B1 asset lifecycle; extend the named kind check in place
-- so rerunning this migration replaces the constraint safely.
ALTER TABLE assistant_mesh_assets
    DROP CONSTRAINT IF EXISTS assistant_mesh_assets_kind_check;

ALTER TABLE assistant_mesh_assets
    ADD CONSTRAINT assistant_mesh_assets_kind_check
    CHECK (kind IN ('skill', 'experience_card', 'work_pulse', 'mcp'));
