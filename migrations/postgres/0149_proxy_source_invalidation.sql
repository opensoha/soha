-- Legacy encrypted material cannot be safely inferred in SQL. Revalidate source
-- then explicitly publish a new bundle/epoch; old snapshots remain fail closed.
ALTER TABLE network_proxy_sources ADD COLUMN node_material jsonb NOT NULL DEFAULT '{}' CHECK(jsonb_typeof(node_material)='object');
ALTER TABLE network_proxy_sources ADD COLUMN deleted_at timestamptz;
ALTER TABLE network_proxy_sources ADD COLUMN compatibility_floor bigint NOT NULL DEFAULT 1;
UPDATE network_proxy_sources SET compatibility_floor=revision+1;
CREATE TABLE network_proxy_node_invalidations (
 instance_id text NOT NULL, source_id text NOT NULL, node_id text NOT NULL,
 invalidated_source_revision bigint NOT NULL CHECK(invalidated_source_revision>0),
 PRIMARY KEY(instance_id,source_id,node_id,invalidated_source_revision),
 FOREIGN KEY(instance_id,source_id) REFERENCES network_proxy_sources(instance_id,source_id)
);
CREATE TABLE network_proxy_credential_scopes (
 identity_key text PRIMARY KEY, node_ids jsonb NOT NULL CHECK(jsonb_typeof(node_ids)='array')
);

UPDATE network_proxy_bundles b SET record=jsonb_set(jsonb_set(b.record,'{Public,state}','"revoked"'::jsonb),'{Public,reasonCode}','"source_material_revalidation_required"'::jsonb),version=version+1
FROM network_proxy_sources s WHERE b.instance_id=s.instance_id AND b.record->'Public'->>'sourceId'=s.source_id AND ((b.record->'Public'->>'sourceRevision')::bigint<s.compatibility_floor OR NOT(b.record ? 'SourceNodeIDs'));
