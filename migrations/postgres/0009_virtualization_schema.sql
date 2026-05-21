CREATE TABLE IF NOT EXISTS virtualization_connections (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    name TEXT NOT NULL,
    endpoint TEXT,
    kubernetes_cluster_id TEXT,
    default_namespace TEXT,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    verify_tls BOOLEAN NOT NULL DEFAULT TRUE,
    encrypted_credential JSONB NOT NULL DEFAULT '{}'::jsonb,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    health JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_synced_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_virtualization_connections_provider ON virtualization_connections(provider);
CREATE INDEX IF NOT EXISTS idx_virtualization_connections_cluster ON virtualization_connections(kubernetes_cluster_id);
CREATE INDEX IF NOT EXISTS idx_virtualization_connections_enabled ON virtualization_connections(enabled);

CREATE TABLE IF NOT EXISTS virtualization_vms (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    connection_id TEXT NOT NULL REFERENCES virtualization_connections(id) ON DELETE CASCADE,
    external_id TEXT NOT NULL,
    name TEXT NOT NULL,
    namespace TEXT,
    status TEXT NOT NULL,
    power_state TEXT,
    node_name TEXT,
    image_id TEXT,
    flavor_id TEXT,
    ip_addresses JSONB NOT NULL DEFAULT '[]'::jsonb,
    labels JSONB NOT NULL DEFAULT '{}'::jsonb,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    raw JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_seen_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider, connection_id, external_id)
);

CREATE INDEX IF NOT EXISTS idx_virtualization_vms_connection ON virtualization_vms(connection_id);
CREATE INDEX IF NOT EXISTS idx_virtualization_vms_namespace ON virtualization_vms(namespace);
CREATE INDEX IF NOT EXISTS idx_virtualization_vms_status ON virtualization_vms(status);
CREATE INDEX IF NOT EXISTS idx_virtualization_vms_last_seen ON virtualization_vms(last_seen_at);

CREATE TABLE IF NOT EXISTS virtualization_images (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    connection_id TEXT NOT NULL REFERENCES virtualization_connections(id) ON DELETE CASCADE,
    external_id TEXT NOT NULL,
    name TEXT NOT NULL,
    status TEXT NOT NULL,
    os_type TEXT,
    architecture TEXT,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    raw JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_seen_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider, connection_id, external_id)
);

CREATE INDEX IF NOT EXISTS idx_virtualization_images_connection ON virtualization_images(connection_id);
CREATE INDEX IF NOT EXISTS idx_virtualization_images_status ON virtualization_images(status);
CREATE INDEX IF NOT EXISTS idx_virtualization_images_last_seen ON virtualization_images(last_seen_at);

CREATE TABLE IF NOT EXISTS virtualization_flavors (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    connection_id TEXT REFERENCES virtualization_connections(id) ON DELETE CASCADE,
    external_id TEXT NOT NULL,
    name TEXT NOT NULL,
    status TEXT NOT NULL,
    cpu_cores INT NOT NULL DEFAULT 0,
    memory_mb INT NOT NULL DEFAULT 0,
    disk_gb INT NOT NULL DEFAULT 0,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    raw JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_seen_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_virtualization_flavors_connection ON virtualization_flavors(connection_id);
CREATE INDEX IF NOT EXISTS idx_virtualization_flavors_status ON virtualization_flavors(status);
CREATE INDEX IF NOT EXISTS idx_virtualization_flavors_last_seen ON virtualization_flavors(last_seen_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_virtualization_flavors_connection_unique ON virtualization_flavors(provider, connection_id, external_id) WHERE connection_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_virtualization_flavors_global_unique ON virtualization_flavors(provider, external_id) WHERE connection_id IS NULL;

CREATE TABLE IF NOT EXISTS virtualization_tasks (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    connection_id TEXT REFERENCES virtualization_connections(id) ON DELETE SET NULL,
    vm_id TEXT REFERENCES virtualization_vms(id) ON DELETE SET NULL,
    task_kind TEXT NOT NULL,
    status TEXT NOT NULL,
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

CREATE INDEX IF NOT EXISTS idx_virtualization_tasks_connection ON virtualization_tasks(connection_id);
CREATE INDEX IF NOT EXISTS idx_virtualization_tasks_vm ON virtualization_tasks(vm_id);
CREATE INDEX IF NOT EXISTS idx_virtualization_tasks_status ON virtualization_tasks(status);
CREATE INDEX IF NOT EXISTS idx_virtualization_tasks_claim ON virtualization_tasks(status, created_at) WHERE status = 'queued';
CREATE INDEX IF NOT EXISTS idx_virtualization_tasks_timeout ON virtualization_tasks(status, last_heartbeat_at, started_at) WHERE status = 'running';
CREATE INDEX IF NOT EXISTS idx_virtualization_tasks_created ON virtualization_tasks(created_at);

CREATE TABLE IF NOT EXISTS virtualization_task_logs (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES virtualization_tasks(id) ON DELETE CASCADE,
    log_level TEXT NOT NULL,
    message TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_virtualization_task_logs_task ON virtualization_task_logs(task_id);
CREATE INDEX IF NOT EXISTS idx_virtualization_task_logs_created ON virtualization_task_logs(created_at);
