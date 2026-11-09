ALTER TABLE network_proxy_instances ADD COLUMN process_epoch text NOT NULL DEFAULT '', ADD COLUMN sample_sequence bigint NOT NULL DEFAULT 0, ADD COLUMN engine_descriptor jsonb, ADD COLUMN retired_epochs jsonb, ADD COLUMN observed_hash text NOT NULL DEFAULT '', ADD COLUMN bundle_id text NOT NULL DEFAULT '', ADD COLUMN bundle_revision bigint NOT NULL DEFAULT 0;
ALTER TABLE network_proxy_connections ADD COLUMN process_epoch text NOT NULL DEFAULT '', ADD COLUMN sample_sequence bigint NOT NULL DEFAULT 0, ADD COLUMN total_active integer;
ALTER TABLE network_proxy_close_commands ADD COLUMN process_epoch text NOT NULL DEFAULT '';
CREATE TABLE network_proxy_runtime_commands (
 id text NOT NULL, instance_id text NOT NULL REFERENCES network_proxy_instances(id), request_id text NOT NULL,
 intent_hash text NOT NULL, process_epoch text NOT NULL, kind text NOT NULL, input jsonb NOT NULL,
 result jsonb, status text NOT NULL CHECK(status IN ('pending','running','succeeded','failed','unsupported','cancelled','expired','unavailable','unknown')),
 reason_code text NOT NULL DEFAULT '', created_by text NOT NULL, created_at timestamptz NOT NULL, expires_at timestamptz NOT NULL,
 PRIMARY KEY(instance_id,id), UNIQUE(instance_id,request_id), CHECK(expires_at > created_at AND expires_at <= created_at + interval '30 seconds')
);
CREATE INDEX network_proxy_commands_pending ON network_proxy_runtime_commands(instance_id,process_epoch,created_at) WHERE status='pending';

ALTER TABLE network_proxy_instances DROP CONSTRAINT network_proxy_instances_engine_check;
ALTER TABLE network_proxy_instances ADD CONSTRAINT network_proxy_instances_engine_check CHECK(engine IN ('mihomo','sing-box','v2ray','external'));

CREATE TABLE network_proxy_desired_batch_receipts (
 publication_id text PRIMARY KEY, intent_hash text NOT NULL, receipt jsonb NOT NULL, created_at timestamptz NOT NULL
);

ALTER TABLE network_proxy_instances
 ADD COLUMN configuration_valid_until timestamptz,
 ADD COLUMN configuration_lease_revision bigint NOT NULL DEFAULT 0,
 ADD COLUMN configuration_lease_hash text NOT NULL DEFAULT '';
