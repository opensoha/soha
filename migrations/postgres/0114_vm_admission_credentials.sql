CREATE TABLE IF NOT EXISTS virtualization_capacity_policies (
    source_id TEXT PRIMARY KEY,
	 source_confirmed BOOLEAN NOT NULL DEFAULT TRUE,
    revision BIGINT NOT NULL CHECK (revision > 0),
    mode TEXT NOT NULL CHECK (mode IN ('observing','enforced')),
    cpu_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    cpu_percent INTEGER NOT NULL DEFAULT 100 CHECK (cpu_percent BETWEEN 100 AND 800),
    memory_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    memory_percent INTEGER NOT NULL DEFAULT 100 CHECK (memory_percent BETWEEN 100 AND 150),
    updated_by TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (cpu_enabled OR cpu_percent = 100),
    CHECK (memory_enabled OR memory_percent = 100)
);
CREATE TABLE IF NOT EXISTS virtualization_capacity_sources (
    connection_id TEXT PRIMARY KEY REFERENCES virtualization_connections(id) ON DELETE CASCADE,
    source_id TEXT NOT NULL REFERENCES virtualization_capacity_policies(source_id) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS virtualization_capacity_sources_source ON virtualization_capacity_sources(source_id);
ALTER TABLE virtualization_capacity_reservations DROP CONSTRAINT IF EXISTS virtualization_capacity_reservations_cpu_check;
ALTER TABLE virtualization_capacity_reservations DROP CONSTRAINT IF EXISTS virtualization_capacity_reservations_memory_mib_check;
ALTER TABLE virtualization_capacity_reservations DROP CONSTRAINT IF EXISTS virtualization_capacity_reservations_disk_gib_check;
ALTER TABLE virtualization_capacity_reservations ADD CONSTRAINT virtualization_capacity_reservations_cpu_check CHECK(cpu >= 0);
ALTER TABLE virtualization_capacity_reservations ADD CONSTRAINT virtualization_capacity_reservations_memory_mib_check CHECK(memory_mib >= 0);
ALTER TABLE virtualization_capacity_reservations ADD CONSTRAINT virtualization_capacity_reservations_disk_gib_check CHECK(disk_gib >= 0);
ALTER TABLE virtualization_capacity_reservations ADD COLUMN IF NOT EXISTS policy_revision BIGINT NOT NULL DEFAULT 0;
ALTER TABLE secrets ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'opaque' CHECK(kind IN ('opaque','ssh_keypair','ssh_public_key','ssh_password'));
ALTER TABLE secrets ADD COLUMN IF NOT EXISTS public_key JSONB;
ALTER TABLE secret_versions ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'opaque' CHECK(kind IN ('opaque','ssh_keypair','ssh_public_key','ssh_password'));
ALTER TABLE secret_versions ADD COLUMN IF NOT EXISTS public_key JSONB;
ALTER TABLE secret_leases ADD COLUMN IF NOT EXISTS purpose TEXT NOT NULL DEFAULT '';
ALTER TABLE secret_leases DROP CONSTRAINT IF EXISTS secret_leases_subject_type_check;
ALTER TABLE secret_leases ADD CONSTRAINT secret_leases_subject_type_check CHECK(subject_type IN ('execution_task','agent_run','vm_ssh_session'));
CREATE TABLE IF NOT EXISTS virtualization_vm_access_bindings (
 vm_id text PRIMARY KEY REFERENCES virtualization_vms(id) ON DELETE CASCADE,
 revision integer NOT NULL CHECK(revision > 0),
 binding jsonb NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS virtualization_vm_ssh_sessions (
 id text PRIMARY KEY,
 vm_id text NOT NULL,
 actor_id text NOT NULL,
 status text NOT NULL CHECK(status IN ('pending','active','closed','revoked','unknown')),
 expires_at timestamptz NOT NULL,
 session jsonb NOT NULL,
 encrypted_lease text NOT NULL,
 reason text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS virtualization_vm_ssh_sessions_actor ON virtualization_vm_ssh_sessions(actor_id,status,expires_at);

-- VM commands reuse the execution ledger without inventing an Application owner.
ALTER TABLE execution_tasks ALTER COLUMN application_id DROP NOT NULL;
ALTER TABLE execution_tasks ADD CONSTRAINT execution_tasks_vm_command_owner CHECK (
  application_id IS NOT NULL OR (task_kind='vm_ssh_command' AND target_kind='vm' AND provider_kind LIKE 'vm_ssh.%' AND COALESCE(payload->>'vmId','') <> '' AND COALESCE(payload->>'actorId','') <> '')
);
