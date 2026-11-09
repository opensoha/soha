-- Keep the browser authorization source private to Core for back-channel policy.
-- Historical sessions remain NULL and cannot satisfy a CIDR policy.
ALTER TABLE public.identity_oidc_sessions ADD COLUMN IF NOT EXISTS authorization_source_ip pg_catalog.inet;
