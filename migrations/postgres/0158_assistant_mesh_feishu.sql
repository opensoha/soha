-- Assistant Mesh Feishu delivery jobs. Only the receipt stage needs the group
-- route. Questions are dropped once the inquiry is stored, and terminal jobs
-- retain only dedupe and outcome metadata.
CREATE TABLE IF NOT EXISTS assistant_mesh_feishu_jobs (
    scope TEXT NOT NULL CHECK (length(btrim(scope)) > 0 AND length(scope) <= 256),
    id TEXT NOT NULL CHECK (length(btrim(id)) > 0 AND length(id) <= 256),
    stage TEXT NOT NULL CHECK (stage IN ('receipt', 'question', 'answer', 'done')),
    group_id TEXT NOT NULL DEFAULT '' CHECK (length(group_id) <= 512),
    sender_id TEXT NOT NULL DEFAULT '' CHECK (length(sender_id) <= 512),
    target_id TEXT NOT NULL DEFAULT '' CHECK (length(target_id) <= 512),
    question TEXT NOT NULL DEFAULT '' CHECK (length(question) <= 8000),
    caller_id TEXT NOT NULL DEFAULT '' CHECK (length(caller_id) <= 512),
    owner_id TEXT NOT NULL DEFAULT '' CHECK (length(owner_id) <= 512),
    inquiry_id TEXT NOT NULL DEFAULT '' CHECK (length(inquiry_id) <= 512),
    claim_id UUID,
    claim_expires_at TIMESTAMPTZ,
    outcome TEXT NOT NULL DEFAULT '' CHECK (length(outcome) <= 128),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_at TIMESTAMPTZ NOT NULL,
    started_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (scope, id)
);

CREATE UNIQUE INDEX IF NOT EXISTS assistant_mesh_feishu_jobs_answer_owner
    ON assistant_mesh_feishu_jobs (scope, inquiry_id)
    WHERE inquiry_id <> '' AND stage IN ('answer', 'done');

CREATE INDEX IF NOT EXISTS assistant_mesh_feishu_jobs_due
    ON assistant_mesh_feishu_jobs (scope, next_at, id) WHERE stage <> 'done';

CREATE INDEX IF NOT EXISTS assistant_mesh_feishu_jobs_expiry
    ON assistant_mesh_feishu_jobs (expires_at);
