-- Move legacy alert inventory and route compatibility tables into the
-- consolidated alert_events / notification_policies model.

SELECT pg_catalog.set_config('search_path', '', false);

DO $$
BEGIN
    IF to_regclass('public.alert_instances') IS NOT NULL THEN
        INSERT INTO public.alert_events (
            id,
            rule_id,
            source_type,
            source_system,
            fingerprint,
            title,
            summary,
            severity,
            status,
            cluster_id,
            namespace,
            labels,
            annotations,
            receiver,
            generator_url,
            current_state,
            last_notification_at,
            starts_at,
            ends_at,
            last_seen_at,
            created_at,
            updated_at
        )
        SELECT
            ai.id,
            NULL,
            'external_webhook',
            NULLIF(ai.source, ''),
            ai.fingerprint,
            ai.title,
            ai.summary,
            ai.severity,
            COALESCE(NULLIF(ai.status, ''), 'firing'),
            NULLIF(ai.cluster_id, ''),
            NULLIF(ai.namespace, ''),
            COALESCE(ai.labels, '{}'::json),
            (
                COALESCE(ai.annotations, '{}'::json)::jsonb ||
                jsonb_strip_nulls(jsonb_build_object(
                    'ownerTeam', NULLIF(ai.owner_team, ''),
                    'assignee', NULLIF(ai.assignee, ''),
                    'acknowledgedAt', CASE
                        WHEN ai.acknowledged_at IS NULL THEN NULL
                        ELSE to_char(ai.acknowledged_at, 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
                    END,
                    'acknowledgedBy', NULLIF(ai.acknowledged_by, ''),
                    'acknowledgedByName', NULLIF(ai.acknowledged_by_name, '')
                ))
            )::json,
            NULLIF(ai.receiver, ''),
            NULLIF(ai.generator_url, ''),
            CASE
                WHEN COALESCE(NULLIF(ai.status, ''), 'firing') = 'resolved' THEN 'resolved'
                WHEN ai.acknowledged_at IS NOT NULL THEN 'acknowledged'
                ELSE COALESCE(NULLIF(ai.status, ''), 'firing')
            END,
            NULL,
            ai.starts_at,
            ai.ends_at,
            ai.last_seen_at,
            ai.created_at,
            ai.updated_at
        FROM public.alert_instances ai
        ON CONFLICT (id) DO UPDATE SET
            source_type = EXCLUDED.source_type,
            source_system = EXCLUDED.source_system,
            fingerprint = EXCLUDED.fingerprint,
            title = EXCLUDED.title,
            summary = EXCLUDED.summary,
            severity = EXCLUDED.severity,
            status = EXCLUDED.status,
            cluster_id = EXCLUDED.cluster_id,
            namespace = EXCLUDED.namespace,
            labels = EXCLUDED.labels,
            annotations = (
                EXCLUDED.annotations::jsonb ||
                jsonb_strip_nulls(jsonb_build_object(
                    'ownerTeam', public.alert_events.annotations::jsonb->>'ownerTeam',
                    'assignee', public.alert_events.annotations::jsonb->>'assignee',
                    'acknowledgedAt', public.alert_events.annotations::jsonb->>'acknowledgedAt',
                    'acknowledgedBy', public.alert_events.annotations::jsonb->>'acknowledgedBy',
                    'acknowledgedByName', public.alert_events.annotations::jsonb->>'acknowledgedByName'
                ))
            )::json,
            receiver = EXCLUDED.receiver,
            generator_url = EXCLUDED.generator_url,
            current_state = CASE
                WHEN EXCLUDED.status = 'resolved' THEN 'resolved'
                WHEN COALESCE(NULLIF(public.alert_events.current_state, ''), public.alert_events.status) = 'acknowledged' AND EXCLUDED.status = 'firing' THEN 'acknowledged'
                ELSE EXCLUDED.current_state
            END,
            starts_at = EXCLUDED.starts_at,
            ends_at = EXCLUDED.ends_at,
            last_seen_at = EXCLUDED.last_seen_at,
            updated_at = EXCLUDED.updated_at;
    END IF;

    IF to_regclass('public.alert_routes') IS NOT NULL THEN
        INSERT INTO public.notification_policies (
            id,
            name,
            matchers,
            processor_chain,
            channel_refs,
            oncall_ref,
            send_resolved,
            cooldown_seconds,
            enabled,
            created_at,
            updated_at
        )
        SELECT
            ar.id,
            ar.name,
            COALESCE(ar.matchers, '{}'::json),
            '["webhook_update"]'::json,
            COALESCE(ar.channel_ids, '[]'::json),
            NULL,
            FALSE,
            0,
            ar.enabled,
            ar.created_at,
            ar.updated_at
        FROM public.alert_routes ar
        ON CONFLICT (id) DO UPDATE SET
            name = EXCLUDED.name,
            matchers = EXCLUDED.matchers,
            processor_chain = EXCLUDED.processor_chain,
            channel_refs = EXCLUDED.channel_refs,
            enabled = EXCLUDED.enabled,
            updated_at = EXCLUDED.updated_at;
    END IF;
END $$;

ALTER TABLE IF EXISTS public.applications DROP CONSTRAINT IF EXISTS applications_business_line_id_fkey;
ALTER TABLE IF EXISTS public.scope_grants DROP CONSTRAINT IF EXISTS scope_grants_business_line_id_fkey;

DROP TABLE IF EXISTS public.alert_instances;
DROP TABLE IF EXISTS public.alert_routes;
DROP TABLE IF EXISTS public.alert_rule_targets;
DROP TABLE IF EXISTS public.business_lines;
DROP TABLE IF EXISTS public.policy_bindings;
DROP TABLE IF EXISTS public.saved_views;
DROP TABLE IF EXISTS public.user_preferences;
