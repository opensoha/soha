CREATE TABLE IF NOT EXISTS docker_project_creations (
    id uuid PRIMARY KEY,
    request_digest text NOT NULL,
    receipt jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE docker_project_creations IS 'Immutable configuration-only creation receipts, retained after project deletion to prevent replay from recreating work.';
