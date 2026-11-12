package proxydeployment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	app "github.com/opensoha/soha/internal/application/proxydeployment"
	"github.com/opensoha/soha/internal/domain/delivery"
	domain "github.com/opensoha/soha/internal/domain/proxydeployment"
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
	pollWait   func(context.Context, time.Duration) bool
}

func NewWorker(client ExecutionClient, runtime ScaleRuntime, executorID string, slots []domain.Slot) (*Worker, error) {
	if client == nil || runtime == nil || executorID == "" {
		return nil, fmt.Errorf("Proxy worker dependencies are required")
	}
	w := &Worker{client: client, runtime: runtime, executorID: executorID, pollWait: wait, slots: map[string]domain.Slot{}}
	for _, slot := range slots {
		if err := domain.ValidatePolicy(slot); err != nil {
			return nil, err
		}
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
	if !ok || !app.SameSlotPolicy(slot, command.Slot) || task.ClaimedByAgentID != w.executorID {
		return
	}
	receipt := domain.Receipt{Generation: command.Generation, OwnerEpoch: command.OwnerEpoch}
	// The durable command deadline includes time spent queued. Kubernetes calls
	// must inherit it, rather than acquiring a fresh timeout after claim.
	ctx, cancel := context.WithDeadline(ctx, command.Deadline)
	defer cancel()
	status := "failed"
	defer func() {
		callbackCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		_, _ = w.client.Callback(callbackCtx, delivery.ExecutionCallbackInput{CallbackToken: task.CallbackToken, Status: status, Payload: map[string]any{"proxyDeployment": receipt}})
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
	if err = w.prepareScale(ctx, task, command, observation, &receipt); err != nil {
		return
	}
	observation = *receipt.Observation
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
		// Persist convergence and resume together so no intermediate callback
		// replaces the retiring range while still withholding survivor service.
		if observation.DesiredReplicas == command.Replicas && observation.Replicas == command.Replicas {
			if receipt.Drained && command.Replicas > 0 && !receipt.Resume {
				receipt.Resume = true
			}
		}
		if !w.heartbeat(ctx, task, *receipt) {
			receipt.Reason = "execution_fence_rejected"
			return "failed"
		}
		// One fresh fence/readiness snapshot per poll; never reuse this for writes.
		current, err := w.current(ctx, task, command)
		if err != nil {
			receipt.Reason = "execution_fence_rejected"
			return "failed"
		}
		if observation.DesiredReplicas == command.Replicas && observation.Replicas == command.Replicas {
			var runtime domain.DrainStatus
			raw, _ := json.Marshal(current.Result["proxyDeploymentDrain"])
			_ = json.Unmarshal(raw, &runtime)
			if command.Replicas == 0 || (observation.ReadyReplicas == command.Replicas && runtime.Instances >= int(command.Replicas) && runtime.Ready) {
				return "completed"
			}
		}
		if !w.pollWait(ctx, 2*time.Second) {
			receipt.Uncertain, receipt.Reason = true, "observation_timeout"
			return "failed"
		}
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
	current, err := w.client.Callback(ctx, delivery.ExecutionCallbackInput{CallbackToken: task.CallbackToken, Status: "running", Payload: map[string]any{"proxyDeployment": receipt}})
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
	for time.Now().Before(command.Deadline) {
		current, err := w.current(ctx, task, command)
		if err != nil {
			return err
		}
		var status domain.DrainStatus
		encoded, _ := json.Marshal(current.Result["proxyDeploymentDrain"])
		if json.Unmarshal(encoded, &status) == nil {
			if drainProof(status, replicas, command.DrainUntil, time.Now()) {
				return nil
			}
			if status.StopRequired && status.ConfirmUntil != nil && !status.ConfirmUntil.After(time.Now()) {
				return context.DeadlineExceeded
			}
		}
		if !wait(ctx, time.Second) {
			return ctx.Err()
		}
	}
	return context.DeadlineExceeded
}
func drainProof(status domain.DrainStatus, replicas int32, drainUntil, now time.Time) bool {
	if !status.CountKnown || status.Instances != int(replicas) || status.AcceptingRequests || status.ActiveChecks != 0 {
		return false
	}
	if status.StopRequired {
		return status.ConfirmUntil != nil && status.ConfirmUntil.After(now)
	}
	return !drainUntil.IsZero() && drainUntil.After(now)
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

func taskDrainProof(task delivery.ExecutionTask, replicas int32, until, now time.Time) bool {
	var status domain.DrainStatus
	raw, err := json.Marshal(task.Result["proxyDeploymentDrain"])
	if err != nil || json.Unmarshal(raw, &status) != nil {
		return false
	}
	return drainProof(status, replicas, until, now)
}

func (w *Worker) prepareScale(ctx context.Context, task delivery.ExecutionTask, command domain.Command, observation domain.Observation, receipt *domain.Receipt) error {
	removed, valid := domain.RetiringRange(command.Replicas, observation.Replicas, observation.DesiredReplicas, command.Slot.MaxReplicas)
	if !valid {
		return apperrors.ErrConflict
	}
	requiredDrainReplicas := removed.Count()
	if !w.heartbeat(ctx, task, *receipt) {
		receipt.Reason = "execution_fence_rejected"
		return apperrors.ErrConflict
	}
	if command.Replicas == 0 || command.Replicas < max(observation.Replicas, observation.DesiredReplicas) {
		if requiredDrainReplicas > 0 {
			if err := w.drain(ctx, task, command, requiredDrainReplicas); err != nil {
				receipt.Reason = "drain_timeout"
				return err
			}
		}
		receipt.Drained = true
	}
	if observation.DesiredReplicas != command.Replicas {
		if err := w.refreshScaleObservation(ctx, command, observation, receipt); err != nil {
			return err
		}
	}
	current, err := w.current(ctx, task, command)
	if err != nil {
		receipt.Reason = "execution_fence_rejected"
		return err
	}
	if receipt.Drained && requiredDrainReplicas > 0 && !taskDrainProof(current, requiredDrainReplicas, command.DrainUntil, time.Now()) {
		receipt.Drained = false
		receipt.Reason = "drain_proof_changed"
		return apperrors.ErrConflict
	}
	if !time.Now().Before(command.Deadline) {
		receipt.Reason = "operation_expired"
		return context.DeadlineExceeded
	}
	return nil
}

func (w *Worker) refreshScaleObservation(ctx context.Context, command domain.Command, initial domain.Observation, receipt *domain.Receipt) error {
	// Drain can change workload status/RV. Refresh before the final task/proof
	// fence, while keeping the original scale intent and the adapter's strict CAS.
	current, err := w.runtime.Observe(ctx, command.Slot)
	if err != nil {
		receipt.Uncertain, receipt.Reason = true, "observation_failed"
		return err
	}
	receipt.Observation = &current
	if !sameScaleIntent(command.Slot, initial, current) {
		receipt.Reason = "observed_state_mismatch"
		return apperrors.ErrConflict
	}
	return nil
}

func sameScaleIntent(slot domain.Slot, initial, current domain.Observation) bool {
	return current.UID == slot.UID && current.TemplateDigest == slot.TemplateDigest &&
		current.UID == initial.UID && current.TemplateDigest == initial.TemplateDigest &&
		current.ResourceVersion != "" && current.DesiredReplicas == initial.DesiredReplicas && current.Replicas == initial.Replicas
}
