SELECT pg_catalog.set_config('search_path', '', false);

ALTER TABLE public.directory_event_inbox ADD COLUMN IF NOT EXISTS payload jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE public.directory_people ADD COLUMN IF NOT EXISTS departed_at timestamptz;
ALTER TABLE public.directory_connections ADD COLUMN IF NOT EXISTS webhook_verified_at timestamptz;
ALTER TABLE public.directory_connections ADD COLUMN IF NOT EXISTS last_event_at timestamptz;
ALTER TABLE public.directory_connections ADD COLUMN IF NOT EXISTS last_incremental_at timestamptz;
ALTER TABLE public.directory_connections ADD COLUMN IF NOT EXISTS last_full_reconcile_at timestamptz;
ALTER TABLE public.directory_connections ADD COLUMN IF NOT EXISTS reconcile_required boolean NOT NULL DEFAULT false;
ALTER TABLE public.directory_connections ADD COLUMN IF NOT EXISTS reconcile_reason text NOT NULL DEFAULT '';
