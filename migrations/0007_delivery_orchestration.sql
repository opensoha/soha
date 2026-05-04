ALTER TABLE build_records ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP NOT NULL DEFAULT NOW();

CREATE TABLE IF NOT EXISTS application_build_sources (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    source_name TEXT NOT NULL,
    source_type TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    build_image TEXT,
    default_tag TEXT,
    config JSON NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_application_build_sources_application_id ON application_build_sources(application_id);

INSERT INTO application_build_sources (
    id,
    application_id,
    source_name,
    source_type,
    enabled,
    is_default,
    build_image,
    default_tag,
    config,
    created_at,
    updated_at
)
SELECT
    'default:' || a.id,
    a.id,
    'Repository Dockerfile',
    'repo_dockerfile',
    a.enabled,
    TRUE,
    a.build_image,
    a.default_tag,
    json_object(
        'contextDir', COALESCE(NULLIF(a.build_context_dir, ''), '.'),
        'dockerfilePath', COALESCE(NULLIF(a.dockerfile_path, ''), 'Dockerfile'),
        'builderKind', 'docker'
    ),
    a.created_at,
    a.updated_at
FROM applications a
WHERE NOT EXISTS (
    SELECT 1 FROM application_build_sources s WHERE s.application_id = a.id
);

CREATE TABLE IF NOT EXISTS build_templates (
    id TEXT PRIMARY KEY,
    template_key TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT,
    builder_kind TEXT NOT NULL DEFAULT 'custom',
    dockerfile_template TEXT,
    build_commands JSON NOT NULL DEFAULT '[]',
    variable_schema JSON NOT NULL DEFAULT '{}',
    default_variables JSON NOT NULL DEFAULT '{}',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS workflow_approvals (
    id TEXT PRIMARY KEY,
    workflow_run_id TEXT NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    node_id TEXT NOT NULL,
    action TEXT NOT NULL,
    comment TEXT,
    actor_id TEXT NOT NULL,
    actor_name TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_workflow_approvals_workflow_run_id ON workflow_approvals(workflow_run_id);
