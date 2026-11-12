package proxydeployment

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	app "github.com/opensoha/soha/internal/application/proxydeployment"
	"github.com/opensoha/soha/internal/domain/delivery"
	domain "github.com/opensoha/soha/internal/domain/proxydeployment"
	"gorm.io/gorm"
)

func (r *Repository) GetForMember(ctx context.Context, runtimeID, instanceID string) (domain.State, error) {
	id, err := memberPool(r.db.WithContext(ctx), runtimeID, instanceID)
	if err != nil {
		return domain.State{}, err
	}
	return r.Get(ctx, id)
}

// validateMutationPin executes after the pool lock and generation CAS. Rebind
// proof cannot be replaced by a pre-transaction observation or terminal failure.
func validateMutationPin(tx *gorm.DB, current domain.State, mutation app.Mutation) error {
	if mutation.Task != nil {
		command, err := app.DecodeCommand(*mutation.Task)
		if err != nil {
			return err
		}
		hash, err := app.SlotPolicyHash(command.Slot)
		if err != nil {
			return err
		}
		if err = app.RequireAcceptedPolicyHash(current, hash); err != nil {
			return err
		}
		if mutation.State.Slot == nil || !app.SameSlotPolicy(*current.Slot, *mutation.State.Slot) {
			return app.RebindRequired()
		}
		return nil
	}
	if mutation.State.Slot == nil {
		return app.RebindRequired()
	}
	if err := app.ValidateBindingPin(current, *mutation.State.Slot); err != nil {
		return err
	}
	if current.Slot == nil || app.SameSlotPolicy(*current.Slot, *mutation.State.Slot) {
		return nil
	}
	return requireSuccessfulStop(tx, current)
}
func requireSuccessfulStop(tx *gorm.DB, state domain.State) error {
	if !app.StoppedForRebind(state) {
		return app.RebindRequired()
	}
	var task delivery.ExecutionTask
	var payload []byte
	err := tx.Raw(`SELECT task_kind,provider_kind,target_kind,status,payload FROM execution_tasks WHERE id=? FOR SHARE`, state.LatestTaskID).Row().Scan(&task.TaskKind, &task.ProviderKind, &task.TargetKind, &task.Status, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return app.RebindRequired()
	}
	if err != nil {
		return err
	}
	if task.Status != "completed" || task.TargetKind != "proxy_slot" {
		return app.RebindRequired()
	}
	if err = json.Unmarshal(payload, &task.Payload); err != nil {
		return app.RebindRequired()
	}
	command, err := app.DecodeCommand(task)
	if err != nil {
		return app.RebindRequired()
	}
	if command.Action != "stop" || command.Replicas != 0 || command.ProxyID != state.ProxyID || command.Generation != state.Generation || command.OwnerEpoch != state.OwnerEpoch || !app.SameSlotPolicy(command.Slot, *state.Slot) {
		return app.RebindRequired()
	}
	return nil
}

func memberWithinTarget(slot domain.Slot, runtimeID, instance string, target int32) bool {
	for _, m := range slot.Members {
		if m.RuntimeID == runtimeID && m.InstanceID == instance {
			return m.Ordinal < target
		}
	}
	return false
}
