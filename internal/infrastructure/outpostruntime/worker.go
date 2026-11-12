package outpostruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	app "github.com/opensoha/soha/internal/application/outpostruntime"
	"github.com/opensoha/soha/internal/domain/delivery"
	domain "github.com/opensoha/soha/internal/domain/outpostruntime"
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
	client     ExecutionClient
	runtime    ScaleRuntime
	executorID string
	slots      map[string]domain.Slot
}

func NewWorker(client ExecutionClient, runtime ScaleRuntime, executorID string, slots []domain.Slot) (*Worker, error) {
	if client == nil || runtime == nil || executorID == "" {
		return nil, fmt.Errorf("Outpost worker dependencies are required")
	}
	w := &Worker{client: client, runtime: runtime, executorID: executorID, slots: map[string]domain.Slot{}}
	for _, slot := range slots {
		if err := validateSlot(slot); err != nil {
			return nil, err
		}
		if slot.ExecutorID != executorID || slot.MaxReplicas < 1 {
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
		}
		if err != nil && !errors.Is(err, apperrors.ErrNotFound) {
			return err
		}
		if !wait(ctx, time.Second) {
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
	if !ok || slot != command.Slot || task.ClaimedByAgentID != w.executorID {
		return
	}
	receipt := domain.Receipt{Generation: command.Generation, OwnerEpoch: command.OwnerEpoch}
	// The durable command deadline includes time spent queued. Kubernetes calls
	// must inherit it, rather than acquiring another 90 seconds after claim.
	ctx, cancel := context.WithDeadline(ctx, command.Deadline)
	defer cancel()
	status := "failed"
	defer func() {
		callbackCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		_, _ = w.client.Callback(callbackCtx, delivery.ExecutionCallbackInput{CallbackToken: task.CallbackToken, Status: status, Payload: map[string]any{"outpostRuntime": receipt}})
	}()
	if command.Action == "reconcile" {
		status = w.reconcile(ctx, task, command, &receipt)
		return
	}
	observation, err := w.runtime.Observe(ctx, slot)
	if err != nil {
		receipt.Uncertain, receipt.Reason = true, "observation_failed"
		return
	}
	receipt.Observation = &observation
	if !w.heartbeat(ctx, task, receipt) {
		receipt.Reason = "execution_fence_rejected"
		return
	}
	if command.Replicas == 0 || command.Replicas < observation.Replicas {
		if err = w.drain(ctx, task, command, observation.Replicas); err != nil {
			receipt.Reason = "drain_timeout"
			return
		}
		receipt.Drained = true
	}
	if _, err = w.current(ctx, task, command); err != nil {
		receipt.Reason = "execution_fence_rejected"
		return
	}
	if !time.Now().Before(command.Deadline) {
		receipt.Reason = "operation_expired"
		return
	}
	if observation.DesiredReplicas != command.Replicas {
		err = w.runtime.Scale(ctx, slot, observation, command.Replicas)
		// No automatic rewrite: even a transport failure may have committed.
		readback, readErr := w.runtime.Observe(ctx, slot)
		if readErr != nil {
			receipt.Uncertain, receipt.Reason = true, "scale_result_unknown"
			return
		}
		observation = readback
		receipt.Observation = &observation
		if err != nil && observation.DesiredReplicas != command.Replicas {
			receipt.Uncertain, receipt.Reason = true, "scale_result_unknown"
			return
		}
	}
	status = w.waitForConvergence(ctx, task, command, observation, &receipt)
}

func (w *Worker) waitForConvergence(ctx context.Context, task delivery.ExecutionTask, command domain.Command, observation domain.Observation, receipt *domain.Receipt) string {
	for {
		if !w.heartbeat(ctx, task, *receipt) {
			receipt.Reason = "execution_fence_rejected"
			return "failed"
		}
		if observation.DesiredReplicas == command.Replicas && observation.Replicas == command.Replicas && observation.ReadyReplicas == command.Replicas {
			return "completed"
		}
		if _, err := w.current(ctx, task, command); err != nil {
			receipt.Reason = "execution_fence_rejected"
			return "failed"
		}
		if !wait(ctx, time.Second) {
			receipt.Uncertain, receipt.Reason = true, "observation_timeout"
			return "failed"
		}
		var err error
		observation, err = w.runtime.Observe(ctx, command.Slot)
		if err != nil {
			receipt.Uncertain, receipt.Reason = true, "observation_failed"
			return "failed"
		}
		receipt.Observation = &observation
	}
}

func (w *Worker) reconcile(ctx context.Context, task delivery.ExecutionTask, command domain.Command, receipt *domain.Receipt) string {
	if _, err := w.current(ctx, task, command); err != nil {
		receipt.Uncertain, receipt.Reason = true, "execution_fence_rejected"
		return "failed"
	}
	observation, err := w.runtime.Observe(ctx, command.Slot)
	if err != nil {
		receipt.Uncertain, receipt.Reason = true, "observation_failed"
		return "failed"
	}
	receipt.Observation = &observation
	if observation.DesiredReplicas != command.Replicas || observation.Replicas != command.Replicas || observation.ReadyReplicas != command.Replicas {
		receipt.Reason = "observed_state_mismatch"
		return "failed"
	}
	return "completed"
}

func (w *Worker) heartbeat(ctx context.Context, task delivery.ExecutionTask, receipt domain.Receipt) bool {
	current, err := w.client.Callback(ctx, delivery.ExecutionCallbackInput{CallbackToken: task.CallbackToken, Status: "running", Payload: map[string]any{"outpostRuntime": receipt}})
	return err == nil && (current.Status == "running" || current.Status == "dispatching")
}

func (w *Worker) current(ctx context.Context, task delivery.ExecutionTask, command domain.Command) (delivery.ExecutionTask, error) {
	current, err := w.client.Status(ctx, task.ID)
	if err != nil {
		return current, err
	}
	if current.CallbackToken != task.CallbackToken || (current.Status != "running" && current.Status != "dispatching") {
		return current, apperrors.ErrConflict
	}
	if !time.Now().Before(command.Deadline) {
		return current, context.DeadlineExceeded
	}
	return current, nil
}

func (w *Worker) drain(ctx context.Context, task delivery.ExecutionTask, command domain.Command, replicas int32) error {
	deadline := time.Now().Add(time.Duration(command.DrainTimeoutSeconds) * time.Second)
	if command.Deadline.Before(deadline) {
		deadline = command.Deadline
	}
	for time.Now().Before(deadline) {
		current, err := w.current(ctx, task, command)
		if err != nil {
			return err
		}
		var drain domain.DrainStatus
		encoded, _ := json.Marshal(current.Result["outpostRuntimeDrain"])
		if json.Unmarshal(encoded, &drain) == nil && drain.Instances >= int(replicas) && !drain.AcceptingRequests && drain.ActiveChecks == 0 {
			return nil
		}
		if !wait(ctx, time.Second) {
			return ctx.Err()
		}
	}
	return context.DeadlineExceeded
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
