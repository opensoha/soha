SELECT pg_catalog.set_config('search_path', '', false);

ALTER TABLE public.network_access_devices
    ADD COLUMN management_state text NOT NULL DEFAULT 'unknown'
    CHECK (management_state IN ('managed', 'unmanaged', 'unknown'));
ALTER TABLE public.network_access_sites
    ADD COLUMN device_governance text NOT NULL DEFAULT 'legacy'
    CHECK (device_governance IN ('legacy', 'managed_only', 'restricted_unmanaged'));
ALTER TABLE public.network_access_policies
    ADD COLUMN device_management_states jsonb NOT NULL DEFAULT '[]'::jsonb
    CHECK (jsonb_typeof(device_management_states) = 'array');

CREATE OR REPLACE FUNCTION public.bump_network_device_posture_version()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.site_id IS DISTINCT FROM NEW.site_id
       OR OLD.status IS DISTINCT FROM NEW.status
       OR OLD.posture_status IS DISTINCT FROM NEW.posture_status
       OR OLD.management_state IS DISTINCT FROM NEW.management_state THEN
        NEW.posture_version := OLD.posture_version + 1;
    ELSE
        NEW.posture_version := OLD.posture_version;
    END IF;
    RETURN NEW;
END;
$$;
