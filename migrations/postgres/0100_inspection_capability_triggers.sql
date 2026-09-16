ALTER TABLE ai_inspection_tasks
    ADD COLUMN capability_config jsonb NOT NULL DEFAULT '{}',
    ADD COLUMN revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    ADD COLUMN execution_token_id text NOT NULL DEFAULT '';

-- An alert row is mutable; a resolved -> firing transition needs its own identity.
ALTER TABLE alert_events
    ADD COLUMN inspection_occurrence bigint NOT NULL DEFAULT 0,
    ADD COLUMN inspection_occurrence_at timestamptz;

CREATE INDEX ai_inspection_alert_registrations ON ai_inspection_tasks
    ((capability_config->'trigger'->>'alertRuleId'))
    WHERE enabled AND capability_config->'capabilityPlan' IS NOT NULL;

CREATE INDEX ai_inspection_pending ON ai_inspection_runs (created_at, id) WHERE status = 'queued';
CREATE UNIQUE INDEX ai_inspection_alert_occurrence ON ai_inspection_runs
    (task_id, (report->>'registrationRevision'), (report->>'eventId'), (report->>'eventOccurrence'))
    WHERE report->>'eventId' IS NOT NULL;

-- The existing inspection receipt hands off to the existing Workflow queue.
-- At most one pending receipt or active/blocked goal per registration, across revisions.
-- A blocked goal can contain an unknown dispatched effect and must be reconciled first.
CREATE FUNCTION soha_inspection_has_active(registration_id text) RETURNS boolean LANGUAGE sql STABLE AS $$
    SELECT EXISTS (
        SELECT 1 FROM ai_inspection_runs r
        LEFT JOIN workflow_runs w ON w.id = r.report->>'capabilityTaskId'
        WHERE r.task_id = registration_id AND (
            r.status = 'queued' OR
            (w.scope = 'capability_task' AND w.status NOT IN ('completed','partially_completed','failed','canceled','inconclusive'))
        )
    )
$$;

CREATE FUNCTION soha_inspection_alert_occurrence() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        NEW.inspection_occurrence := CASE WHEN NEW.status = 'firing' THEN 1 ELSE 0 END;
        NEW.inspection_occurrence_at := CASE WHEN NEW.status = 'firing' THEN clock_timestamp() ELSE NULL END;
    ELSE
        NEW.inspection_occurrence := OLD.inspection_occurrence;
        NEW.inspection_occurrence_at := OLD.inspection_occurrence_at;
        IF NEW.status = 'firing' AND (OLD.status <> 'firing' OR NEW.starts_at IS DISTINCT FROM OLD.starts_at) THEN
            NEW.inspection_occurrence := OLD.inspection_occurrence + 1;
            NEW.inspection_occurrence_at := clock_timestamp();
        END IF;
    END IF;
    RETURN NEW;
END
$$;
CREATE TRIGGER soha_inspection_alert_occurrence BEFORE INSERT OR UPDATE ON alert_events
    FOR EACH ROW EXECUTE FUNCTION soha_inspection_alert_occurrence();

CREATE FUNCTION soha_inspection_capture_alert() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE registration ai_inspection_tasks%ROWTYPE;
BEGIN
    IF NEW.status <> 'firing' OR NEW.source_type <> 'internal_rule' OR NEW.rule_id IS NULL THEN RETURN NULL; END IF;
    IF TG_OP = 'UPDATE' AND NEW.inspection_occurrence = OLD.inspection_occurrence THEN RETURN NULL; END IF;
    FOR registration IN
        SELECT * FROM ai_inspection_tasks t
        WHERE t.enabled AND t.interval_minutes > 0
          AND t.capability_config->'capabilityPlan' IS NOT NULL
          AND t.capability_config->'trigger'->>'kind' = 'alert'
          AND t.capability_config->'trigger'->>'alertRuleId' = NEW.rule_id
          AND (COALESCE(t.cluster_id,'') = '' OR t.cluster_id = NEW.cluster_id)
          AND (COALESCE(t.namespace,'') = '' OR t.namespace = NEW.namespace)
        ORDER BY t.id FOR UPDATE
    LOOP
        IF registration.updated_at > NEW.inspection_occurrence_at OR
           (registration.last_run_at IS NOT NULL AND registration.last_run_at + make_interval(mins => registration.interval_minutes) > NEW.inspection_occurrence_at) OR
           soha_inspection_has_active(registration.id) THEN CONTINUE; END IF;
        INSERT INTO ai_inspection_runs(id,task_id,triggered_by,status,severity,summary,findings,report,started_at,created_at)
        VALUES(gen_random_uuid()::text,registration.id,'alert','queued','info','Awaiting capability handoff','[]',
            json_build_object('registrationRevision',registration.revision,'eventId',NEW.id,
                'eventOccurrence',NEW.inspection_occurrence,'eventOccurredAt',NEW.inspection_occurrence_at),
            NEW.inspection_occurrence_at,NEW.inspection_occurrence_at)
        ON CONFLICT DO NOTHING;
        UPDATE ai_inspection_tasks SET last_run_at = NEW.inspection_occurrence_at WHERE id = registration.id;
    END LOOP;
    RETURN NULL;
END
$$;
CREATE TRIGGER soha_inspection_capture_alert AFTER INSERT OR UPDATE ON alert_events
    FOR EACH ROW EXECUTE FUNCTION soha_inspection_capture_alert();
