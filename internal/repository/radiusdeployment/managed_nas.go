package radiusdeployment

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	access "github.com/opensoha/soha/internal/domain/networkaccess"
	runtime "github.com/opensoha/soha/internal/domain/networkruntime"
	domain "github.com/opensoha/soha/internal/domain/radiusdeployment"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	repoaccess "github.com/opensoha/soha/internal/repository/networkaccess"
	"gorm.io/gorm"
)

func (r *Repository) ResolveManagedNAS(ctx context.Context, id networkidentity.Identity, nas string, proof *networkprotocol.ManagedNASProof, pin string, required bool) (access.NASBinding, error) {
	var binding access.NASBinding
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		binding, err = validateManagedNAS(tx, id.ID, id.CertificateFingerprint, nas, proof, pin, required, time.Now().UTC())
		return err
	})
	return binding, err
}
func validateManagedNAS(tx *gorm.DB, runtimeID, fingerprint, nas string, proof *networkprotocol.ManagedNASProof, pin string, required bool, now time.Time) (access.NASBinding, error) {
	state, record, err := loadMember(tx, runtimeID)
	if err != nil {
		return access.NASBinding{}, apperrors.ErrUnauthorized
	}
	if state.SlotHash != pin || record.Member.RuntimeID != runtimeID {
		return access.NASBinding{}, apperrors.ErrUnauthorized
	}
	var c struct {
		ID        string
		ExpiresAt time.Time
	}
	var capabilities []byte
	err = tx.Raw(`SELECT id,expires_at,capabilities FROM network_runtime_credentials c WHERE tenant_id='default' AND workspace_id='default' AND runtime_id=? AND runtime_kind='nas' AND certificate_fingerprint=? AND status='active' AND revoked_at IS NULL AND not_before<=? AND expires_at>? AND generation=(SELECT MAX(n.generation) FROM network_runtime_credentials n WHERE n.runtime_id=c.runtime_id AND n.tenant_id=c.tenant_id AND n.workspace_id=c.workspace_id) FOR SHARE`, runtimeID, fingerprint, now, now).Row().Scan(&c.ID, &c.ExpiresAt, &capabilities)
	if err != nil {
		return access.NASBinding{}, apperrors.ErrUnauthorized
	}
	var caps []string
	if json.Unmarshal(capabilities, &caps) != nil {
		return access.NASBinding{}, apperrors.ErrUnauthorized
	}
	managedCap := false
	for _, capability := range caps {
		managedCap = managedCap || capability == domain.Capability
	}
	if !managedCap {
		return access.NASBinding{}, apperrors.ErrUnauthorized
	}
	binding, err := repoaccess.New(tx).GetNASBinding(tx.Statement.Context, state.Pool.NASBindingID)
	if err != nil {
		return binding, err
	}
	if binding.RuntimeID != state.Pool.LogicalRuntimeID || binding.SiteID != state.Pool.SiteID || binding.NASID != nas || binding.Status != access.StatusActive {
		return access.NASBinding{}, apperrors.ErrUnauthorized
	}
	if required {
		if err := validRuntimeProof(state, record, c.ID, proof, now); err != nil {
			return access.NASBinding{}, err
		}
	}
	return binding, nil
}
func validRuntimeProof(state domain.State, record domain.MemberRecord, credentialID string, proof *networkprotocol.ManagedNASProof, now time.Time) error {
	if proof == nil || record.Control == nil || !record.Ready(state, now) {
		return apperrors.ErrUnauthorized
	}
	control := record.Control
	if proof.PoolID != state.Pool.PoolID || proof.MemberID != record.Member.MemberID || proof.CredentialID != credentialID || proof.CredentialID != control.CredentialID || proof.ProcessEpoch != control.ProcessEpoch || proof.OwnerEpoch != int64(state.Pool.OwnerEpoch) || proof.ControlRevision != state.ControlRevision || proof.ConfigurationRevision != int64(state.Pool.ConfigurationRevision) || proof.ConfigurationHash != state.Pool.ConfigurationHash {
		return apperrors.ErrUnauthorized
	}
	if !control.RunProcess || control.StopRequired || !control.ServeExistingUntil.After(now) || record.Member.Ordinal >= int32(state.Pool.DesiredReplicas) && !state.Retiring(record.Member.Ordinal) {
		return apperrors.ErrUnauthorized
	}
	return nil
}

// ValidateNASAuthorization is called inside the same transaction as the NAS
// policy/receipt/session write, using only server-authenticated identity data.
func ValidateNASAuthorization(tx *gorm.DB, authorization runtime.NASAuthorization) (access.NASBinding, error) {
	managed := strings.HasPrefix(authorization.RuntimeID, domain.MemberPrefix) || strings.HasPrefix(authorization.RuntimeID, domain.PoolPrefix)
	if !managed {
		if authorization.ManagedRuntimeProof != nil {
			return access.NASBinding{}, apperrors.ErrUnauthorized
		}
		return access.NASBinding{}, nil
	}
	if authorization.ManagedRuntimeProof == nil || authorization.ManagedSlotHash == "" || authorization.ManagedBindingID == "" || authorization.ManagedRuntimeFingerprint == "" {
		return access.NASBinding{}, apperrors.ErrUnauthorized
	}
	binding, err := validateManagedNAS(tx, authorization.RuntimeID, authorization.ManagedRuntimeFingerprint, authorization.NASID, authorization.ManagedRuntimeProof, authorization.ManagedSlotHash, true, time.Now().UTC())
	if err != nil {
		return binding, err
	}
	if binding.ID != authorization.ManagedBindingID {
		return access.NASBinding{}, apperrors.ErrUnauthorized
	}
	if authorization.Decision == access.DecisionAllow {
		var encoded []byte
		if err := tx.Raw(`SELECT record FROM network_radius_members WHERE runtime_id=?`, authorization.RuntimeID).Row().Scan(&encoded); err != nil {
			return binding, err
		}
		var record domain.MemberRecord
		if err := json.Unmarshal(encoded, &record); err != nil {
			return binding, err
		}
		at := authorization.CreatedAt
		record.NASUsedAt = &at
		if err := saveMember(tx, authorization.ManagedRuntimeProof.PoolID, record, false); err != nil {
			return binding, err
		}
	}
	return binding, nil
}
func ValidateEnrollment(tx *gorm.DB, consumption runtime.EnrollmentConsumption) error {
	if strings.HasPrefix(consumption.RuntimeID, domain.PoolPrefix) {
		return apperrors.ErrUnauthorized
	}
	if !strings.HasPrefix(consumption.RuntimeID, domain.MemberPrefix) {
		for _, capability := range consumption.Capabilities {
			if capability == domain.Capability {
				return apperrors.ErrUnauthorized
			}
		}
		return nil
	}
	if consumption.ManagedNAS == nil || consumption.RuntimeKind != "nas" || !slices.Contains(consumption.Capabilities, domain.Capability) {
		return apperrors.ErrUnauthorized
	}
	state, record, err := loadMember(tx, consumption.RuntimeID)
	if err != nil {
		return apperrors.ErrUnauthorized
	}
	proof := consumption.ManagedNAS
	if proof.PoolID != state.Pool.PoolID || proof.SlotHash != state.SlotHash || proof.MemberID != record.Member.MemberID {
		return apperrors.ErrUnauthorized
	}
	return tx.Exec(`SELECT set_config('soha.radius_member_writer',?,true)`, state.Pool.PoolID+":"+state.SlotHash).Error
}
