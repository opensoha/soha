SELECT pg_catalog.set_config('search_path', '', false);

ALTER TABLE public.network_access_devices
    ADD COLUMN IF NOT EXISTS posture_version bigint NOT NULL DEFAULT 1 CHECK (posture_version >= 1);

CREATE OR REPLACE FUNCTION public.bump_network_device_posture_version()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.site_id IS DISTINCT FROM NEW.site_id
       OR OLD.status IS DISTINCT FROM NEW.status
       OR OLD.posture_status IS DISTINCT FROM NEW.posture_status THEN
        NEW.posture_version := OLD.posture_version + 1;
    ELSE
        NEW.posture_version := OLD.posture_version;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS network_access_devices_posture_version ON public.network_access_devices;
CREATE TRIGGER network_access_devices_posture_version
    BEFORE UPDATE ON public.network_access_devices
    FOR EACH ROW
    EXECUTE FUNCTION public.bump_network_device_posture_version();
