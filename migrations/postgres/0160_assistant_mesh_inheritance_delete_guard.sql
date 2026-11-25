-- Keep published, inheritable assets available for offboarding before the
-- existing user foreign-key cascade can remove their version history.
CREATE OR REPLACE FUNCTION assistant_mesh_guard_published_asset_owner_delete() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM id FROM assistant_mesh_assets
      WHERE owner_id = OLD.id AND kind IN ('skill', 'experience_card')
      ORDER BY id FOR UPDATE;
    IF EXISTS (
        SELECT 1 FROM assistant_mesh_assets
          WHERE owner_id = OLD.id AND kind IN ('skill', 'experience_card')
            AND jsonb_typeof(payload -> 'published') = 'object'
    ) THEN
        RAISE EXCEPTION USING ERRCODE = 'AM001', MESSAGE = 'owner has inheritable Assistant Mesh assets';
    END IF;
    RETURN OLD;
END;
$$;

DROP TRIGGER IF EXISTS assistant_mesh_guard_published_asset_owner_delete ON users;
CREATE TRIGGER assistant_mesh_guard_published_asset_owner_delete
BEFORE DELETE ON users
FOR EACH ROW EXECUTE FUNCTION assistant_mesh_guard_published_asset_owner_delete();
