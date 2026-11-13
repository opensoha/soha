package radiusdeployment

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/radiusdeployment"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	repoaccess "github.com/opensoha/soha/internal/repository/networkaccess"
	"gorm.io/gorm"
)

type currentCredential struct {
	ID           string
	ExpiresAt    time.Time
	Capabilities []string
}

func credential(tx *gorm.DB, id networkidentity.Identity, now time.Time) (currentCredential, error) {
	var c currentCredential
	var caps []byte
	if id.Scope != networkidentity.ScopeNetworkControl || id.Kind != "nas" || id.Certificate == nil {
		return c, apperrors.ErrUnauthorized
	}
	err := tx.Raw(`SELECT id,expires_at,capabilities FROM network_runtime_credentials c WHERE tenant_id='default' AND workspace_id='default' AND runtime_kind='nas' AND runtime_id=? AND certificate_fingerprint=? AND status='active' AND revoked_at IS NULL AND not_before<=? AND expires_at>? AND generation=(SELECT MAX(n.generation) FROM network_runtime_credentials n WHERE n.runtime_id=c.runtime_id AND n.tenant_id=c.tenant_id AND n.workspace_id=c.workspace_id) FOR SHARE`, id.ID, id.CertificateFingerprint, now, now).Row().Scan(&c.ID, &c.ExpiresAt, &caps)
	if err != nil {
		return c, apperrors.ErrUnauthorized
	}
	if err := json.Unmarshal(caps, &c.Capabilities); err != nil {
		return c, err
	}
	if !slices.Contains(c.Capabilities, domain.Capability) {
		return c, apperrors.ErrUnauthorized
	}
	c.ExpiresAt = earlier(c.ExpiresAt, id.Certificate.NotAfter)
	if !c.ExpiresAt.After(now) {
		return c, apperrors.ErrUnauthorized
	}
	return c, nil
}
func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
func loadMember(tx *gorm.DB, runtime string) (domain.State, domain.MemberRecord, error) {
	var pool string
	var encoded []byte
	err := tx.Raw(`SELECT pool_id,record FROM network_radius_members WHERE runtime_id=?`, runtime).Row().Scan(&pool, &encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.State{}, domain.MemberRecord{}, apperrors.ErrNotFound
	}
	if err != nil {
		return domain.State{}, domain.MemberRecord{}, err
	}
	if err := lockPool(tx, pool); err != nil {
		return domain.State{}, domain.MemberRecord{}, err
	}
	state, err := New(tx).Get(tx.Statement.Context, pool)
	if err != nil {
		return state, domain.MemberRecord{}, err
	}
	// Re-read after obtaining the parent lock; a concurrent ACK may have advanced.
	if err := tx.Raw(`SELECT record FROM network_radius_members WHERE runtime_id=?`, runtime).Row().Scan(&encoded); err != nil {
		return state, domain.MemberRecord{}, err
	}
	var member domain.MemberRecord
	err = json.Unmarshal(encoded, &member)
	return state, member, err
}
func (r *Repository) GrantControl(ctx context.Context, id networkidentity.Identity, memberID, epoch, pin string) (api.NetworkRADIUSRuntimeControl, error) {
	var result api.NetworkRADIUSRuntimeControl
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		state, record, err := loadMember(tx, id.ID)
		if err != nil {
			return err
		}
		if state.SlotHash != pin || record.Member.MemberID != memberID || !domain.ValidIdentity(epoch) {
			return apperrors.ErrUnauthorized
		}
		now := time.Now().UTC()
		c, err := credential(tx, id, now)
		if err != nil {
			return err
		}
		binding, err := repoaccess.New(tx).GetNASBinding(ctx, state.Pool.NASBindingID)
		if err != nil {
			return err
		}
		if binding.RuntimeID != state.Pool.LogicalRuntimeID || binding.SiteID != state.Pool.SiteID || binding.Status != "active" {
			return apperrors.ErrConflict
		}
		if record.Control == nil || record.Control.ProcessEpoch != epoch {
			var used bool
			if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM network_radius_retired_epochs WHERE runtime_id=? AND process_epoch=?)`, id.ID, epoch).Scan(&used).Error; err != nil {
				return err
			}
			if used {
				return apperrors.ErrConflict
			}
			if record.Control != nil {
				if !stopped(record) {
					return apperrors.ErrConflict
				}
				if err := tx.Exec(`INSERT INTO network_radius_retired_epochs(runtime_id,process_epoch) VALUES(?,?) ON CONFLICT DO NOTHING`, id.ID, record.Control.ProcessEpoch).Error; err != nil {
					return err
				}
			}
		}
		control, err := nextControl(state, &record, c, epoch, binding.NASID, now)
		if err != nil {
			return err
		}
		record.Control = &control
		if err := saveMember(tx, state.Pool.PoolID, record, false); err != nil {
			return err
		}
		result = control
		return nil
	})
	return result, err
}
func stopped(record domain.MemberRecord) bool {
	return record.ACK != nil && record.ACK.ProcessStopped && record.ACK.StateCountKnown && record.ACK.ActiveStates != nil && *record.ACK.ActiveStates == 0 && !record.ACK.AcceptingNewAuthentications
}
func nextControl(state domain.State, record *domain.MemberRecord, c currentCredential, epoch, nas string, now time.Time) (api.NetworkRADIUSRuntimeControl, error) {
	old := record.Control
	if old != nil && old.ProcessEpoch != epoch {
		if !stopped(*record) {
			return api.NetworkRADIUSRuntimeControl{}, fmt.Errorf("%w: previous_child_stop_required", apperrors.ErrConflict)
		}
		record.Control = nil
		record.ACK = nil
		record.ReceivedAt = nil
		record.NASUsedAt = nil
		record.UnknownLatched = false
		old = nil
	}
	if old != nil && (!old.ControlLeaseUntil.After(now) || old.CredentialID != c.ID || old.ConfigurationHash != state.Pool.ConfigurationHash) {
		record.UnknownLatched = true
	}
	control := api.NetworkRADIUSRuntimeControl{PoolID: state.Pool.PoolID, MemberID: record.Member.MemberID, RuntimeID: record.Member.RuntimeID, CredentialID: c.ID, ProcessEpoch: epoch, OwnerEpoch: state.Pool.OwnerEpoch, ControlRevision: int(state.ControlRevision), ConfigurationRevision: state.Pool.ConfigurationRevision, ConfigurationHash: state.Pool.ConfigurationHash, NASBindingID: state.Pool.NASBindingID, NASID: nas, SiteID: state.Pool.SiteID, Configuration: state.Configuration.Material, ControlLeaseUntil: earlier(now.Add(10*time.Second), c.ExpiresAt), ConfigurationLeaseUntil: earlier(now.Add(10*time.Second), c.ExpiresAt), ServeExistingUntil: c.ExpiresAt}
	if old != nil {
		control.ServeExistingUntil = earlier(old.ServeExistingUntil, control.ServeExistingUntil)
	}
	applyControlIntent(state, *record, &control, old, now)

	return control, nil
}
func (r *Repository) RecordACK(ctx context.Context, id networkidentity.Identity, ack api.NetworkRADIUSRuntimeACK, pin string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		state, record, err := loadMember(tx, id.ID)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		c, err := credential(tx, id, now)
		if err != nil {
			return err
		}
		if state.SlotHash != pin || record.Control == nil {
			return apperrors.ErrConflict
		}
		control := record.Control
		if !matchesACKIdentity(state, record, c, ack, id.ID) {
			return apperrors.ErrConflict
		}
		if !control.ControlLeaseUntil.After(now) || ack.ObservedAt.Before(now.Add(-30*time.Second)) || ack.ObservedAt.After(now.Add(30*time.Second)) {
			return apperrors.ErrConflict
		}
		if err := validateACK(control, record.UnknownLatched, ack, now); err != nil {
			return err
		}
		if stopped(record) && !ack.ProcessStopped {
			return apperrors.ErrConflict
		}
		if !ack.StateCountKnown {
			record.UnknownLatched = true
		}
		if ack.ProcessStopped {
			record.UnknownLatched = false
		}
		record.ACK = &ack
		record.ReceivedAt = &now
		return saveMember(tx, state.Pool.PoolID, record, false)
	})
}
func validateACK(c *api.NetworkRADIUSRuntimeControl, unknown bool, a api.NetworkRADIUSRuntimeACK, now time.Time) error {
	if a.StateCountKnown != (a.ActiveStates != nil) || a.ActiveStates != nil && (*a.ActiveStates < 0 || *a.ActiveStates > 65536) || a.AcceptingNewAuthentications && !c.AcceptNewAuthentications {
		return apperrors.ErrInvalidArgument
	}
	if a.ProcessStopped {
		return validateStoppedACK(c, a, now)
	}
	if unknown || c.StopRequired || !c.RunProcess || !c.ConfigurationLeaseUntil.After(now) || !c.ServeExistingUntil.After(now) {
		return apperrors.ErrConflict
	}
	if a.Ready && (!a.HooksReady || !a.ConfigurationApplied || !a.StateCountKnown) {
		return apperrors.ErrInvalidArgument
	}
	return nil
}
func validateStoppedACK(c *api.NetworkRADIUSRuntimeControl, a api.NetworkRADIUSRuntimeACK, now time.Time) error {
	if !a.StateCountKnown || a.ActiveStates == nil || *a.ActiveStates != 0 || a.Ready || a.AcceptingNewAuthentications {
		return apperrors.ErrInvalidArgument
	}
	if c.StopRequired && (c.StopConfirmUntil == nil || !c.StopConfirmUntil.After(now)) {
		return apperrors.ErrConflict
	}
	return nil
}
func (r *Repository) Drain(ctx context.Context, id string, revision int64, now time.Time) (domain.DrainStatus, error) {
	state, err := r.Get(ctx, id)
	if err != nil {
		return domain.DrainStatus{}, err
	}
	if int64(state.Pool.Revision) != revision {
		return domain.DrainStatus{}, apperrors.ErrConflict
	}
	return drainForState(r.db.WithContext(ctx), state, now)
}
func drainForState(tx *gorm.DB, state domain.State, now time.Time) (domain.DrainStatus, error) {
	members, err := New(tx).Members(tx.Statement.Context, state.Pool.PoolID)
	if err != nil {
		return domain.DrainStatus{}, err
	}
	result := domain.DrainStatus{DrainProven: state.DrainProven, ExpectedMembers: int(max(int32(0), state.RetiringEnd-state.RetiringFrom)), CountKnown: true, Ready: true, Deadline: state.DrainUntil}
	seenRetiring, seenActive := 0, 0
	var total int64
	for _, record := range members {
		if int(record.Member.Ordinal) < state.Pool.DesiredReplicas {
			seenActive++
		}
		if int(record.Member.Ordinal) < state.Pool.DesiredReplicas && !record.Ready(state, now) {
			result.Ready = false
		}
		if !state.Retiring(record.Member.Ordinal) {
			continue
		}
		seenRetiring++
		if !record.Fresh(state, now) || !record.ACK.StateCountKnown || record.ACK.ActiveStates == nil {
			result.CountKnown = false
			continue
		}
		total += int64(*record.ACK.ActiveStates)
		result.AcceptingNew = result.AcceptingNew || record.ACK.AcceptingNewAuthentications
		if record.KnownZero(state, now) {
			result.ConfirmedMembers++
		}
	}
	if seenRetiring != result.ExpectedMembers {
		result.CountKnown = false
	}
	if seenActive != state.Pool.DesiredReplicas {
		result.Ready = false
	}
	if result.CountKnown {
		result.ActiveStates = &total
	}
	return result, nil
}

func applyControlIntent(state domain.State, record domain.MemberRecord, control *api.NetworkRADIUSRuntimeControl, old *api.NetworkRADIUSRuntimeControl, now time.Time) {
	retiring := state.Retiring(record.Member.Ordinal)
	control.RunProcess = int(record.Member.Ordinal) < state.Pool.DesiredReplicas && state.Pool.Enabled || retiring
	control.AcceptNewAuthentications = control.RunProcess && !retiring && state.Pool.State != "unknown" && !record.UnknownLatched
	if retiring && state.DrainUntil != nil {
		control.DrainUntil = state.DrainUntil
		control.ServeExistingUntil = earlier(control.ServeExistingUntil, *state.DrainUntil)
		control.StopConfirmUntil = state.StopConfirmUntil
		control.StopRequired = !state.DrainUntil.After(now)
	}
	if record.UnknownLatched {
		control.StopRequired = true
		control.AcceptNewAuthentications = false
		if old != nil && old.ControlRevision == control.ControlRevision && old.StopConfirmUntil != nil {
			control.StopConfirmUntil = old.StopConfirmUntil
		} else {
			until := earlier(now.Add(15*time.Second), control.ControlLeaseUntil)
			control.StopConfirmUntil = &until
		}
	}
	if control.StopRequired {
		control.RunProcess = false
		control.AcceptNewAuthentications = false
	}
}

func matchesACKIdentity(state domain.State, record domain.MemberRecord, c currentCredential, ack api.NetworkRADIUSRuntimeACK, runtime string) bool {
	control := record.Control
	return ack.RuntimeID == runtime && ack.PoolID == state.Pool.PoolID && ack.MemberID == record.Member.MemberID && ack.CredentialID == c.ID && ack.CredentialID == control.CredentialID && ack.ProcessEpoch == control.ProcessEpoch && ack.OwnerEpoch == state.Pool.OwnerEpoch && int64(ack.ControlRevision) == state.ControlRevision && ack.ConfigurationRevision == state.Pool.ConfigurationRevision && ack.ConfigurationHash == state.Pool.ConfigurationHash
}
