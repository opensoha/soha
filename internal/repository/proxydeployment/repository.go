package proxydeployment

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"

	app "github.com/opensoha/soha/internal/application/proxydeployment"
	"github.com/opensoha/soha/internal/domain/delivery"
	domain "github.com/opensoha/soha/internal/domain/proxydeployment"
	"github.com/opensoha/soha/internal/platform/apperrors"
	repodelivery "github.com/opensoha/soha/internal/repository/delivery"
	"gorm.io/gorm"
)

type Repository struct{ db *gorm.DB }

func New(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Get(ctx context.Context, id string) (domain.State, error) {
	var exists string
	if err := r.db.WithContext(ctx).Raw(`SELECT id FROM network_proxy_instances WHERE id = ? AND tenant_id='default' AND workspace_id='default'`, id).Row().Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.State{}, apperrors.ErrNotFound
		}
		return domain.State{}, err
	}
	var encoded []byte
	err := r.db.WithContext(ctx).Raw(`SELECT state FROM network_proxy_deployments WHERE proxy_id = ?`, id).Row().Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.State{ProxyID: id, Owner: "external", Phase: "unmanaged"}, nil
	}
	var state domain.State
	if err == nil {
		err = json.Unmarshal(encoded, &state)
	}
	return state, err
}

func (r *Repository) Commit(ctx context.Context, mutation app.Mutation) (domain.State, error) {
	var result domain.State
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockPool(ctx, tx, mutation.State.ProxyID); err != nil {
			return err
		}
		replayed, found, err := replay(tx, mutation)
		if err != nil {
			return err
		}
		if found {
			result = replayed
			return nil
		}
		current, err := New(tx).Get(ctx, mutation.State.ProxyID)
		if err != nil {
			return err
		}
		if current.Generation != mutation.ExpectedGeneration || mutation.State.Generation != current.Generation+1 {
			return apperrors.ErrConflict
		}
		if err := requireReconciledOutcome(current, mutation); err != nil {
			return err
		}
		if err := validateMutationPin(tx, current, mutation); err != nil {
			return err
		}
		if err := prepareMutation(ctx, tx, current, &mutation); err != nil {
			return err
		}
		if err := ensurePreviousTaskStopped(ctx, tx, current.LatestTaskID); err != nil {
			return err
		}
		if mutation.Task != nil {
			if err := createControlTask(ctx, tx, *mutation.Task); err != nil {
				return err
			}
		}
		if err := save(tx, mutation.State); err != nil {
			return err
		}
		encoded, err := json.Marshal(mutation.State)
		if err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO network_proxy_deployment_requests(proxy_id,request_id,fingerprint,response) VALUES(?,?,?,?::jsonb)`, mutation.State.ProxyID, mutation.RequestID, mutation.Fingerprint, string(encoded)).Error; err != nil {
			return err
		}
		result = mutation.State
		return nil
	})
	return result, runtimeSlotOwnerError(err)
}

func requireReconciledOutcome(current domain.State, mutation app.Mutation) error {
	if current.Phase != "unknown" {
		return nil
	}
	if mutation.Task != nil {
		command, err := app.DecodeCommand(*mutation.Task)
		if err == nil && command.Action == "reconcile" {
			return nil
		}
	}
	return fmt.Errorf("%w: unknown runtime outcome requires read-only reconciliation", apperrors.ErrConflict)
}

func prepareReconciliation(current domain.State, mutation *app.Mutation) {
	if mutation.Task == nil {
		return
	}
	command, err := app.DecodeCommand(*mutation.Task)
	if err != nil || command.Action != "reconcile" {
		return
	}
	// Results can update observations without changing the spec generation.
	// Preserve the authoritative goal and gate while holding the Proxy lock.
	next := current
	next.Generation, next.LatestTaskID, next.Phase, next.Reason = mutation.State.Generation, mutation.Task.ID, "queued", ""
	next.DrainHeld = current.RequiresDrain()
	next.ControlRevision = current.ControlRevision + 1
	if current.Phase == "unknown" {
		next.Phase = "unknown"
	}
	mutation.State = next
	command.Replicas = current.DesiredReplicas
	mutation.Task.Payload["proxyDeployment"] = command
}

func lockPool(ctx context.Context, tx *gorm.DB, id string) error {
	if err := tx.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "network-access-policy-snapshot:default:default").Error; err != nil {
		return err
	}
	var existing string
	err := tx.Raw("SELECT proxy_id FROM network_proxy_deployments WHERE proxy_id=? FOR UPDATE", id).Row().Scan(&existing)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	err = tx.Raw("SELECT id FROM network_proxy_instances WHERE id=? AND tenant_id='default' AND workspace_id='default'", id).Row().Scan(&existing)
	if errors.Is(err, sql.ErrNoRows) {
		return apperrors.ErrNotFound
	}
	return err
}

func replay(tx *gorm.DB, mutation app.Mutation) (domain.State, bool, error) {
	var fingerprint string
	var encoded []byte
	err := tx.Raw(`SELECT fingerprint,response FROM network_proxy_deployment_requests WHERE proxy_id=? AND request_id=?`, mutation.State.ProxyID, mutation.RequestID).Row().Scan(&fingerprint, &encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.State{}, false, nil
	}
	if err != nil {
		return domain.State{}, false, err
	}
	if fingerprint != mutation.Fingerprint {
		return domain.State{}, false, fmt.Errorf("%w: request ID was used for different input", apperrors.ErrConflict)
	}
	var state domain.State
	err = json.Unmarshal(encoded, &state)
	return state, true, err
}

func ensurePreviousTaskStopped(ctx context.Context, tx *gorm.DB, id string) error {
	if id == "" {
		return nil
	}
	task, err := repodelivery.New(tx).GetExecutionTask(ctx, id)
	if err != nil {
		return err
	}
	switch task.Status {
	case "completed", "failed", "canceled", "callback_timeout":
		return nil
	default:
		return fmt.Errorf("%w: previous runtime operation is still active", apperrors.ErrConflict)
	}
}

func save(tx *gorm.DB, state domain.State) error {
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	members, err := json.Marshal(state.Members)
	if err != nil {
		return err
	}
	if err := tx.Exec(`INSERT INTO network_proxy_deployments(proxy_id,slot_id,generation,state,owner_epoch,member_set_hash,members) VALUES(?,?,?,?::jsonb,?,?,?::jsonb) ON CONFLICT(proxy_id) DO UPDATE SET slot_id=EXCLUDED.slot_id,generation=EXCLUDED.generation,state=EXCLUDED.state,owner_epoch=EXCLUDED.owner_epoch,member_set_hash=EXCLUDED.member_set_hash,members=EXCLUDED.members,updated_at=now()`, state.ProxyID, state.SlotID, state.Generation, string(encoded), state.OwnerEpoch, state.MemberSetHash, string(members)).Error; err != nil {
		return err
	}
	for _, member := range state.Members {
		result := tx.Exec(`INSERT INTO network_proxy_deployment_members AS existing(runtime_id,proxy_id,instance_id,ordinal) VALUES(?,?,?,?) ON CONFLICT(runtime_id) DO UPDATE SET instance_id=EXCLUDED.instance_id,ordinal=EXCLUDED.ordinal WHERE existing.proxy_id=EXCLUDED.proxy_id`, member.RuntimeID, state.ProxyID, member.InstanceID, member.Ordinal)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return apperrors.ErrConflict
		}
	}
	return nil
}

func (r *Repository) RecordResult(ctx context.Context, task delivery.ExecutionTask, command domain.Command, receipt *domain.Receipt) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockPool(ctx, tx, command.ProxyID); err != nil {
			return err
		}
		state, err := New(tx).Get(ctx, command.ProxyID)
		if err != nil {
			return err
		}
		if state.Generation != command.Generation || state.OwnerEpoch != command.OwnerEpoch || state.LatestTaskID != task.ID || state.UID != command.Slot.UID || state.Owner != "soha-managed" {
			return nil
		}
		if receipt != nil && (receipt.Generation != state.Generation || receipt.OwnerEpoch != state.OwnerEpoch) {
			return apperrors.ErrConflict
		}
		applyResult(&state, task, command, receipt)
		if state.DrainHeld && !state.DrainLeasePinned && receipt != nil && receipt.Observation != nil && command.Action != "reconcile" {
			if err := pinDrainLease(ctx, tx, &state, command); err != nil {
				return err
			}
		}
		return save(tx, state)
	})
}

func applyResult(state *domain.State, task delivery.ExecutionTask, command domain.Command, receipt *domain.Receipt) {
	wasUnknown := state.Phase == "unknown"
	if wasUnknown && command.Action == "reconcile" && (task.Status == "running" || task.Status == "dispatching") {
		defer func() { state.Phase = "unknown" }()
	}
	drainHeld := state.RequiresDrain()
	if canResume(*state, task, command, receipt) {
		state.DrainHeld = false
		state.DrainUntil = nil
		state.StopConfirmUntil = nil
		state.DrainLeasePinned = false
		state.ControlRevision++
		state.Phase = "resuming"
		drainHeld = false
	}
	if state.Phase != "resuming" {
		state.Phase = "reconciling"
	}
	if receipt == nil {
		if task.Status != "running" && task.Status != "dispatching" {
			state.Phase, state.Reason = "unknown", "observation_required"
			state.DrainHeld = drainHeld
		}
		return
	}
	applyObservedReceipt(state, task, command, receipt, drainHeld)
}
func applyObservedReceipt(state *domain.State, task delivery.ExecutionTask, command domain.Command, receipt *domain.Receipt, drainHeld bool) {
	state.Reason = receipt.Reason
	uncertain := receipt.Uncertain || task.Status == "callback_timeout" || task.Status == "canceled"
	if uncertain {
		state.Phase = "unknown"
		state.DrainHeld = drainHeld
	}
	observation := receipt.Observation
	if observation == nil {
		return
	}
	if observation.UID != state.UID || observation.TemplateDigest != command.Slot.TemplateDigest {
		if command.Action == "reconcile" {
			state.Phase, state.DrainHeld = "unknown", drainHeld
		} else if !uncertain {
			state.Phase = "conflict"
		}
		state.Reason = "workload_identity_changed"
		return
	}
	// The first trusted observation can reveal a shrink before any previous
	// replica observation existed. Pin the retiring range before waiting for ACKs.
	if !uncertain && !receipt.Resume && command.Action != "reconcile" && command.Replicas < max(observation.Replicas, observation.DesiredReplicas) && !state.DrainHeld {
		state.DrainHeld = true
		state.ControlRevision++
	}
	observedDesired := observation.DesiredReplicas
	state.ObservedDesiredReplicas = &observedDesired
	actual, ready, observedAt := observation.Replicas, observation.ReadyReplicas, observation.ObservedAt
	state.ActualReplicas, state.ReadyReplicas, state.ObservedAt = &actual, &ready, &observedAt
	if uncertain {
		if state.Reason == "" {
			state.Reason = "observation_required"
		}
		return
	}
	if command.Action == "reconcile" {
		applyReconciliation(state, task, command, observation)
		return
	}
	applyObservedExecutionResult(state, task, command, receipt, observation)
}

func applyObservedExecutionResult(state *domain.State, task delivery.ExecutionTask, command domain.Command, receipt *domain.Receipt, observation *domain.Observation) {
	actual, ready := observation.Replicas, observation.ReadyReplicas
	if receipt.Drained && actual > 0 && actual == state.DesiredReplicas && !receipt.Uncertain {
		state.Phase = "resuming"
	}
	if task.Status == "completed" && !receipt.Uncertain && actual == state.DesiredReplicas && ready == actual && (actual != 0 || receipt.Drained) {
		state.Phase, state.Reason, state.ObservedGeneration = "converged", "", command.Generation
	} else if task.Status == "failed" {
		state.Phase = "degraded"
	}
}

func applyReconciliation(state *domain.State, task delivery.ExecutionTask, command domain.Command, observation *domain.Observation) {
	if task.Status != "completed" && task.Status != "failed" {
		return
	}
	state.ObservedGeneration = command.Generation
	state.Phase, state.Reason = "degraded", "observed_state_mismatch"
	if observation.DesiredReplicas != state.DesiredReplicas || observation.Replicas != state.DesiredReplicas || observation.ReadyReplicas != state.DesiredReplicas {
		return
	}
	if state.DrainHeld && state.DesiredReplicas > 0 {
		state.Reason = "drain_held"
		return
	}
	state.Phase, state.Reason, state.ObservedGeneration = "converged", "", command.Generation
}

var _ app.Repository = (*Repository)(nil)

func (r *Repository) TaskByToken(ctx context.Context, token string) (delivery.ExecutionTask, error) {
	return repodelivery.New(r.db).GetExecutionTaskByCallbackToken(ctx, token)
}

func canResume(state domain.State, task delivery.ExecutionTask, command domain.Command, receipt *domain.Receipt) bool {
	if receipt == nil || !receipt.Resume || !receipt.Drained || receipt.Uncertain || task.Status != "running" || !state.DrainHeld || state.DesiredReplicas <= 0 || receipt.Observation == nil {
		return false
	}
	observed := receipt.Observation
	return observed.UID == state.UID && observed.TemplateDigest == command.Slot.TemplateDigest && observed.Replicas == state.DesiredReplicas && observed.DesiredReplicas == state.DesiredReplicas
}

func prepareMutation(ctx context.Context, tx *gorm.DB, current domain.State, mutation *app.Mutation) error {
	mutation.State.ControlRevision = current.ControlRevision + 1
	if mutation.Task != nil {
		command, err := app.DecodeCommand(*mutation.Task)
		if err != nil {
			return err
		}
		if command.Action != "reconcile" {
			observed := int32(0)
			if current.ActualReplicas != nil {
				observed = *current.ActualReplicas
			}
			if current.ObservedDesiredReplicas != nil {
				observed = max(observed, *current.ObservedDesiredReplicas)
			}
			mutation.State.DrainHeld = command.Action == "stop" || command.Replicas < observed
			mutation.State.DrainLeasePinned = false
			mutation.State.StopConfirmUntil = nil
			if mutation.State.DrainHeld {
				until := command.DrainUntil
				mutation.State.DrainUntil = &until
			} else {
				mutation.State.DrainUntil = nil
			}
		}
	}
	mutation.State.ActualReplicas = current.ActualReplicas
	mutation.State.ReadyReplicas = current.ReadyReplicas
	mutation.State.ObservedAt = current.ObservedAt
	mutation.State.ObservedDesiredReplicas = current.ObservedDesiredReplicas
	prepareReconciliation(current, mutation)
	if mutation.Task != nil && mutation.State.DrainHeld && current.ActualReplicas != nil {
		command, err := app.DecodeCommand(*mutation.Task)
		if err != nil {
			return err
		}
		if err = pinDrainLease(ctx, tx, &mutation.State, command); err != nil {
			return err
		}
	}
	return nil
}
func createControlTask(ctx context.Context, tx *gorm.DB, task delivery.ExecutionTask) error {
	command, err := app.DecodeCommand(task)
	if err != nil {
		return err
	}
	if err = ensureMemberIdentities(tx, command); err != nil {
		return err
	}
	_, err = repodelivery.New(tx).CreateExecutionTask(ctx, task)
	return err
}
func ensureMemberIdentities(tx *gorm.DB, command domain.Command) error {
	if command.Action != "start" && command.Action != "scale" {
		return nil
	}
	for _, member := range command.Slot.Members {
		if member.Ordinal >= command.Replicas {
			continue
		}
		var exists bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM network_runtime_credentials WHERE runtime_id=? AND runtime_kind='proxy' AND tenant_id='default' AND workspace_id='default' AND status='active' AND revoked_at IS NULL AND not_before<=now() AND expires_at>now())`, member.RuntimeID).Row().Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("%w: approved member identity is unavailable", apperrors.ErrServiceUnavailable)
		}
	}
	return nil
}

// Only the cross-runtime parent UID fence is translated here. Unrelated
// uniqueness violations retain their original error and diagnostics.
func runtimeSlotOwnerError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" && (pg.ConstraintName == "runtime_slot_owner_conflict" || (pg.ConstraintName == "" && pg.Message == "runtime_slot_owner_conflict")) {
		return fmt.Errorf("%w: runtime_slot_owner_conflict", apperrors.ErrConflict)
	}
	return err
}
