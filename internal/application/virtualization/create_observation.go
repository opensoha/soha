package virtualization

import (
	"context"
	"time"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
)

// The existing VM worker observes canceled creation attempts; this is not a
// second executor. Only the original task can authorize provider writes.
func (s *Service) reconcileCanceledCreations(ctx context.Context) {
	before := time.Now().UTC().Add(-30 * time.Second)
	tasks, err := s.tasks.ListTasks(ctx, domain.TaskFilter{Status: TaskStatusCanceling, TaskKind: TaskKindVMCreate, ReconcileBefore: &before, Limit: 20})
	if err != nil {
		return
	}
	for _, task := range tasks {
		if ctx.Err() != nil {
			return
		}
		s.observeCanceledCreation(ctx, task)
	}
}

func creationAttemptStopped(task domain.Task, now time.Time) bool {
	if boolValue(task.Result, "providerAttemptFinished") {
		return true
	}
	deadline, err := time.Parse(time.RFC3339Nano, payloadString(task.Payload, "providerAttemptDeadline"))
	return err == nil && now.After(deadline.Add(time.Second))
}

func (s *Service) observeCanceledCreation(ctx context.Context, task domain.Task) {
	if task.Status != TaskStatusCanceling {
		return
	}
	if isWorkerCreation(task) && !boolValue(task.Payload, "providerDispatchStarted") {
		deadline, err := time.Parse(time.RFC3339Nano, payloadString(task.Payload, "workerBootstrapDeadline"))
		if err == nil && time.Now().After(deadline.Add(time.Second)) {
			s.finishWorkerBootstrap(ctx, task)
			return
		}
	}
	if !boolValue(task.Payload, "providerIdentityPrepared") || !creationAttemptStopped(task, time.Now()) {
		s.recordCreationObservation(ctx, task, nil)
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	connection, vm, found, err := s.readCreatedVM(ctx, task)
	persistCtx, persistCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer persistCancel()
	if err != nil || !found {
		// Keep unknown outcomes reserved. Persist the observation time even on
		// errors so a permanently unavailable provider cannot starve other tasks.
		s.recordCreationObservation(persistCtx, task, err)
		return
	}
	record, ips, endpoint := createdVMRecord(connection, task.Payload, vm)
	stored, err := s.vms.UpsertVM(persistCtx, record)
	if err != nil {
		return
	}
	populateVMCreateTaskResult(&task, stored, vm, ips, endpoint)
	task.Result["providerObservedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
	task.Result["message"] = "creation stopped; the original provider VM is retained"
	_ = s.finishVMCreate(persistCtx, task)
	s.finishWorkerBootstrap(persistCtx, task)
}

func (s *Service) recordCreationObservation(ctx context.Context, task domain.Task, err error) {
	task.Result = mergeMaps(task.Result, map[string]any{"providerObservedAt": time.Now().UTC().Format(time.RFC3339Nano)})
	if err != nil {
		task.Result["providerObservationError"] = err.Error()
	}
	_, _ = s.tasks.UpdateTask(ctx, task)
}

func (s *Service) readCreatedVM(ctx context.Context, task domain.Task) (domain.Connection, domain.AdapterVM, bool, error) {
	connection, err := s.connections.GetConnection(ctx, task.ConnectionID)
	if err != nil {
		return connection, domain.AdapterVM{}, false, err
	}
	if err := checkVMCreateConnection(task, connection); err != nil {
		return connection, domain.AdapterVM{}, false, err
	}
	adapter, providerConnection, err := s.adapterForConnection(ctx, connection)
	if err != nil {
		return connection, domain.AdapterVM{}, false, err
	}
	observer, ok := adapter.(domain.VMCreationObserver)
	if !ok {
		return connection, domain.AdapterVM{}, false, domain.ErrUnsupported
	}
	input := adapterCreateVMInput(task.Payload)
	input.OperationID = task.ID
	vm, found, err := observer.ObserveVMCreation(ctx, providerConnection, input)
	return connection, vm, found, err
}
