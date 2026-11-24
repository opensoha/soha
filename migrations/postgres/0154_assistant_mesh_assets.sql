CREATE TABLE IF NOT EXISTS assistant_mesh_assets (
    id UUID PRIMARY KEY,
    owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('skill', 'experience_card', 'mcp')),
    payload JSONB NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS assistant_mesh_assets_owner_created
    ON assistant_mesh_assets (owner_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS assistant_mesh_assets_kind_created
    ON assistant_mesh_assets (kind, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS assistant_mesh_retired_versions (
    asset_id UUID NOT NULL REFERENCES assistant_mesh_assets(id) ON DELETE CASCADE,
    version INTEGER NOT NULL CHECK (version > 0),
    snapshot JSONB NOT NULL CHECK (jsonb_typeof(snapshot) = 'object'),
    retired_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (asset_id, version)
);

CREATE OR REPLACE FUNCTION assistant_mesh_archive_publication() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO assistant_mesh_retired_versions (asset_id, version, snapshot)
    VALUES (OLD.id, (OLD.payload -> 'published' ->> 'version')::integer, OLD.payload -> 'published');
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS assistant_mesh_archive_publication ON assistant_mesh_assets;
CREATE TRIGGER assistant_mesh_archive_publication
AFTER UPDATE OF payload ON assistant_mesh_assets
FOR EACH ROW WHEN (
    OLD.payload ? 'published' AND OLD.payload -> 'published' IS DISTINCT FROM NEW.payload -> 'published'
)
EXECUTE FUNCTION assistant_mesh_archive_publication();
