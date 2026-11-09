SELECT pg_catalog.set_config('search_path', '', false);

-- Dedicated TTLS/PAP protection state. No submitted password or password hash
-- is stored here. Tokens are opaque, random 256-bit values, stored by SHA-256.
CREATE TABLE IF NOT EXISTS public.network_password_budgets (
    bucket_hash text PRIMARY KEY CHECK (bucket_hash ~ '^[a-f0-9]{64}$'),
    window_start timestamptz NOT NULL,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    failures integer NOT NULL DEFAULT 0 CHECK (failures >= 0),
    locked_until timestamptz,
    active_attempt_id text,
    active_until timestamptz,
    expires_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS network_password_budgets_expiry ON public.network_password_budgets(expires_at);

CREATE TABLE IF NOT EXISTS public.network_password_receipts (
    token_hash text PRIMARY KEY CHECK (token_hash ~ '^[a-f0-9]{64}$'),
    runtime_id text NOT NULL,
    nas_id text NOT NULL,
    site_id text NOT NULL,
    station_id text NOT NULL,
    subject_id uuid NOT NULL REFERENCES public.users(id),
    password_updated_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    authorization_request_id text,
    consumed_at timestamptz,
    CHECK (expires_at > created_at AND expires_at <= created_at + interval '30 seconds')
);
CREATE INDEX IF NOT EXISTS network_password_receipts_expiry ON public.network_password_receipts(expires_at);
CREATE INDEX IF NOT EXISTS network_password_receipts_subject ON public.network_password_receipts(subject_id);

CREATE TABLE IF NOT EXISTS public.network_password_stations (
    device_id text PRIMARY KEY REFERENCES public.network_access_devices(id) ON DELETE CASCADE,
    runtime_id text NOT NULL,
    nas_id text NOT NULL,
    site_id text NOT NULL REFERENCES public.network_access_sites(id),
    station_id text NOT NULL,
    subject_id uuid NOT NULL REFERENCES public.users(id),
    UNIQUE(runtime_id,nas_id,station_id,subject_id)
);

ALTER TABLE public.network_eap_credentials
    ADD COLUMN activation_profile_id text NOT NULL DEFAULT '',
    ADD COLUMN activation_profile_revision text NOT NULL DEFAULT '';

CREATE OR REPLACE FUNCTION public.guard_network_password_station()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS(SELECT 1 FROM public.network_password_stations WHERE device_id=OLD.id)
       AND (NEW.owner_user_id IS DISTINCT FROM OLD.owner_user_id
            OR NEW.site_id IS DISTINCT FROM OLD.site_id
            OR NEW.management_state<>'unmanaged'
            OR NEW.posture_status<>'unknown'
            OR NEW.credential_generation<>OLD.credential_generation) THEN
        RAISE EXCEPTION 'password station cannot become an enrolled or managed device';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER network_password_station_guard BEFORE UPDATE ON public.network_access_devices
    FOR EACH ROW EXECUTE FUNCTION public.guard_network_password_station();

ALTER TABLE public.network_runtime_sessions
    DROP CONSTRAINT IF EXISTS network_runtime_sessions_authentication_method_check;
ALTER TABLE public.network_runtime_sessions
    ADD CONSTRAINT network_runtime_sessions_authentication_method_check
    CHECK (authentication_method IS NULL OR authentication_method IN ('eap-tls','password-compatible','eap-ttls-pap'));
ALTER TABLE public.network_nas_authorizations
    DROP CONSTRAINT IF EXISTS network_nas_authorizations_authentication_method_check;
ALTER TABLE public.network_nas_authorizations
    ADD CONSTRAINT network_nas_authorizations_authentication_method_check
    CHECK (authentication_method IN ('eap-tls','password-compatible','eap-ttls-pap'));
