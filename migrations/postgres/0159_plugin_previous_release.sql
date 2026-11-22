CREATE TABLE IF NOT EXISTS public.plugin_previous_releases (
    plugin_id text PRIMARY KEY REFERENCES public.installed_plugins(id) ON DELETE CASCADE,
    snapshot jsonb NOT NULL
);
