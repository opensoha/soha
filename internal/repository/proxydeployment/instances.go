package proxydeployment

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	app "github.com/opensoha/soha/internal/application/proxydeployment"
	proxy "github.com/opensoha/soha/internal/domain/networkproxy"
	domain "github.com/opensoha/soha/internal/domain/proxydeployment"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	proxyrepo "github.com/opensoha/soha/internal/repository/networkproxy"
	"gorm.io/gorm"
	"reflect"
	"slices"
	"time"
)

type memberRecord struct {
	ReceivedAt time.Time
	Grant      domain.InstanceControl
	ACK        *domain.InstanceACK
	Retired    []string
}
type credential struct {
	ID         string
	Generation int64
	ExpiresAt  time.Time
}

func memberPool(tx *gorm.DB, runtimeID, instanceID string) (string, error) {
	var id string
	err := tx.Raw(`SELECT proxy_id FROM network_proxy_deployment_members WHERE runtime_id=? AND instance_id=?`, runtimeID, instanceID).Row().Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		err = apperrors.ErrNotFound
	}
	return id, err
}
func currentCredential(tx *gorm.DB, identity networkidentity.Identity, now time.Time) (credential, error) {
	var c credential
	err := tx.Raw(`SELECT id,generation,expires_at FROM network_runtime_credentials WHERE tenant_id='default' AND workspace_id='default' AND runtime_kind='proxy' AND runtime_id=? AND certificate_fingerprint=? AND status='active' AND revoked_at IS NULL AND not_before<=? AND expires_at>? FOR SHARE`, identity.ID, identity.CertificateFingerprint, now, now).Row().Scan(&c.ID, &c.Generation, &c.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = apperrors.ErrUnauthorized
	}
	return c, err
}
func readMember(tx *gorm.DB, pool, instance string) (memberRecord, error) {
	var raw []byte
	err := tx.Raw(`SELECT record FROM network_proxy_deployment_instances WHERE proxy_id=? AND instance_id=? FOR UPDATE`, pool, instance).Row().Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return memberRecord{}, nil
	}
	var record memberRecord
	if err == nil {
		err = json.Unmarshal(raw, &record)
	}
	return record, err
}
func saveMember(tx *gorm.DB, pool, instance string, record memberRecord) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO network_proxy_deployment_instances(proxy_id,instance_id,record) VALUES(?,?,?::jsonb) ON CONFLICT(proxy_id,instance_id) DO UPDATE SET record=EXCLUDED.record`, pool, instance, string(raw)).Error
}
func (r *Repository) GrantControl(ctx context.Context, identity networkidentity.Identity, instance, epoch, expectedPolicyHash string) (domain.InstanceControl, error) {
	var result domain.InstanceControl
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		pool, err := memberPool(tx, identity.ID, instance)
		if err != nil {
			return err
		}
		if err = lockPool(ctx, tx, pool); err != nil {
			return err
		}
		state, err := New(tx).Get(ctx, pool)
		if err != nil {
			return err
		}
		if err := app.RequireAcceptedPolicyHash(state, expectedPolicyHash); err != nil {
			return err
		}
		if !app.PolicyContainsMember(*state.Slot, identity.ID, instance) {
			return apperrors.ErrAccessDenied
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		cred, err := currentCredential(tx, identity, now)
		if err != nil {
			return err
		}
		old, err := readMember(tx, pool, instance)
		if err != nil {
			return err
		}
		if err := advanceProcess(&old, epoch, now); err != nil {
			return err
		}

		result, err = grantMemberControl(ctx, tx, &state, cred, identity.ID, instance, epoch, now)
		if err != nil {
			return err
		}

		if old.Grant.CredentialID != result.CredentialID || old.Grant.CredentialEpoch != result.CredentialEpoch || old.Grant.ControlRevision != result.ControlRevision || old.Grant.ProcessEpoch != epoch {
			old.ACK = nil
		}
		old.Grant = result
		return saveMember(tx, pool, instance, old)
	})
	return result, err
}
func validACK(ack domain.InstanceACK, grant domain.InstanceControl, now time.Time) bool {
	return validStopACK(ack, grant) && ack.InstanceID == grant.InstanceID && ack.RuntimeID == grant.RuntimeID && ack.ProcessEpoch == grant.ProcessEpoch && ack.CredentialID == grant.CredentialID && ack.CredentialEpoch == grant.CredentialEpoch && ack.OwnerEpoch == grant.OwnerEpoch && ack.ControlRevision == grant.ControlRevision && ack.ExpiresAt.Equal(grant.ExpiresAt) && grant.ExpiresAt.After(now) && !ack.ObservedAt.Before(now.Add(-10*time.Second)) && !ack.ObservedAt.After(now.Add(time.Second)) && ack.ActiveConnections >= 0 && (ack.ActiveCountKnown || ack.ActiveConnections == 0) && (!ack.AcceptingRequests || grant.AcceptingRequests)
}
func (r *Repository) RecordACK(ctx context.Context, identity networkidentity.Identity, ack domain.InstanceACK) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		pool, err := memberPool(tx, identity.ID, ack.InstanceID)
		if err != nil {
			return err
		}
		if err = lockPool(ctx, tx, pool); err != nil {
			return err
		}
		state, err := New(tx).Get(ctx, pool)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		cred, err := currentCredential(tx, identity, now)
		if err != nil {
			return err
		}
		record, err := readMember(tx, pool, ack.InstanceID)
		if err != nil {
			return err
		}
		if !currentACKMatches(state, cred, identity.ID, record.Grant, ack, now) {
			return apperrors.ErrConflict
		}
		if record.Grant.StopRequired {
			if err := validateStoppedTask(tx, state, record.Grant, now); err != nil {
				return err
			}
		}
		if record.ACK != nil && !ack.ObservedAt.After(record.ACK.ObservedAt) {
			return apperrors.ErrConflict
		}
		if ack.AcceptingRequests {
			ready, enabled, err := configurationStatus(tx, identity.ID)
			if err != nil {
				return err
			}
			draining, drainErr := checkedMemberDrain(tx, state, identity.ID, ack.InstanceID, now)
			if drainErr != nil {
				return drainErr
			}
			if !ready || !enabled || draining {
				return apperrors.ErrConflict
			}
		}
		record.ReceivedAt = now.UTC()
		record.ACK = &ack
		return saveMember(tx, pool, ack.InstanceID, record)
	})
}
func configurationStatus(tx *gorm.DB, id string) (bool, bool, error) {
	var row struct {
		Enabled, SecurityRestricted       bool
		DesiredRevision, ObservedRevision int64
		DesiredHash, ObservedHash         string
		Binding, DesiredProtection        []byte
		ObservedPolicyVersion             int
		BundleID                          string
		BundleRevision                    int64
	}
	if err := tx.Table("network_proxy_instances").Where("id=? AND tenant_id='default' AND workspace_id='default'", id).Take(&row).Error; err != nil {
		return false, false, err
	}
	var binding *proxy.Binding
	var protection *proxy.Protection
	if err := optionalJSON(row.Binding, &binding); err != nil {
		return false, false, err
	}
	if err := optionalJSON(row.DesiredProtection, &protection); err != nil {
		return false, false, err
	}
	if row.DesiredRevision < 1 || row.DesiredRevision != row.ObservedRevision {
		return false, row.Enabled, nil
	}
	if row.Enabled {
		if row.SecurityRestricted || protection == nil || protection.PolicyVersion < 1 || protection.PolicyVersion != row.ObservedPolicyVersion {
			return false, true, nil
		}
		var currentPolicy int
		if err := tx.Raw(`SELECT COALESCE(max(policy_version),0) FROM network_access_policy_snapshots WHERE tenant_id='default' AND workspace_id='default'`).Row().Scan(&currentPolicy); err != nil {
			return false, true, err
		}
		if currentPolicy != protection.PolicyVersion {
			return false, true, nil
		}
	}

	config := proxy.Configuration{Enabled: row.Enabled, ContentHash: row.DesiredHash, Binding: binding, Protection: protection}
	if row.BundleID != "" {
		config.Publication = &api.NetworkProxyServerPublication{BundleID: row.BundleID, Revision: row.BundleRevision}
	}
	return config.Digest() == row.ObservedHash, row.Enabled, nil
}
func (r *Repository) Members(ctx context.Context, id string) ([]api.NetworkProxyDeploymentMember, error) {
	result := []api.NetworkProxyDeploymentMember{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockPool(ctx, tx, id); err != nil {
			return err
		}
		state, err := New(tx).Get(ctx, id)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		for _, m := range state.Members {
			view := api.NetworkProxyDeploymentMember{InstanceID: m.InstanceID, RuntimeID: m.RuntimeID}
			record, err := readMember(tx, id, m.InstanceID)
			if err != nil {
				return err
			}
			ack := record.ACK
			if ack != nil {
				view.ProcessEpoch = ack.ProcessEpoch
				view.CredentialID = ack.CredentialID
				view.CredentialEpoch = ack.CredentialEpoch
				view.ObservedAt = record.ReceivedAt
				view.AckRevision = ack.ControlRevision
				view.ControlExpiresAt = &ack.ExpiresAt
			}
			if ack != nil && validACK(*ack, record.Grant, now) && ack.OwnerEpoch == state.OwnerEpoch && ack.ControlRevision == state.ControlRevision {
				var valid bool
				if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM network_runtime_credentials WHERE id=? AND generation=? AND runtime_id=? AND runtime_kind='proxy' AND status='active' AND revoked_at IS NULL AND not_before<=? AND expires_at>?)`, ack.CredentialID, ack.CredentialEpoch, m.RuntimeID, now, now).Row().Scan(&valid); err != nil {
					return err
				}
				if valid {
					view.ActiveCountKnown = ack.ActiveCountKnown
					view.ActiveConnections = ack.ActiveConnections
					view.AcceptingRequests = ack.AcceptingRequests
					if record.Grant.StopRequired {
						result = append(result, view)
						continue
					}
					ready, enabled, err := configurationStatus(tx, m.RuntimeID)
					if err != nil {
						return err
					}
					draining, drainErr := checkedMemberDrain(tx, state, m.RuntimeID, m.InstanceID, now)
					if drainErr != nil {
						return drainErr
					}
					view.Ready = deploymentReady(ready, enabled, ack.AcceptingRequests, draining)
					view.TrafficReady = ready && enabled && ack.AcceptingRequests && !draining
				}
			}
			result = append(result, view)
		}
		return nil
	})
	return result, err
}
func (r *Repository) DrainStatus(ctx context.Context, id string, generation int64, now time.Time) (domain.DrainStatus, error) {
	state, err := r.Get(ctx, id)
	if err != nil {
		return domain.DrainStatus{}, err
	}
	if state.Generation != generation {
		return domain.DrainStatus{}, apperrors.ErrConflict
	}
	members, err := r.Members(ctx, id)
	if err != nil {
		return domain.DrainStatus{}, err
	}
	return aggregateDrainStatus(state, members, now), nil
}

