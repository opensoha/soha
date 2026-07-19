CREATE TABLE IF NOT EXISTS repositories (
    id text PRIMARY KEY,
    name text NOT NULL,
    provider text NOT NULL,
    url text NOT NULL,
    protocol text NOT NULL,
    gitlab_project_id text,
    path text,
    credential_ref text,
    default_branch text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS application_repositories (
    application_id text NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    repository_id text NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    PRIMARY KEY (application_id, repository_id)
);
ALTER TABLE application_services ADD COLUMN IF NOT EXISTS repository_id text REFERENCES repositories(id) ON DELETE SET NULL;
