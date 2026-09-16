package delivery

import (
	"fmt"
	"slices"

	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

func CheckManifestResourceOwners(tx *gorm.DB, clusterID, bindingID string, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	rows, err := tx.Raw(`
 SELECT document->>'apiVersion', document->>'kind', document->>'namespace', document->>'name'
 FROM manifest_deployments deployment JOIN manifest_bindings binding ON binding.id = deployment.binding_id
 CROSS JOIN LATERAL jsonb_array_elements(COALESCE(deployment.delivery_snapshot->'documents', '[]'::jsonb) || COALESCE(deployment.delivery_snapshot->'gitOpsDocuments', '[]'::jsonb)) document
 WHERE binding.cluster_id = ? AND binding.id <> ?
 UNION ALL
 SELECT inventory.api_version, inventory.kind, inventory.namespace, inventory.name
 FROM manifest_resource_inventory inventory JOIN manifest_deployments deployment ON deployment.id = inventory.deployment_id
 JOIN manifest_bindings binding ON binding.id = deployment.binding_id
 WHERE binding.cluster_id = ? AND binding.id <> ? AND inventory.generation =
 (SELECT MAX(known.generation) FROM manifest_resource_inventory known WHERE known.deployment_id = deployment.id)
 UNION ALL
 SELECT document->>'apiVersion', document->>'kind', document->>'namespace', document->>'name'
 FROM manifest_operation_runs operation JOIN execution_tasks task ON task.id = operation.execution_task_id
 JOIN manifest_bindings binding ON binding.id = operation.binding_id
 CROSS JOIN LATERAL jsonb_array_elements(COALESCE(task.payload::jsonb->'documents', '[]'::jsonb) || COALESCE(task.payload::jsonb->'gitOpsDocuments', '[]'::jsonb)) document
 WHERE binding.cluster_id = ? AND binding.id <> ? AND operation.action IN ('apply','repair','rollback','adopt')
 AND task.status IN ('queued','dispatching','running','canceling')
 `, clusterID, bindingID, clusterID, bindingID, clusterID, bindingID).Rows()
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var document domainmanifest.RenderedDocument
		if err := rows.Scan(&document.APIVersion, &document.Kind, &document.Namespace, &document.Name); err != nil {
			return err
		}
		if slices.Contains(keys, domainmanifest.ResourceKeys(clusterID, []domainmanifest.RenderedDocument{document})[0]) {
			return fmt.Errorf("%w: %s %s/%s is already owned by another Manifest binding", apperrors.ErrInvalidArgument, document.Kind, document.Namespace, document.Name)
		}
	}
	return rows.Err()
}
