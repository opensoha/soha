CREATE OR REPLACE FUNCTION assistant_mesh_inquiry_retention_days(settings_value json) RETURNS integer
LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE WHEN days BETWEEN 1 AND 365 THEN days ELSE 7 END
    FROM (SELECT CASE WHEN json_typeof(settings_value -> 'inquiryRetentionDays') = 'number'
                           AND settings_value ->> 'inquiryRetentionDays' ~ '^[0-9]{1,3}$'
                      THEN (settings_value ->> 'inquiryRetentionDays')::integer END AS days) parsed
$$;

CREATE OR REPLACE FUNCTION assistant_mesh_effective_inquiry_retention_seconds(fallback_seconds integer) RETURNS integer
LANGUAGE sql STABLE AS $$
    SELECT COALESCE((SELECT assistant_mesh_inquiry_retention_days(value) * 86400
                     FROM app_settings WHERE setting_key = 'assistantMesh.organization'), fallback_seconds)
$$;

-- Close overdue rows under the old policy before a settings change can expose them again.
CREATE OR REPLACE FUNCTION assistant_mesh_freeze_due_inquiry_retention() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    previous_days integer := 7;
    changed_at timestamptz := statement_timestamp();
BEGIN
    IF TG_OP = 'UPDATE' THEN
        previous_days := assistant_mesh_inquiry_retention_days(OLD.value);
    END IF;
    IF previous_days <> assistant_mesh_inquiry_retention_days(NEW.value) THEN
        UPDATE assistant_mesh_inquiries
        SET status = 'expired', closed_at = due_at,
            purge_after = due_at + make_interval(secs => previous_days * 86400)
        WHERE status IN ('pending', 'processing') AND due_at <= changed_at;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS assistant_mesh_freeze_due_inquiry_retention ON app_settings;
CREATE TRIGGER assistant_mesh_freeze_due_inquiry_retention
AFTER INSERT OR UPDATE OF value ON app_settings
FOR EACH ROW WHEN (NEW.setting_key = 'assistantMesh.organization')
EXECUTE FUNCTION assistant_mesh_freeze_due_inquiry_retention();

CREATE OR REPLACE FUNCTION assistant_mesh_invalidate_reception_access() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    participant UUID;
    body_retention_days integer;
BEGIN
    IF TG_TABLE_NAME = 'users' THEN
        participant := NEW.id;
    ELSE
        IF NEW.user_id !~* '^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$' THEN
            RETURN NEW;
        END IF;
        participant := NEW.user_id::uuid;
    END IF;

    SELECT assistant_mesh_inquiry_retention_days(value)
      INTO body_retention_days
      FROM app_settings WHERE setting_key = 'assistantMesh.organization';

    UPDATE assistant_mesh_profiles
    SET open = false, updated_at = now()
    WHERE owner_id = participant AND open;

    UPDATE assistant_mesh_inquiries
    SET status = 'invalidated', closed_at = now(),
        purge_after = now() + make_interval(secs => COALESCE(body_retention_days, 7) * 86400)
    WHERE status IN ('pending', 'processing') AND due_at > now()
      AND (caller_id = participant OR owner_id = participant);
    RETURN NEW;
END;
$$;

ALTER TABLE assistant_mesh_inquiries
    ADD COLUMN IF NOT EXISTS citation_versions JSONB NOT NULL DEFAULT '[]'::jsonb;

CREATE OR REPLACE FUNCTION assistant_mesh_retained_citation_versions(citations jsonb) RETURNS jsonb
LANGUAGE sql IMMUTABLE AS $$
    SELECT COALESCE(jsonb_agg(jsonb_build_object('assetId', citation->'assetId', 'version', citation->'version')), '[]'::jsonb)
    FROM jsonb_array_elements(citations) AS citation
    WHERE jsonb_typeof(citation) = 'object' AND citation ? 'assetId' AND citation ? 'version'
$$;
