-- Active references are direct configuration links; they never own runtime objects.
CREATE TABLE environment_service_references (
 id text PRIMARY KEY,
 application_id text NOT NULL REFERENCES applications(id) ON DELETE RESTRICT,
 consumer_binding_id text NOT NULL REFERENCES application_environments(id) ON DELETE CASCADE,
 service_id text NOT NULL REFERENCES application_services(id) ON DELETE RESTRICT,
 source_binding_id text NOT NULL REFERENCES application_environments(id) ON DELETE RESTRICT,
 revision bigint NOT NULL CHECK(revision > 0),
 preview jsonb NOT NULL CHECK(jsonb_typeof(preview) = 'object'),
 source_identity_revision text NOT NULL CHECK(source_identity_revision ~ '^[a-f0-9]{64}$'),
 control_revision text NOT NULL CHECK(control_revision ~ '^[a-f0-9]{64}$'),
 consumer_configuration_revision text NOT NULL CHECK(consumer_configuration_revision ~ '^[a-f0-9]{64}$'),
 created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
 UNIQUE(consumer_binding_id, service_id),
 CHECK(consumer_binding_id <> source_binding_id),
 CHECK(preview->>'applicationId' IS NOT DISTINCT FROM application_id),
 CHECK(preview->>'serviceId' IS NOT DISTINCT FROM service_id),
 CHECK(preview->'consumer'->>'applicationEnvironmentId' IS NOT DISTINCT FROM consumer_binding_id),
 CHECK(preview->'source'->>'applicationEnvironmentId' IS NOT DISTINCT FROM source_binding_id)
);
CREATE INDEX environment_service_references_source ON environment_service_references(source_binding_id,service_id,id);
CREATE TABLE environment_service_reference_targets (
 reference_id text NOT NULL REFERENCES environment_service_references(id) ON DELETE CASCADE,
 target_id text NOT NULL REFERENCES release_targets(id) ON DELETE RESTRICT,
 PRIMARY KEY(reference_id,target_id)
);
CREATE INDEX environment_service_reference_targets_source ON environment_service_reference_targets(target_id);

CREATE FUNCTION service_reference_source_busy(binding_id text) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM delivery_plans WHERE application_environment_id=binding_id AND status='confirming')
 OR EXISTS(SELECT 1 FROM execution_tasks WHERE application_environment_id=binding_id AND status IN ('dispatching','running','canceling'))
$$;

CREATE FUNCTION guard_service_reference_write() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE app_id text; source_id text; consumer_id text; svc_id text; target_count integer; indexed_count integer;
BEGIN
 IF TG_OP='DELETE' THEN
  app_id:=OLD.application_id; source_id:=OLD.source_binding_id;
 ELSE
  app_id:=NEW.application_id; source_id:=NEW.source_binding_id;
 END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended('service-references:'||app_id,0));
 IF service_reference_source_busy(source_id) OR (TG_OP='UPDATE' AND service_reference_source_busy(OLD.source_binding_id)) THEN
  RAISE EXCEPTION 'source publication is in progress' USING ERRCODE='23514';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 consumer_id:=NEW.consumer_binding_id; svc_id:=NEW.service_id;
 IF NOT EXISTS(SELECT 1 FROM application_environments c JOIN application_environments s ON s.application_id=c.application_id JOIN application_services a ON a.application_id=c.application_id WHERE c.id=consumer_id AND s.id=source_id AND c.application_id=app_id AND a.id=svc_id AND a.enabled) THEN
  RAISE EXCEPTION 'reference application or service changed' USING ERRCODE='23514';
 END IF;
 IF EXISTS(SELECT 1 FROM release_targets WHERE application_environment_id=consumer_id AND metadata->>'serviceId'=svc_id)
 OR EXISTS(SELECT 1 FROM environment_service_references WHERE consumer_binding_id=source_id AND service_id=svc_id)
 OR EXISTS(SELECT 1 FROM environment_service_references WHERE source_binding_id=consumer_id AND service_id=svc_id) THEN
  RAISE EXCEPTION 'references require an empty consumer slot and a directly owned source' USING ERRCODE='23514';
 END IF;
 SELECT count(*) INTO target_count FROM release_targets WHERE application_environment_id=source_id AND metadata->>'serviceId'=svc_id;
 SELECT count(DISTINCT t.id) INTO indexed_count FROM release_targets t JOIN jsonb_array_elements(NEW.preview->'targets') p ON t.id=p->>'targetId' WHERE t.application_environment_id=source_id AND t.metadata->>'serviceId'=svc_id;
 IF target_count<1 OR target_count>100 OR target_count<>indexed_count OR target_count<>jsonb_array_length(NEW.preview->'targets') THEN
  RAISE EXCEPTION 'reference requires the complete source target group' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER service_reference_write BEFORE INSERT OR UPDATE OR DELETE ON environment_service_references FOR EACH ROW EXECUTE FUNCTION guard_service_reference_write();

