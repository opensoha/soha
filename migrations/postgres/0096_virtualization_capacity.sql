CREATE TABLE IF NOT EXISTS virtualization_capacity_reservations (
    task_id TEXT PRIMARY KEY REFERENCES virtualization_tasks(id) ON DELETE RESTRICT,
    source_id TEXT NOT NULL,
    node_name TEXT NOT NULL,
    namespace TEXT NOT NULL DEFAULT '',
    storage_key TEXT NOT NULL,
    cpu BIGINT NOT NULL CHECK (cpu > 0),
    memory_mib BIGINT NOT NULL CHECK (memory_mib > 0),
    disk_gib BIGINT NOT NULL CHECK (disk_gib > 0),
    state TEXT NOT NULL DEFAULT 'reserved' CHECK (state IN ('reserved', 'accounted', 'released')),
    observed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS virtualization_capacity_reservations_source
ON virtualization_capacity_reservations(source_id) WHERE state = 'reserved';
