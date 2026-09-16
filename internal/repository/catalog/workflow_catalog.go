package catalog

import (
	"context"
	"encoding/json"
	"fmt"

	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
)

// ListWorkflowCatalogCandidates reads definition metadata and authorization
// attributes in one query. Published definitions and runtime records stay unloaded.
func (r *Repository) ListWorkflowCatalogCandidates(ctx context.Context) ([]domaincatalog.WorkflowCatalogCandidate, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		WITH app_scope AS (
			SELECT a.id, jsonb_build_object('applicationId', a.id, 'applicationName', a.name,
				'applicationKey', a.app_key, 'businessLineId', COALESCE(a.business_line_id, ''),
				'applicationGroup', a.app_group, 'exists', true) AS scope
			FROM applications a
		), binding_scope AS (
			SELECT ae.id, ae.application_id, s.scope || jsonb_build_object(
				'applicationEnvironmentId', ae.id, 'environmentId', ae.environment_id,
				'environmentKey', COALESCE(e.environment_key, ae.environment_id),
				'environmentName', COALESCE(e.name, ae.environment_id)) AS scope
			FROM application_environments ae JOIN app_scope s ON s.id = ae.application_id
			LEFT JOIN delivery_environments e ON e.id = ae.environment_id
		)
		SELECT 'build_source/' || a.id || '/' || b.id AS id, 'build_source' AS source_kind,
			b.id AS source_id, b.source_name AS name, COALESCE(NULLIF(b.build_image, ''), b.source_type) AS context,
			a.enabled AND b.enabled AS enabled, jsonb_build_array(s.scope) AS scopes
		FROM application_build_sources b JOIN applications a ON a.id = b.application_id JOIN app_scope s ON s.id = a.id
		UNION ALL
		SELECT 'build_source/' || a.id || '/default:' || a.id, 'build_source', 'default:' || a.id,
			'Repository Dockerfile', COALESCE(NULLIF(BTRIM(a.build_image), ''), 'repo_dockerfile'),
			a.enabled, jsonb_build_array(s.scope)
		FROM applications a JOIN app_scope s ON s.id = a.id
		WHERE NOT EXISTS (SELECT 1 FROM application_build_sources b WHERE b.application_id = a.id)
			AND (BTRIM(COALESCE(a.build_image, '')) <> '' OR BTRIM(COALESCE(a.dockerfile_path, '')) <> '' OR BTRIM(COALESCE(a.build_context_dir, '')) <> '')
		UNION ALL
		SELECT 'application_workflow/' || a.id || '/' || ae.id, 'application_workflow', ae.id,
			COALESCE(v.snapshot->>'name', w.name, ae.workflow_template_id),
			COALESCE(v.snapshot->>'description', ''),
			a.enabled AND COALESCE(w.enabled, false) AND COALESCE((v.snapshot->>'enabled')::boolean, false),
			jsonb_build_array(s.scope)
		FROM application_environments ae JOIN applications a ON a.id = ae.application_id
		JOIN binding_scope s ON s.id = ae.id
		LEFT JOIN workflow_templates w ON w.id = ae.workflow_template_id
		LEFT JOIN catalog_template_versions v ON v.kind = 'workflow' AND v.template_id = ae.workflow_template_id AND v.version = ae.workflow_template_version
		WHERE COALESCE(ae.workflow_template_id, '') <> ''
		UNION ALL
		SELECT 'delivery_workflow/' || w.id, 'delivery_workflow', w.id, w.definition->>'name',
			COALESCE(w.definition->>'mode', 'service_serial'), true,
			COALESCE((SELECT jsonb_agg(
				COALESCE(bs.scope, aps.scope, '{}'::jsonb) || jsonb_build_object(
					'applicationId', t->>'applicationId', 'serviceId', COALESCE(t->>'serviceId', ''),
					'exists', aps.id IS NOT NULL AND (COALESCE(t->>'applicationEnvironmentId', '') = '' OR bs.id IS NOT NULL)))
				FROM jsonb_array_elements(w.definition->'targets') t
				LEFT JOIN app_scope aps ON aps.id = t->>'applicationId'
				LEFT JOIN binding_scope bs ON bs.id = t->>'applicationEnvironmentId' AND bs.application_id = aps.id), '[]'::jsonb)
		FROM delivery_workflows w
		ORDER BY id
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query workflow catalog: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := []domaincatalog.WorkflowCatalogCandidate{}
	for rows.Next() {
		var item domaincatalog.WorkflowCatalogCandidate
		var scopes []byte
		if err := rows.Scan(&item.ID, &item.SourceKind, &item.SourceID, &item.Name, &item.Context, &item.Enabled, &scopes); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(scopes, &item.AuthorizationScopes); err != nil {
			return nil, fmt.Errorf("decode workflow catalog scopes: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
