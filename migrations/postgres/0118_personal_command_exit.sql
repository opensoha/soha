-- Preserve original commands and distinguish a confirmed nonzero sandbox exit.
ALTER TABLE ai_personal_commands DROP CONSTRAINT IF EXISTS ai_personal_commands_state_check;
ALTER TABLE ai_personal_commands ADD CONSTRAINT ai_personal_commands_state_check
 CHECK (state IN ('dispatching','succeeded','failed','unknown','not_dispatched','cancelling','canceled','cancellation_unknown'));
