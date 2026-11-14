package radiusdeployment

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	app "github.com/opensoha/soha/internal/application/radiusdeployment"
	"github.com/opensoha/soha/internal/domain/delivery"
	domain "github.com/opensoha/soha/internal/domain/radiusdeployment"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type ExecutionClient interface {
	Claim(context.Context) (delivery.ExecutionTask, error)
	Status(context.Context, string) (delivery.ExecutionTask, error)
	Callback(context.Context, delivery.ExecutionCallbackInput) (delivery.ExecutionTask, error)
}
type ScaleRuntime interface {
	Observe(context.Context, domain.Slot) (domain.Observation, error)
	Scale(context.Context, domain.Slot, domain.Observation, int32) error
}
type Worker struct {
	client   ExecutionClient
	runtime  ScaleRuntime
	executor string
	slots    map[string]domain.Slot
	wait     func(context.Context, time.Duration) bool
}

func NewWorker(client ExecutionClient, runtime ScaleRuntime, executor string, slots []domain.Slot) (*Worker, error) {
	if client == nil || runtime == nil || executor == "" {
		return nil, apperrors.ErrInvalidArgument
	}
	w := &Worker{client: client, runtime: runtime, executor: executor, slots: map[string]domain.Slot{}, wait: wait}
	for _, slot := range slots {
		if domain.ValidateSlot(slot) != nil || slot.ExecutorID != executor {
			return nil, apperrors.ErrInvalidArgument
		}
		w.slots[slot.ID] = slot
	}
	return w, nil
}
func (w *Worker) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		task, err := w.client.Claim(ctx)
		if err == nil {
			w.Execute(ctx, task)
		} else if !errors.Is(err, apperrors.ErrNotFound) {
			return err
		}
		if !w.wait(ctx, time.Second) {
			break
		}
	}
	return ctx.Err()
}
func (w *Worker) Execute(ctx context.Context, task delivery.ExecutionTask) {
	command, err := app.DecodeCommand(task)
	if err != nil {
		return
	}
	slot, ok := w.slots[command.Slot.ID]
	if !ok || slot.Hash() != command.SlotHash || task.ClaimedByAgentID != w.executor {
		return
	}
	ctx, cancel := context.WithDeadline(ctx, command.Deadline)
	defer cancel()
	receipt := domain.Receipt{Revision: command.Revision, OwnerEpoch: command.OwnerEpoch}
	status := "failed"
	defer func() {
		callbackCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		_, _ = w.client.Callback(callbackCtx, delivery.ExecutionCallbackInput{CallbackToken: task.CallbackToken, Status: status, Payload: map[string]any{"radiusDeployment": receipt}})
	}()
	observation, err := w.runtime.Observe(ctx, slot)
	if err != nil {
		receipt.Uncertain = true
		receipt.Reason = "observation_failed"
		return
	}
	receipt.Observation = &observation
	if !w.heartbeat(ctx, task, receipt) {
		receipt.Uncertain = true
		receipt.Reason = "execution_fence_rejected"
		return
	}
	if command.Action != "reconcile" && observation.DesiredReplicas != command.Replicas {
		if err := w.prepareScale(ctx, task, command, observation, &receipt); err != nil {
			receipt.Uncertain = true
			return
		}
		observation = *receipt.Observation
		err = w.runtime.Scale(ctx, slot, observation, command.Replicas)
		readback, readErr := w.runtime.Observe(ctx, slot)
		if readErr != nil {
			receipt.Uncertain = true
			receipt.Reason = "scale_result_unknown"
			return
		}
		receipt.Observation = &readback
		observation = readback
		if err != nil && readback.DesiredReplicas != command.Replicas {
			receipt.Uncertain = true
			receipt.Reason = "scale_result_unknown"
			return
		}
	}
	status = w.converge(ctx, task, command, observation, &receipt)
}
func (w *Worker) heartbeat(ctx context.Context, task delivery.ExecutionTask, receipt domain.Receipt) bool {
	current, err := w.client.Callback(ctx, delivery.ExecutionCallbackInput{CallbackToken: task.CallbackToken, Status: "running", Payload: map[string]any{"radiusDeployment": receipt}})
	return err == nil && (current.Status == "running" || current.Status == "dispatching")
}
func (w *Worker) current(ctx context.Context, task delivery.ExecutionTask) (delivery.ExecutionTask, error) {
	current, err := w.client.Status(ctx, task.ID)
	if err != nil {
		return current, err
	}
	if current.CallbackToken != task.CallbackToken || (current.Status != "running" && current.Status != "dispatching") {
		return current, apperrors.ErrConflict
	}
	return current, nil
}
func drainProof(task delivery.ExecutionTask, expected int) bool {
	var status domain.DrainStatus
	encoded, _ := json.Marshal(task.Result["radiusDeploymentDrain"])
	if json.Unmarshal(encoded, &status) != nil {
		return false
	}
	return status.CountKnown && status.ActiveStates != nil && *status.ActiveStates == 0 && !status.AcceptingNew && status.ExpectedMembers == expected && status.ConfirmedMembers == expected
}
func (w *Worker) prepareScale(ctx context.Context, task delivery.ExecutionTask, command domain.Command, initial domain.Observation, receipt *domain.Receipt) error {
	retiring := max(initial.Replicas, initial.DesiredReplicas) - command.Replicas
	if retiring > 0 {
		for {
			current, err := w.current(ctx, task)
			if err != nil {
				return err
			}
			if drainProof(current, int(retiring)) {
				break
			}
			if !w.wait(ctx, time.Second) {
				receipt.Reason = "drain_not_confirmed"
				return context.Canceled
			}
		}
	}
	fresh, err := w.runtime.Observe(ctx, command.Slot)
	if err != nil {
		receipt.Reason = "observation_failed"
		return err
	}
	if fresh.UID != initial.UID || fresh.TemplateDigest != initial.TemplateDigest || fresh.DesiredReplicas != initial.DesiredReplicas || fresh.Replicas != initial.Replicas {
		receipt.Reason = "scale_intent_changed"
		return apperrors.ErrConflict
	}
	current, err := w.current(ctx, task)
	if err != nil {
		return err
	}
	if retiring > 0 && !drainProof(current, int(retiring)) {
		receipt.Reason = "drain_proof_changed"
		return apperrors.ErrConflict
	}
	if !command.Deadline.After(time.Now()) {
		return context.DeadlineExceeded
	}
	receipt.Observation = &fresh
	receipt.Drained = retiring > 0
	if receipt.Drained && !w.heartbeat(ctx, task, *receipt) {
		return apperrors.ErrConflict
	}
	return nil
}
func (w *Worker) converge(ctx context.Context, task delivery.ExecutionTask, command domain.Command, observed domain.Observation, receipt *domain.Receipt) string {
	for {
		receipt.Observation = &observed
		if !w.heartbeat(ctx, task, *receipt) {
			receipt.Uncertain = true
			receipt.Reason = "execution_fence_rejected"
			return "failed"
		}
		current, err := w.current(ctx, task)
		if err != nil {
			receipt.Uncertain = true
			return "failed"
		}
		var native domain.DrainStatus
		encoded, _ := json.Marshal(current.Result["radiusDeploymentDrain"])
		_ = json.Unmarshal(encoded, &native)
		if observed.DesiredReplicas == command.Replicas && observed.Replicas == command.Replicas && (native.DrainProven || native.ExpectedMembers == 0) && (command.Replicas == 0 || observed.ReadyReplicas == command.Replicas && native.Ready) {
			return "completed"
		}
		if command.Action == "reconcile" && observed.DesiredReplicas != command.Replicas {
			receipt.Uncertain = true
			receipt.Reason = "observed_state_mismatch"
			return "failed"
		}
		if !w.wait(ctx, time.Second) {
			receipt.Uncertain = true
			receipt.Reason = "convergence_timeout"
			return "failed"
		}
		observed, err = w.runtime.Observe(ctx, command.Slot)
		if err != nil {
			receipt.Uncertain = true
			receipt.Reason = "observation_failed"
			return "failed"
		}
	}
}
func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
