CREATE TABLE IF NOT EXISTS docker_hosts (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    endpoint TEXT,
    agent_id TEXT,
    agent_version TEXT,
    docker_version TEXT,
    compose_version TEXT,
    environment TEXT,
    owner TEXT,
    team TEXT,
    virtualization_connection_id TEXT,
    vm_id TEXT,
    vm_name TEXT,
    ip_address TEXT,
    cpu_core_count INT NOT NULL DEFAULT 0,
    memory_bytes BIGINT NOT NULL DEFAULT 0,
    disk_bytes BIGINT NOT NULL DEFAULT 0,
    available_port_start INT NOT NULL DEFAULT 20000,
    available_port_end INT NOT NULL DEFAULT 39999,
    labels JSONB NOT NULL DEFAULT '{}'::jsonb,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_heartbeat_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_docker_hosts_status ON docker_hosts(status);
CREATE INDEX IF NOT EXISTS idx_docker_hosts_environment ON docker_hosts(environment);
CREATE INDEX IF NOT EXISTS idx_docker_hosts_vm ON docker_hosts(vm_id);
CREATE INDEX IF NOT EXISTS idx_docker_hosts_agent ON docker_hosts(agent_id);

CREATE TABLE IF NOT EXISTS docker_projects (
    id TEXT PRIMARY KEY,
    host_id TEXT NOT NULL REFERENCES docker_hosts(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    slug TEXT NOT NULL,
    description TEXT,
    environment TEXT,
    owner TEXT,
    team TEXT,
    source_kind TEXT,
    source_ref TEXT,
    compose_content TEXT,
    env_content TEXT,
    status TEXT NOT NULL DEFAULT 'draft',
    desired_state TEXT,
    template_id TEXT,
    ttl_seconds INT NOT NULL DEFAULT 0,
    expires_at TIMESTAMPTZ,
    last_deployed_at TIMESTAMPTZ,
    labels JSONB NOT NULL DEFAULT '{}'::jsonb,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (host_id, slug)
);

CREATE INDEX IF NOT EXISTS idx_docker_projects_host ON docker_projects(host_id);
CREATE INDEX IF NOT EXISTS idx_docker_projects_status ON docker_projects(status);
CREATE INDEX IF NOT EXISTS idx_docker_projects_environment ON docker_projects(environment);
CREATE INDEX IF NOT EXISTS idx_docker_projects_expires ON docker_projects(expires_at);

CREATE TABLE IF NOT EXISTS docker_services (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES docker_projects(id) ON DELETE CASCADE,
    host_id TEXT NOT NULL REFERENCES docker_hosts(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    image TEXT,
    status TEXT NOT NULL DEFAULT 'unknown',
    container_id TEXT,
    restart_count INT NOT NULL DEFAULT 0,
    cpu_percent DOUBLE PRECISION NOT NULL DEFAULT 0,
    memory_bytes BIGINT NOT NULL DEFAULT 0,
    network_rx_bytes BIGINT NOT NULL DEFAULT 0,
    network_tx_bytes BIGINT NOT NULL DEFAULT 0,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_seen_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, name)
);

CREATE INDEX IF NOT EXISTS idx_docker_services_project ON docker_services(project_id);
CREATE INDEX IF NOT EXISTS idx_docker_services_host ON docker_services(host_id);
CREATE INDEX IF NOT EXISTS idx_docker_services_status ON docker_services(status);

CREATE TABLE IF NOT EXISTS docker_port_mappings (
    id TEXT PRIMARY KEY,
    host_id TEXT NOT NULL REFERENCES docker_hosts(id) ON DELETE CASCADE,
    project_id TEXT REFERENCES docker_projects(id) ON DELETE SET NULL,
    service_id TEXT REFERENCES docker_services(id) ON DELETE SET NULL,
    name TEXT NOT NULL,
    host_ip TEXT,
    host_port INT NOT NULL,
    container_port INT NOT NULL,
    protocol TEXT NOT NULL DEFAULT 'tcp',
    exposure_scope TEXT NOT NULL DEFAULT 'internal',
    status TEXT NOT NULL DEFAULT 'active',
    domain_name TEXT,
    domain_scheme TEXT,
    domain_tls_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    access_url TEXT,
    owner TEXT,
    expires_at TIMESTAMPTZ,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_docker_ports_host ON docker_port_mappings(host_id);
CREATE INDEX IF NOT EXISTS idx_docker_ports_project ON docker_port_mappings(project_id);
CREATE INDEX IF NOT EXISTS idx_docker_ports_service ON docker_port_mappings(service_id);
CREATE INDEX IF NOT EXISTS idx_docker_ports_status ON docker_port_mappings(status);
CREATE INDEX IF NOT EXISTS idx_docker_ports_domain ON docker_port_mappings(domain_name);
CREATE UNIQUE INDEX IF NOT EXISTS idx_docker_ports_unique_active
    ON docker_port_mappings(host_id, COALESCE(host_ip, ''), host_port, protocol)
    WHERE status <> 'released';
CREATE UNIQUE INDEX IF NOT EXISTS idx_docker_ports_unique_active_domain
    ON docker_port_mappings(LOWER(domain_name))
    WHERE status <> 'released' AND domain_name IS NOT NULL AND domain_name <> '';

CREATE TABLE IF NOT EXISTS docker_templates (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    template_kind TEXT NOT NULL DEFAULT 'compose',
    compose_content TEXT,
    env_content TEXT,
    variables JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_docker_templates_enabled ON docker_templates(enabled);
CREATE INDEX IF NOT EXISTS idx_docker_templates_kind ON docker_templates(template_kind);

CREATE TABLE IF NOT EXISTS docker_operations (
    id TEXT PRIMARY KEY,
    host_id TEXT REFERENCES docker_hosts(id) ON DELETE SET NULL,
    project_id TEXT REFERENCES docker_projects(id) ON DELETE SET NULL,
    service_id TEXT REFERENCES docker_services(id) ON DELETE SET NULL,
    operation_kind TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    requested_by TEXT,
    claimed_by_worker_id TEXT,
    attempt_count INT NOT NULL DEFAULT 0,
    max_retries INT NOT NULL DEFAULT 1,
    timeout_seconds INT NOT NULL DEFAULT 1800,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    result JSONB NOT NULL DEFAULT '{}'::jsonb,
    started_at TIMESTAMPTZ,
    last_heartbeat_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_docker_operations_host ON docker_operations(host_id);
CREATE INDEX IF NOT EXISTS idx_docker_operations_project ON docker_operations(project_id);
CREATE INDEX IF NOT EXISTS idx_docker_operations_service ON docker_operations(service_id);
CREATE INDEX IF NOT EXISTS idx_docker_operations_status ON docker_operations(status);
CREATE INDEX IF NOT EXISTS idx_docker_operations_kind ON docker_operations(operation_kind);
CREATE INDEX IF NOT EXISTS idx_docker_operations_created ON docker_operations(created_at);

CREATE TABLE IF NOT EXISTS docker_operation_logs (
    id TEXT PRIMARY KEY,
    operation_id TEXT NOT NULL REFERENCES docker_operations(id) ON DELETE CASCADE,
    log_level TEXT NOT NULL,
    message TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_docker_operation_logs_operation ON docker_operation_logs(operation_id);
CREATE INDEX IF NOT EXISTS idx_docker_operation_logs_created ON docker_operation_logs(created_at);