func aggregateDrainStatus(state domain.State, members []api.NetworkProxyDeploymentMember, now time.Time) domain.DrainStatus {
	status := domain.DrainStatus{StopRequired: state.StopConfirmUntil != nil, ConfirmUntil: state.StopConfirmUntil, CountKnown: true, Ready: true}
	selected := domain.ReplicaRange{End: state.DesiredReplicas}
	if state.DrainHeld {
		var valid bool
		selected, valid = trustedDrainRange(state)
		if !valid {
			status.CountKnown, status.Ready = false, false
			return status
		}
	}
	views := map[string]api.NetworkProxyDeploymentMember{}
	for _, member := range members {
		views[member.RuntimeID+"\x00"+member.InstanceID] = member
	}
	for _, expected := range state.Members {
		if !selected.Contains(expected.Ordinal) {
			continue
		}
		m, found := views[expected.RuntimeID+"\x00"+expected.InstanceID]
		if !found || m.ControlExpiresAt == nil || !m.ControlExpiresAt.After(now) || m.ObservedAt.Before(now.Add(-10*time.Second)) || m.AckRevision != state.ControlRevision || m.CredentialID == "" {
			status.CountKnown, status.Ready = false, false
			continue
		}
		status.Instances++
		status.CountKnown = status.CountKnown && m.ActiveCountKnown
		status.ActiveChecks += m.ActiveConnections
		status.AcceptingRequests = status.AcceptingRequests || m.AcceptingRequests
		status.Ready = status.Ready && m.Ready
	}
	if status.Instances != int(selected.Count()) {
		status.CountKnown, status.Ready = false, false
	}
	return status
}

