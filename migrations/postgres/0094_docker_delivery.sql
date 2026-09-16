ALTER TABLE release_targets ADD COLUMN docker_configuration jsonb NOT NULL DEFAULT 'null'::jsonb;
ALTER TABLE release_targets ALTER COLUMN cluster_id DROP NOT NULL;
ALTER TABLE release_targets ADD CONSTRAINT release_targets_runtime_identity CHECK (
    (executor_kind = 'docker_compose' AND cluster_id IS NULL AND jsonb_typeof(docker_configuration) = 'object')
    OR (executor_kind <> 'docker_compose' AND cluster_id IS NOT NULL)
);
ALTER TABLE delivery_plans ADD COLUMN docker_snapshots jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE delivery_plans ADD COLUMN docker_prepared jsonb NOT NULL DEFAULT '{}'::jsonb;
