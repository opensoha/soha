-- Preserve operation history and legacy standalone tasks; no destructive backfill.
ALTER TABLE manifest_operation_runs ADD COLUMN IF NOT EXISTS delivery_context JSONB;
ALTER TABLE manifest_operation_runs ADD CONSTRAINT manifest_operation_delivery_context_valid
 CHECK (delivery_context IS NULL OR COALESCE((
   jsonb_typeof(delivery_context) = 'object'
   AND jsonb_typeof(delivery_context->'version') = 'number'
   AND jsonb_typeof(delivery_context->'planId') = 'string'
   AND jsonb_typeof(delivery_context->'targetId') = 'string'
   AND jsonb_typeof(delivery_context->'targetRevision') = 'string'
   AND jsonb_typeof(delivery_context->'scopeRevision') = 'string'
   AND delivery_context->>'version' = '1'
   AND length(delivery_context->>'planId') > 0
   AND length(delivery_context->>'targetId') > 0
   AND delivery_context->>'targetRevision' ~ '^[0-9a-f]{64}$'
   AND delivery_context->>'scopeRevision' ~ '^[0-9a-f]{64}$'
   AND delivery_context ?& ARRAY['version','planId','targetId','targetRevision','scopeRevision']
 ), FALSE));
