-- Materialize the organization's default reception visibility for newly
-- approved Heya users. Existing rows are never rewritten.
CREATE OR REPLACE FUNCTION assistant_mesh_seed_profile_default() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    participant UUID;
    participant_status TEXT;
    profile_open BOOLEAN := false;
BEGIN
    IF NEW.user_id !~ '^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$' THEN
        RETURN NEW;
    END IF;
    participant := NEW.user_id::uuid;
    -- Serialize with status changes so access loss cannot miss a newly seeded profile.
    SELECT status INTO participant_status FROM users WHERE id = participant FOR SHARE;
    IF NOT FOUND THEN
        RETURN NEW;
    END IF;
    IF participant_status = 'active' AND EXISTS (
        SELECT 1 FROM app_settings
        WHERE setting_key = 'assistantMesh.organization'
          AND json_typeof(value -> 'defaultProfileOpen') = 'boolean'
          AND value ->> 'defaultProfileOpen' = 'true'
    ) THEN
        profile_open := true;
    END IF;
    INSERT INTO assistant_mesh_profiles (owner_id, open)
    VALUES (participant, profile_open)
    ON CONFLICT (owner_id) DO NOTHING;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS assistant_mesh_profile_default_on_approval_insert ON ai_personal_agent_access;
CREATE TRIGGER assistant_mesh_profile_default_on_approval_insert
AFTER INSERT ON ai_personal_agent_access
FOR EACH ROW WHEN (NEW.status = 'approved')
EXECUTE FUNCTION assistant_mesh_seed_profile_default();

DROP TRIGGER IF EXISTS assistant_mesh_profile_default_on_approval_update ON ai_personal_agent_access;
CREATE TRIGGER assistant_mesh_profile_default_on_approval_update
AFTER UPDATE OF status ON ai_personal_agent_access
FOR EACH ROW WHEN (NEW.status = 'approved' AND OLD.status IS DISTINCT FROM 'approved')
EXECUTE FUNCTION assistant_mesh_seed_profile_default();

-- A previously approved user can first reapply at version 4 and be rejected at version 5.
-- Keep existing approvals and ambiguous repeat applications private across the upgrade.
INSERT INTO assistant_mesh_profiles (owner_id, open)
SELECT u.id, false
FROM users u
JOIN ai_personal_agent_access a ON a.user_id = u.id::text
WHERE a.status IN ('approved', 'revoked')
   OR (a.status = 'pending' AND a.version >= 4)
   OR (a.status = 'rejected' AND a.version >= 5)
ON CONFLICT (owner_id) DO NOTHING;
