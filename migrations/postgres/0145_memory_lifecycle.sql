-- pgx binary timestamps truncate to microseconds, while PostgreSQL text input rounds.
CREATE FUNCTION ai_memory_timestamp_matches(stored timestamptz, raw text) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT CASE
   WHEN raw IS NULL THEN stored IS NULL
   WHEN stored IS NULL OR NOT pg_input_is_valid(raw,'timestamptz') THEN false
   ELSE stored=raw::timestamptz OR
        stored=regexp_replace(raw,'([.][0-9]{6})[0-9]+([Zz]|[+-][0-9:]+)$','\1\2')::timestamptz
 END
$$;

ALTER TABLE ai_memory_sync_outbox ADD COLUMN operation_purged boolean NOT NULL DEFAULT false;
CREATE INDEX ai_memory_sync_revalidation ON ai_memory_sync_outbox(next_attempt_at)
 WHERE kind='retain' AND attempted AND state IN ('done','failed');
CREATE FUNCTION ai_memory_require_purge_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind='delete' AND NEW.state='done' AND NOT NEW.operation_purged
    AND EXISTS(SELECT 1 FROM ai_memory_sync_outbox o WHERE o.id=NEW.dependency_id AND o.attempted) THEN
  RAISE EXCEPTION 'attempted retain cleanup requires an operation purge receipt';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER ai_memory_require_purge_receipt BEFORE UPDATE OF state ON ai_memory_sync_outbox
 FOR EACH ROW EXECUTE FUNCTION ai_memory_require_purge_receipt();

-- Once cut over, old writers cannot mutate a live revision or create an active
-- row that bypasses the outbox. The deferred check sees Core's atomic insert.
CREATE FUNCTION ai_memory_revision_fence() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT (SELECT enabled FROM ai_runtime_upgrade_gate WHERE id) THEN RETURN NULL; END IF;
 IF TG_OP='UPDATE' THEN
  IF NEW.revision=OLD.revision AND to_jsonb(NEW) IS DISTINCT FROM to_jsonb(OLD) THEN
   RAISE EXCEPTION 'memory write requires a new revision';
  END IF;
  IF NEW.payload=OLD.payload THEN RETURN NULL; END IF;
 END IF;
 IF NEW.status='active' AND EXISTS(SELECT 1 FROM ai_memory_records m WHERE m.id=NEW.id AND m.revision=NEW.revision)
    AND NOT EXISTS(SELECT 1 FROM ai_memory_sync_outbox o WHERE o.record_id=NEW.id AND o.revision=NEW.revision AND o.kind='retain') THEN
  RAISE EXCEPTION 'active memory write requires an outbox job';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER ai_memory_revision_fence AFTER INSERT OR UPDATE ON ai_memory_records
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION ai_memory_revision_fence();

CREATE FUNCTION ai_memory_queue_cleanup() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO ai_memory_sync_outbox(id,record_id,revision,document_id,kind,payload,dependency_id)
 SELECT gen_random_uuid()::text,record_id,revision,document_id,'delete',payload,id
 FROM ai_memory_sync_outbox WHERE record_id=NEW.id AND kind='retain'
 ON CONFLICT(record_id,revision,kind) DO NOTHING;
 RETURN NULL;
END $$;
CREATE TRIGGER ai_memory_queue_cleanup AFTER UPDATE ON ai_memory_records
 FOR EACH ROW WHEN (OLD.revision IS DISTINCT FROM NEW.revision) EXECUTE FUNCTION ai_memory_queue_cleanup();

