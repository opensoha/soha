ALTER TABLE public.application_environments
    ADD COLUMN IF NOT EXISTS alias text,
    ADD COLUMN IF NOT EXISTS cluster_id text REFERENCES public.clusters(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS namespace text,
    ADD COLUMN IF NOT EXISTS registry_id text REFERENCES public.registry_connections(id) ON DELETE SET NULL;
