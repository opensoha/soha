package radiusdeployment

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	app "github.com/opensoha/soha/internal/application/radiusdeployment"
	"github.com/opensoha/soha/internal/domain/delivery"
	access "github.com/opensoha/soha/internal/domain/networkaccess"
	domain "github.com/opensoha/soha/internal/domain/radiusdeployment"
	"github.com/opensoha/soha/internal/platform/apperrors"
	repodelivery "github.com/opensoha/soha/internal/repository/delivery"
	repoaccess "github.com/opensoha/soha/internal/repository/networkaccess"
	"gorm.io/gorm"
)

type Repository struct{ db *gorm.DB }

func New(db *gorm.DB) *Repository { return &Repository{db: db} }
func conflict(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && (pg.Code == "23505" || pg.Code == "23503") {
		return fmt.Errorf("%w: runtime_slot_or_binding_reserved", apperrors.ErrConflict)
	}
	return err
}
func (r *Repository) Get(ctx context.Context, id string) (domain.State, error) {
	var data []byte
	err := r.db.WithContext(ctx).Raw(`SELECT state FROM network_radius_pools WHERE pool_id=?`, id).Row().Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.State{}, apperrors.ErrNotFound
	}
	var state domain.State
	if err == nil {
		err = json.Unmarshal(data, &state)
	}
	return state, err
}
func (r *Repository) List(ctx context.Context) ([]domain.State, error) {
	rows, err := r.db.WithContext(ctx).Raw(`SELECT state FROM network_radius_pools ORDER BY pool_id LIMIT 129`).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []domain.State
	for rows.Next() {
		var data []byte
		var state domain.State
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &state); err != nil {
			return nil, err
		}
		result = append(result, state)
	}
	if len(result) > 128 {
		return nil, apperrors.ErrServiceUnavailable
	}
	return result, rows.Err()
}
func (r *Repository) Binding(ctx context.Context, id string) (access.NASBinding, error) {
	return repoaccess.New(r.db).GetNASBinding(ctx, id)
}
func (r *Repository) Members(ctx context.Context, pool string) ([]domain.MemberRecord, error) {
	rows, err := r.db.WithContext(ctx).Raw(`SELECT record FROM network_radius_members WHERE pool_id=? ORDER BY ordinal`, pool).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []domain.MemberRecord{}
	for rows.Next() {
		var encoded []byte
		var record domain.MemberRecord
		if err := rows.Scan(&encoded); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(encoded, &record); err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, rows.Err()
}
func lockPool(tx *gorm.DB, id string) error {
	return tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "radius-pool:"+id).Error
}
func save(tx *gorm.DB, state domain.State, create bool) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	p := state.Pool
	if create {
		return tx.Exec(`INSERT INTO network_radius_pools(pool_id,site_id,nas_binding_id,logical_runtime_id,slot_id,workload_uid,revision,state) VALUES(?,?,?,?,?,?,?,?::jsonb)`, p.PoolID, p.SiteID, p.NASBindingID, p.LogicalRuntimeID, p.SlotID, p.ObjectUID, p.Revision, string(data)).Error
	}
	return tx.Exec(`UPDATE network_radius_pools SET revision=?,state=?::jsonb,updated_at=now() WHERE pool_id=?`, p.Revision, string(data), p.PoolID).Error
}
func saveMember(tx *gorm.DB, pool string, record domain.MemberRecord, create bool) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if create {
		return tx.Exec(`INSERT INTO network_radius_members(runtime_id,pool_id,member_id,ordinal,record) VALUES(?,?,?,?,?::jsonb)`, record.Member.RuntimeID, pool, record.Member.MemberID, record.Member.Ordinal, string(data)).Error
	}
	return tx.Exec(`UPDATE network_radius_members SET record=?::jsonb WHERE runtime_id=? AND pool_id=?`, string(data), record.Member.RuntimeID, pool).Error
}

