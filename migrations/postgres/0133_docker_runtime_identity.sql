-- Existing projects and old SQL writers keep their original runtime namespace.
-- Only new control-plane creation explicitly selects the identity protocol.
BEGIN;
SELECT pg_advisory_xact_lock(hashtextextended('opensoha/docker/runtime-identity/0133', 0));
ALTER TABLE docker_projects ADD COLUMN IF NOT EXISTS runtime_identity_version integer NOT NULL DEFAULT 0;
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'docker_projects'::regclass AND conname = 'docker_projects_runtime_identity_version_check') THEN
        ALTER TABLE docker_projects ADD CONSTRAINT docker_projects_runtime_identity_version_check CHECK (runtime_identity_version IN (0, 1));
    END IF;
END $$;
CREATE OR REPLACE FUNCTION guard_docker_project_runtime_identity() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.runtime_identity_version IS DISTINCT FROM OLD.runtime_identity_version THEN
        RAISE EXCEPTION USING ERRCODE = '23514', MESSAGE = 'Docker runtime identity cannot be adopted by updating configuration';
    END IF;
    RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS docker_project_runtime_identity_guard ON docker_projects;
CREATE TRIGGER docker_project_runtime_identity_guard BEFORE UPDATE ON docker_projects
    FOR EACH ROW EXECUTE FUNCTION guard_docker_project_runtime_identity();
COMMIT;
