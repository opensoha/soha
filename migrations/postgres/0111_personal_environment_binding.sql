-- Freeze the legacy route without changing leases, volume names, labels,
-- tombstones or unknown operations. data_ref is a logical owner identity;
-- the original Agent still resolves the physical volume from the stored lease.
ALTER TABLE ai_personal_environments
    ADD COLUMN IF NOT EXISTS backend TEXT NOT NULL DEFAULT 'container',
    ADD COLUMN IF NOT EXISTS provider TEXT NOT NULL DEFAULT 'docker',
    ADD COLUMN IF NOT EXISTS policy_revision TEXT NOT NULL DEFAULT 'legacy-docker-v1',
    ADD COLUMN IF NOT EXISTS compute_ref TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS compute_generation BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS data_ref TEXT NOT NULL DEFAULT '';

UPDATE ai_personal_environments
SET data_ref = 'personal-data:' || assistant_id,
    compute_ref = CASE WHEN data_creation_attempted THEN lease->>'id' ELSE '' END,
    compute_generation = CASE WHEN data_creation_attempted THEN 1 ELSE 0 END
WHERE data_ref = '';

ALTER TABLE ai_personal_environments
    ADD CONSTRAINT personal_environment_backend_provider CHECK (
        (backend = 'container' AND provider = 'docker') OR
        (backend = 'vm' AND provider IN ('pve', 'kubevirt'))),
    ADD CONSTRAINT personal_environment_policy_revision CHECK (length(policy_revision) BETWEEN 1 AND 128),
    ADD CONSTRAINT personal_environment_data_ref CHECK (length(data_ref) BETWEEN 1 AND 128),
    ADD CONSTRAINT personal_environment_compute_generation CHECK (compute_generation >= 0),
    ADD CONSTRAINT personal_environment_compute_ref CHECK (
        (compute_generation = 0 AND compute_ref = '') OR
        (compute_generation > 0 AND length(compute_ref) BETWEEN 1 AND 128));

-- Older Core writers omit the new columns during rolling upgrades or rollback.
-- Mirror their lease changes without modifying the lease or claiming readiness.
CREATE FUNCTION sync_legacy_personal_environment_binding() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.data_ref = '' AND NEW.backend = 'container' AND NEW.provider = 'docker' THEN
            NEW.data_ref := 'personal-data:' || NEW.assistant_id;
        END IF;
        IF NEW.backend = 'container' AND NEW.provider = 'docker'
            AND NEW.compute_generation = 0 AND NEW.compute_ref = '' AND NEW.data_creation_attempted THEN
            NEW.compute_generation := 1;
            NEW.compute_ref := NEW.lease->>'id';
        END IF;
    ELSIF NEW.backend = 'container' AND NEW.provider = 'docker'
        AND NEW.compute_generation = OLD.compute_generation AND NEW.compute_ref = OLD.compute_ref
        AND (NEW.data_creation_attempted <> OLD.data_creation_attempted
            OR NEW.lease->>'id' IS DISTINCT FROM OLD.lease->>'id') THEN
        IF NEW.data_creation_attempted THEN
            NEW.compute_generation := OLD.compute_generation + 1;
            NEW.compute_ref := NEW.lease->>'id';
        ELSE
            NEW.compute_generation := 0;
            NEW.compute_ref := '';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER personal_environment_legacy_binding
BEFORE INSERT OR UPDATE ON ai_personal_environments
FOR EACH ROW EXECUTE FUNCTION sync_legacy_personal_environment_binding();
