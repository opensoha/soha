-- Database-owned identity and configuration guards; legacy writers remain compatible.
BEGIN;
SELECT pg_advisory_xact_lock(hashtextextended('opensoha/docker/resource-identity/0130', 0));

ALTER TABLE docker_hosts
    ADD COLUMN IF NOT EXISTS resource_uid uuid NOT NULL DEFAULT gen_random_uuid(),
    ADD COLUMN IF NOT EXISTS configuration_revision uuid NOT NULL DEFAULT gen_random_uuid();
CREATE UNIQUE INDEX IF NOT EXISTS docker_hosts_resource_uid_idx ON docker_hosts(resource_uid);

CREATE OR REPLACE FUNCTION guard_docker_hosts_identity() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        NEW.resource_uid := gen_random_uuid();
        NEW.configuration_revision := gen_random_uuid();
    ELSE
        IF NEW.resource_uid IS DISTINCT FROM OLD.resource_uid OR
           NEW.configuration_revision IS DISTINCT FROM OLD.configuration_revision THEN
            RAISE EXCEPTION USING ERRCODE = '23514', MESSAGE = 'Docker identity fields are database-owned';
        END IF;
        IF ROW(NEW.id, NEW.name, NEW.endpoint, NEW.environment, NEW.owner, NEW.team, NEW.virtualization_connection_id, NEW.vm_id, NEW.vm_name, NEW.available_port_start, NEW.available_port_end, NEW.labels, NEW.config)
            IS DISTINCT FROM ROW(OLD.id, OLD.name, OLD.endpoint, OLD.environment, OLD.owner, OLD.team, OLD.virtualization_connection_id, OLD.vm_id, OLD.vm_name, OLD.available_port_start, OLD.available_port_end, OLD.labels, OLD.config) THEN
            NEW.configuration_revision := gen_random_uuid();
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS docker_hosts_identity_guard ON docker_hosts;
CREATE TRIGGER docker_hosts_identity_guard
    BEFORE INSERT OR UPDATE ON docker_hosts
    FOR EACH ROW EXECUTE FUNCTION guard_docker_hosts_identity();

ALTER TABLE docker_projects
    ADD COLUMN IF NOT EXISTS resource_uid uuid NOT NULL DEFAULT gen_random_uuid(),
    ADD COLUMN IF NOT EXISTS configuration_revision uuid NOT NULL DEFAULT gen_random_uuid();
CREATE UNIQUE INDEX IF NOT EXISTS docker_projects_resource_uid_idx ON docker_projects(resource_uid);

CREATE OR REPLACE FUNCTION guard_docker_projects_identity() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        NEW.resource_uid := gen_random_uuid();
        NEW.configuration_revision := gen_random_uuid();
    ELSE
        IF NEW.resource_uid IS DISTINCT FROM OLD.resource_uid OR
           NEW.configuration_revision IS DISTINCT FROM OLD.configuration_revision THEN
            RAISE EXCEPTION USING ERRCODE = '23514', MESSAGE = 'Docker identity fields are database-owned';
        END IF;
        IF ROW(NEW.id, NEW.host_id, NEW.name, NEW.slug, NEW.description, NEW.environment, NEW.owner, NEW.team, NEW.source_kind, NEW.source_ref, NEW.compose_content, NEW.env_content, NEW.template_id, NEW.ttl_seconds, NEW.expires_at, NEW.labels, NEW.config)
            IS DISTINCT FROM ROW(OLD.id, OLD.host_id, OLD.name, OLD.slug, OLD.description, OLD.environment, OLD.owner, OLD.team, OLD.source_kind, OLD.source_ref, OLD.compose_content, OLD.env_content, OLD.template_id, OLD.ttl_seconds, OLD.expires_at, OLD.labels, OLD.config) THEN
            NEW.configuration_revision := gen_random_uuid();
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS docker_projects_identity_guard ON docker_projects;
CREATE TRIGGER docker_projects_identity_guard
    BEFORE INSERT OR UPDATE ON docker_projects
    FOR EACH ROW EXECUTE FUNCTION guard_docker_projects_identity();

COMMIT;
