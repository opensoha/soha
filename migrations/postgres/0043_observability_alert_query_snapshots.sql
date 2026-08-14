ALTER TABLE public.alert_events
    ADD COLUMN IF NOT EXISTS query_snapshot jsonb;

ALTER TABLE public.alert_rule_runs
    ADD COLUMN IF NOT EXISTS query_snapshot jsonb;
