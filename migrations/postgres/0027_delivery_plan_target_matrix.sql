ALTER TABLE public.delivery_plans
    ADD COLUMN IF NOT EXISTS target_ids jsonb DEFAULT '[]'::jsonb NOT NULL;

UPDATE public.delivery_plans
SET target_ids = jsonb_build_array(target_id)
WHERE target_id IS NOT NULL
  AND btrim(target_id) <> ''
  AND target_ids = '[]'::jsonb;
