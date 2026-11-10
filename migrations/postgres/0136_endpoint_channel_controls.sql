CREATE TABLE public.endpoint_channel_policies (
 device_id TEXT PRIMARY KEY REFERENCES public.network_access_devices(id) ON DELETE CASCADE,
 revision BIGINT NOT NULL CHECK(revision>0), draft JSONB NOT NULL,
 published JSONB, published_version BIGINT NOT NULL DEFAULT 0,
 updated_at TIMESTAMPTZ NOT NULL,
 CHECK(published_version>=0 AND published_version<=revision),
 CHECK((published IS NULL AND published_version=0) OR (published IS NOT NULL AND published_version>0))
);
CREATE TABLE public.endpoint_channel_challenges (
 credential_id TEXT PRIMARY KEY REFERENCES public.network_runtime_credentials(id),
 device_id TEXT NOT NULL, policy_version BIGINT NOT NULL,
 token_hash TEXT NOT NULL CHECK(token_hash ~ '^[a-f0-9]{64}$'),
 expires_at TIMESTAMPTZ NOT NULL, consumed BOOLEAN NOT NULL DEFAULT false
);
CREATE TABLE public.endpoint_channel_reports (
 device_id TEXT NOT NULL, credential_id TEXT PRIMARY KEY, policy_version BIGINT NOT NULL,
 observation JSONB NOT NULL, received_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX endpoint_channel_reports_device ON public.endpoint_channel_reports(device_id,received_at DESC);
-- Never forget a runtime which may retain native restrictions, including after
-- credential expiry/revocation. A current generation of that runtime must ack release.
CREATE TABLE public.endpoint_channel_runtime_owners (
 device_id TEXT NOT NULL REFERENCES public.network_access_devices(id) ON DELETE CASCADE,
 runtime_id TEXT NOT NULL, PRIMARY KEY(device_id,runtime_id)
);
CREATE TABLE public.endpoint_channel_events (
 id BIGSERIAL PRIMARY KEY, device_id TEXT NOT NULL, actor_id TEXT NOT NULL,
 action TEXT NOT NULL CHECK(action IN ('saved','published','reported')),
 revision BIGINT NOT NULL, metadata JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX endpoint_channel_events_device ON public.endpoint_channel_events(device_id,id DESC);

CREATE FUNCTION public.guard_endpoint_channel_device_removal() RETURNS trigger LANGUAGE plpgsql AS $$
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
     AND cred.device_id=OLD.id AND cred.subject_id=OLD.owner_user_id AND cred.status='active' AND cred.revoked_at IS NULL
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
-- Urgent device deactivation/revocation must remain possible without releasing
-- native restrictions. Ownership/platform retirement still needs confirmed release.
CREATE TRIGGER endpoint_channel_release_required BEFORE DELETE OR UPDATE OF owner_user_id,platform ON public.network_access_devices
 FOR EACH ROW EXECUTE FUNCTION public.guard_endpoint_channel_device_removal();
