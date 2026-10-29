SELECT pg_catalog.set_config('search_path', '', false);

ALTER TABLE public.network_access_sites ADD COLUMN management_issuer_key text NOT NULL DEFAULT '';
ALTER TABLE public.network_access_sites DROP CONSTRAINT network_access_sites_device_governance_check;
ALTER TABLE public.network_access_sites ADD CONSTRAINT network_access_sites_device_governance_check
 CHECK(device_governance IN ('legacy','managed_only','restricted_unmanaged','verified_managed_only'));
ALTER TABLE public.network_access_sites ADD CONSTRAINT network_access_sites_management_issuer_check CHECK(device_governance<>'verified_managed_only' OR length(management_issuer_key)=44);
CREATE TABLE public.network_management_proofs (
 device_id text PRIMARY KEY REFERENCES public.network_access_devices(id) ON DELETE CASCADE,
 site_id text NOT NULL REFERENCES public.network_access_sites(id) ON DELETE CASCADE,
 issuer_key text NOT NULL,
 credential_generation integer NOT NULL CHECK(credential_generation>0),
 public_key_fingerprint text NOT NULL,
 sequence bigint NOT NULL CHECK(sequence>0),
 management_state text NOT NULL CHECK(management_state IN ('managed','unmanaged')),
 observed_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK(expires_at>observed_at AND expires_at<=observed_at+interval '5 minutes')
);
