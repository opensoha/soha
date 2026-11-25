-- Assistant Mesh L3 delegation. Device proof, input, ticket nonce and result
-- digests are stored; raw proof and ticket material are never persisted.
CREATE TABLE IF NOT EXISTS assistant_mesh_managed_devices (
    owner_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    device_id TEXT NOT NULL CHECK (length(btrim(device_id)) > 0 AND length(device_id) <= 200),
    session_id TEXT NOT NULL CHECK (length(btrim(session_id)) > 0 AND length(session_id) <= 512),
    proof_hash TEXT NOT NULL CHECK (proof_hash ~ '^[0-9a-f]{64}$'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS assistant_mesh_delegations (
    id UUID PRIMARY KEY,
    caller_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    request_id UUID NOT NULL,
    capability TEXT NOT NULL CHECK (capability IN ('desktop.os-version.v1')),
    capability_field TEXT NOT NULL CHECK (capability_field IN ('productVersion', 'buildVersion')),
    input_hash TEXT NOT NULL CHECK (input_hash ~ '^[0-9a-f]{64}$'),
    status TEXT NOT NULL DEFAULT 'awaiting_owner_approval'
        CHECK (status IN ('awaiting_owner_approval', 'approved', 'executing', 'awaiting_confirmation',
                          'completed', 'declined', 'invalidated', 'expired', 'failed')),
    result TEXT NOT NULL DEFAULT '',
    result_hash TEXT CHECK (result_hash IS NULL OR result_hash ~ '^[0-9a-f]{64}$'),
    device_id TEXT CHECK (device_id IS NULL OR (length(btrim(device_id)) > 0 AND length(device_id) <= 200)),
    mode TEXT CHECK (mode IS NULL OR length(mode) <= 32),
    ticket_hash TEXT CHECK (ticket_hash IS NULL OR ticket_hash ~ '^[0-9a-f]{64}$'),
    ticket_expires_at TIMESTAMPTZ,
    fence INTEGER NOT NULL DEFAULT 0 CHECK (fence >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    closed_at TIMESTAMPTZ,
    CHECK (expires_at > created_at),
    UNIQUE (caller_id, request_id)
);

CREATE INDEX IF NOT EXISTS assistant_mesh_delegations_owner_created
    ON assistant_mesh_delegations (owner_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS assistant_mesh_delegations_caller_created
    ON assistant_mesh_delegations (caller_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS assistant_mesh_delegations_owner_open
    ON assistant_mesh_delegations (owner_id, created_at)
    WHERE status IN ('awaiting_owner_approval', 'approved', 'executing', 'awaiting_confirmation');
CREATE INDEX IF NOT EXISTS assistant_mesh_delegations_pair_created
    ON assistant_mesh_delegations (caller_id, owner_id, created_at);
CREATE INDEX IF NOT EXISTS assistant_mesh_delegations_expiry
    ON assistant_mesh_delegations (expires_at)
    WHERE status IN ('awaiting_owner_approval', 'approved', 'executing', 'awaiting_confirmation');
CREATE INDEX IF NOT EXISTS assistant_mesh_delegations_result_retention
    ON assistant_mesh_delegations (closed_at)
    WHERE result <> '' OR result_hash IS NOT NULL;

-- Access-loss and consent-loss fence every open delegation for either
-- participant. Reuse the B2 trigger approach and keep it rerunnable.
CREATE OR REPLACE FUNCTION assistant_mesh_invalidate_delegations() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    participant UUID;
BEGIN
    IF TG_TABLE_NAME = 'users' THEN
        participant := NEW.id;
    ELSIF TG_TABLE_NAME = 'ai_personal_agent_access' THEN
        IF NEW.user_id !~* '^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$' THEN
            RETURN NEW;
        END IF;
        participant := NEW.user_id::uuid;
    ELSE
        participant := NEW.owner_id;
    END IF;

    WITH invalidated AS (
        UPDATE assistant_mesh_delegations
        SET status = 'invalidated', closed_at = now(),
            ticket_hash = NULL, ticket_expires_at = NULL, result_hash = NULL
        WHERE status IN ('awaiting_owner_approval', 'approved', 'executing', 'awaiting_confirmation')
          AND (caller_id = participant OR owner_id = participant)
        RETURNING id, caller_id, owner_id, capability, input_hash
    )
    INSERT INTO audit_logs (id, actor_id, resource_kind, resource_name, action, result, summary, metadata)
    SELECT gen_random_uuid()::text, 'system', 'AssistantMeshAsset', id::text,
           'delegation.invalidate', 'success', 'Heya Assistant Mesh delegation invalidated',
           json_build_object('callerId', caller_id, 'ownerId', owner_id,
                             'capability', capability, 'inputHash', input_hash,
                             'source', TG_TABLE_NAME, 'participantId', participant)
    FROM invalidated;
    DELETE FROM assistant_mesh_managed_devices WHERE owner_id = participant;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS assistant_mesh_delegation_users_access_loss ON users;
CREATE TRIGGER assistant_mesh_delegation_users_access_loss
AFTER UPDATE OF status ON users
FOR EACH ROW WHEN (OLD.status = 'active' AND NEW.status IS DISTINCT FROM OLD.status)
EXECUTE FUNCTION assistant_mesh_invalidate_delegations();

DROP TRIGGER IF EXISTS assistant_mesh_delegation_heya_access_loss ON ai_personal_agent_access;
CREATE TRIGGER assistant_mesh_delegation_heya_access_loss
AFTER UPDATE OF status ON ai_personal_agent_access
FOR EACH ROW WHEN (OLD.status = 'approved' AND NEW.status IS DISTINCT FROM OLD.status)
EXECUTE FUNCTION assistant_mesh_invalidate_delegations();

DROP TRIGGER IF EXISTS assistant_mesh_delegation_profile_closed ON assistant_mesh_profiles;
CREATE TRIGGER assistant_mesh_delegation_profile_closed
AFTER UPDATE OF open ON assistant_mesh_profiles
FOR EACH ROW WHEN (OLD.open AND NOT NEW.open)
EXECUTE FUNCTION assistant_mesh_invalidate_delegations();
