-- Missing legacy evidence remains missing: callers must create a new Plan.
CREATE TABLE delivery_plan_service_reference_impacts (
 plan_id TEXT PRIMARY KEY REFERENCES delivery_plans(id) ON DELETE CASCADE,
 snapshot JSONB NOT NULL,
 CHECK (COALESCE((jsonb_typeof(snapshot) = 'object'
  AND snapshot->>'version' = '1'
  AND snapshot->>'planId' = plan_id
  AND jsonb_typeof(snapshot->'consumers') = 'array'
  AND jsonb_typeof(snapshot->'sourceRevisions') = 'object'
  AND snapshot->>'revision' ~ '^[0-9a-f]{64}$'
  AND snapshot->>'databaseRevision' ~ '^[0-9a-f]{64}$'
  AND length(snapshot->>'applicationId') > 0
  AND length(snapshot->>'sourceBindingId') > 0),FALSE))
);