func (r *Repository) Commit(ctx context.Context, mutation app.Mutation) (domain.State, error) {
	var result domain.State
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		key := mutation.State.Pool.PoolID
		if mutation.Create {
			key = mutation.State.Pool.NASBindingID
		}
		if err := lockPool(tx, key); err != nil {
			return err
		}
		var oldHash string
		var response []byte
		err := tx.Raw(`SELECT fingerprint,response FROM network_radius_requests WHERE scope_id=? AND request_id=?`, key, mutation.RequestID).Row().Scan(&oldHash, &response)
		if err == nil {
			if oldHash != mutation.Fingerprint {
				return apperrors.ErrConflict
			}
			return json.Unmarshal(response, &result)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if mutation.Create {
			if err := prepareCreate(tx, mutation); err != nil {
				return err
			}
		} else {
			if err := prepareUpdate(tx, mutation); err != nil {
				return err
			}
		}
		if mutation.Task != nil {
			if _, err := repodelivery.New(tx).CreateExecutionTask(ctx, *mutation.Task); err != nil {
				return err
			}
		}
		if err := save(tx, mutation.State, mutation.Create); err != nil {
			return err
		}
		if mutation.Create {
			for _, member := range mutation.State.Slot.Members {
				if err := saveMember(tx, mutation.State.Pool.PoolID, domain.MemberRecord{Member: member}, true); err != nil {
					return err
				}
			}
		}
		if err := updateBinding(tx, mutation.State); err != nil {
			return err
		}
		encoded, _ := json.Marshal(mutation.State)
		if err := tx.Exec(`INSERT INTO network_radius_requests(scope_id,request_id,fingerprint,response) VALUES(?,?,?,?::jsonb)`, key, mutation.RequestID, mutation.Fingerprint, string(encoded)).Error; err != nil {
			return err
		}
		result = mutation.State
		return nil
	})
	return result, conflict(err)
}
func prepareCreate(tx *gorm.DB, m app.Mutation) error {
	binding, err := creationBinding(tx, m)
	if err != nil {
		return err
	}
	if binding.SiteID != m.State.Pool.SiteID || binding.NASID != m.State.Slot.NASID || binding.AccessMedium != m.State.Slot.AccessMedium || binding.SSID != m.State.Slot.SSID {
		return apperrors.ErrAccessDenied
	}
	var activeSite string
	if err := tx.Raw(`SELECT id FROM network_access_sites WHERE id=? AND status='active' FOR SHARE`, binding.SiteID).Row().Scan(&activeSite); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return apperrors.ErrAccessDenied
		}
		return err
	}
	if !m.CreateApprovedBinding {
		if m.ExpectedBindingUpdatedAt == nil || !binding.UpdatedAt.Equal(*m.ExpectedBindingUpdatedAt) || binding.Status == access.StatusActive || strings.HasPrefix(binding.RuntimeID, domain.PoolPrefix) {
			return fmt.Errorf("%w: existing NAS binding cannot be taken over", apperrors.ErrConflict)
		}
		if err := rejectExternalActivity(tx, binding); err != nil {
			return err
		}
	}
	for _, member := range m.State.Slot.Members {
		var active bool
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-runtime:"+member.RuntimeID).Error; err != nil {
			return err
		}
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM network_runtime_credentials WHERE runtime_id=? AND status='active' AND revoked_at IS NULL AND expires_at>now())`, member.RuntimeID).Scan(&active).Error; err != nil {
			return err
		}
		if active {
			return fmt.Errorf("%w: member_runtime_already_active", apperrors.ErrConflict)
		}
	}
	return nil
}
func creationBinding(tx *gorm.DB, m app.Mutation) (access.NASBinding, error) {
	p, slot := m.State.Pool, m.State.Slot
	var id string
	err := tx.Raw(`SELECT id FROM network_access_nas_bindings WHERE id=? FOR UPDATE`, p.NASBindingID).Row().Scan(&id)
	if err == nil {
		if m.CreateApprovedBinding {
			return access.NASBinding{}, fmt.Errorf("%w: approved_binding_already_exists", apperrors.ErrConflict)
		}
		return repoaccess.New(tx).GetNASBinding(tx.Statement.Context, id)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return access.NASBinding{}, err
	}
	if !m.CreateApprovedBinding {
		return access.NASBinding{}, apperrors.ErrNotFound
	}
	var active bool
	if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM network_access_sites WHERE id=? AND status='active')`, p.SiteID).Scan(&active).Error; err != nil {
		return access.NASBinding{}, err
	}
	if !active {
		return access.NASBinding{}, apperrors.ErrAccessDenied
	}
	if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM network_access_nas_bindings WHERE site_id=? AND nas_id=? AND status='active')`, p.SiteID, slot.NASID).Scan(&active).Error; err != nil {
		return access.NASBinding{}, err
	}
	if active {
		return access.NASBinding{}, fmt.Errorf("%w: nas_already_served", apperrors.ErrConflict)
	}
	trust, _ := json.Marshal(m.State.Configuration.EAPServerTrust)
	err = tx.Exec(`INSERT INTO network_access_nas_bindings(id,nas_id,runtime_id,site_id,name,access_medium,ssid,status,coa_supported,disconnect_supported,eap_server_trust,created_at,updated_at) VALUES(?,?,?,?,?,?,NULLIF(?,''),'active',false,false,?::jsonb,now(),now())`, p.NASBindingID, slot.NASID, p.LogicalRuntimeID, p.SiteID, slot.NASName, slot.AccessMedium, slot.SSID, string(trust)).Error
	if err != nil {
		return access.NASBinding{}, err
	}
	return repoaccess.New(tx).GetNASBinding(tx.Statement.Context, p.NASBindingID)
}
func rejectExternalActivity(tx *gorm.DB, binding access.NASBinding) error {
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-runtime:"+binding.RuntimeID).Error; err != nil {
		return err
	}
	var active bool
	if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM network_runtime_credentials WHERE runtime_id=? AND status='active' AND revoked_at IS NULL AND expires_at>now()) OR EXISTS(SELECT 1 FROM network_runtime_sessions WHERE runtime_id=? AND nas_id=? AND site_id=? AND status NOT IN ('expired','revoked') AND valid_until>now())`, binding.RuntimeID, binding.RuntimeID, binding.NASID, binding.SiteID).Scan(&active).Error; err != nil {
		return err
	}
	if active {
		return fmt.Errorf("%w: active_external_runtime", apperrors.ErrConflict)
	}
	return nil
}
func prepareUpdate(tx *gorm.DB, m app.Mutation) error {
	current, err := New(tx).Get(tx.Statement.Context, m.State.Pool.PoolID)
	if err != nil {
		return err
	}
	if int64(current.Pool.Revision) != m.ExpectedRevision || m.State.Pool.Revision != current.Pool.Revision+1 || current.SlotHash != m.State.SlotHash || current.Pool.ObjectUID != m.State.Pool.ObjectUID || current.Pool.OwnerEpoch != m.State.Pool.OwnerEpoch {
		return apperrors.ErrConflict
	}
	if current.Pool.OperationID != "" {
		task, err := repodelivery.New(tx).GetExecutionTask(tx.Statement.Context, current.Pool.OperationID)
		if err != nil {
			return err
		}
		if task.Status == "queued" || task.Status == "dispatching" || task.Status == "running" {
			return fmt.Errorf("%w: operation_in_progress", apperrors.ErrConflict)
		}
	}
	if current.Pool.State == "unknown" {
		if m.Task == nil {
			return apperrors.ErrConflict
		}
		command, err := app.DecodeCommand(*m.Task)
		if err != nil || command.Action != "reconcile" {
			return apperrors.ErrConflict
		}
	}
	if m.State.Pool.ConfigurationRevision != current.Pool.ConfigurationRevision {
		if current.Pool.Enabled || current.Pool.ObservedReplicas == nil || *current.Pool.ObservedReplicas != 0 {
			return fmt.Errorf("%w: stop_required", apperrors.ErrConflict)
		}
	}
	return nil
}
func updateBinding(tx *gorm.DB, state domain.State) error {
	trust, _ := json.Marshal(state.Configuration.EAPServerTrust)
	if err := tx.Exec(`SELECT set_config('soha.radius_pool_writer',?,true)`, state.Pool.PoolID).Error; err != nil {
		return err
	}
	return tx.Exec(`UPDATE network_access_nas_bindings SET runtime_id=?,status='active',eap_server_trust=?::jsonb,updated_at=now() WHERE id=? AND site_id=?`, state.Pool.LogicalRuntimeID, string(trust), state.Pool.NASBindingID, state.Pool.SiteID).Error
}

