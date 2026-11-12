package proxypublication

import (
	"context"
	"encoding/json"
	"errors"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	app "github.com/opensoha/soha/internal/application/proxypublication"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
	"time"
)

type bundleRow struct {
	InstanceID string `gorm:"primaryKey"`
	BundleID   string `gorm:"primaryKey"`
	Revision   int64
	Version    int64
	Record     []byte `gorm:"type:jsonb"`
}

func (bundleRow) TableName() string { return "network_proxy_bundles" }
func (r *Repository) GetBundle(ctx context.Context, instance, id string) (domain.BundleRecord, error) {
	var row bundleRow
	var out domain.BundleRecord
	err := r.db.WithContext(ctx).Where("instance_id = ? AND bundle_id = ?", instance, id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return out, apperrors.ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if json.Unmarshal(row.Record, &out) != nil {
		return out, apperrors.ErrServiceUnavailable
	}
	out.Version = row.Version
	return out, nil
}
func (r *Repository) ListBundles(ctx context.Context, instance string) ([]domain.BundleRecord, error) {
	var rows []bundleRow
	if err := r.db.WithContext(ctx).Where("instance_id = ?", instance).Order("bundle_id").Limit(256).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := []domain.BundleRecord{}
	for _, row := range rows {
		var record domain.BundleRecord
		if json.Unmarshal(row.Record, &record) != nil {
			return nil, apperrors.ErrServiceUnavailable
		}
		record.Version = row.Version
		out = append(out, record)
	}
	return out, nil
}
func (r *Repository) SaveBundle(ctx context.Context, record domain.BundleRecord, expected int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := policyLock(tx); err != nil {
			return err
		}
		if expected == 0 || record.Public.Revision != expected {
			if err := validateBundleScope(tx, record.Scope); err != nil {
				return err
			}
		}
		// Source and org snapshots are checked again under the same policy lock.
		if err := validateBundleSource(tx, record, expected); err != nil {
			return err
		}
		org, err := readRules(tx, record.InstanceID, "")
		if err != nil {
			return err
		}
		if org.OrgRevision != record.Public.OrgRevision {
			return apperrors.ErrConflict
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if expected == 0 {
			result := tx.Exec("INSERT INTO network_proxy_bundles(instance_id,bundle_id,revision,version,record) VALUES(?,?,?,1,?::jsonb) ON CONFLICT DO NOTHING", record.InstanceID, record.Public.BundleID, record.Public.Revision, string(raw))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return apperrors.ErrConflict
			}
			return nil
		}
		result := tx.Model(&bundleRow{}).Where("instance_id = ? AND bundle_id = ? AND revision = ? AND version = ?", record.InstanceID, record.Public.BundleID, expected, record.Version).Updates(map[string]any{"revision": record.Public.Revision, "version": record.Version + 1, "record": string(raw)})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return app.ErrBundleCASConflict
		}
		return nil
	})
}

type credentialRow struct {
	RenewalKey string
	ExpiresAt  time.Time
	ID         string `gorm:"primaryKey"`
	BundleID   string
	InstanceID string
	Record     []byte `gorm:"type:jsonb"`
}

func (credentialRow) TableName() string { return "network_proxy_employee_credentials" }
func (r *Repository) GetCredential(ctx context.Context, id string) (domain.Credential, error) {
	var row credentialRow
	var c domain.Credential
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return c, apperrors.ErrAccessDenied
	}
	if err != nil {
		return c, err
	}
	if json.Unmarshal(row.Record, &c) != nil {
		return c, apperrors.ErrServiceUnavailable
	}
	var ceiling scopeRow
	if err = r.db.WithContext(ctx).Where("identity_key=?", domain.CredentialIdentity(c)).Take(&ceiling).Error; err != nil {
		return c, apperrors.ErrAccessDenied
	}
	var allowed []string
	if json.Unmarshal(ceiling.NodeIDs, &allowed) != nil {
		return c, apperrors.ErrServiceUnavailable
	}
	c.NodeIDs = domain.IntersectNodes(c.NodeIDs, allowed)
	return c, nil
}

func (r *Repository) FindBundle(ctx context.Context, id string) (domain.BundleRecord, error) {
	var row bundleRow
	err := r.db.WithContext(ctx).Where("bundle_id = ?", id).Take(&row).Error
	if err != nil {
		return domain.BundleRecord{}, apperrors.ErrNotFound
	}
	return r.GetBundle(ctx, row.InstanceID, id)
}
func (r *Repository) ServerPublication(ctx context.Context, instance, id string, revision int64) (api.NetworkProxyServerPublication, error) {
	record, err := r.FindBundle(ctx, id)
	if err != nil {
		return api.NetworkProxyServerPublication{}, err
	}
	member := false
	for _, candidate := range record.Pool.Members {
		if candidate.RuntimeID == instance {
			member = true
			break
		}
	}
	if !member {
		return api.NetworkProxyServerPublication{}, apperrors.ErrAccessDenied
	}
	if record.Public.Revision != revision {
		return api.NetworkProxyServerPublication{}, apperrors.ErrConflict
	}
	return record.Publication(record.Public.ValidUntil), nil
}

