SELECT pg_catalog.set_config('search_path', '', false);

ALTER TABLE public.identity_oidc_clients
    ADD COLUMN IF NOT EXISTS client_secret_ciphertext text,
    ADD COLUMN IF NOT EXISTS client_secret_hash_at_encryption text;