func (r *Repository) TaskByToken(ctx context.Context, token string) (delivery.ExecutionTask, error) {
	return repodelivery.New(r.db).GetExecutionTaskByCallbackToken(ctx, token)
}
func (r *Repository) RecordResult(ctx context.Context, task delivery.ExecutionTask, command domain.Command, receipt *domain.Receipt) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockPool(tx, command.PoolID); err != nil {
			return err
		}
		state, err := New(tx).Get(ctx, command.PoolID)
		if err != nil {
			return err
		}
		if int64(state.Pool.Revision) != command.Revision || int64(state.Pool.OwnerEpoch) != command.OwnerEpoch || state.Pool.OperationID != task.ID || state.SlotHash != command.SlotHash {
			return apperrors.ErrConflict
		}
		if receipt == nil || receipt.Revision != command.Revision || receipt.OwnerEpoch != command.OwnerEpoch {
			state.Pool.State = "unknown"
			state.Pool.ReasonCode = "missing_runtime_receipt"
			return save(tx, state, false)
		}
		if err := applyRuntimeReceipt(tx, &state, task, command, receipt); err != nil {
			return err
		}
		return save(tx, state, false)
	})
}
func applyObservation(state *domain.State, o domain.Observation, c domain.Command) error {
	if o.UID != state.Slot.UID || o.TemplateDigest != state.Slot.TemplateDigest || o.ResourceVersion == "" || o.ObservedAt.After(time.Now().Add(30*time.Second)) || o.ObservedAt.Before(time.Now().Add(-30*time.Second)) || o.ReadyReplicas < 0 || o.ReadyReplicas > o.Replicas || o.Replicas < 0 || o.Replicas > state.Slot.MaxReplicas || o.DesiredReplicas < 0 || o.DesiredReplicas > state.Slot.MaxReplicas {
		return apperrors.ErrConflict
	}
	actual := int(o.Replicas)
	state.Pool.ObservedReplicas = &actual
	state.ObservedDesiredReplicas = &o.DesiredReplicas
	state.ObservedAt = &o.ObservedAt
	if c.Action != "reconcile" {
		state.RetiringFrom = c.Replicas
		state.RetiringEnd = max(state.RetiringEnd, max(o.Replicas, o.DesiredReplicas))
	}
	if c.Action == "reconcile" && (o.DesiredReplicas != c.Replicas || o.Replicas != c.Replicas) {
		state.Pool.State = "unknown"
		return apperrors.ErrConflict
	}
	return nil
}

