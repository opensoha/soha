CREATE TABLE IF NOT EXISTS virtualization_worker_pools (
    id uuid PRIMARY KEY,
    connection_id text NOT NULL REFERENCES virtualization_connections(id),
    cluster_id text NOT NULL REFERENCES clusters(id),
    revision bigint NOT NULL CHECK (revision > 0),
    spec jsonb NOT NULL,
    identity jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS virtualization_worker_pools_connection ON virtualization_worker_pools(connection_id);
CREATE INDEX IF NOT EXISTS virtualization_tasks_worker_pool ON virtualization_tasks ((payload->>'workerPoolId')) WHERE payload->>'workerPoolId' IS NOT NULL;
