ALTER TABLE ai_memory_records ADD COLUMN revision bigint NOT NULL DEFAULT 1;
CREATE TABLE ai_memory_sync_outbox (
 id text PRIMARY KEY,
 record_id text NOT NULL REFERENCES ai_memory_records(id),
 revision bigint NOT NULL,
 document_id text NOT NULL,
 kind text NOT NULL CHECK(kind IN ('retain','delete')),
 payload jsonb NOT NULL,
 dependency_id text REFERENCES ai_memory_sync_outbox(id),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','retry','done','failed','unknown')),
 attempted boolean NOT NULL DEFAULT false,
 lease_token text NOT NULL DEFAULT '',
 lease_until timestamptz,
 next_attempt_at timestamptz NOT NULL DEFAULT NOW(),
 created_at timestamptz NOT NULL DEFAULT NOW(),
 UNIQUE(record_id,revision,kind)
);
CREATE INDEX ai_memory_sync_pending ON ai_memory_sync_outbox(next_attempt_at) WHERE state IN ('pending','retry','unknown');
CREATE INDEX ai_memory_sync_document ON ai_memory_sync_outbox(document_id);

CREATE TABLE ai_memory_backend_banks (
 owner_type text NOT NULL,
 owner_id text NOT NULL,
 scope_hash text NOT NULL,
 bank_id text NOT NULL UNIQUE,
 PRIMARY KEY(owner_type,owner_id,scope_hash)
);
