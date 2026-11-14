SELECT pg_catalog.set_config('search_path', '', false);

-- UID claims belong to parent deployment owners, never individual members.
-- Stopping does not release a claim. There is deliberately no release API.
CREATE TABLE public.runtime_slot_owners (
    workload_uid text PRIMARY KEY,
    owner_kind text NOT NULL CHECK(owner_kind IN ('proxy','outpost','radius')),
    owner_id text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE FUNCTION public.reserve_runtime_slot_uid(uid text, kind text, ownerid text)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE existing public.runtime_slot_owners%ROWTYPE;
BEGIN
    IF uid IS NULL OR uid='' THEN RETURN; END IF;
    INSERT INTO public.runtime_slot_owners(workload_uid,owner_kind,owner_id)
        VALUES(uid,kind,ownerid) ON CONFLICT DO NOTHING;
    SELECT * INTO existing FROM public.runtime_slot_owners WHERE workload_uid=uid FOR UPDATE;
    IF existing.owner_kind<>kind OR existing.owner_id<>ownerid THEN
        RAISE EXCEPTION USING ERRCODE='23505', MESSAGE='runtime_slot_owner_conflict', CONSTRAINT='runtime_slot_owner_conflict';
    END IF;
END;
$$;

CREATE TABLE public.network_radius_pools (
    pool_id text PRIMARY KEY,
    site_id text NOT NULL REFERENCES public.network_access_sites(id),
    nas_binding_id text NOT NULL UNIQUE REFERENCES public.network_access_nas_bindings(id),
    logical_runtime_id text NOT NULL UNIQUE CHECK(logical_runtime_id LIKE 'radius-pool:%'),
    slot_id text NOT NULL UNIQUE,
    workload_uid text NOT NULL,
    revision bigint NOT NULL CHECK(revision>0),
    state jsonb NOT NULL CHECK(jsonb_typeof(state)='object'),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE public.network_radius_members (
    runtime_id text PRIMARY KEY CHECK(runtime_id LIKE 'radius-member:%'),
    pool_id text NOT NULL REFERENCES public.network_radius_pools(pool_id),
    member_id text NOT NULL,
    ordinal integer NOT NULL CHECK(ordinal BETWEEN 0 AND 127),
    record jsonb NOT NULL CHECK(jsonb_typeof(record)='object'),
    UNIQUE(pool_id,member_id), UNIQUE(pool_id,ordinal)
);
CREATE TABLE public.network_radius_requests (
    scope_id text NOT NULL, request_id text NOT NULL,
    fingerprint text NOT NULL, response jsonb NOT NULL,
    PRIMARY KEY(scope_id,request_id)
);

CREATE FUNCTION public.reserve_parent_runtime_uid()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME='network_proxy_deployments' THEN
        PERFORM public.reserve_runtime_slot_uid(NEW.state->>'workloadUid','proxy',NEW.proxy_id);
    ELSIF TG_TABLE_NAME='identity_outpost_runtime_deployments' THEN
        PERFORM public.reserve_runtime_slot_uid(NEW.state->>'workloadUid','outpost',NEW.outpost_id);
    ELSE
        PERFORM public.reserve_runtime_slot_uid(NEW.workload_uid,'radius',NEW.pool_id);
    END IF;
    RETURN NEW;
END;
$$;
-- Some focused repository fixtures omit unrelated runtime tables. Production
-- migrations include them; whichever exists participates in the same fence.
DO $$
DECLARE row record;
BEGIN
    IF to_regclass('public.network_proxy_deployments') IS NOT NULL THEN
        FOR row IN SELECT proxy_id,state->>'workloadUid' AS uid FROM public.network_proxy_deployments ORDER BY proxy_id LOOP
            PERFORM public.reserve_runtime_slot_uid(row.uid,'proxy',row.proxy_id);
        END LOOP;
        CREATE TRIGGER proxy_parent_slot_owner BEFORE INSERT OR UPDATE ON public.network_proxy_deployments
            FOR EACH ROW EXECUTE FUNCTION public.reserve_parent_runtime_uid();
    END IF;
    IF to_regclass('public.identity_outpost_runtime_deployments') IS NOT NULL THEN
        FOR row IN SELECT outpost_id,state->>'workloadUid' AS uid FROM public.identity_outpost_runtime_deployments ORDER BY outpost_id LOOP
            PERFORM public.reserve_runtime_slot_uid(row.uid,'outpost',row.outpost_id);
        END LOOP;
        CREATE TRIGGER outpost_parent_slot_owner BEFORE INSERT OR UPDATE ON public.identity_outpost_runtime_deployments
            FOR EACH ROW EXECUTE FUNCTION public.reserve_parent_runtime_uid();
    END IF;
END;
$$;
CREATE TRIGGER radius_parent_slot_owner BEFORE INSERT OR UPDATE ON public.network_radius_pools
    FOR EACH ROW EXECUTE FUNCTION public.reserve_parent_runtime_uid();

CREATE FUNCTION public.guard_managed_radius_binding()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE poolid text;
BEGIN
    SELECT pool_id INTO poolid FROM public.network_radius_pools WHERE nas_binding_id=OLD.id;
    IF poolid IS NOT NULL AND current_setting('soha.radius_pool_writer',true) IS DISTINCT FROM poolid THEN
        RAISE EXCEPTION USING ERRCODE='23505', MESSAGE='managed_radius_binding_conflict';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER managed_radius_binding_owner BEFORE UPDATE OF runtime_id,site_id,nas_id,status,eap_server_trust
    ON public.network_access_nas_bindings FOR EACH ROW EXECUTE FUNCTION public.guard_managed_radius_binding();

CREATE TABLE public.network_radius_retired_epochs (
 runtime_id text NOT NULL REFERENCES public.network_radius_members(runtime_id),
 process_epoch text NOT NULL, retired_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(runtime_id,process_epoch)
);
CREATE FUNCTION public.guard_managed_radius_credential()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE expected text;
BEGIN
 IF NEW.runtime_id LIKE 'radius-pool:%' THEN
  RAISE EXCEPTION USING ERRCODE='23505',MESSAGE='logical_radius_identity_reserved';
 END IF;
 IF NEW.runtime_id LIKE 'radius-member:%' THEN
  SELECT p.pool_id||':'||(p.state->>'slotHash') INTO expected
  FROM public.network_radius_members m JOIN public.network_radius_pools p ON p.pool_id=m.pool_id
  WHERE m.runtime_id=NEW.runtime_id;
  IF expected IS NULL OR current_setting('soha.radius_member_writer',true) IS DISTINCT FROM expected
   OR NEW.runtime_kind<>'nas' OR NOT (NEW.capabilities @> '["radius-managed-runtime-v1"]'::jsonb) THEN
   RAISE EXCEPTION USING ERRCODE='23505',MESSAGE='managed_radius_member_reserved';
  END IF;
 ELSIF NEW.capabilities @> '["radius-managed-runtime-v1"]'::jsonb THEN
  RAISE EXCEPTION USING ERRCODE='23505',MESSAGE='managed_radius_member_required';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER managed_radius_credential_owner BEFORE INSERT ON public.network_runtime_credentials
 FOR EACH ROW EXECUTE FUNCTION public.guard_managed_radius_credential();

-- Extend the existing execution ownership constraint without weakening the
-- Application, VM, Outpost or Proxy branches already installed by prior migrations.
DO $$
DECLARE previous text;
BEGIN
 SELECT pg_get_expr(conbin,conrelid) INTO STRICT previous FROM pg_constraint
 WHERE conrelid='public.execution_tasks'::regclass AND conname='execution_tasks_vm_command_owner';
 ALTER TABLE public.execution_tasks DROP CONSTRAINT execution_tasks_vm_command_owner;
 EXECUTE format($sql$ALTER TABLE public.execution_tasks ADD CONSTRAINT execution_tasks_vm_command_owner CHECK ((%s) OR COALESCE((
 task_kind='radius_deployment_control' AND target_kind='radius_slot'
 AND jsonb_typeof(payload #> '{radiusDeployment,slot,executorId}')='string'
 AND (payload #>> '{radiusDeployment,slot,executorId}') ~ '^[A-Za-z0-9_-]{1,64}$'
 AND provider_kind='radius_deployment.'||(payload #>> '{radiusDeployment,slot,executorId}')
 AND jsonb_typeof(payload #> '{radiusDeployment,poolId}')='string'
 AND btrim(payload #>> '{radiusDeployment,poolId}')<>''
 AND jsonb_typeof(payload #> '{radiusDeployment,slot,id}')='string'
 AND btrim(payload #>> '{radiusDeployment,slot,id}')<>''
 AND jsonb_typeof(payload #> '{radiusDeployment,slot,uid}')='string'
 AND btrim(payload #>> '{radiusDeployment,slot,uid}')<>''
 AND queue_key=(payload #>> '{radiusDeployment,slot,id}')
 AND lock_key='radius-slot/'||(payload #>> '{radiusDeployment,slot,id}')
 ),false))$sql$,previous);
END;
$$;
