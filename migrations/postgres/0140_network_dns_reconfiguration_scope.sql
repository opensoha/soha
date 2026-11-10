SELECT pg_catalog.set_config('search_path', '', false);

-- Replace the function in place so installations that already applied 0133
-- keep its trigger and migration checksum. Only this mutation's revoked
-- sessions may invalidate current endpoint or peer-gateway configurations.
CREATE OR REPLACE FUNCTION public.expire_gateway_dns_trust() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 affected jsonb;
 affected_sessions text[];
 affected_runtimes text[];
BEGIN
 IF NEW.dns_servers=OLD.dns_servers AND NEW.runtime_id=OLD.runtime_id AND NEW.site_id=OLD.site_id AND NEW.administrative_status=OLD.administrative_status THEN RETURN NEW; END IF;
 SELECT COALESCE(jsonb_agg(resource_id),'[]'::jsonb) INTO affected FROM public.network_site_dns_mappings WHERE runtime_id=OLD.runtime_id AND tenant_id=OLD.tenant_id AND workspace_id=OLD.workspace_id;
 WITH revoked AS (
  UPDATE public.network_runtime_sessions s SET status='revoked',revoked_at=CURRENT_TIMESTAMP,revoke_reason='resource_dns_changed',updated_at=CURRENT_TIMESTAMP
  WHERE s.tenant_id=OLD.tenant_id AND s.workspace_id=OLD.workspace_id AND s.status IN ('pending','active','restricted','quarantine') AND EXISTS
   (SELECT 1 FROM public.network_runtime_leases l WHERE l.session_id=s.id AND l.tenant_id=s.tenant_id AND l.workspace_id=s.workspace_id AND l.lease_kind='resource' AND l.status IN ('issued','active','revoking') AND l.resource_ids ?| ARRAY(SELECT jsonb_array_elements_text(affected)))
  RETURNING s.id,s.runtime_id
 ) SELECT COALESCE(array_agg(id),ARRAY[]::text[]),COALESCE(array_agg(DISTINCT runtime_id),ARRAY[]::text[])
 INTO affected_sessions,affected_runtimes FROM revoked;
 UPDATE public.network_runtime_leases l SET status='revoked',revoked_at=CURRENT_TIMESTAMP,revoke_reason='resource_dns_changed',updated_at=CURRENT_TIMESTAMP
 WHERE l.tenant_id=OLD.tenant_id AND l.workspace_id=OLD.workspace_id AND l.status IN ('issued','active','revoking') AND l.session_id=ANY(affected_sessions);
 UPDATE public.network_wireguard_peers p SET status='revoked',revoked_at=CURRENT_TIMESTAMP,revoke_reason='resource_dns_changed',updated_at=CURRENT_TIMESTAMP
 WHERE p.tenant_id=OLD.tenant_id AND p.workspace_id=OLD.workspace_id AND p.status='active' AND p.session_id=ANY(affected_sessions);
 UPDATE public.network_access_grants SET status='revoked',token_hash=NULL,revoked_at=CURRENT_TIMESTAMP,reason_code='resource_dns_changed',updated_at=CURRENT_TIMESTAMP
 WHERE tenant_id=OLD.tenant_id AND workspace_id=OLD.workspace_id AND status='issued' AND resource_ids ?| ARRAY(SELECT jsonb_array_elements_text(affected));
 UPDATE public.network_vpn_connection_intents i SET status='rejected',token_hash=NULL FROM public.network_vpn_documents p
 WHERE i.tenant_id=OLD.tenant_id AND i.workspace_id=OLD.workspace_id AND i.status='issued' AND p.kind='profile' AND p.id=i.profile_id AND p.tenant_id=i.tenant_id AND p.workspace_id=i.workspace_id AND p.published_configuration->'resourceIds' ?| ARRAY(SELECT jsonb_array_elements_text(affected));
 UPDATE public.network_runtime_configurations c SET valid_until=LEAST(valid_until,CURRENT_TIMESTAMP)
 WHERE c.tenant_id=OLD.tenant_id AND c.workspace_id=OLD.workspace_id AND (c.runtime_id=OLD.runtime_id OR c.runtime_id=ANY(affected_runtimes) OR c.runtime_id IN
  (SELECT g.runtime_id FROM public.network_wireguard_peers p JOIN public.network_access_gateways g ON g.id=p.gateway_id AND g.tenant_id=p.tenant_id AND g.workspace_id=p.workspace_id
   WHERE p.tenant_id=OLD.tenant_id AND p.workspace_id=OLD.workspace_id AND p.session_id=ANY(affected_sessions)));
 DELETE FROM public.network_site_dns_mappings WHERE runtime_id=OLD.runtime_id AND tenant_id=OLD.tenant_id AND workspace_id=OLD.workspace_id;
 RETURN NEW;
END $$;
