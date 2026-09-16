-- Reserve a PVE VMID in the original task before any provider write. Historical
-- records without a source key are retained and cannot acquire a false identity.
CREATE UNIQUE INDEX IF NOT EXISTS virtualization_task_provider_identity
ON virtualization_tasks ((payload->>'providerSourceId'), (payload->'providerParams'->>'vmid'))
WHERE provider = 'pve' AND task_kind = 'vm_create'
  AND payload->>'providerIdentityPrepared' = 'true'
  AND NULLIF(payload->>'providerSourceId', '') IS NOT NULL;
