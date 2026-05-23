CREATE TABLE IF NOT EXISTS application_services (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    service_key TEXT NOT NULL,
    service_name TEXT NOT NULL,
    description TEXT,
    service_kind TEXT NOT NULL DEFAULT 'kubernetes_workload',
    owner_team TEXT,
    repository_provider TEXT,
    repository_project_id TEXT,
    repository_path TEXT,
    default_branch TEXT,
    build_source_id TEXT,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT application_services_application_key_unique UNIQUE (application_id, service_key)
);

CREATE TABLE IF NOT EXISTS application_service_containers (
    id TEXT PRIMARY KEY,
    service_id TEXT NOT NULL REFERENCES application_services(id) ON DELETE CASCADE,
    container_name TEXT NOT NULL,
    image_repository TEXT,
    default_tag_template TEXT,
    dockerfile_path TEXT,
    build_context_dir TEXT,
    runtime_ports JSONB NOT NULL DEFAULT '[]'::jsonb,
    env_schema JSONB NOT NULL DEFAULT '{}'::jsonb,
    resource_profile JSONB NOT NULL DEFAULT '{}'::jsonb,
    health_check JSONB NOT NULL DEFAULT '{}'::jsonb,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT application_service_containers_service_name_unique UNIQUE (service_id, container_name)
);

CREATE INDEX IF NOT EXISTS idx_application_services_application_id ON application_services(application_id);
CREATE INDEX IF NOT EXISTS idx_application_services_build_source_id ON application_services(build_source_id);
CREATE INDEX IF NOT EXISTS idx_application_service_containers_service_id ON application_service_containers(service_id);
