ALTER TABLE docker_hosts
    ADD COLUMN IF NOT EXISTS architecture TEXT;

CREATE INDEX IF NOT EXISTS idx_docker_hosts_architecture ON docker_hosts(architecture);
