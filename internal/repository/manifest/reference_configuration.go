package manifest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/opensoha/soha/internal/platform/dbtx"
)

// ServiceReferenceManifestConfigurationRevision describes only bindings used by
// the Plan or its active source groups. Published content is represented by its
// immutable revision digest. Draft files and the package's aggregate binding
// projection are deliberately excluded.
func (r *Repository) ServiceReferenceManifestConfigurationRevision(ctx context.Context, environmentID string, bindingIDs []string) (string, error) {
	raw := []byte("[]")
	if len(bindingIDs) != 0 {
		err := dbtx.DB(ctx, r.db).Raw(`
			SELECT COALESCE(jsonb_agg(jsonb_build_object(
				'binding', jsonb_build_object(
					'id', b.id, 'environmentKey', b.environment_key, 'cluster', b.cluster_id,
					'namespace', b.namespace, 'enabled', b.enabled, 'overlay', b.overlay,
					'kustomize', b.kustomize, 'templateParameters', b.template_parameters,
					'templateSelection', b.template_selection - 'adoptionId',
					'rolloutStrategy', b.rollout_strategy_id, 'verificationPolicy', b.verification_policy_id,
					'driftPolicy', b.drift_policy, 'deletionPolicy', b.deletion_policy),
				'package', jsonb_build_object(
					'id', p.id, 'application', p.application_id, 'service', p.service_id,
					'businessLine', p.business_line_id, 'renderer', p.renderer, 'archived', p.archived_at IS NOT NULL,
					'publishedRevision', p.current_revision, 'publishedDigest', published.digest),
				'source', jsonb_build_object(
					'id', s.id, 'mode', s.mode, 'repository', s.repository_id, 'refType', s.ref_type,
					'refValue', s.ref_value, 'path', s.source_path, 'include', s.include_patterns,
					'exclude', s.exclude_patterns, 'syncPolicy', s.sync_policy,
					'autoPublish', s.auto_publish, 'autoDeploy', s.auto_deploy)
			) ORDER BY b.id), '[]'::jsonb)
			FROM manifest_bindings b
			JOIN manifest_packages p ON p.id = b.package_id
			LEFT JOIN manifest_revisions published ON published.package_id = p.id AND published.version = p.current_revision
			LEFT JOIN manifest_sources s ON s.package_id = p.id
			WHERE b.application_environment_id = ? AND b.id IN ?`, environmentID, bindingIDs).Row().Scan(&raw)
		if err != nil {
			return "", fmt.Errorf("read Manifest reference configuration: %w", err)
		}
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