CREATE FUNCTION guard_service_reference_target() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE app_id text; binding_id text; svc_id text;
BEGIN
 IF TG_OP='DELETE' THEN binding_id:=OLD.application_environment_id; svc_id:=OLD.metadata->>'serviceId';
 ELSE binding_id:=NEW.application_environment_id; svc_id:=NEW.metadata->>'serviceId'; END IF;
 SELECT application_id INTO app_id FROM application_environments WHERE id=binding_id;
 IF app_id IS NOT NULL THEN PERFORM pg_advisory_xact_lock(hashtextextended('service-references:'||app_id,0)); END IF;
 IF TG_OP<>'DELETE' AND EXISTS(SELECT 1 FROM environment_service_references WHERE consumer_binding_id=binding_id AND service_id=svc_id) THEN
  RAISE EXCEPTION 'active reference occupies this consumer service slot' USING ERRCODE='23514';
 END IF;
 IF TG_OP='INSERT' AND EXISTS(SELECT 1 FROM environment_service_references WHERE source_binding_id=binding_id AND service_id=svc_id) AND NOT EXISTS(SELECT 1 FROM release_targets WHERE id=NEW.id AND application_environment_id=binding_id AND metadata->>'serviceId'=svc_id) THEN
  RAISE EXCEPTION 'detach consumers before adding source targets' USING ERRCODE='23514';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 IF TG_OP='UPDATE' AND EXISTS(SELECT 1 FROM environment_service_reference_targets WHERE target_id=OLD.id) AND
 (ROW(OLD.application_environment_id,OLD.cluster_id,OLD.namespace,OLD.target_kind,OLD.executor_kind,OLD.config_ref,OLD.group_key,OLD.wave_key,OLD.region_key,OLD.workload_kind,OLD.workload_name,OLD.container_name,OLD.enabled,
 OLD.metadata->'serviceId',OLD.metadata->'ownershipMode',OLD.metadata->'importSource',OLD.metadata->'resourceUid',OLD.metadata->'resourceApiVersion',OLD.metadata->'resourceKind',OLD.metadata->'crdName',OLD.metadata->'resourceSource',OLD.helm_configuration->'releaseName') IS DISTINCT FROM
 ROW(NEW.application_environment_id,NEW.cluster_id,NEW.namespace,NEW.target_kind,NEW.executor_kind,NEW.config_ref,NEW.group_key,NEW.wave_key,NEW.region_key,NEW.workload_kind,NEW.workload_name,NEW.container_name,NEW.enabled,
 NEW.metadata->'serviceId',NEW.metadata->'ownershipMode',NEW.metadata->'importSource',NEW.metadata->'resourceUid',NEW.metadata->'resourceApiVersion',NEW.metadata->'resourceKind',NEW.metadata->'crdName',NEW.metadata->'resourceSource',NEW.helm_configuration->'releaseName')) THEN
  RAISE EXCEPTION 'detach consumers before changing source target identity or control' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER service_reference_target BEFORE INSERT OR UPDATE OR DELETE ON release_targets FOR EACH ROW EXECUTE FUNCTION guard_service_reference_target();

CREATE FUNCTION guard_service_reference_binding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended('service-references:'||OLD.application_id,0));
 IF TG_OP='UPDATE' AND OLD.application_id IS DISTINCT FROM NEW.application_id AND EXISTS(SELECT 1 FROM environment_service_references WHERE source_binding_id=OLD.id OR consumer_binding_id=OLD.id) THEN
  RAISE EXCEPTION 'detach references before reassigning their application' USING ERRCODE='23514';
 END IF;
 IF TG_OP='UPDATE' AND EXISTS(SELECT 1 FROM environment_service_references WHERE source_binding_id=OLD.id) AND
 ROW(OLD.application_id,OLD.environment_id,OLD.cluster_id,OLD.namespace) IS DISTINCT FROM ROW(NEW.application_id,NEW.environment_id,NEW.cluster_id,NEW.namespace) THEN
  RAISE EXCEPTION 'detach consumers before relocating a source environment' USING ERRCODE='23514';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER service_reference_binding BEFORE UPDATE OR DELETE ON application_environments FOR EACH ROW EXECUTE FUNCTION guard_service_reference_binding();

