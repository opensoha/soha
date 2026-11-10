-- Expand only: older servers ignore these nullable columns. Upgrade all servers
-- before issuing governed AI credentials; rollback must first drain their tasks.
ALTER TABLE public.ai_gateway_approval_requests
    ADD COLUMN IF NOT EXISTS actor_token_id text,
    ADD COLUMN IF NOT EXISTS actor_permission_ceiling json;
