ALTER TABLE public.identity_application_assignments
    DROP CONSTRAINT IF EXISTS identity_application_assignments_effect_check;

ALTER TABLE public.identity_application_assignments
    ADD CONSTRAINT identity_application_assignments_effect_check
    CHECK (effect IN ('allow', 'deny'));

UPDATE public.menus
SET enabled = false,
    updated_at = NOW()
WHERE id = 'identity-policies';
