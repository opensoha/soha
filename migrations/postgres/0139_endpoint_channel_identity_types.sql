-- Keep UUID identity columns compatible with TEXT runtime subjects.
-- Forward-only correction: retain policies, receipts, challenges and removal guard.
CREATE FUNCTION public.network_security_subject_restricted(actor UUID) RETURNS BOOLEAN
 LANGUAGE SQL STABLE AS $$ SELECT public.network_security_subject_restricted(actor::text) $$;

CREATE OR REPLACE FUNCTION public.guard_endpoint_channel_device_removal() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' THEN
  IF NEW.owner_user_id IS NOT DISTINCT FROM OLD.owner_user_id AND NEW.platform IS NOT DISTINCT FROM OLD.platform THEN RETURN NEW; END IF;
 END IF;
 IF EXISTS (
  SELECT 1 FROM public.endpoint_channel_policies p WHERE p.device_id=OLD.id AND p.published_version>0 AND
   (p.published->>'usb'='true' OR p.published->>'print'='true' OR p.published->>'clipboard'='true' OR EXISTS (
    SELECT 1 FROM public.endpoint_channel_runtime_owners owner WHERE owner.device_id=p.device_id AND NOT EXISTS (
    SELECT 1 FROM public.endpoint_channel_reports r JOIN public.network_runtime_credentials cred ON cred.id=r.credential_id
    WHERE r.device_id=p.device_id AND r.policy_version=p.published_version AND cred.runtime_id=owner.runtime_id
     AND r.received_at BETWEEN clock_timestamp()-interval '1 minute' AND clock_timestamp()+interval '30 seconds'
     AND cred.device_id=OLD.id AND cred.subject_id=OLD.owner_user_id::text AND cred.status='active' AND cred.revoked_at IS NULL
     AND cred.runtime_kind='endpoint' AND cred.tenant_id=OLD.tenant_id AND cred.workspace_id=OLD.workspace_id AND cred.capabilities @> '["windows-channel-controls-v1"]'::jsonb
     AND cred.not_before<=clock_timestamp() AND cred.expires_at>clock_timestamp()
     AND cred.generation=(SELECT MAX(n.generation) FROM public.network_runtime_credentials n WHERE n.runtime_id=cred.runtime_id AND n.tenant_id=cred.tenant_id AND n.workspace_id=cred.workspace_id)
     AND jsonb_array_length(r.observation->'channels')=3 AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements(r.observation->'channels') c WHERE c->>'state'<>'off')
   )))
 ) THEN
  RAISE EXCEPTION 'release endpoint channel controls and confirm restoration before removing or reassigning the device'
   USING ERRCODE='23503',CONSTRAINT='endpoint_channel_release_required';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF; RETURN NEW;
END $$;
