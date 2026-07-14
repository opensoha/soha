CREATE TABLE IF NOT EXISTS ai_evaluation_executor_profiles (
    id VARCHAR(128) PRIMARY KEY,
    payload JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ai_evaluation_sample_attempts (
    run_id VARCHAR(128) NOT NULL,
    sample_id VARCHAR(128) NOT NULL,
    attempt INTEGER NOT NULL CHECK (attempt > 0 AND attempt <= 10),
    payload JSONB NOT NULL,
    completed_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (run_id, sample_id, attempt)
);
CREATE INDEX IF NOT EXISTS idx_ai_evaluation_sample_attempts_run ON ai_evaluation_sample_attempts(run_id, completed_at);

CREATE TABLE IF NOT EXISTS ai_evaluation_replay_plans (
    id VARCHAR(128) PRIMARY KEY,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS ai_evaluation_gate_policies (
    id VARCHAR(128) NOT NULL,
    version VARCHAR(64) NOT NULL,
    payload JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, version)
);

CREATE TABLE IF NOT EXISTS ai_evaluation_gate_decisions (
    id VARCHAR(128) PRIMARY KEY,
    candidate_run_id VARCHAR(128) NOT NULL,
    decision VARCHAR(16) NOT NULL CHECK (decision IN ('pass','warn','block','error')),
    payload JSONB NOT NULL,
    evaluated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ai_evaluation_gate_decisions_candidate ON ai_evaluation_gate_decisions(candidate_run_id, evaluated_at);

CREATE TABLE IF NOT EXISTS ai_evaluation_feedback_samples (
    id VARCHAR(128) PRIMARY KEY,
    trace_ref VARCHAR(512) NOT NULL,
    decision VARCHAR(16) NOT NULL CHECK (decision IN ('pending','accepted','rejected','deleted')),
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS ai_memory_policies (
    id VARCHAR(128) NOT NULL,
    version VARCHAR(64) NOT NULL,
    payload JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, version)
);

CREATE TABLE IF NOT EXISTS ai_memory_records (
    id VARCHAR(128) PRIMARY KEY,
    owner_type VARCHAR(16) NOT NULL CHECK (owner_type IN ('user','session','agent')),
    owner_id VARCHAR(160) NOT NULL,
    scope_hash VARCHAR(96) NOT NULL,
    status VARCHAR(16) NOT NULL CHECK (status IN ('active','expired','deleted')),
    expires_at TIMESTAMPTZ,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    deleted_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_ai_memory_records_owner ON ai_memory_records(owner_type, owner_id, status, expires_at);

CREATE TABLE IF NOT EXISTS ai_knowledge_graph_revisions (
    id VARCHAR(128) PRIMARY KEY,
    knowledge_base_id VARCHAR(128) NOT NULL,
    source_index_ref VARCHAR(128) NOT NULL,
    status VARCHAR(16) NOT NULL CHECK (status IN ('building','verified','active','failed','superseded')),
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    published_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_ai_knowledge_graph_one_active ON ai_knowledge_graph_revisions(knowledge_base_id) WHERE status = 'active';

CREATE TABLE IF NOT EXISTS ai_multi_agent_plans (
    id VARCHAR(128) PRIMARY KEY,
    status VARCHAR(16) NOT NULL CHECK (status IN ('running','completed','failed','cancelled')),
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_ai_multi_agent_plans_status ON ai_multi_agent_plans(status, created_at);
