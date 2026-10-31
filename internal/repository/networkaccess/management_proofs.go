package networkaccess

import (
	"context"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

func (r *Repository) readManagementProof(ctx context.Context, device *domain.Device) error {
	values := []domain.Device{*device}
	err := r.readManagementProofs(ctx, values)
	*device = values[0]
	return err
}
func (r *Repository) readManagementProofs(ctx context.Context, devices []domain.Device) error {
	if len(devices) == 0 {
		return nil
	}
	ids := make([]string, 0, len(devices))
	for _, device := range devices {
		ids = append(ids, device.ID)
	}
	var proofs []struct {
		DeviceID  string
		ExpiresAt time.Time
		IssuerKey string
	}
	err := r.db.WithContext(ctx).Raw(`SELECT DISTINCT p.device_id,p.expires_at,p.issuer_key FROM network_management_proofs p
 JOIN network_access_sites s ON s.id=p.site_id AND s.management_issuer_key=p.issuer_key
 JOIN network_runtime_credentials c ON c.device_id=p.device_id AND c.generation=p.credential_generation AND c.public_key_fingerprint=p.public_key_fingerprint
 WHERE p.device_id IN ? AND s.tenant_id='default' AND s.workspace_id='default' AND c.tenant_id='default' AND c.workspace_id='default'
 AND c.runtime_kind='endpoint' AND c.status='active' AND c.revoked_at IS NULL AND c.not_before<=CURRENT_TIMESTAMP AND c.expires_at>=p.expires_at
 AND p.management_state='managed' AND p.expires_at>CURRENT_TIMESTAMP`, ids).Scan(&proofs).Error
	if err != nil {
		return err
	}
	byID := map[string]int{}
	for i := range devices {
		byID[devices[i].ID] = i
	}
	for _, proof := range proofs {
		if index, ok := byID[proof.DeviceID]; ok {
			expiry := proof.ExpiresAt
			devices[index].ManagementVerifiedUntil = &expiry
			devices[index].ManagementIssuerKey = proof.IssuerKey
		}
	}
	return nil
}

func (r *Repository) SaveManagementProof(ctx context.Context, c domain.ManagementProofClaims, key string, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext('network-wireguard-mutations'))`).Error; err != nil {
			return err
		}
		var id string
		if err := tx.Raw(`SELECT d.id FROM network_access_devices d JOIN network_access_sites s ON s.id=d.site_id
 WHERE d.tenant_id='default' AND d.workspace_id='default' AND s.tenant_id=d.tenant_id AND s.workspace_id=d.workspace_id AND d.id=? AND s.id=? AND s.management_issuer_key=? FOR UPDATE OF d FOR SHARE OF s`, c.DeviceID, c.SiteID, key).Row().Scan(&id); err != nil {
			return apperrors.ErrConflict
		}
		if err := tx.Raw(`SELECT id FROM network_runtime_credentials WHERE tenant_id='default' AND workspace_id='default' AND device_id=? AND runtime_kind='endpoint' AND generation=? AND public_key_fingerprint=? AND status='active' AND revoked_at IS NULL AND not_before<=? AND expires_at>=? FOR SHARE`, c.DeviceID, c.CredentialGeneration, c.PublicKeyFingerprint, now, c.ExpiresAt).Row().Scan(&id); err != nil {
			return apperrors.ErrAccessDenied
		}
		var continuous bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM network_management_proofs WHERE device_id=? AND issuer_key=? AND credential_generation=? AND public_key_fingerprint=? AND management_state='managed' AND expires_at>?)`, c.DeviceID, key, c.CredentialGeneration, c.PublicKeyFingerprint, now).Scan(&continuous).Error; err != nil {
			return err
		}
		result := tx.Exec(`INSERT INTO network_management_proofs(device_id,site_id,issuer_key,credential_generation,public_key_fingerprint,sequence,management_state,observed_at,expires_at)
 VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(device_id) DO UPDATE SET site_id=EXCLUDED.site_id,issuer_key=EXCLUDED.issuer_key,credential_generation=EXCLUDED.credential_generation,public_key_fingerprint=EXCLUDED.public_key_fingerprint,sequence=EXCLUDED.sequence,management_state=EXCLUDED.management_state,observed_at=EXCLUDED.observed_at,expires_at=EXCLUDED.expires_at
 WHERE (network_management_proofs.issuer_key<>EXCLUDED.issuer_key OR network_management_proofs.sequence<EXCLUDED.sequence) AND network_management_proofs.observed_at<EXCLUDED.observed_at`, c.DeviceID, c.SiteID, key, c.CredentialGeneration, c.PublicKeyFingerprint, c.Sequence, c.ManagementState, c.ObservedAt, c.ExpiresAt)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return apperrors.ErrConflict
		}
		return tx.Exec(`UPDATE network_access_devices SET management_state=CASE WHEN ?='unmanaged' THEN 'unmanaged' ELSE management_state END,posture_version=posture_version+CASE WHEN ? THEN 0 ELSE 1 END,updated_at=? WHERE tenant_id='default' AND workspace_id='default' AND id=?`, c.ManagementState, continuous && c.ManagementState == "managed", now, c.DeviceID).Error
	})
}
