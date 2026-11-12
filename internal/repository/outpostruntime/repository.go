package outpostruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"

	app "github.com/opensoha/soha/internal/application/outpostruntime"
	"github.com/opensoha/soha/internal/domain/delivery"
	domain "github.com/opensoha/soha/internal/domain/outpostruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
	repodelivery "github.com/opensoha/soha/internal/repository/delivery"
	"gorm.io/gorm"
)

type Repository struct{ db *gorm.DB }

func New(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Get(ctx context.Context, id string) (domain.State, error) {
	state, err := r.readState(ctx, id)
	if err != nil {
		return state, err
	}
	task, err := r.unprojectedTask(ctx, state)
	if err != nil || !terminalRuntimeTask(task.Status) {
		return state, err
	}
	// The task ledger is authoritative if a terminal callback committed
	// before its result sink. Repair even on a read so the result stays visible.
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockOutpost(ctx, tx, id); err != nil {
			return err
		}
		var err error
		state, err = New(tx).readState(ctx, id)
		if err != nil {
			return err
		}
		_, _, err = settlePreviousTask(ctx, tx, &state)
		return err
	})
	return state, err
}

func (r *Repository) readState(ctx context.Context, id string) (domain.State, error) {
	var exists string
	if err := r.db.WithContext(ctx).Raw(`SELECT id FROM identity_outposts WHERE id = ?`, id).Row().Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.State{}, apperrors.ErrNotFound
		}
		return domain.State{}, err
	}
	var encoded []byte
	err := r.db.WithContext(ctx).Raw(`SELECT state FROM identity_outpost_runtime_deployments WHERE outpost_id = ?`, id).Row().Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.State{OutpostID: id, Owner: "external", Phase: "unmanaged"}, nil
	}
	var state domain.State
	if err == nil {
		err = json.Unmarshal(encoded, &state)
	}
	return state, err
}

func (r *Repository) Commit(ctx context.Context, mutation app.Mutation) (domain.State, error) {
	var result domain.State
	var rejection error
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockOutpost(ctx, tx, mutation.State.OutpostID); err != nil {
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
		current, err := New(tx).readState(ctx, mutation.State.OutpostID)
		if err != nil {
			return err
		}
		if current.Generation != mutation.ExpectedGeneration || mutation.State.Generation != current.Generation+1 {
			return apperrors.ErrConflict
		}
		active, repaired, err := settlePreviousTask(ctx, tx, &current)
		if err != nil {
			return err
		}
		if active {
			return fmt.Errorf("%w: previous runtime operation is still active", apperrors.ErrConflict)
		}
		rejection = validateNextMutation(current, mutation)
		if rejection != nil {
			if repaired {
				// Commit the repair while refusing to enqueue the new mutation.
				return nil
			}
			return rejection
		}
		prepareReconciliation(current, &mutation)
		if mutation.Task != nil {
			if _, err := repodelivery.New(tx).CreateExecutionTask(ctx, *mutation.Task); err != nil {
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
		if err := tx.Exec(`INSERT INTO identity_outpost_runtime_requests(outpost_id,request_id,fingerprint,response) VALUES(?,?,?,?::jsonb)`, mutation.State.OutpostID, mutation.RequestID, mutation.Fingerprint, string(encoded)).Error; err != nil {
			return err
		}
		result = mutation.State
		return nil
	})
	if err == nil {
		err = rejection
	}
	return result, runtimeSlotOwnerError(err)
}

func validateNextMutation(current domain.State, mutation app.Mutation) error {
	if mutation.Task != nil {
		command, err := app.DecodeCommand(*mutation.Task)
		if err == nil && command.Action == "reconcile" {
			return nil
		}
	}
	if current.Phase == "unknown" {
		return fmt.Errorf("%w: unknown runtime outcome requires read-only reconciliation", apperrors.ErrConflict)
	}
	if mutation.State.ProjectedTaskID != current.ProjectedTaskID {
		return fmt.Errorf("%w: previous runtime result changed; reload before retrying", apperrors.ErrConflict)
	}
	return nil
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
	// Preserve the authoritative goal and gate while holding the Outpost lock.
	next := current
	next.Generation, next.LatestTaskID, next.Phase, next.Reason = mutation.State.Generation, mutation.Task.ID, "queued", ""
	next.DrainHeld = current.RequiresDrain()
	mutation.State = next
	command.Replicas = current.DesiredReplicas
	mutation.Task.Payload["outpostRuntime"] = command
}

func lockOutpost(ctx context.Context, tx *gorm.DB, id string) error {
	var exists string
	err := tx.WithContext(ctx).Raw(`SELECT id FROM identity_outposts WHERE id = ? FOR UPDATE`, id).Row().Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return apperrors.ErrNotFound
	}
	return err
}

