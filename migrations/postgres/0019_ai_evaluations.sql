SELECT pg_catalog.set_config('search_path', '', false);

CREATE TABLE IF NOT EXISTS public.ai_evaluation_datasets (
    id text NOT NULL,
    version text NOT NULL,
    schema_version text NOT NULL,
    name text NOT NULL,
    samples jsonb NOT NULL,
    created_at timestamp without time zone NOT NULL,
    PRIMARY KEY (id, version)
);
CREATE INDEX IF NOT EXISTS idx_ai_evaluation_datasets_created
    ON public.ai_evaluation_datasets (created_at DESC, id ASC, version ASC);

CREATE TABLE IF NOT EXISTS public.ai_evaluation_runs (
    id text PRIMARY KEY,
    schema_version text NOT NULL,
    dataset_id text NOT NULL,
    dataset_version text NOT NULL,
    candidate_refs jsonb NOT NULL,
    status text NOT NULL CHECK (status IN ('running', 'completed')),
    started_at timestamp without time zone NOT NULL,
    completed_at timestamp without time zone,
    aggregate_scores jsonb,
    CONSTRAINT fk_ai_evaluation_runs_dataset
        FOREIGN KEY (dataset_id, dataset_version)
        REFERENCES public.ai_evaluation_datasets (id, version),
    CONSTRAINT chk_ai_evaluation_runs_completion
        CHECK ((status = 'running' AND completed_at IS NULL) OR
               (status = 'completed' AND completed_at IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS idx_ai_evaluation_runs_started
    ON public.ai_evaluation_runs (started_at DESC, id ASC);

CREATE INDEX IF NOT EXISTS idx_ai_evaluation_runs_dataset
    ON public.ai_evaluation_runs (dataset_id, dataset_version, started_at DESC);

CREATE TABLE IF NOT EXISTS public.ai_evaluation_results (
    run_id text NOT NULL REFERENCES public.ai_evaluation_runs (id) ON DELETE CASCADE,
    sample_id text NOT NULL,
    ordinal integer NOT NULL CHECK (ordinal >= 0),
    schema_version text NOT NULL,
    retrieved_sources jsonb NOT NULL,
    produced_facts jsonb NOT NULL,
    actions jsonb NOT NULL,
    scores jsonb NOT NULL,
    passed boolean NOT NULL,
    failure_reasons jsonb NOT NULL,
    PRIMARY KEY (run_id, sample_id),
    CONSTRAINT uq_ai_evaluation_results_ordinal UNIQUE (run_id, ordinal)
);
