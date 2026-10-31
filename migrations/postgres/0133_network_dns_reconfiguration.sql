SELECT pg_catalog.set_config('search_path', '', false);
CREATE TRIGGER network_gateway_dns_lock BEFORE UPDATE OF dns_servers,runtime_id,site_id,administrative_status ON public.network_access_gateways
 FOR EACH STATEMENT EXECUTE FUNCTION public.network_workload_mutation_lock();
CREATE FUNCTION public.expire_gateway_dns_trust() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE affected jsonb;
BEGIN
 IF NEW.dns_servers=OLD.dns_servers AND NEW.runtime_id=OLD.runtime_id AND NEW.site_id=OLD.site_id AND NEW.administrative_status=OLD.administrative_status THEN RETURN NEW; END IF;
 SELECT COALESCE(jsonb_agg(resource_id),'[]'::jsonb) INTO affected FROM public.network_site_dns_mappings WHERE runtime_id=OLD.runtime_id AND tenant_id=OLD.tenant_id AND workspace_id=OLD.workspace_id;
 UPDATE public.network_runtime_sessions s SET status='revoked',revoked_at=CURRENT_TIMESTAMP,revoke_reason='resource_dns_changed',updated_at=CURRENT_TIMESTAMP
 WHERE s.tenant_id=OLD.tenant_id AND s.workspace_id=OLD.workspace_id AND s.status IN ('pending','active','restricted','quarantine') AND EXISTS
 (SELECT 1 FROM public.network_runtime_leases l WHERE l.session_id=s.id AND l.tenant_id=s.tenant_id AND l.workspace_id=s.workspace_id AND l.lease_kind='resource' AND l.resource_ids ?| ARRAY(SELECT jsonb_array_elements_text(affected)));
 UPDATE public.network_runtime_leases l SET status='revoked',revoked_at=CURRENT_TIMESTAMP,revoke_reason='resource_dns_changed',updated_at=CURRENT_TIMESTAMP
 WHERE l.tenant_id=OLD.tenant_id AND l.workspace_id=OLD.workspace_id AND l.status IN ('issued','active','revoking') AND EXISTS(SELECT 1 FROM public.network_runtime_sessions s WHERE s.id=l.session_id AND s.status='revoked' AND s.revoke_reason='resource_dns_changed');
 UPDATE public.network_wireguard_peers p SET status='revoked',revoked_at=CURRENT_TIMESTAMP,revoke_reason='resource_dns_changed',updated_at=CURRENT_TIMESTAMP
 WHERE p.tenant_id=OLD.tenant_id AND p.workspace_id=OLD.workspace_id AND p.status='active' AND EXISTS(SELECT 1 FROM public.network_runtime_sessions s WHERE s.id=p.session_id AND s.status='revoked');
 UPDATE public.network_access_grants SET status='revoked',token_hash=NULL,revoked_at=CURRENT_TIMESTAMP,reason_code='resource_dns_changed',updated_at=CURRENT_TIMESTAMP
 WHERE tenant_id=OLD.tenant_id AND workspace_id=OLD.workspace_id AND status='issued' AND resource_ids ?| ARRAY(SELECT jsonb_array_elements_text(affected));
 UPDATE public.network_vpn_connection_intents i SET status='rejected',token_hash=NULL,updated_at=CURRENT_TIMESTAMP FROM public.network_vpn_documents p
 WHERE i.tenant_id=OLD.tenant_id AND i.workspace_id=OLD.workspace_id AND i.status='issued' AND p.kind='profile' AND p.id=i.profile_id AND p.tenant_id=i.tenant_id AND p.workspace_id=i.workspace_id AND p.payload->'resourceIds' ?| ARRAY(SELECT jsonb_array_elements_text(affected));
 UPDATE public.network_runtime_configurations c SET valid_until=LEAST(valid_until,CURRENT_TIMESTAMP)
 WHERE c.tenant_id=OLD.tenant_id AND c.workspace_id=OLD.workspace_id AND (c.runtime_id=OLD.runtime_id OR c.runtime_id IN
 (SELECT s.runtime_id FROM public.network_runtime_sessions s WHERE s.status='revoked' AND s.revoke_reason='resource_dns_changed') OR c.runtime_id IN
 (SELECT g.runtime_id FROM public.network_wireguard_peers p JOIN public.network_access_gateways g ON g.id=p.gateway_id JOIN public.network_runtime_sessions s ON s.id=p.session_id WHERE s.status='revoked' AND s.revoke_reason='resource_dns_changed'));
 DELETE FROM public.network_site_dns_mappings WHERE runtime_id=OLD.runtime_id AND tenant_id=OLD.tenant_id AND workspace_id=OLD.workspace_id;
 RETURN NEW;
END $$;
CREATE TRIGGER expire_gateway_dns_trust AFTER UPDATE OF dns_servers,runtime_id,site_id,administrative_status ON public.network_access_gateways FOR EACH ROW EXECUTE FUNCTION public.expire_gateway_dns_trust();
