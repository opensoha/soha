package networkruntime

import (
	"context"
	"errors"
	"time"

	access "github.com/opensoha/soha/internal/domain/networkaccess"
	domain "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	accessrepo "github.com/opensoha/soha/internal/repository/networkaccess"
	"gorm.io/gorm"
)

type eapCredentialRow struct {
	ID                        string
	TenantID                  string
	WorkspaceID               string
	ParentCredentialID        string
	RuntimeID                 string
	DeviceID                  string
	SubjectID                 string
	RequestID                 string
	CSRHash                   string
	Generation                int
	CertificatePEM            string `gorm:"column:certificate_pem"`
	CACertificatePEM          string `gorm:"column:ca_certificate_pem"`
	CertificateFingerprint    string
	CertificateSerial         string
	CertificateAuthorityKeyID string
	Status                    string
	NotBefore                 time.Time
	ExpiresAt                 time.Time
	CreatedAt                 time.Time
	ActivatedAt               *time.Time
	RevokedAt                 *time.Time
}

func (eapCredentialRow) TableName() string { return "network_eap_credentials" }

func (row eapCredentialRow) domain() domain.EAPCredential {
	return domain.EAPCredential{EAPCertificateResult: networkprotocol.EAPCertificateResult{
		CredentialID: row.ID, CertificatePEM: row.CertificatePEM, CACertificatePEM: row.CACertificatePEM, Fingerprint: row.CertificateFingerprint, ExpiresAt: row.ExpiresAt, Status: row.Status},
		ParentCredentialID: row.ParentCredentialID, RuntimeID: row.RuntimeID, DeviceID: row.DeviceID, SubjectID: row.SubjectID,
		RequestID: row.RequestID, CSRHash: row.CSRHash, Generation: row.Generation, CertificateSerial: row.CertificateSerial,
		CertificateAuthorityKeyID: row.CertificateAuthorityKeyID, NotBefore: row.NotBefore, CreatedAt: row.CreatedAt}
}

func (r *Repository) SaveEAPCertificate(ctx context.Context, value domain.EAPCredential, now time.Time) (domain.EAPCredential, error) {
	var saved domain.EAPCredential
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEAPParent(ctx, tx, value.ParentCredentialID, value.RuntimeID, value.DeviceID, value.SubjectID, now); err != nil {
			return err
		}
		if err := tx.Model(&eapCredentialRow{}).Where("tenant_id='default' AND workspace_id='default' AND runtime_id=? AND status='pending' AND (created_at<=? OR parent_credential_id<>?)", value.RuntimeID, now.Add(-domain.EAPCertificatePendingTTL), value.ParentCredentialID).Updates(map[string]any{"status": "revoked", "revoked_at": now}).Error; err != nil {
			return err
		}
		var existing eapCredentialRow
		err := tx.Where("tenant_id='default' AND workspace_id='default' AND runtime_id=? AND request_id=?", value.RuntimeID, value.RequestID).First(&existing).Error
		if err == nil {
			if existing.CSRHash != value.CSRHash || existing.ParentCredentialID != value.ParentCredentialID || existing.Status == "revoked" || !existing.ExpiresAt.After(now) {
				return staleEAPCertificate()
			}
			saved = existing.domain()
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var pending int64
		if err := tx.Model(&eapCredentialRow{}).Where("tenant_id='default' AND workspace_id='default' AND runtime_id=? AND status='pending'", value.RuntimeID).Count(&pending).Error; err != nil {
			return err
		}
		if pending > 0 {
			return apperrors.NewBusiness(apperrors.ErrConflict, "eap_certificate_pending", "Complete the pending EAP installation or retry after its 15 minute activation window.", "请完成待安装 EAP 证书，或在 15 分钟激活窗口结束后重试。")
		}
		var generation int
		if err := tx.Raw(`SELECT COALESCE(MAX(generation),0)+1 FROM network_eap_credentials WHERE tenant_id='default' AND workspace_id='default' AND runtime_id=?`, value.RuntimeID).Scan(&generation).Error; err != nil {
			return err
		}
		row := eapCredentialRow{ID: value.CredentialID, TenantID: "default", WorkspaceID: "default", ParentCredentialID: value.ParentCredentialID,
			RuntimeID: value.RuntimeID, DeviceID: value.DeviceID, SubjectID: value.SubjectID, RequestID: value.RequestID, CSRHash: value.CSRHash,
			Generation: generation, CertificatePEM: value.CertificatePEM, CACertificatePEM: value.CACertificatePEM, CertificateFingerprint: value.Fingerprint,
			CertificateSerial: value.CertificateSerial, CertificateAuthorityKeyID: value.CertificateAuthorityKeyID, Status: "pending", NotBefore: value.NotBefore, ExpiresAt: value.ExpiresAt, CreatedAt: now}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		saved = row.domain()
		return nil
	})
	return saved, err
}

func lockEAPParent(ctx context.Context, tx *gorm.DB, credentialID, runtimeID, deviceID, subjectID string, now time.Time) error {
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-endpoint-device:"+deviceID).Error; err != nil {
		return err
	}
	// Enrollment rotation also holds network-runtime, so activation and revocation cannot cross.
	var parent credentialRow
	if err := tx.Where("id=? AND tenant_id='default' AND workspace_id='default'", credentialID).First(&parent).Error; err != nil {
		return err
	}
	if parent.RuntimeID != runtimeID {
		return staleEAPCertificate()
	}
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-runtime:"+parent.RuntimeID).Error; err != nil {
		return err
	}
	current, err := accessrepo.New(tx).EndpointCredentialCurrent(ctx, credentialID, deviceID, subjectID, now)
	if err != nil {
		return err
	}
	if !current {
		return staleEAPCertificate()
	}
	return nil
}

