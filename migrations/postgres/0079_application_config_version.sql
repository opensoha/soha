ALTER TABLE applications ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0);

-- Repository management also writes this relation. Invalidate application drafts
-- in the same transaction, including deletions through a repository foreign key.
CREATE OR REPLACE FUNCTION bump_application_repository_version() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        UPDATE applications SET version = version + 1, updated_at = NOW() WHERE id = OLD.application_id;
    ELSIF TG_OP = 'INSERT' THEN
        UPDATE applications SET version = version + 1, updated_at = NOW() WHERE id = NEW.application_id;
    ELSE
        UPDATE applications SET version = version + 1, updated_at = NOW()
        WHERE id IN (OLD.application_id, NEW.application_id);
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS application_repository_version ON application_repositories;
CREATE TRIGGER application_repository_version AFTER INSERT OR UPDATE OR DELETE
ON application_repositories FOR EACH ROW EXECUTE FUNCTION bump_application_repository_version();
