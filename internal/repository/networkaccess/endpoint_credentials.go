package networkaccess

import (
	"context"
	"time"
)

func (r *Repository) EndpointCredentialCurrent(ctx context.Context, credentialID, deviceID, subjectID string, now time.Time) (bool, error) {
	var active bool
	// Credential generations belong to a runtime, independently of the device inventory generation.
	err := r.db.WithContext(ctx).Raw(`WITH current_control AS (
        SELECT c.* FROM network_runtime_credentials c
        WHERE c.tenant_id='default' AND c.workspace_id='default' AND c.runtime_kind='endpoint'
          AND c.device_id=? AND c.subject_id=? AND c.status='active' AND c.revoked_at IS NULL
          AND c.not_before<=? AND c.expires_at>?
          AND c.generation=(SELECT MAX(newest.generation) FROM network_runtime_credentials newest
              WHERE newest.tenant_id=c.tenant_id AND newest.workspace_id=c.workspace_id AND newest.runtime_id=c.runtime_id)
        ) SELECT EXISTS (
        SELECT 1 FROM current_control WHERE id=?
        UNION ALL
        SELECT 1 FROM network_eap_credentials e JOIN current_control c ON c.id=e.parent_credential_id
        WHERE e.tenant_id='default' AND e.workspace_id='default' AND e.id=? AND e.runtime_id=c.runtime_id
          AND e.device_id=c.device_id AND e.subject_id=c.subject_id AND e.status='active' AND e.revoked_at IS NULL
          AND e.not_before<=? AND e.expires_at>?
          AND e.generation=(SELECT MAX(newest.generation) FROM network_eap_credentials newest
              WHERE newest.tenant_id=e.tenant_id AND newest.workspace_id=e.workspace_id AND newest.runtime_id=e.runtime_id AND newest.activated_at IS NOT NULL)
        )`, deviceID, subjectID, now, now, credentialID, credentialID, now, now).Scan(&active).Error
	return active, err
}