func advanceProcess(old *memberRecord, epoch string, now time.Time) error {
	if slices.Contains(old.Retired, epoch) {
		return apperrors.ErrAccessDenied
	}
	if old.Grant.ProcessEpoch != "" && old.Grant.ProcessEpoch != epoch {
		if old.Grant.ExpiresAt.After(now) {
			return apperrors.ErrConflict
		}
		old.Retired = append(old.Retired, old.Grant.ProcessEpoch)
		old.ACK = nil
	}
	return nil
}

func optionalJSON(raw []byte, dst any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, dst)
}
func deploymentReady(configReady, enabled, accepting, draining bool) bool {
	return configReady && (!enabled || (accepting && !draining))
}

func pinDrainLease(ctx context.Context, tx *gorm.DB, state *domain.State, command domain.Command) error {
	if state.DrainLeasePinned {
		return nil
	}
	until := command.DrainUntil
	if until.IsZero() {
		until = command.Deadline
	}
	if command.Deadline.Before(until) {
		until = command.Deadline
	}
	required := int32(0)
	if state.ActualReplicas != nil {
		required = *state.ActualReplicas
	}
	if state.ObservedDesiredReplicas != nil && *state.ObservedDesiredReplicas > required {
		required = *state.ObservedDesiredReplicas
	}
	selected := domain.ReplicaRange{End: required}
	if state.ActualReplicas != nil && state.ObservedDesiredReplicas != nil {
		if removed, valid := domain.RetiringRange(command.Replicas, *state.ActualReplicas, *state.ObservedDesiredReplicas, int32(len(state.Members))); valid {
			selected = removed
		}
	}
	ids := []string{}
	for _, m := range state.Members {
		if selected.Contains(m.Ordinal) {
			ids = append(ids, m.RuntimeID)
		}
	}
	if len(ids) > 0 {
		members, err := proxyrepo.New(tx).ObserveMemberConfigurations(ctx, ids)
		if err != nil {
			return err
		}
		if len(members) != len(ids) {
			until = time.Now().UTC()
		}
		for _, m := range members {
			if m.ConfigurationValidUntil == nil {
				until = time.Now().UTC()
				break
			}
			if m.ConfigurationValidUntil.Before(until) {
				until = *m.ConfigurationValidUntil
			}
		}
	}
	state.DrainUntil = &until
	state.DrainLeasePinned = true
	return nil
}

