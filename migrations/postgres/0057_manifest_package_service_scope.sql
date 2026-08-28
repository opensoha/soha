ALTER TABLE public.manifest_packages
    ADD COLUMN IF NOT EXISTS service_id text;

ALTER TABLE public.manifest_packages
    DROP CONSTRAINT IF EXISTS manifest_packages_service_id_fkey;

ALTER TABLE public.manifest_packages
    ADD CONSTRAINT manifest_packages_service_id_fkey
    FOREIGN KEY (service_id) REFERENCES public.application_services(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_manifest_packages_service
    ON public.manifest_packages(service_id)
    WHERE service_id IS NOT NULL;
