ALTER TABLE public.manifest_packages
    ADD COLUMN IF NOT EXISTS archived_at timestamptz;

ALTER TABLE public.manifest_packages
    DROP CONSTRAINT IF EXISTS manifest_packages_application_id_fkey;

ALTER TABLE public.manifest_packages
    ADD CONSTRAINT manifest_packages_application_id_fkey
    FOREIGN KEY (application_id) REFERENCES public.applications(id) ON DELETE RESTRICT;

CREATE INDEX IF NOT EXISTS idx_manifest_packages_active_updated
    ON public.manifest_packages(updated_at DESC)
    WHERE archived_at IS NULL;
