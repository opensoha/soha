-- Preserve existing Plan evidence. Only new preflights write version 2.
ALTER TABLE delivery_plan_service_reference_impacts
 DROP CONSTRAINT delivery_plan_service_reference_impacts_check,
 ADD CONSTRAINT delivery_plan_service_reference_impacts_check CHECK (
  COALESCE((jsonb_typeof(snapshot) = 'object'
   AND snapshot->>'version' IN ('1', '2')
   AND snapshot->>'planId' = plan_id
   AND jsonb_typeof(snapshot->'consumers') = 'array'
   AND jsonb_typeof(snapshot->'sourceRevisions') = 'object'
   AND snapshot->>'revision' ~ '^[0-9a-f]{64}$'
   AND snapshot->>'databaseRevision' ~ '^[0-9a-f]{64}$'
   AND length(snapshot->>'applicationId') > 0
   AND length(snapshot->>'sourceBindingId') > 0), FALSE));
