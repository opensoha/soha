package networkaccess

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

const workloadColumns = `id,service_account_id,runtime_id,device_id,site_id,network_space_id,resource_ids,enabled,expires_at,revision`

func scanWorkload(row scanner) (domain.WorkloadBinding, error) {
	var binding domain.WorkloadBinding
	var resources []byte
	err := row.Scan(&binding.ID, &binding.ServiceAccountID, &binding.RuntimeID, &binding.DeviceID, &binding.SiteID, &binding.NetworkSpaceID, &resources, &binding.Enabled, &binding.ExpiresAt, &binding.Revision)
	if err != nil {
		return binding, err
	}
	err = json.Unmarshal(resources, &binding.ResourceIDs)
	return binding, err
}
func (r *Repository) GetWorkloadBinding(ctx context.Context, id string) (domain.WorkloadBinding, error) {
	return scanOne(r.db.WithContext(ctx).Raw(`SELECT `+workloadColumns+` FROM network_workload_bindings WHERE tenant_id='default' AND workspace_id='default' AND id=?`, id).Row(), scanWorkload, "workload binding")
}
func (r *Repository) WorkloadForRuntime(ctx context.Context, runtimeID, subjectID string) (domain.WorkloadBinding, error) {
	return scanOne(r.db.WithContext(ctx).Raw(`SELECT `+workloadColumns+` FROM network_workload_bindings w WHERE tenant_id='default' AND workspace_id='default' AND runtime_id=? AND 'service_account:'||service_account_id=? AND enabled=true AND expires_at>CURRENT_TIMESTAMP
 AND EXISTS(SELECT 1 FROM service_accounts sa WHERE sa.id=w.service_account_id AND sa.status='active')`, runtimeID, subjectID).Row(), scanWorkload, "workload binding")
}
func (r *Repository) ListWorkloadBindings(ctx context.Context, limit, offset int) ([]domain.WorkloadBinding, error) {
	return queryMany(ctx, r.db, `SELECT `+workloadColumns+` FROM network_workload_bindings WHERE tenant_id='default' AND workspace_id='default' ORDER BY id LIMIT ? OFFSET ?`, []any{limit, offset}, scanWorkload)
}
func (r *Repository) PutWorkloadBinding(ctx context.Context, b domain.WorkloadBinding, now time.Time) (domain.WorkloadBinding, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext('network-wireguard-mutations'))`).Error; err != nil {
			return err
		}
		if err := lockWorkloadBindingIdentity(tx, b); err != nil {
			return err
		}
		if err := validateWorkloadBindingResources(tx, b); err != nil {
			return err
		}

		raw, err := json.Marshal(b.ResourceIDs)
		if err != nil {
			return err
		}
		result := tx.Exec(`INSERT INTO network_workload_bindings(id,service_account_id,runtime_id,device_id,site_id,network_space_id,resource_ids,enabled,expires_at,revision,updated_at)
 VALUES(?,?,?,?,?,?,?::jsonb,?,?,1,?) ON CONFLICT(id) DO UPDATE SET service_account_id=EXCLUDED.service_account_id,runtime_id=EXCLUDED.runtime_id,device_id=EXCLUDED.device_id,site_id=EXCLUDED.site_id,network_space_id=EXCLUDED.network_space_id,resource_ids=EXCLUDED.resource_ids,enabled=EXCLUDED.enabled,expires_at=EXCLUDED.expires_at,revision=network_workload_bindings.revision+1,updated_at=EXCLUDED.updated_at
 WHERE network_workload_bindings.tenant_id='default' AND network_workload_bindings.workspace_id='default' AND network_workload_bindings.revision=?`, b.ID, b.ServiceAccountID, b.RuntimeID, b.DeviceID, b.SiteID, b.NetworkSpaceID, string(raw), b.Enabled, b.ExpiresAt, now, b.Revision)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return apperrors.ErrConflict
		}
		// A changed ceiling invalidates old credentials, so a fresh SAT-backed
		// enrollment is required even when an administrator broadens the binding.
		return tx.Exec(`UPDATE network_runtime_credentials SET status='revoked',revoked_at=? WHERE tenant_id='default' AND workspace_id='default' AND subject_id='service_account:'||? AND runtime_id=? AND status='active'`, now, b.ServiceAccountID, b.RuntimeID).Error
	})
	if err != nil {
		return domain.WorkloadBinding{}, normalizeDatabaseError(err)
	}
	return r.GetWorkloadBinding(ctx, b.ID)
}

func lockWorkloadBindingIdentity(tx *gorm.DB, b domain.WorkloadBinding) error {
	ctx := tx.Statement.Context
	var version int
	err := tx.Raw(`SELECT revision FROM network_workload_bindings WHERE tenant_id='default' AND workspace_id='default' AND id=? FOR UPDATE`, b.ID).Row().Scan(&version)
	if err != nil && !errors.Is(err, apperrors.ErrNotFound) && !errors.Is(err, gorm.ErrRecordNotFound) && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if version > 0 {
		old, err := New(tx).GetWorkloadBinding(ctx, b.ID)
		if err != nil {
			return err
		}
		if old.ServiceAccountID != b.ServiceAccountID || old.RuntimeID != b.RuntimeID || old.DeviceID != b.DeviceID || old.SiteID != b.SiteID || old.NetworkSpaceID != b.NetworkSpaceID {
			return apperrors.ErrConflict
		}
	}
	var conflicts int64
	if err := tx.Raw(`SELECT COUNT(*) FROM network_runtime_credentials WHERE tenant_id='default' AND workspace_id='default' AND runtime_id=? AND status='active' AND (runtime_kind<>'endpoint' OR device_id<>? OR subject_id<>'service_account:'||?)`, b.RuntimeID, b.DeviceID, b.ServiceAccountID).Scan(&conflicts).Error; err != nil {
		return err
	}
	if conflicts > 0 {
		return apperrors.ErrConflict
	}
	if version != b.Revision {
		return apperrors.ErrConflict
	}

	return nil
}

func validateWorkloadBindingResources(tx *gorm.DB, b domain.WorkloadBinding) error {
	ctx := tx.Statement.Context
	var account string
	if err := tx.Raw(`SELECT id FROM service_accounts WHERE id=? AND status='active' FOR SHARE`, b.ServiceAccountID).Row().Scan(&account); err != nil {
		return err
	}
	device, err := New(tx).GetDevice(ctx, b.DeviceID)
	if err != nil {
		return err
	}
	if device.SiteID != b.SiteID || device.DeviceType != "server" && device.DeviceType != "virtual" {
		return apperrors.ErrInvalidArgument
	}
	space, err := New(tx).GetSpace(ctx, b.NetworkSpaceID)
	if err != nil {
		return err
	}
	if space.SiteID != b.SiteID {
		return apperrors.ErrInvalidArgument
	}
	resources, err := New(tx).ResourcesByIDs(ctx, b.ResourceIDs)
	if err != nil {
		return err
	}
	if len(resources) != len(b.ResourceIDs) {
		return apperrors.ErrInvalidArgument
	}
	for _, resource := range resources {
		if resource.SpaceID != space.ID {
			return apperrors.ErrInvalidArgument
		}
	}
	return nil
}
