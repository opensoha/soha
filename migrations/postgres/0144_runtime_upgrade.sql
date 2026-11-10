-- Expansion only. Normal startup never enables this gate or migrates business rows.
CREATE TABLE ai_runtime_upgrade_gate (
 id boolean PRIMARY KEY DEFAULT true CHECK(id),
 enabled boolean NOT NULL DEFAULT false,
 legacy_provider_ids text[] NOT NULL DEFAULT '{}',
 checkpoint text NOT NULL DEFAULT ''
);
INSERT INTO ai_runtime_upgrade_gate(id) VALUES(true);
CREATE TABLE ai_runtime_upgrade_checkpoints (
 digest text PRIMARY KEY,
 backup_sha256 text NOT NULL,
 plan jsonb NOT NULL,
 state text NOT NULL CHECK(state IN ('applied','rolled_back')),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE ai_runtime_upgrade_items (
 checkpoint text NOT NULL REFERENCES ai_runtime_upgrade_checkpoints(digest),
 entity text NOT NULL,
 id text NOT NULL,
 original jsonb NOT NULL,
 PRIMARY KEY(checkpoint,entity,id)
);
CREATE TABLE ai_runtime_upgrade_quarantine (
 entity text NOT NULL,
 id text NOT NULL,
 checkpoint text NOT NULL REFERENCES ai_runtime_upgrade_checkpoints(digest),
 reason text NOT NULL,
 PRIMARY KEY(entity,id)
);
CREATE FUNCTION ai_runtime_upgrade_fence() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT (SELECT enabled FROM ai_runtime_upgrade_gate WHERE id) THEN RETURN NEW; END IF;
 IF NEW.provider_kind IN ('native','codex') OR NEW.provider_id=ANY((SELECT legacy_provider_ids FROM ai_runtime_upgrade_gate WHERE id)::text[])
    OR (TG_OP='UPDATE' AND (OLD.provider_kind IN ('native','codex') OR OLD.provider_id=ANY((SELECT legacy_provider_ids FROM ai_runtime_upgrade_gate WHERE id)::text[]))) THEN
  RAISE EXCEPTION 'legacy runtime is fenced; history is read only';
 END IF;
 IF NEW.provider_kind='dsh' AND COALESCE(NEW.claimed_by_agent_id,'')<>''
    AND NEW.claimed_by_agent_id NOT LIKE 'dsh-core:%' THEN
  RAISE EXCEPTION 'dsh execution requires governed core ownership';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER ai_runtime_upgrade_fence BEFORE INSERT OR UPDATE ON ai_agent_runs
 FOR EACH ROW EXECUTE FUNCTION ai_runtime_upgrade_fence();

-- Older memory workers also fail closed at their durable pre-egress write.
CREATE FUNCTION ai_runtime_upgrade_memory_fence() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind='retain' AND EXISTS(SELECT 1 FROM ai_runtime_upgrade_quarantine WHERE entity='memory' AND id=NEW.record_id)
    AND (TG_OP='INSERT' OR (NEW.attempted AND NOT OLD.attempted)) THEN
  RAISE EXCEPTION 'quarantined memory cannot be submitted';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER ai_runtime_upgrade_memory_fence BEFORE INSERT OR UPDATE ON ai_memory_sync_outbox
 FOR EACH ROW EXECUTE FUNCTION ai_runtime_upgrade_memory_fence();

CREATE FUNCTION ai_runtime_upgrade_record_fence() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT (SELECT enabled FROM ai_runtime_upgrade_gate WHERE id) THEN RETURN NEW; END IF;
 IF NEW.owner_type<>OLD.owner_type OR NEW.owner_id<>OLD.owner_id OR NEW.scope_hash<>OLD.scope_hash THEN
  RAISE EXCEPTION 'memory ownership and scope are immutable';
 END IF;
 IF (OLD.status IN ('deleted','expired') OR OLD.deleted_at IS NOT NULL OR OLD.expires_at<=now()) AND NEW.status='active' THEN
  RAISE EXCEPTION 'disabled memory cannot be revived';
 END IF;
 IF EXISTS(SELECT 1 FROM ai_runtime_upgrade_quarantine WHERE entity='memory' AND id=OLD.id) AND NEW.status NOT IN ('deleted','expired') THEN
  RAISE EXCEPTION 'quarantined memory is read only';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER ai_runtime_upgrade_record_fence BEFORE UPDATE ON ai_memory_records
 FOR EACH ROW EXECUTE FUNCTION ai_runtime_upgrade_record_fence();
