-- Assistant Mesh reception state. Inquiry bodies are stored only for the
-- bounded pending/answered window and are stripped after purge_after.
CREATE TABLE IF NOT EXISTS assistant_mesh_profiles (
    owner_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    open BOOLEAN NOT NULL DEFAULT false,
    away BOOLEAN NOT NULL DEFAULT false,
    intro TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS assistant_mesh_profiles_open_owner
    ON assistant_mesh_profiles (owner_id) WHERE open;

CREATE TABLE IF NOT EXISTS assistant_mesh_inquiries (
    id UUID PRIMARY KEY,
    caller_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    request_id TEXT NOT NULL CHECK (length(btrim(request_id)) > 0),
    question_key TEXT NOT NULL CHECK (length(btrim(question_key)) > 0),
    question TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('answered', 'processing', 'pending', 'replied', 'expired', 'invalidated')),
    escalated BOOLEAN NOT NULL DEFAULT false,
    answer TEXT NOT NULL DEFAULT '',
    citations JSONB NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(citations) = 'array'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    due_at TIMESTAMPTZ NOT NULL,
    closed_at TIMESTAMPTZ,
    purge_after TIMESTAMPTZ,
    UNIQUE (caller_id, request_id)
);

CREATE INDEX IF NOT EXISTS assistant_mesh_inquiries_owner_created
    ON assistant_mesh_inquiries (owner_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS assistant_mesh_inquiries_caller_created
    ON assistant_mesh_inquiries (caller_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS assistant_mesh_inquiries_owner_pending_due
    ON assistant_mesh_inquiries (owner_id, due_at) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS assistant_mesh_inquiries_pair_created
    ON assistant_mesh_inquiries (caller_id, owner_id, created_at);
CREATE INDEX IF NOT EXISTS assistant_mesh_inquiries_caller_created_window
    ON assistant_mesh_inquiries (caller_id, created_at);
CREATE INDEX IF NOT EXISTS assistant_mesh_inquiries_pending_merge
    ON assistant_mesh_inquiries (caller_id, owner_id, question_key, created_at DESC)
    WHERE status IN ('pending', 'processing');
CREATE INDEX IF NOT EXISTS assistant_mesh_inquiries_processing
    ON assistant_mesh_inquiries (created_at) WHERE status = 'processing';
CREATE INDEX IF NOT EXISTS assistant_mesh_inquiries_purge_after
    ON assistant_mesh_inquiries (purge_after) WHERE purge_after IS NOT NULL;

-- Merged inquiries reuse the original receipt. Aliases bind the superseded
-- caller request id to that inquiry and carry no inquiry bodies.
CREATE TABLE IF NOT EXISTS assistant_mesh_inquiry_aliases (
    caller_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    request_id TEXT NOT NULL CHECK (length(btrim(request_id)) > 0),
    inquiry_id UUID NOT NULL REFERENCES assistant_mesh_inquiries(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (caller_id, request_id)
);

CREATE INDEX IF NOT EXISTS assistant_mesh_inquiry_aliases_inquiry
    ON assistant_mesh_inquiry_aliases (inquiry_id);

-- An access-loss transition closes the profile and fences previous inquiries
-- in the same transaction as the account or Heya decision.
CREATE OR REPLACE FUNCTION assistant_mesh_invalidate_reception_access() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    participant UUID;
BEGIN
    IF TG_TABLE_NAME = 'users' THEN
        participant := NEW.id;
    ELSE
        IF NEW.user_id !~* '^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$' THEN
            RETURN NEW;
        END IF;
        participant := NEW.user_id::uuid;
    END IF;

    UPDATE assistant_mesh_profiles
    SET open = false, updated_at = now()
    WHERE owner_id = participant AND open;

    UPDATE assistant_mesh_inquiries
    SET status = 'invalidated', closed_at = now(), purge_after = now() + interval '7 days'
    WHERE status IN ('pending', 'processing') AND due_at > now()
      AND (caller_id = participant OR owner_id = participant);
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS assistant_mesh_users_access_loss ON users;
CREATE TRIGGER assistant_mesh_users_access_loss
AFTER UPDATE OF status ON users
FOR EACH ROW WHEN (OLD.status = 'active' AND NEW.status IS DISTINCT FROM OLD.status)
EXECUTE FUNCTION assistant_mesh_invalidate_reception_access();

DROP TRIGGER IF EXISTS assistant_mesh_heya_access_loss ON ai_personal_agent_access;
CREATE TRIGGER assistant_mesh_heya_access_loss
AFTER UPDATE OF status ON ai_personal_agent_access
FOR EACH ROW WHEN (OLD.status = 'approved' AND NEW.status IS DISTINCT FROM OLD.status)
EXECUTE FUNCTION assistant_mesh_invalidate_reception_access();
