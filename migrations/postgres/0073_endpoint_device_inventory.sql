ALTER TABLE public.network_access_devices
    ADD COLUMN IF NOT EXISTS device_type text NOT NULL DEFAULT 'unknown'
        CHECK (device_type IN ('desktop', 'laptop', 'server', 'mobile', 'tablet', 'virtual', 'unknown')),
    ADD COLUMN IF NOT EXISTS ownership_type text NOT NULL DEFAULT 'unassigned'
        CHECK (ownership_type IN ('company', 'personal', 'temporary', 'unassigned')),
    ADD COLUMN IF NOT EXISTS reported_facts jsonb
        CHECK (reported_facts IS NULL OR jsonb_typeof(reported_facts) = 'object');
