ALTER TABLE workflow_runs DROP CONSTRAINT workflow_runs_scope_check;
ALTER TABLE workflow_runs DROP CONSTRAINT workflow_run_scope;
ALTER TABLE workflow_runs ADD CONSTRAINT workflow_runs_scope_check
    CHECK (scope IN ('application', 'delivery_batch', 'capability_task'));
ALTER TABLE workflow_runs ADD CONSTRAINT workflow_run_scope CHECK (
    (scope = 'application' AND delivery_batch_id IS NULL) OR
    (scope = 'delivery_batch' AND application_id = '' AND delivery_batch_id IS NOT NULL) OR
    (scope = 'capability_task' AND application_id = '' AND delivery_batch_id IS NULL)
);
CREATE UNIQUE INDEX workflow_runs_capability_intent
    ON workflow_runs ((metadata->>'capabilityActorId'), (metadata->>'capabilityIdempotencyKey'))
    WHERE scope = 'capability_task';
CREATE INDEX workflow_runs_capability_dispatch ON workflow_runs (lease_until, updated_at)
    WHERE scope = 'capability_task' AND status NOT IN ('completed', 'partially_completed', 'failed', 'canceled', 'inconclusive', 'blocked');