CREATE FUNCTION guard_service_reference_manifest_binding() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE app_id text;
BEGIN
 SELECT application_id INTO app_id FROM application_environments WHERE id=OLD.application_environment_id;
 IF app_id IS NOT NULL THEN PERFORM pg_advisory_xact_lock(hashtextextended('service-references:'||app_id,0)); END IF;
 IF EXISTS(SELECT 1 FROM release_targets t JOIN environment_service_reference_targets i ON i.target_id=t.id WHERE t.config_ref=OLD.id) THEN
  IF TG_OP='DELETE' THEN RAISE EXCEPTION 'detach consumers before deleting a Manifest source' USING ERRCODE='23514'; END IF;
  IF ROW(OLD.package_id,OLD.application_environment_id,OLD.cluster_id,OLD.namespace,OLD.enabled) IS DISTINCT FROM ROW(NEW.package_id,NEW.application_environment_id,NEW.cluster_id,NEW.namespace,NEW.enabled) THEN
   RAISE EXCEPTION 'detach consumers before relocating a Manifest source' USING ERRCODE='23514';
  END IF;
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER service_reference_manifest_binding BEFORE UPDATE OR DELETE ON manifest_bindings FOR EACH ROW EXECUTE FUNCTION guard_service_reference_manifest_binding();

-- Keep the index exact even for non-application SQL writers.
CREATE FUNCTION index_service_reference_targets() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 DELETE FROM environment_service_reference_targets WHERE reference_id=NEW.id;
 INSERT INTO environment_service_reference_targets(reference_id,target_id)
 SELECT NEW.id,p->>'targetId' FROM jsonb_array_elements(NEW.preview->'targets') p;
 RETURN NEW;
END $$;
CREATE TRIGGER service_reference_index AFTER INSERT OR UPDATE ON environment_service_references FOR EACH ROW EXECUTE FUNCTION index_service_reference_targets();

CREATE FUNCTION guard_service_reference_manifest_package() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended('service-references:'||OLD.application_id,0));
 IF EXISTS(SELECT 1 FROM manifest_bindings b JOIN release_targets t ON t.config_ref=b.id JOIN environment_service_reference_targets i ON i.target_id=t.id WHERE b.package_id=OLD.id) THEN
  IF TG_OP='DELETE' THEN RAISE EXCEPTION 'detach consumers before deleting a Manifest package' USING ERRCODE='23514'; END IF;
  IF ROW(OLD.application_id,OLD.service_id,OLD.renderer,OLD.archived_at) IS DISTINCT FROM ROW(NEW.application_id,NEW.service_id,NEW.renderer,NEW.archived_at) THEN
   RAISE EXCEPTION 'detach consumers before archiving or reassociating a Manifest package' USING ERRCODE='23514';
  END IF;
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER service_reference_manifest_package BEFORE UPDATE OR DELETE ON manifest_packages FOR EACH ROW EXECUTE FUNCTION guard_service_reference_manifest_package();

CREATE FUNCTION guard_service_reference_manifest_source() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE app_id text;
BEGIN
 SELECT application_id INTO app_id FROM manifest_packages WHERE id=OLD.package_id;
 IF app_id IS NOT NULL THEN PERFORM pg_advisory_xact_lock(hashtextextended('service-references:'||app_id,0)); END IF;
 IF EXISTS(SELECT 1 FROM manifest_bindings b JOIN release_targets t ON t.config_ref=b.id JOIN environment_service_reference_targets i ON i.target_id=t.id WHERE b.package_id=OLD.package_id) THEN
  IF TG_OP='DELETE' THEN RAISE EXCEPTION 'detach consumers before deleting a Manifest source' USING ERRCODE='23514'; END IF;
  IF ROW(OLD.package_id,OLD.mode,OLD.repository_id,OLD.ref_type,OLD.ref_value,OLD.source_path,OLD.include_patterns,OLD.exclude_patterns) IS DISTINCT FROM ROW(NEW.package_id,NEW.mode,NEW.repository_id,NEW.ref_type,NEW.ref_value,NEW.source_path,NEW.include_patterns,NEW.exclude_patterns) THEN
   RAISE EXCEPTION 'detach consumers before relocating a Manifest source' USING ERRCODE='23514';
  END IF;
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER service_reference_manifest_source BEFORE UPDATE OR DELETE ON manifest_sources FOR EACH ROW EXECUTE FUNCTION guard_service_reference_manifest_source();
