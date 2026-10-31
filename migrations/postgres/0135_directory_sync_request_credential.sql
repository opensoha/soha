-- Existing nullable idempotency keys and active-run uniqueness are preserved.
-- This irreversible binding is an identity hash, never an access token.
ALTER TABLE public.directory_sync_runs
    ADD COLUMN IF NOT EXISTS request_credential_id text NOT NULL DEFAULT '';
ALTER TABLE public.directory_sync_runs
    ADD COLUMN IF NOT EXISTS cancel_requested boolean NOT NULL DEFAULT false;
ALTER TABLE public.directory_sync_runs
    ADD COLUMN IF NOT EXISTS creator_stopped_at timestamptz;

-- Old Core cancel SQL does not know the original request's creator context.
-- Reject that update (zero affected rows) so old handlers cannot report a
-- successful cancellation. New cancellation writes only its request flag.
CREATE OR REPLACE FUNCTION public.soha_directory_original_cancel_fence()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.request_credential_id <> '' AND OLD.status = 'running'
       AND NEW.status = 'canceled' AND NEW.creator_stopped_at IS NULL THEN
        RETURN NULL;
    END IF;
    RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS directory_original_cancel_fence ON public.directory_sync_runs;
CREATE TRIGGER directory_original_cancel_fence
BEFORE UPDATE OF status ON public.directory_sync_runs
FOR EACH ROW EXECUTE FUNCTION public.soha_directory_original_cancel_fence();
