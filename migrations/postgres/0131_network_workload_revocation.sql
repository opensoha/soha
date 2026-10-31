SELECT pg_catalog.set_config('search_path', '', false);

CREATE FUNCTION public.network_workload_mutation_lock() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(hashtext('network-wireguard-mutations'));
 RETURN NULL;
END $$;
CREATE TRIGGER network_workload_token_lock BEFORE UPDATE OF revoked_at,expires_at,permission_keys OR DELETE ON public.service_account_tokens FOR EACH STATEMENT EXECUTE FUNCTION public.network_workload_mutation_lock();
CREATE TRIGGER network_workload_account_lock BEFORE UPDATE OF status,role_ids,team_ids ON public.service_accounts FOR EACH STATEMENT EXECUTE FUNCTION public.network_workload_mutation_lock();
CREATE TRIGGER network_workload_credential_lock BEFORE UPDATE OF status OR DELETE ON public.network_runtime_credentials FOR EACH STATEMENT EXECUTE FUNCTION public.network_workload_mutation_lock();

-- Keep the network ledger in the same transaction as its owning identity
-- revocation. Gateways still report actual data-plane removal separately.
CREATE FUNCTION public.revoke_workload_credential_network() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.subject_id NOT LIKE 'service_account:%' OR OLD.status<>'active' THEN RETURN NEW; END IF;
 IF TG_OP='UPDATE' AND NEW.status='active' THEN RETURN NEW; END IF;
 PERFORM pg_advisory_xact_lock(hashtext('network-wireguard-mutations'));
 UPDATE public.network_runtime_sessions SET status='revoked',revoked_at=CURRENT_TIMESTAMP,revoke_reason='workload_credential_revoked',updated_at=CURRENT_TIMESTAMP
 WHERE tenant_id=OLD.tenant_id AND workspace_id=OLD.workspace_id AND runtime_id=OLD.runtime_id AND subject_id=OLD.subject_id AND device_id=OLD.device_id AND status IN ('pending','active','restricted','quarantine');
 UPDATE public.network_runtime_leases SET status='revoked',revoked_at=CURRENT_TIMESTAMP,revoke_reason='workload_credential_revoked',updated_at=CURRENT_TIMESTAMP
 WHERE tenant_id=OLD.tenant_id AND workspace_id=OLD.workspace_id AND status IN ('issued','active','revoking') AND session_id IN
 (SELECT id FROM public.network_runtime_sessions WHERE tenant_id=OLD.tenant_id AND workspace_id=OLD.workspace_id AND runtime_id=OLD.runtime_id AND subject_id=OLD.subject_id AND device_id=OLD.device_id AND status='revoked');
 UPDATE public.network_wireguard_peers SET status='revoked',revoked_at=CURRENT_TIMESTAMP,revoke_reason='workload_credential_revoked',updated_at=CURRENT_TIMESTAMP
 WHERE tenant_id=OLD.tenant_id AND workspace_id=OLD.workspace_id AND runtime_id=OLD.runtime_id AND device_id=OLD.device_id AND status='active';
 UPDATE public.network_runtime_configurations SET valid_until=LEAST(valid_until,CURRENT_TIMESTAMP)
 WHERE tenant_id=OLD.tenant_id AND workspace_id=OLD.workspace_id AND
 (runtime_id=OLD.runtime_id OR runtime_id IN (SELECT g.runtime_id FROM public.network_wireguard_peers p JOIN public.network_access_gateways g ON g.id=p.gateway_id WHERE p.tenant_id=OLD.tenant_id AND p.workspace_id=OLD.workspace_id AND p.runtime_id=OLD.runtime_id));
 RETURN NEW;
END $$;
CREATE TRIGGER revoke_workload_credential_network AFTER UPDATE OF status OR DELETE ON public.network_runtime_credentials
 FOR EACH ROW EXECUTE FUNCTION public.revoke_workload_credential_network();

CREATE FUNCTION public.revoke_workload_token_credentials() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' THEN
  IF NEW.revoked_at IS NULL AND (NEW.expires_at IS NULL OR NEW.expires_at>CURRENT_TIMESTAMP)
   AND NEW.permission_keys::jsonb=OLD.permission_keys::jsonb
   AND (NEW.expires_at IS NULL OR OLD.expires_at IS NOT NULL AND NEW.expires_at>=OLD.expires_at) THEN RETURN NEW; END IF;
 END IF;
 PERFORM pg_advisory_xact_lock(hashtext('network-wireguard-mutations'));
 UPDATE public.network_runtime_credentials SET status='revoked',revoked_at=CURRENT_TIMESTAMP
 WHERE source_token_id=OLD.id AND subject_id='service_account:'||OLD.service_account_id AND status='active';
 RETURN NEW;
END $$;
CREATE TRIGGER revoke_workload_token_credentials AFTER UPDATE OF revoked_at,expires_at,permission_keys OR DELETE ON public.service_account_tokens
 FOR EACH ROW EXECUTE FUNCTION public.revoke_workload_token_credentials();

CREATE FUNCTION public.revoke_service_account_network_credentials() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status=OLD.status AND NEW.role_ids::jsonb=OLD.role_ids::jsonb AND NEW.team_ids::jsonb=OLD.team_ids::jsonb THEN RETURN NEW; END IF;
 PERFORM pg_advisory_xact_lock(hashtext('network-wireguard-mutations'));
 UPDATE public.network_runtime_credentials SET status='revoked',revoked_at=CURRENT_TIMESTAMP
 WHERE subject_id='service_account:'||OLD.id AND runtime_kind='endpoint' AND status='active';
 RETURN NEW;
END $$;
CREATE TRIGGER revoke_service_account_network_credentials AFTER UPDATE OF status,role_ids,team_ids ON public.service_accounts
 FOR EACH ROW EXECUTE FUNCTION public.revoke_service_account_network_credentials();
