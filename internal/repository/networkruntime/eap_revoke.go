package networkruntime

import (
	"context"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkprotocol"
	"gorm.io/gorm"
)

func (r *Repository) RevokeEAPCertificate(ctx context.Context, parent domain.Credential, request networkprotocol.EAPCertificateRevokeRequest, now time.Time) (networkprotocol.EAPCertificateRevokeResult, error) {
	var result networkprotocol.EAPCertificateRevokeResult
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEAPParent(ctx, tx, parent.ID, parent.RuntimeID, parent.DeviceID, parent.SubjectID, now); err != nil {
			return err
		}
		var row eapCredentialRow
		if err := tx.Where("id=? AND parent_credential_id=? AND runtime_id=? AND device_id=? AND subject_id=? AND certificate_fingerprint=?", request.CredentialID, parent.ID, parent.RuntimeID, parent.DeviceID, parent.SubjectID, request.Fingerprint).First(&row).Error; err != nil {
			return staleEAPCertificate()
		}
		if row.Status != "revoked" {
			if err := tx.Model(&eapCredentialRow{}).Where("id=?", row.ID).Updates(map[string]any{"status": "revoked", "revoked_at": now}).Error; err != nil {
				return err
			}
		}
		// Existing admission reconciliation observes CredentialActive=false and
		// issues its normal bounded Disconnect command for matching sessions.
		if err := tx.Exec(`UPDATE network_runtime_sessions SET admission_restriction_reason='eap_certificate_revoked',updated_at=? WHERE id IN (SELECT session_id FROM network_nas_authorizations WHERE credential_id=?) AND status NOT IN ('revoked','expired')`, now, row.ID).Error; err != nil {
			return err
		}
		result = networkprotocol.EAPCertificateRevokeResult{RequestID: request.RequestID, CredentialID: row.ID, Fingerprint: row.CertificateFingerprint, Status: "revoked"}
		return nil
	})
	return result, err
}
