CREATE INDEX IF NOT EXISTS idx_alert_events_updated_at_id
    ON public.alert_events (updated_at ASC, id ASC);
