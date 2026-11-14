package proxydeployment

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	proxy "github.com/opensoha/soha/internal/domain/networkproxy"
	domain "github.com/opensoha/soha/internal/domain/proxydeployment"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

func (r *Repository) LockDesiredBatch(ctx context.Context, tx *gorm.DB, batch proxy.DesiredBatch) error {
	if err := tx.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "network-access-policy-snapshot:default:default").Error; err != nil {
		return err
	}
	var epoch int64
	var hash string
	var raw []byte
	err := tx.Raw(`SELECT owner_epoch,member_set_hash,members FROM network_proxy_deployments WHERE proxy_id=? FOR UPDATE`, batch.MasterID).Row().Scan(&epoch, &hash, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		var bound bool
		if queryErr := tx.Raw(`SELECT EXISTS(SELECT 1 FROM network_proxy_deployment_members WHERE runtime_id=?)`, batch.MasterID).Row().Scan(&bound); queryErr != nil {
			return queryErr
		}
		if bound {
			return apperrors.ErrConflict
		}

		if batch.OwnerEpoch == 0 && batch.MemberSetHash == "" && len(batch.Members) == 1 && batch.Members[0].InstanceID == batch.MasterID {
			return nil
		}
		return apperrors.ErrConflict
	}
	if err != nil {
		return err
	}
	var members []domain.Member
	if err = json.Unmarshal(raw, &members); err != nil {
		return err
	}
	if epoch != batch.OwnerEpoch || hash != batch.MemberSetHash || len(members) != len(batch.Members) {
		return apperrors.ErrConflict
	}
	wanted := map[string]bool{}
	for _, m := range members {
		wanted[m.RuntimeID] = true
	}
	for _, m := range batch.Members {
		if !wanted[m.InstanceID] {
			return apperrors.ErrConflict
		}
		delete(wanted, m.InstanceID)
	}
	if len(wanted) != 0 {
		return apperrors.ErrConflict
	}
	return nil
}

// LockSingleDesired excludes pool-owned instances from legacy single-instance writes.
// The caller keeps this lock until its configuration transaction commits.
func (r *Repository) LockSingleDesired(ctx context.Context, tx *gorm.DB, id string) error {
	if err := tx.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "network-access-policy-snapshot:default:default").Error; err != nil {
		return err
	}
	var bound bool
	if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM network_proxy_deployments WHERE proxy_id=? UNION ALL SELECT 1 FROM network_proxy_deployment_members WHERE runtime_id=?)`, id, id).Row().Scan(&bound); err != nil {
		return err
	}
	if bound {
		return apperrors.ErrConflict
	}
	return nil
}
