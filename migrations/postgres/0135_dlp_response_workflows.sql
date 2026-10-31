CREATE TABLE public.docker_volume_dlp_bindings (
 event_id TEXT PRIMARY KEY REFERENCES public.docker_volume_dlp_events(id),
 binding_key BYTEA NOT NULL CHECK (octet_length(binding_key)=32),
 binding_tag BYTEA NOT NULL CHECK (octet_length(binding_tag)=32),
 project_updated_at TIMESTAMPTZ NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE public.docker_volume_dlp_authorizations (
 event_id TEXT PRIMARY KEY REFERENCES public.docker_volume_dlp_bindings(event_id),
 actor_id TEXT NOT NULL, project_id TEXT NOT NULL, policy_version BIGINT NOT NULL,
 token_hash TEXT NOT NULL UNIQUE CHECK (token_hash ~ '^[a-f0-9]{64}$'),
 kind TEXT NOT NULL CHECK(kind IN ('acknowledge','approved_appeal')),
 expires_at TIMESTAMPTZ NOT NULL, consumed_at TIMESTAMPTZ
);
CREATE TABLE public.docker_volume_dlp_appeals (
 event_id TEXT PRIMARY KEY REFERENCES public.docker_volume_dlp_events(id),
 project_id TEXT NOT NULL, actor_id TEXT NOT NULL,
 reason_code TEXT NOT NULL CHECK(reason_code IN ('false_positive','business_need')),
 status TEXT NOT NULL CHECK(status IN ('pending','approved','rejected')),
 revision BIGINT NOT NULL CHECK(revision>0), reviewer_id TEXT, review_reason TEXT,
 created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL,
 CHECK(reviewer_id IS NULL OR reviewer_id<>actor_id)
);
CREATE INDEX docker_volume_dlp_pending ON public.docker_volume_dlp_appeals(project_id,actor_id) WHERE status='pending';

CREATE TABLE public.network_security_holds (
 id TEXT PRIMARY KEY, source_event_id TEXT NOT NULL UNIQUE REFERENCES public.docker_volume_dlp_events(id),
 source_type TEXT NOT NULL CHECK(source_type='content_dlp'),
 project_id TEXT NOT NULL, subject_id TEXT NOT NULL, active BOOLEAN NOT NULL,
 revision BIGINT NOT NULL CHECK(revision>0), created_at TIMESTAMPTZ NOT NULL,
 released_at TIMESTAMPTZ, released_by TEXT
);
CREATE UNIQUE INDEX network_security_hold_active_project ON public.network_security_holds(subject_id,project_id) WHERE active;
CREATE INDEX network_security_hold_actor ON public.network_security_holds(subject_id) WHERE active;
CREATE TABLE public.network_security_hold_targets (
 hold_id TEXT NOT NULL REFERENCES public.network_security_holds(id), kind TEXT NOT NULL,
 runtime_id TEXT NOT NULL DEFAULT '', session_id TEXT NOT NULL DEFAULT '', command_id TEXT NOT NULL DEFAULT '',
 base_version BIGINT NOT NULL DEFAULT 0, state TEXT NOT NULL,
 PRIMARY KEY(hold_id,kind,runtime_id,session_id)
);
CREATE FUNCTION public.network_security_subject_restricted(actor TEXT) RETURNS BOOLEAN
 LANGUAGE SQL STABLE AS $$ SELECT EXISTS(SELECT 1 FROM public.network_security_holds WHERE subject_id=actor AND active) $$;
ALTER TABLE public.network_proxy_instances ADD COLUMN security_restricted BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE public.docker_volume_dlp_events DROP CONSTRAINT docker_volume_dlp_events_action_check;
ALTER TABLE public.docker_volume_dlp_events ADD CONSTRAINT docker_volume_dlp_events_action_check
 CHECK(action IN ('transfer','policy_saved','policy_published','appeal_submitted','appeal_reviewed','warning_acknowledged','appeal_authorization_issued'));

-- Resolve coalesced holds without scanning the entire event history.
CREATE INDEX docker_volume_dlp_events_hold ON public.docker_volume_dlp_events ((metadata->>'networkHoldId')) WHERE action='transfer';

-- Project-row deletion locks serialize with the DLP project's FOR SHARE lock.
-- Retain the live project needed by scoped appeal review, including cascades.
CREATE FUNCTION public.guard_docker_project_dlp_deletion() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS (SELECT 1 FROM public.network_security_holds WHERE project_id=OLD.id AND active) THEN
  RAISE EXCEPTION 'active DLP network restrictions must be resolved before project deletion'
   USING ERRCODE='23514',CONSTRAINT='docker_project_active_dlp_hold';
 END IF;
 RETURN OLD;
END $$;
CREATE TRIGGER docker_project_active_dlp_hold BEFORE DELETE ON public.docker_projects
 FOR EACH ROW EXECUTE FUNCTION public.guard_docker_project_dlp_deletion();
