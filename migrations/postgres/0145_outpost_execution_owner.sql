-- Keep Application and VM ownership unchanged. Only the dedicated, structurally
-- bound Outpost slot task may use the execution ledger without an Application.
-- Installation policy and executor authorization remain application checks.
ALTER TABLE execution_tasks
  DROP CONSTRAINT execution_tasks_vm_command_owner,
  ADD CONSTRAINT execution_tasks_vm_command_owner CHECK (
    application_id IS NOT NULL OR (task_kind='vm_ssh_command' AND target_kind='vm' AND provider_kind LIKE 'vm_ssh.%' AND COALESCE(payload->>'vmId','') <> '' AND COALESCE(payload->>'actorId','') <> '')
    OR COALESCE((
      task_kind = 'outpost_runtime_control'
      AND target_kind = 'outpost_slot'
      AND jsonb_typeof(payload #> '{outpostRuntime,slot,executorId}') = 'string'
      AND (payload #>> '{outpostRuntime,slot,executorId}') ~ '^[A-Za-z0-9_-]{1,64}$'
      AND provider_kind = 'outpost_runtime.' || (payload #>> '{outpostRuntime,slot,executorId}')
      AND jsonb_typeof(payload #> '{outpostRuntime,outpostId}') = 'string'
      AND btrim(payload #>> '{outpostRuntime,outpostId}') <> ''
      AND jsonb_typeof(payload #> '{outpostRuntime,slot,id}') = 'string'
      AND btrim(payload #>> '{outpostRuntime,slot,id}') <> ''
      AND jsonb_typeof(payload #> '{outpostRuntime,slot,uid}') = 'string'
      AND btrim(payload #>> '{outpostRuntime,slot,uid}') <> ''
      AND jsonb_typeof(payload #> '{outpostRuntime,slot,outpostId}') = 'string'
      AND (payload #>> '{outpostRuntime,slot,outpostId}') = (payload #>> '{outpostRuntime,outpostId}')
    ), false)
  );
