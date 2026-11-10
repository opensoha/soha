-- Expand only. Protected replay input is never part of the approval HTTP DTO.
-- Keep encryption keys and do not roll back while these approvals are pending.
ALTER TABLE public.ai_gateway_approval_requests
    ADD COLUMN IF NOT EXISTS protected_tool_input text;
