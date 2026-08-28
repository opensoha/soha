ALTER TABLE public.software_packages
    ADD COLUMN IF NOT EXISTS download_count bigint NOT NULL DEFAULT 0
    CHECK (download_count >= 0);
