ALTER TABLE public.execution_artifacts
    ALTER COLUMN execution_task_id DROP NOT NULL,
    ADD COLUMN IF NOT EXISTS workflow_run_id text,
    ADD COLUMN IF NOT EXISTS workflow_node_id text,
    ADD COLUMN IF NOT EXISTS retention_until timestamp with time zone;

CREATE INDEX IF NOT EXISTS idx_execution_artifacts_workflow_run_id
    ON public.execution_artifacts USING btree (workflow_run_id);

CREATE INDEX IF NOT EXISTS idx_execution_artifacts_workflow_node_id
    ON public.execution_artifacts USING btree (workflow_run_id, workflow_node_id);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'execution_artifacts_workflow_run_id_fkey'
    ) THEN
        ALTER TABLE public.execution_artifacts
            ADD CONSTRAINT execution_artifacts_workflow_run_id_fkey
            FOREIGN KEY (workflow_run_id)
            REFERENCES public.workflow_runs(id)
            ON DELETE SET NULL;
    END IF;
END $$;