func grantExpiry(state domain.State, credentialUntil, now time.Time) (time.Time, error) {
	until := now.Add(30 * time.Second)
	if state.RequiresDrain() {
		if state.Owner != "soha-managed" || state.Phase == "unknown" || state.Phase == "degraded" || state.Phase == "conflict" || !state.DrainLeasePinned || state.DrainUntil == nil {
			return time.Time{}, apperrors.ErrAccessDenied
		}
		until = *state.DrainUntil
		if state.StopConfirmUntil != nil {
			until = *state.StopConfirmUntil
		}
	}
	if credentialUntil.Before(until) {
		until = credentialUntil
	}
	if !until.After(now) {
		return time.Time{}, apperrors.ErrAccessDenied
	}
	return until, nil
}

func validStopACK(ack domain.InstanceACK, grant domain.InstanceControl) bool {
	if ack.ProcessStopped && (ack.AcceptingRequests || !ack.ActiveCountKnown || ack.ActiveConnections != 0) {
		return false
	}
	if grant.StopRequired {
		return ack.ProcessStopped
	}
	return !ack.ProcessStopped
}

func currentACKMatches(state domain.State, cred credential, runtimeID string, grant domain.InstanceControl, ack domain.InstanceACK, now time.Time) bool {
	return validACK(ack, grant, now) && ack.RuntimeID == runtimeID && cred.ID == ack.CredentialID && cred.Generation == ack.CredentialEpoch && state.OwnerEpoch == ack.OwnerEpoch && state.ControlRevision == ack.ControlRevision
}

