-- Version snapshots remain immutable. Draft policy changes affect only future publications.
ALTER TABLE build_templates ADD COLUMN migration_policy jsonb;
ALTER TABLE workflow_templates ADD COLUMN migration_policy jsonb;
ALTER TABLE manifest_deployment_status ADD COLUMN applied_template jsonb;

-- Only backfill a proven same-generation apply, never a desired/configuration-only snapshot.
UPDATE manifest_deployment_status status
SET applied_template = jsonb_build_object(
 'templateId', deployment.delivery_snapshot->'templateInputs'->'templateId',
 'templateVersion', deployment.delivery_snapshot->'templateInputs'->'templateVersion',
 'templateDigest', deployment.delivery_snapshot->'templateInputs'->'templateDigest')
FROM manifest_deployments deployment
WHERE deployment.id=status.deployment_id
 AND status.applied_digest = deployment.delivery_snapshot->>'renderedDigest'
 AND length(trim(deployment.delivery_snapshot->'templateInputs'->>'templateId')) > 0
 AND jsonb_typeof(deployment.delivery_snapshot->'templateInputs'->'templateVersion') = 'number'
 AND (deployment.delivery_snapshot->'templateInputs'->>'templateVersion') ~ '^[1-9][0-9]*$'
 AND length(trim(deployment.delivery_snapshot->'templateInputs'->>'templateDigest')) > 0
 AND length(status.applied_digest) > 0
 AND EXISTS (SELECT 1 FROM manifest_deployment_conditions condition
  WHERE condition.deployment_id=deployment.id AND condition.condition_type='Synced' AND condition.reason='TaskCompleted'
   AND condition.status='true' AND condition.observed_generation=deployment.generation);
