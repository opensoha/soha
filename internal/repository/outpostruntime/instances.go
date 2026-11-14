package outpostruntime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	domain "github.com/opensoha/soha/internal/domain/outpostruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

type instanceGrant struct {
	Epoch, CredentialHash            string
	ConfigurationVersion, Generation int64
	IssuedAt, ExpiresAt              time.Time
}

func loadInstanceGrant(tx *gorm.DB, id, instanceID string) (instanceGrant, bool, error) {
	var grant instanceGrant
	err := tx.Raw(`SELECT process_epoch,control_credential_hash,control_configuration_version,control_generation,control_issued_at,control_expires_at FROM identity_outpost_runtime_instances WHERE outpost_id=? AND instance_id=? FOR UPDATE`, id, instanceID).Row().Scan(&grant.Epoch, &grant.CredentialHash, &grant.ConfigurationVersion, &grant.Generation, &grant.IssuedAt, &grant.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return grant, false, nil
	}
	return grant, err == nil, err
}

// validateInstanceCredential runs after the Outpost row lock. Credential rotation
// and configuration-grant publication take the same lock, so an identity check
// completed before a rotation cannot authorize a later instance write.
func validateInstanceCredential(tx *gorm.DB, id string, credential domain.InstanceCredential, now time.Time) error {
	if credential.TokenHash == "" || credential.ConfigurationVersion < 1 || !credential.ExpiresAt.After(now) {
		return apperrors.ErrAccessDenied
	}
	var valid bool
	err := tx.Raw(`SELECT EXISTS (
 SELECT 1 FROM identity_outposts o JOIN identity_outpost_runtime_leases l
 ON l.outpost_id=o.id AND l.token_hash=o.token_hash
 WHERE o.id=? AND o.token_hash=? AND l.configuration_version=?
 AND l.issued_at<=? AND l.expires_at>? AND ?<=l.expires_at
)`, id, credential.TokenHash, credential.ConfigurationVersion, now, now, credential.ExpiresAt).Row().Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("%w: instance credential or configuration grant changed", apperrors.ErrAccessDenied)
	}
	return nil
}

func (r *Repository) GrantControl(ctx context.Context, id, instanceID, epoch string, credential domain.InstanceCredential) (domain.InstanceControl, error) {
	var control domain.InstanceControl
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockOutpost(ctx, tx, id); err != nil {
			return err
		}
		previous, exists, err := loadInstanceGrant(tx, id, instanceID)
		if err != nil {
			return err
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		if exists && !now.After(previous.IssuedAt) {
			return fmt.Errorf("%w: control issuance clock did not advance", apperrors.ErrConflict)
		}
		if err = validateInstanceCredential(tx, id, credential, now); err != nil {
			return err
		}
		if err = authorizeEpoch(tx, id, instanceID, epoch, previous, exists, now); err != nil {
			return err
		}
		state, err := New(tx).readState(ctx, id)
		if err != nil {
			return err
		}
		if _, _, err := settlePreviousTask(ctx, tx, &state); err != nil {
			return err
		}
		control = domain.ControlForState(state, now)
		if credential.ExpiresAt.Before(control.ExpiresAt) {
			control.ExpiresAt = credential.ExpiresAt
		}
		return storeControlGrant(tx, id, instanceID, epoch, credential, control)
	})
	return control, err
}