// A survivor exemption is valid only inside a pinned, trusted shrink operation.
func trustedDrainRange(state domain.State) (domain.ReplicaRange, bool) {
	if state.Owner != "soha-managed" || !state.DrainHeld || !state.DrainLeasePinned || state.DrainUntil == nil || (state.Phase != "queued" && state.Phase != "reconciling") {
		return domain.ReplicaRange{}, false
	}
	if _, err := app.AcceptedPolicyHash(state); err != nil {
		return domain.ReplicaRange{}, false
	}
	if state.MemberSetHash != state.Slot.MemberSetHash() || !reflect.DeepEqual(state.Members, state.Slot.Members) || state.ActualReplicas == nil || state.ObservedDesiredReplicas == nil || state.ObservedAt == nil {
		return domain.ReplicaRange{}, false
	}
	removed, valid := domain.RetiringRange(state.DesiredReplicas, *state.ActualReplicas, *state.ObservedDesiredReplicas, state.Slot.MaxReplicas)
	return removed, valid && removed.Count() > 0
}
func memberRequiresDrain(state domain.State, runtimeID, instance string) bool {
	if !state.RequiresDrain() {
		return false
	}
	removed, valid := trustedDrainRange(state)
	return !valid || !memberWithinTarget(*state.Slot, runtimeID, instance, removed.First)
}
func checkedMemberDrain(tx *gorm.DB, state domain.State, runtimeID, instance string, now time.Time) (bool, error) {
	draining := memberRequiresDrain(state, runtimeID, instance)
	if !draining && state.RequiresDrain() {
		if _, err := stopTaskDeadline(tx, state, now); err != nil {
			return false, err
		}
	}
	return draining, nil
}
func memberGrantExpiry(state domain.State, credentialUntil, now time.Time, draining bool) (time.Time, error) {
	if draining {
		return grantExpiry(state, credentialUntil, now)
	}
	until := now.Add(30 * time.Second)
	if credentialUntil.Before(until) {
		until = credentialUntil
	}
	if !until.After(now) {
		return time.Time{}, apperrors.ErrAccessDenied
	}
	return until, nil
}

func grantMemberControl(ctx context.Context, tx *gorm.DB, state *domain.State, cred credential, runtimeID, instance, epoch string, now time.Time) (domain.InstanceControl, error) {

	allowed := memberWithinTarget(*state.Slot, runtimeID, instance, state.DesiredReplicas)
	if !allowed && !state.RequiresDrain() {
		return domain.InstanceControl{}, apperrors.ErrAccessDenied
	}
	if removed, valid := trustedDrainRange(*state); valid && !memberWithinTarget(*state.Slot, runtimeID, instance, removed.End) {
		return domain.InstanceControl{}, apperrors.ErrAccessDenied
	}
	draining, err := checkedMemberDrain(tx, *state, runtimeID, instance, now)
	if err != nil {
		return domain.InstanceControl{}, err
	}
	if draining {
		if err = ensureStopConfirmation(ctx, tx, state, cred.ExpiresAt, now); err != nil {
			return domain.InstanceControl{}, err
		}
	}
	until, err := memberGrantExpiry(*state, cred.ExpiresAt, now, draining)
	if err != nil {
		return domain.InstanceControl{}, err
	}

	result := domain.InstanceControl{InstanceID: instance, RuntimeID: runtimeID, ProcessEpoch: epoch, CredentialID: cred.ID, CredentialEpoch: cred.Generation, OwnerEpoch: state.OwnerEpoch, ControlRevision: state.ControlRevision, StopRequired: draining && state.StopConfirmUntil != nil, AcceptingRequests: allowed && !draining, IssuedAt: now, ExpiresAt: until}
	return result, nil
}
