package proxydeployment

import (
	"context"
	"encoding/json"
	app "github.com/opensoha/soha/internal/application/proxydeployment"
	"github.com/opensoha/soha/internal/domain/delivery"
	domain "github.com/opensoha/soha/internal/domain/proxydeployment"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
	"reflect"
	"time"
)

func canConfirmStop(state domain.State, now time.Time) bool {
	return state.Owner == "soha-managed" && state.DrainHeld && state.DrainLeasePinned && state.DrainUntil != nil && !state.DrainUntil.After(now) && (state.Phase == "queued" || state.Phase == "reconciling")
}

// stopTaskDeadline reads only the active private task bound to this pool. The
// confirmation window never renews the original authorization or task deadline.
func stopTaskDeadline(tx *gorm.DB, state domain.State, now time.Time) (time.Time, error) {
	var task delivery.ExecutionTask
	var payload []byte
	task.ID = state.LatestTaskID
	if err := tx.Raw(`SELECT task_kind,provider_kind,target_kind,status,payload FROM execution_tasks WHERE id=? FOR SHARE`, state.LatestTaskID).Row().Scan(&task.TaskKind, &task.ProviderKind, &task.TargetKind, &task.Status, &payload); err != nil {
		return time.Time{}, err
	}
	if task.TargetKind != "proxy_slot" || !activeControlTask(task.Status) {
		return time.Time{}, apperrors.ErrConflict
	}
	if err := json.Unmarshal(payload, &task.Payload); err != nil {
		return time.Time{}, err
	}
	command, err := app.DecodeCommand(task)
	if err != nil {
		return time.Time{}, err
	}
	if !sameStopTask(state, command) || !command.Deadline.After(now) {
		return time.Time{}, apperrors.ErrConflict
	}
	return command.Deadline, nil
}
func activeControlTask(status string) bool {
	return status == "queued" || status == "dispatching" || status == "running"
}
func sameStopTask(state domain.State, command domain.Command) bool {
	return state.Slot != nil && reflect.DeepEqual(*state.Slot, command.Slot) && command.ProxyID == state.ProxyID && command.Generation == state.Generation && command.OwnerEpoch == state.OwnerEpoch && command.Replicas == state.DesiredReplicas && command.Slot.ID == state.SlotID && command.Slot.UID == state.UID && command.Slot.MemberSetHash() == state.MemberSetHash && physicalControlAction(command.Action)
}
func beginStopConfirmation(state *domain.State, taskDeadline, certificateUntil, now time.Time) bool {
	if state.StopConfirmUntil != nil {
		return false
	}
	until := now.Add(30 * time.Second)
	if taskDeadline.Before(until) {
		until = taskDeadline
	}
	if certificateUntil.Before(until) {
		until = certificateUntil
	}
	state.StopConfirmUntil = &until
	state.ControlRevision++
	state.Reason = "stop_confirmation_required"
	return true
}
func ensureStopConfirmation(ctx context.Context, tx *gorm.DB, state *domain.State, certificateUntil, now time.Time) error {
	if !canConfirmStop(*state, now) {
		if state.StopConfirmUntil != nil {
			return apperrors.ErrAccessDenied
		}
		return nil
	}
	deadline, err := stopTaskDeadline(tx, *state, now)
	if err != nil {
		return err
	}
	if !beginStopConfirmation(state, deadline, certificateUntil, now) {
		return nil
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	result := tx.WithContext(ctx).Exec(`UPDATE network_proxy_deployments SET state=?::jsonb,updated_at=now() WHERE proxy_id=? AND generation=? AND owner_epoch=?`, string(raw), state.ProxyID, state.Generation, state.OwnerEpoch)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return apperrors.ErrConflict
	}
	return nil
}

func validateStoppedTask(tx *gorm.DB, state domain.State, grant domain.InstanceControl, now time.Time) error {
	if !canConfirmStop(state, now) || state.StopConfirmUntil == nil || !state.StopConfirmUntil.After(now) || grant.ExpiresAt.After(*state.StopConfirmUntil) {
		return apperrors.ErrConflict
	}
	_, err := stopTaskDeadline(tx, state, now)
	return err
}

func physicalControlAction(action string) bool {
	return action == "start" || action == "stop" || action == "scale"
}