func authorizeEpoch(tx *gorm.DB, id, instanceID, epoch string, previous instanceGrant, exists bool, now time.Time) error {
	var retired bool
	if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM identity_outpost_runtime_retired_epochs WHERE outpost_id=? AND instance_id=? AND process_epoch=?)`, id, instanceID, epoch).Row().Scan(&retired); err != nil {
		return err
	}
	if retired {
		return fmt.Errorf("%w: process epoch was retired", apperrors.ErrAccessDenied)
	}
	if !exists || previous.Epoch == epoch {
		return nil
	}
	if previous.ExpiresAt.After(now) {
		return fmt.Errorf("%w: previous process control lease remains active", apperrors.ErrConflict)
	}
	return tx.Exec(`INSERT INTO identity_outpost_runtime_retired_epochs(outpost_id,instance_id,process_epoch,retired_at) VALUES(?,?,?,?) ON CONFLICT DO NOTHING`, id, instanceID, previous.Epoch, now).Error
}

func storeControlGrant(tx *gorm.DB, id, instanceID, epoch string, credential domain.InstanceCredential, control domain.InstanceControl) error {
	return tx.Exec(`INSERT INTO identity_outpost_runtime_instances AS existing
 (outpost_id,instance_id,process_epoch,generation,accepting_requests,active_checks,
 control_credential_hash,control_configuration_version,control_generation,control_issued_at,control_expires_at)
 VALUES(?,?,?,0,true,0,?,?,?,?,?)
 ON CONFLICT(outpost_id,instance_id) DO UPDATE SET
 observed_at=CASE WHEN existing.process_epoch=EXCLUDED.process_epoch
   AND existing.control_credential_hash=EXCLUDED.control_credential_hash
   AND existing.control_configuration_version=EXCLUDED.control_configuration_version
   THEN existing.observed_at ELSE NULL END,
 process_epoch=EXCLUDED.process_epoch,control_credential_hash=EXCLUDED.control_credential_hash,
 control_configuration_version=EXCLUDED.control_configuration_version,
 control_generation=EXCLUDED.control_generation,control_issued_at=EXCLUDED.control_issued_at,
 control_expires_at=EXCLUDED.control_expires_at`, id, instanceID, epoch, credential.TokenHash, credential.ConfigurationVersion, control.Generation, control.IssuedAt, control.ExpiresAt).Error
}

func (r *Repository) RecordInstance(ctx context.Context, item domain.InstanceObservation) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockOutpost(ctx, tx, item.OutpostID); err != nil {
			return err
		}
		grant, exists, err := loadInstanceGrant(tx, item.OutpostID, item.InstanceID)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		credential := domain.InstanceCredential{TokenHash: item.CredentialHash, ConfigurationVersion: item.ConfigurationVersion, ExpiresAt: item.LeaseExpiresAt}
		if err = validateInstanceCredential(tx, item.OutpostID, credential, now); err != nil {
			return err
		}
		if err = validateInstanceACK(item, grant, exists, now); err != nil {
			return err
		}
		if grant.ExpiresAt.Before(item.LeaseExpiresAt) {
			item.LeaseExpiresAt = grant.ExpiresAt
		}
		result := tx.Exec(`UPDATE identity_outpost_runtime_instances SET generation=?,credential_hash=?,configuration_version=?,accepting_requests=?,active_checks=?,observed_at=?,lease_expires_at=? WHERE outpost_id=? AND instance_id=? AND process_epoch=? AND (observed_at IS NULL OR observed_at<=?)`, item.Generation, item.CredentialHash, item.ConfigurationVersion, item.AcceptingRequests, item.ActiveChecks, item.ObservedAt, item.LeaseExpiresAt, item.OutpostID, item.InstanceID, item.ProcessEpoch, item.ObservedAt)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return apperrors.ErrConflict
		}
		return nil
	})
}

func validateInstanceACK(item domain.InstanceObservation, grant instanceGrant, exists bool, now time.Time) error {
	if !exists || item.ProcessEpoch != grant.Epoch || item.CredentialHash != grant.CredentialHash || item.ConfigurationVersion != grant.ConfigurationVersion {
		return fmt.Errorf("%w: instance registration changed", apperrors.ErrAccessDenied)
	}
	if item.Generation < 0 || item.Generation > grant.Generation {
		return fmt.Errorf("%w: instance acknowledged an unissued control generation", apperrors.ErrConflict)
	}
	if !grant.ExpiresAt.After(now) || item.ActiveChecks < 0 || item.ObservedAt.After(now.Add(time.Second)) || item.ObservedAt.Before(now.Add(-10*time.Second)) {
		return fmt.Errorf("%w: instance control grant or observation expired", apperrors.ErrConflict)
	}
	return nil
}

func (r *Repository) DrainStatus(ctx context.Context, id string, generation int64, now time.Time) (domain.DrainStatus, error) {
	var status domain.DrainStatus
	err := r.db.WithContext(ctx).Raw(`SELECT count(*),COALESCE(sum(i.active_checks),0),COALESCE(bool_or(i.accepting_requests),false)
 FROM identity_outpost_runtime_instances i
 JOIN identity_outposts o ON o.id=i.outpost_id AND o.token_hash=i.credential_hash
 JOIN identity_outpost_runtime_leases l ON l.outpost_id=o.id AND l.token_hash=o.token_hash
 AND l.configuration_version=i.configuration_version
 WHERE i.outpost_id=? AND i.generation=? AND i.observed_at>=? AND i.lease_expires_at>?
 AND i.control_expires_at>? AND l.expires_at>?
 AND i.control_credential_hash=i.credential_hash AND i.control_configuration_version=i.configuration_version
 AND i.generation<=i.control_generation`, id, generation, now.Add(-domain.ControlLeaseTTL), now, now, now).Row().Scan(&status.Instances, &status.ActiveChecks, &status.AcceptingRequests)
	return status, err
}
