ALTER TABLE public.docker_operations
    ADD COLUMN IF NOT EXISTS callback_token text;
