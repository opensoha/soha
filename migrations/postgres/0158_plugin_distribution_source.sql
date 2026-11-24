ALTER TABLE public.installed_plugins ADD COLUMN IF NOT EXISTS source_info jsonb;
