-- Node identity is also encoded into the server-generated task primary key.
-- This constraint prevents another ID from creating a second logical attempt.
CREATE UNIQUE INDEX execution_tasks_delivery_node_attempt_uidx ON execution_tasks
    ((payload->>'workflowRunId'), (payload->>'workflowNodeId'), (payload->>'workflowAttempt'))
    WHERE payload->>'workflowScope' = 'delivery_batch';