func (r *Repository) ActivateEAPCertificate(ctx context.Context, parent domain.Credential, request networkprotocol.EAPCertificateActivateRequest, now time.Time) (networkprotocol.EAPCertificateActivateResult, error) {
	var result networkprotocol.EAPCertificateActivateResult
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEAPParent(ctx, tx, parent.ID, parent.RuntimeID, parent.DeviceID, parent.SubjectID, now); err != nil {
			return err
		}
		var row eapCredentialRow
		err := tx.Where("id=? AND tenant_id='default' AND workspace_id='default' AND parent_credential_id=? AND runtime_id=? AND certificate_fingerprint=? AND not_before<=? AND expires_at>? AND status IN ('pending','active')", request.CredentialID, parent.ID, parent.RuntimeID, request.Fingerprint, now, now).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return staleEAPCertificate()
		}
		if err != nil {
			return err
		}
		if row.Status == "pending" && !row.CreatedAt.Add(domain.EAPCertificatePendingTTL).After(now) {
			return staleEAPCertificate()
		}
		var newer bool
		if err := tx.Raw(`SELECT EXISTS (SELECT 1 FROM network_eap_credentials WHERE tenant_id='default' AND workspace_id='default' AND runtime_id=? AND generation>? AND activated_at IS NOT NULL)`, parent.RuntimeID, row.Generation).Scan(&newer).Error; err != nil {
			return err
		}
		if newer {
			return staleEAPCertificate()
		}
		if row.Status == "pending" {
			if err := tx.Model(&eapCredentialRow{}).Where("tenant_id='default' AND workspace_id='default' AND runtime_id=? AND status='active'", parent.RuntimeID).Updates(map[string]any{"status": "revoked", "revoked_at": now}).Error; err != nil {
				return err
			}
			if err := tx.Model(&eapCredentialRow{}).Where("id=?", row.ID).Updates(map[string]any{"status": "active", "activated_at": now}).Error; err != nil {
				return err
			}
		}
		result = networkprotocol.EAPCertificateActivateResult{CredentialID: row.ID, Fingerprint: row.CertificateFingerprint, ExpiresAt: row.ExpiresAt, Status: "active"}
		return nil
	})
	return result, err
}

func staleEAPCertificate() error {
	return apperrors.NewBusiness(apperrors.ErrConflict, "eap_certificate_stale", "The EAP certificate or its parent credential is no longer current.", "EAP 证书或其父服务凭据已失效。")
}

func (r *Repository) activeEAPCredential(ctx context.Context, serial, authorityKeyID string, now time.Time) (domain.Credential, error) {
	var row eapCredentialRow
	if err := r.db.WithContext(ctx).Where("tenant_id='default' AND workspace_id='default' AND certificate_serial=? AND certificate_authority_key_id=?", serial, authorityKeyID).First(&row).Error; err != nil {
		return domain.Credential{}, err
	}
	current, err := accessrepo.New(r.db).EndpointCredentialCurrent(ctx, row.ID, row.DeviceID, row.SubjectID, now)
	if err != nil {
		return domain.Credential{}, err
	}
	if !current {
		return domain.Credential{}, apperrors.ErrUnauthorized
	}
	var parent credentialRow
	if err := r.db.WithContext(ctx).Where("id=?", row.ParentCredentialID).First(&parent).Error; err != nil {
		return domain.Credential{}, err
	}
	credential := parent.domain()
	credential.ID, credential.Generation = row.ID, row.Generation
	credential.CertificateFingerprint, credential.CertificateSerial, credential.CertificateAuthorityKeyID = row.CertificateFingerprint, row.CertificateSerial, row.CertificateAuthorityKeyID
	credential.NotBefore, credential.ExpiresAt = row.NotBefore, row.ExpiresAt
	return credential, nil
}

func (r *Repository) EAPFingerprint(ctx context.Context, parentID string, now time.Time) (string, error) {
	var value string
	err := r.db.WithContext(ctx).Raw(`SELECT certificate_fingerprint FROM network_eap_credentials WHERE tenant_id='default' AND workspace_id='default' AND parent_credential_id=? AND status='active' AND revoked_at IS NULL AND not_before<=? AND expires_at>?`, parentID, now, now).Scan(&value).Error
	return value, err
}

func (r *Repository) AdmissionNASBindings(ctx context.Context, after string, limit int) ([]access.NASBinding, error) {
	return accessrepo.New(r.db).AdmissionNASBindings(ctx, after, limit)
}

func (r *Repository) EndpointAdmissionEpoch(ctx context.Context, deviceID string) (int, error) {
	var epoch int
	err := r.db.WithContext(ctx).Raw(`SELECT COALESCE(SUM(epoch),0) FROM network_endpoint_response_epochs WHERE tenant_id='default' AND workspace_id='default' AND device_id=?`, deviceID).Scan(&epoch).Error
	return epoch, err
}