CREATE FUNCTION ai_memory_revoke_source() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE session_prefix text; message_ref text;
BEGIN
 IF TG_TABLE_NAME='ai_sessions' THEN
  IF TG_OP='UPDATE' AND NOT ((OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
      OR (COALESCE(OLD.metadata->>'archivedAt','')='' AND COALESCE(NEW.metadata->>'archivedAt','')<>'')) THEN
   RETURN NULL;
  END IF;
  session_prefix := 'soha://session/' || OLD.id || '/message/';
 ELSE
  IF TG_OP='UPDATE' AND OLD.content=NEW.content AND OLD.role=NEW.role THEN RETURN NULL; END IF;
  message_ref := 'soha://session/' || OLD.session_id || '/message/' || OLD.id;
 END IF;
 UPDATE ai_memory_records m SET status='deleted',deleted_at=now(),revision=revision+1,
  payload=payload || jsonb_build_object('status','deleted','deletedAt',now())
 WHERE m.status='active' AND m.owner_type='user'
   AND EXISTS(SELECT 1 FROM jsonb_array_elements_text(
     CASE WHEN jsonb_typeof(m.payload->'sourceRefs')='array' THEN m.payload->'sourceRefs' ELSE '[]'::jsonb END
   ) ref WHERE ref=message_ref OR starts_with(ref,session_prefix));
 RETURN NULL;
END $$;
CREATE TRIGGER ai_memory_revoke_session_source AFTER UPDATE OF deleted_at,metadata OR DELETE ON ai_sessions
 FOR EACH ROW EXECUTE FUNCTION ai_memory_revoke_source();
CREATE TRIGGER ai_memory_revoke_message_source AFTER UPDATE OF content,role OR DELETE ON ai_messages
 FOR EACH ROW EXECUTE FUNCTION ai_memory_revoke_source();

CREATE FUNCTION ai_memory_chat_policy_id(owner text) RETURNS text LANGUAGE sql IMMUTABLE STRICT AS $$
 SELECT 'personal-' || encode(sha256(convert_to(owner,'UTF8') || decode('00','hex') || convert_to('chat','UTF8')),'hex')
$$;

-- Revoking consent fences the current remote revision. Local records remain
-- available for management and explicit re-save after a later opt-in.
CREATE FUNCTION ai_memory_revoke_policy() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE body jsonb; previous jsonb; policy_id text; policy_version text;
BEGIN
 IF TG_OP<>'DELETE' THEN body := NEW.payload; END IF;
 IF TG_OP='INSERT' THEN
  previous := NEW.payload;
  policy_id := NEW.id;
  policy_version := NEW.version;
 ELSE
  previous := OLD.payload;
  policy_id := OLD.id;
  policy_version := OLD.version;
 END IF;
 UPDATE ai_memory_records m SET revision=revision+1
 WHERE m.status='active'
   AND EXISTS(SELECT 1 FROM ai_memory_sync_outbox o WHERE o.record_id=m.id AND o.revision=m.revision AND o.kind='retain')
   AND (
    (m.payload->>'policyId'=policy_id AND m.payload->>'policyVersion'=policy_version AND (
      body IS NULL OR body->'enabled' IS DISTINCT FROM 'true'::jsonb OR body->'readEnabled'='false'::jsonb
      OR COALESCE(NULLIF(body->>'mode',''),'chat')<>COALESCE(NULLIF(previous->>'mode',''),'chat')
      OR (COALESCE(body->>'ownerId','')<>'' AND body->>'ownerId'<>m.owner_id)
      OR NOT COALESCE((body->'ownerTypes') ? m.owner_type,false)
      OR (body->'explicitWriteOnly'='true'::jsonb AND m.payload->>'sourceType'<>'explicit_user')
      OR (jsonb_typeof(body->'minimumConfidence')='number' AND jsonb_typeof(m.payload->'confidence')='number' AND body->'minimumConfidence'>m.payload->'confidence')
    ))
    OR (previous->>'mode'='chat' AND previous->>'ownerId'=m.owner_id
     AND policy_version='v1' AND policy_id=ai_memory_chat_policy_id(m.owner_id) AND
     body IS NOT NULL AND (body->'enabled' IS DISTINCT FROM 'true'::jsonb OR body->'readEnabled'='false'::jsonb)
     AND EXISTS(SELECT 1 FROM ai_memory_policies p WHERE p.id=m.payload->>'policyId' AND p.version=m.payload->>'policyVersion'
       AND COALESCE(p.payload->>'ownerId','')='' AND COALESCE(p.payload->>'mode','') IN ('','chat')))
   );
 RETURN NULL;
END $$;
CREATE TRIGGER ai_memory_revoke_policy AFTER INSERT OR UPDATE OF payload OR DELETE ON ai_memory_policies
 FOR EACH ROW EXECUTE FUNCTION ai_memory_revoke_policy();

-- Reconcile revisions disabled by a prior writer before a delete was enqueued.
INSERT INTO ai_memory_sync_outbox(id,record_id,revision,document_id,kind,payload,dependency_id)
SELECT gen_random_uuid()::text,o.record_id,o.revision,o.document_id,'delete',o.payload,o.id
FROM ai_memory_sync_outbox o JOIN ai_memory_records m ON m.id=o.record_id
WHERE o.kind='retain' AND (m.revision<>o.revision OR m.status<>'active' OR m.deleted_at IS NOT NULL OR m.expires_at<=now()
 OR EXISTS(SELECT 1 FROM ai_runtime_upgrade_quarantine q WHERE q.entity='memory' AND q.id=m.id))
ON CONFLICT(record_id,revision,kind) DO NOTHING;

-- A previous document-only success did not prove task payload cleanup.
UPDATE ai_memory_sync_outbox d SET state='pending',next_attempt_at=now()
FROM ai_memory_sync_outbox o
WHERE d.kind='delete' AND d.state='done' AND d.dependency_id=o.id AND o.attempted;
