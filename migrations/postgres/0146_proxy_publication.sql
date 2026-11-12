-- Private source material is application-encrypted; public contains configured metadata only.
CREATE TABLE IF NOT EXISTS network_proxy_sources (
 instance_id text NOT NULL REFERENCES network_proxy_instances(id),
 source_id text NOT NULL, revision bigint NOT NULL CHECK(revision > 0),
 public jsonb NOT NULL, encrypted_input text NOT NULL, encrypted_ir text NOT NULL,
 PRIMARY KEY(instance_id,source_id)
);
CREATE TABLE IF NOT EXISTS network_proxy_rule_sets (
 instance_id text NOT NULL REFERENCES network_proxy_instances(id), owner_id text NOT NULL DEFAULT '',
 revision bigint NOT NULL CHECK(revision > 0), rules jsonb NOT NULL DEFAULT '[]',
 PRIMARY KEY(instance_id,owner_id)
);

ALTER TABLE network_mihomo_profiles ADD COLUMN IF NOT EXISTS delivery_mode text NOT NULL DEFAULT 'direct_subscription' CHECK(delivery_mode IN ('direct_subscription','mapped_nodes','gateway'));
ALTER TABLE network_mihomo_profiles ADD COLUMN IF NOT EXISTS bundle_id text NOT NULL DEFAULT '';
ALTER TABLE network_mihomo_profiles ADD COLUMN IF NOT EXISTS org_revision bigint NOT NULL DEFAULT 0;
ALTER TABLE network_mihomo_profiles ADD COLUMN IF NOT EXISTS source_revision bigint NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS network_proxy_bundles (
 instance_id text NOT NULL REFERENCES network_proxy_instances(id), bundle_id text NOT NULL,
 revision bigint NOT NULL CHECK(revision > 0), version bigint NOT NULL DEFAULT 1,
 record jsonb NOT NULL, PRIMARY KEY(instance_id,bundle_id)
);
CREATE TABLE IF NOT EXISTS network_proxy_employee_credentials (
 id text PRIMARY KEY, instance_id text NOT NULL, bundle_id text NOT NULL, renewal_key text NOT NULL UNIQUE, expires_at timestamptz NOT NULL, record jsonb NOT NULL,
 FOREIGN KEY(instance_id,bundle_id) REFERENCES network_proxy_bundles(instance_id,bundle_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS network_proxy_bundle_global_id ON network_proxy_bundles(bundle_id);

CREATE TABLE IF NOT EXISTS network_proxy_projection_receipts (
 credential_id text NOT NULL, profile_id text NOT NULL, process_epoch text NOT NULL,
 receipt jsonb NOT NULL, PRIMARY KEY(credential_id,profile_id,process_epoch)
);

CREATE INDEX IF NOT EXISTS network_proxy_employee_credential_expiry ON network_proxy_employee_credentials(expires_at);

ALTER TABLE network_mihomo_profiles DROP CONSTRAINT network_mihomo_profiles_source_check;
ALTER TABLE network_mihomo_profiles ADD CONSTRAINT network_mihomo_profiles_source_check CHECK (
 (bundle_id <> '' AND mode='managed_follow' AND source_type IS NULL AND subscription_url_ciphertext IS NULL AND manual_node_ciphertext IS NULL AND fail_closed AND source_revision>0 AND org_revision>=0)
 OR
 (bundle_id='' AND delivery_mode='direct_subscription' AND (
  (mode='managed_follow' AND source_type='managed_subscription' AND subscription_url_ciphertext IS NOT NULL AND manual_node_ciphertext IS NULL AND selected_proxy IS NOT NULL AND fail_closed)
  OR (mode='managed_follow' AND source_type='manual_node' AND subscription_url_ciphertext IS NULL AND manual_node_ciphertext IS NOT NULL AND selected_proxy IS NULL AND fail_closed)
  OR (mode='app_subscription' AND source_type IS NULL AND subscription_url_ciphertext IS NULL AND manual_node_ciphertext IS NULL AND selected_proxy IS NULL)
 ))
);