func (r *Repository) SaveProjectionApplied(ctx context.Context, e domain.EndpointContext, epoch string, input api.NetworkProxyProjectionApplied) error {
	// Require an actual projection issued to this endpoint boot before accepting readback.
	var count int64
	err := r.db.WithContext(ctx).Raw("SELECT count(*) FROM network_proxy_employee_credentials WHERE record->>'EndpointCredentialID' = ? AND record->>'ProfileID' = ? AND record->>'ProcessEpoch' = ? AND record->>'BundleID' = ? AND (record->>'Revision')::bigint = ?", e.CredentialID, e.ProfileID, epoch, input.BundleID, input.Revision).Scan(&count).Error
	if err != nil {
		return err
	}
	if count == 0 {
		return apperrors.ErrConflict
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Exec("INSERT INTO network_proxy_projection_receipts(credential_id,profile_id,process_epoch,receipt) VALUES(?,?,?,?::jsonb) ON CONFLICT(credential_id,profile_id,process_epoch) DO UPDATE SET receipt=EXCLUDED.receipt", e.CredentialID, e.ProfileID, epoch, string(raw)).Error
}

func validateBundleScope(tx *gorm.DB, scope api.NetworkProxyScope) error {
	var users, devices int64
	if len(scope.UserIDs) == 0 || len(scope.DeviceIDs) == 0 {
		return apperrors.ErrAccessDenied
	}
	if err := tx.Raw("SELECT count(*) FROM users WHERE id::text IN ? AND status='active'", scope.UserIDs).Scan(&users).Error; err != nil {
		return err
	}
	if users != int64(len(scope.UserIDs)) {
		return apperrors.ErrAccessDenied
	}
	if err := tx.Raw("SELECT count(*) FROM network_access_devices WHERE tenant_id='default' AND workspace_id='default' AND id IN ? AND owner_user_id::text IN ? AND status='active'", scope.DeviceIDs, scope.UserIDs).Scan(&devices).Error; err != nil {
		return err
	}
	if devices != int64(len(scope.DeviceIDs)) {
		return apperrors.ErrAccessDenied
	}
	return nil
}

func credentialCurrent(tx *gorm.DB, c domain.Credential) (domain.Credential, error) {
	r, err := New(tx).GetBundle(tx.Statement.Context, c.InstanceID, c.BundleID)
	if err != nil {
		return c, err
	}
	if r.Public.State != "activated" || r.Public.Revision != c.Revision || r.Public.CredentialEpoch != c.Epoch {
		return c, apperrors.ErrAccessDenied
	}
	allowed, err := allowedSourceNodes(tx, c.InstanceID, r.Public.SourceID, r.Public.SourceRevision, r.SourceNodeIDs)
	if err != nil {
		return c, err
	}
	r, err = domain.NarrowBundle(r, allowed)
	if err != nil {
		return c, apperrors.ErrAccessDenied
	}
	c.NodeIDs = domain.IntersectNodes(c.NodeIDs, r.Scope.NodeIDs)
	if len(c.NodeIDs) == 0 {
		return c, apperrors.ErrAccessDenied
	}
	org, err := readRules(tx, c.InstanceID, "")
	if err != nil || org.OrgRevision != r.Public.OrgRevision {
		return c, apperrors.ErrAccessDenied
	}
	var count int64
	err = tx.Raw(`SELECT count(*) FROM network_runtime_credentials c
 JOIN network_access_devices d ON d.id=c.device_id AND d.tenant_id=c.tenant_id AND d.workspace_id=c.workspace_id
 JOIN users u ON u.id::text=c.subject_id
 JOIN network_mihomo_profiles p ON p.device_id=d.id AND p.tenant_id=d.tenant_id AND p.workspace_id=d.workspace_id
 WHERE c.tenant_id='default' AND c.workspace_id='default' AND c.id=? AND c.runtime_id=? AND c.runtime_kind='endpoint' AND c.status='active' AND c.not_before<=CURRENT_TIMESTAMP AND c.expires_at>=? AND c.capabilities @> '["mihomo","mihomo-policy-v1","mihomo-dns-pin-v1"]'::jsonb
 AND d.id=? AND d.owner_user_id::text=? AND d.status='active' AND u.status='active' AND NOT network_security_subject_restricted(c.subject_id)
 AND p.id=? AND p.status='active' AND p.bundle_id=? AND p.org_revision=? AND p.source_revision=?
 AND EXISTS(SELECT 1 FROM network_runtime_configurations cfg WHERE cfg.tenant_id=c.tenant_id AND cfg.workspace_id=c.workspace_id AND cfg.runtime_id=c.runtime_id AND cfg.valid_until>=? AND cfg.policy_version=(SELECT max(policy_version) FROM network_access_policy_snapshots WHERE tenant_id=c.tenant_id AND workspace_id=c.workspace_id) AND cfg.configuration_version=(SELECT max(newest.configuration_version) FROM network_runtime_configurations newest WHERE newest.runtime_id=c.runtime_id AND newest.tenant_id=c.tenant_id AND newest.workspace_id=c.workspace_id))`, c.EndpointCredentialID, c.RuntimeID, c.ExpiresAt, c.DeviceID, c.UserID, c.ProfileID, c.BundleID, r.Public.OrgRevision, r.Public.SourceRevision, c.ExpiresAt).Scan(&count).Error
	if err != nil {
		return c, err
	}
	if count != 1 {
		return c, apperrors.ErrAccessDenied
	}
	return c, nil
}

func validateBundleSource(tx *gorm.DB, record domain.BundleRecord, expected int64) error {
	if expected == 0 || record.Public.Revision != expected {
		source, err := sourceByID(tx, record.InstanceID, record.Public.SourceID, true)
		if err != nil {
			return err
		}
		if source.DeletedAt != nil || source.Revision != record.Public.SourceRevision || record.Public.SourceRevision < source.CompatibilityFloor || len(record.SourceNodeIDs) == 0 {
			return apperrors.ErrConflict
		}
		return nil
	}
	allowed, err := allowedSourceNodes(tx, record.InstanceID, record.Public.SourceID, record.Public.SourceRevision, record.SourceNodeIDs)
	if err != nil {
		return err
	}
	if _, err = domain.NarrowBundle(record, allowed); err != nil {
		return apperrors.ErrAccessDenied
	}
	return nil
}