func replay(tx *gorm.DB, mutation app.Mutation) (domain.State, bool, error) {
	var fingerprint string
	var encoded []byte
	err := tx.Raw(`SELECT fingerprint,response FROM identity_outpost_runtime_requests WHERE outpost_id=? AND request_id=?`, mutation.State.OutpostID, mutation.RequestID).Row().Scan(&fingerprint, &encoded)
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

func terminalRuntimeTask(status string) bool {
	switch status {
	case "completed", "failed", "canceled", "callback_timeout":
		return true
	default:
		return false
	}
}

func (r *Repository) unprojectedTask(ctx context.Context, state domain.State) (delivery.ExecutionTask, error) {
	if state.LatestTaskID == "" || state.ProjectedTaskID == state.LatestTaskID {
		return delivery.ExecutionTask{}, nil
	}
	var task delivery.ExecutionTask
	var payload, result []byte
	err := r.db.WithContext(ctx).Raw(`
		SELECT id, task_kind, provider_kind, status, payload, result
		FROM execution_tasks WHERE id = ?
	`, state.LatestTaskID).Row().Scan(&task.ID, &task.TaskKind, &task.ProviderKind, &task.Status, &payload, &result)
	if err != nil {
		return delivery.ExecutionTask{}, fmt.Errorf("read previous Outpost runtime task: %w", err)
	}
	// An unrecognized persisted result is handled as uncertain by projection.
	_ = json.Unmarshal(payload, &task.Payload)
	_ = json.Unmarshal(result, &task.Result)
	return task, nil
}

// settlePreviousTask is called under the parent lock. A non-terminal task
// blocks new mutations; a terminal one is projected before deciding the gate.
func settlePreviousTask(ctx context.Context, tx *gorm.DB, state *domain.State) (active, repaired bool, err error) {
	task, err := New(tx).unprojectedTask(ctx, *state)
	if err != nil || task.ID == "" {
		return false, false, err
	}
	if !terminalRuntimeTask(task.Status) {
		return true, false, nil
	}
	projectTerminalTask(state, task)
	return false, true, save(tx, *state)
}

func taskMatchesState(state domain.State, taskID string, command domain.Command) bool {
	return state.OutpostID == command.OutpostID && state.Generation == command.Generation && state.OwnerEpoch == command.OwnerEpoch &&
		state.LatestTaskID == taskID && state.UID == command.Slot.UID && state.Owner == "soha"
}

func projectTerminalTask(state *domain.State, task delivery.ExecutionTask) {
	state.ProjectedTaskID = task.ID
	command, err := app.DecodeCommand(task)
	receipt, receiptErr := app.DecodeReceipt(task.Result)
	if err != nil || !taskMatchesState(*state, task.ID, command) || receiptErr != nil ||
		(receipt != nil && (receipt.Generation != state.Generation || receipt.OwnerEpoch != state.OwnerEpoch)) {
		applyResult(state, task, command, nil)
		return
	}
	if receipt != nil && receipt.Observation == nil && !receipt.Uncertain {
		applyResult(state, task, command, nil)
		return
	}
	applyResult(state, task, command, receipt)
}

func save(tx *gorm.DB, state domain.State) error {
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO identity_outpost_runtime_deployments(outpost_id,slot_id,generation,state) VALUES(?,?,?,?::jsonb) ON CONFLICT(outpost_id) DO UPDATE SET slot_id=EXCLUDED.slot_id,generation=EXCLUDED.generation,state=EXCLUDED.state,updated_at=now()`, state.OutpostID, state.SlotID, state.Generation, string(encoded)).Error
}

func (r *Repository) RecordResult(ctx context.Context, task delivery.ExecutionTask, command domain.Command) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockOutpost(ctx, tx, command.OutpostID); err != nil {
			return err
		}
		repo := New(tx)
		state, err := repo.readState(ctx, command.OutpostID)
		if err != nil {
			return err
		}
		if !taskMatchesState(state, task.ID, command) {
			return nil
		}
		// A delayed running sink must never overwrite a committed terminal result.
		current, err := repo.unprojectedTask(ctx, state)
		if err != nil || current.ID == "" {
			return err
		}
		if terminalRuntimeTask(current.Status) {
			projectTerminalTask(&state, current)
			return save(tx, state)
		}
		receipt, err := app.DecodeReceipt(current.Result)
		if err != nil {
			return err
		}
		if receipt != nil && (receipt.Generation != state.Generation || receipt.OwnerEpoch != state.OwnerEpoch) {
			return apperrors.ErrConflict
		}
		applyResult(&state, current, command, receipt)
		return save(tx, state)
	})
}

func applyResult(state *domain.State, task delivery.ExecutionTask, command domain.Command, receipt *domain.Receipt) {
	drainHeld := state.RequiresDrain()
	state.Phase = "reconciling"
	if receipt == nil {
		if task.Status != "running" && task.Status != "dispatching" {
			state.Phase, state.Reason = "unknown", "observation_required"
			state.DrainHeld = drainHeld
		}
		return
	}
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

// Only the cross-runtime parent UID fence is translated here. Unrelated
// uniqueness violations retain their original error and diagnostics.
func runtimeSlotOwnerError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" && (pg.ConstraintName == "runtime_slot_owner_conflict" || (pg.ConstraintName == "" && pg.Message == "runtime_slot_owner_conflict")) {
		return fmt.Errorf("%w: runtime_slot_owner_conflict", apperrors.ErrConflict)
	}
	return err
}
