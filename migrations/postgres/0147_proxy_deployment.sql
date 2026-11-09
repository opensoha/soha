CREATE TABLE network_proxy_deployments(
 proxy_id text PRIMARY KEY REFERENCES network_proxy_instances(id) ON DELETE RESTRICT,
 slot_id text NOT NULL UNIQUE,generation bigint NOT NULL CHECK(generation>0),state jsonb NOT NULL,
 owner_epoch bigint NOT NULL,member_set_hash text NOT NULL,members jsonb NOT NULL CHECK(jsonb_typeof(members)='array'),updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE network_proxy_deployment_requests(proxy_id text NOT NULL REFERENCES network_proxy_deployments(proxy_id),request_id text NOT NULL,fingerprint text NOT NULL,response jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(proxy_id,request_id));
CREATE TABLE network_proxy_deployment_members(runtime_id text PRIMARY KEY REFERENCES network_proxy_instances(id) ON DELETE RESTRICT,proxy_id text NOT NULL REFERENCES network_proxy_deployments(proxy_id),instance_id text NOT NULL,ordinal integer NOT NULL CHECK(ordinal>=0),UNIQUE(proxy_id,instance_id),UNIQUE(proxy_id,ordinal));
CREATE TABLE network_proxy_deployment_instances(proxy_id text NOT NULL REFERENCES network_proxy_deployments(proxy_id),instance_id text NOT NULL,record jsonb NOT NULL,PRIMARY KEY(proxy_id,instance_id));
-- Keep Application and VM ownership unchanged. Only the dedicated, structurally
-- bound Outpost or Proxy slot task may use the execution ledger without an Application.
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
 OR COALESCE((task_kind='proxy_deployment_control' AND target_kind='proxy_slot'
 AND jsonb_typeof(payload #> '{proxyDeployment,slot,executorId}')='string'
 AND (payload #>> '{proxyDeployment,slot,executorId}') ~ '^[A-Za-z0-9_-]{1,64}$'
 AND provider_kind='proxy_deployment.'||(payload #>> '{proxyDeployment,slot,executorId}')
 AND jsonb_typeof(payload #> '{proxyDeployment,proxyId}')='string'
 AND btrim(payload #>> '{proxyDeployment,proxyId}')<>''
 AND jsonb_typeof(payload #> '{proxyDeployment,slot,id}')='string'
 AND btrim(payload #>> '{proxyDeployment,slot,id}')<>''
 AND jsonb_typeof(payload #> '{proxyDeployment,slot,uid}')='string'
 AND btrim(payload #>> '{proxyDeployment,slot,uid}')<>''
 AND jsonb_typeof(payload #> '{proxyDeployment,slot,proxyId}')='string'
 AND (payload #>> '{proxyDeployment,slot,proxyId}')=(payload #>> '{proxyDeployment,proxyId}')
 ),false)

  );