func (r *Repository) Reservation(ctx context.Context, uid string) (string, string, error) {
	var kind, id string
	err := r.db.WithContext(ctx).Raw(`SELECT owner_kind,owner_id FROM runtime_slot_owners WHERE workload_uid=?`, uid).Row().Scan(&kind, &id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	return kind, id, err
}
func (r *Repository) SiteActive(ctx context.Context, id string) (bool, error) {
	var active bool
	err := r.db.WithContext(ctx).Raw(`SELECT EXISTS(SELECT 1 FROM network_access_sites WHERE id=? AND status='active')`, id).Scan(&active).Error
	return active, err
}

func (r *Repository) BindingActivity(ctx context.Context, b access.NASBinding) (bool, error) {
	var active bool
	e := r.db.WithContext(ctx).Raw(`SELECT EXISTS(SELECT 1 FROM network_runtime_credentials WHERE runtime_id=? AND status='active' AND revoked_at IS NULL AND expires_at>now()) OR EXISTS(SELECT 1 FROM network_runtime_sessions WHERE runtime_id=? AND nas_id=? AND site_id=? AND status NOT IN ('expired','revoked') AND valid_until>now())`, b.RuntimeID, b.RuntimeID, b.NASID, b.SiteID).Scan(&active).Error
	return active, e
}

func applyRuntimeReceipt(tx *gorm.DB, state *domain.State, task delivery.ExecutionTask, command domain.Command, receipt *domain.Receipt) error {
	if receipt.Observation != nil {
		if err := applyObservation(state, *receipt.Observation, command); err != nil {
			state.Pool.State = "unknown"
			state.Pool.ReasonCode = "invalid_runtime_observation"
			return nil
		}
	}
	if receipt.Drained && !state.DrainProven && !receipt.Uncertain {
		drain, err := drainForState(tx, *state, time.Now().UTC())
		if err != nil {
			return err
		}
		if !drain.CountKnown || drain.ActiveStates == nil || *drain.ActiveStates != 0 || drain.AcceptingNew || drain.ConfirmedMembers != drain.ExpectedMembers {
			return apperrors.ErrConflict
		}
		state.DrainProven = true
	}
	if receipt.Uncertain || receipt.Observation == nil {
		state.Pool.State = "unknown"
		state.Pool.ReasonCode = "readback_required"
	} else {
		switch task.Status {
		case "completed":
			return completeRuntimeResult(tx, state, command, receipt)
		case "running", "dispatching":
			state.Pool.State = "starting"
			if state.RetiringEnd > state.RetiringFrom {
				state.Pool.State = "draining"
			}
		default:
			state.Pool.State = "unknown"
			state.Pool.ReasonCode = "readback_required"
		}
	}
	return nil
}

func completeRuntimeResult(tx *gorm.DB, state *domain.State, command domain.Command, receipt *domain.Receipt) error {

	if receipt.Observation.DesiredReplicas != command.Replicas || receipt.Observation.Replicas != command.Replicas || (state.RetiringEnd > state.RetiringFrom && !state.DrainProven) {
		state.Pool.State = "unknown"
		state.Pool.ReasonCode = "completion_unconfirmed"
		return nil
	}
	native, nativeErr := drainForState(tx, *state, time.Now().UTC())
	if nativeErr != nil {
		return nativeErr
	}
	if command.Replicas > 0 && (!native.Ready || receipt.Observation.ReadyReplicas != command.Replicas) {
		state.Pool.State = "unknown"
		state.Pool.ReasonCode = "native_readiness_unconfirmed"
		return nil
	}
	state.Pool.State = "ready"
	if command.Replicas == 0 {
		state.Pool.State = "stopped"
	}
	state.RetiringFrom, state.RetiringEnd = command.Replicas, command.Replicas
	state.DrainUntil, state.StopConfirmUntil = nil, nil
	return nil
}
