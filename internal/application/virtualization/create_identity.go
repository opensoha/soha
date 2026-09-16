package virtualization

import (
	"context"
	"errors"
	"fmt"
	"time"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) createOrObserveVM(ctx context.Context, task domain.Task, connection domain.Connection, providerConnection domain.AdapterConnection, adapter Adapter) (domain.AdapterVM, domain.Task, error) {
	if isWorkerCreation(task) && boolValue(task.Payload, "providerDispatchStarted") && workerBootstrapClosed(task) {
		observer, ok := adapter.(domain.VMCreationObserver)
		if !ok {
			return domain.AdapterVM{}, task, apperrors.ErrUnsupportedOperation
		}
		input := adapterCreateVMInput(task.Payload)
		input.OperationID = task.ID
		vm, found, err := observer.ObserveVMCreation(ctx, providerConnection, input)
		if err != nil {
			return vm, task, err
		}
		if !found {
			return vm, task, fmt.Errorf("%w: original VM remains unconfirmed after worker bootstrap closed", apperrors.ErrConflict)
		}
		return vm, task, nil
	}
	input, checkpointed, err := s.prepareVMDispatch(ctx, task, connection, providerConnection, adapter)
	if checkpointed.ID != "" {
		task = checkpointed
	}
	if err != nil {
		return domain.AdapterVM{}, task, err
	}
	vm, err := adapter.CreateVM(ctx, providerConnection, input)
	if err != nil && input.CloudInit != "" {
		err = fmt.Errorf("VM provider request with protected bootstrap content failed; inspect the authorized provider operation")
	}
	return vm, task, err
}

func workerBootstrapClosed(task domain.Task) bool {
	expires, err := time.Parse(time.RFC3339Nano, payloadString(task.Payload, "workerBootstrapExpiresAt"))
	return err != nil || !expires.After(time.Now()) || boolValue(task.Result, "workerBootstrapRevoked")
}

func (s *Service) prepareVMDispatch(ctx context.Context, task domain.Task, connection domain.Connection, providerConnection domain.AdapterConnection, adapter Adapter) (domain.AdapterCreateVMInput, domain.Task, error) {
	var err error
	task, err = s.prepareWorkerBootstrapTask(ctx, task)
	if err != nil {
		return domain.AdapterCreateVMInput{}, task, err
	}
	for attempt := 0; ; attempt++ {
		input := adapterCreateVMInput(task.Payload)
		input.OperationID = task.ID
		if isWorkerCreation(task) {
			input.WorkerSystemUUID = task.ID
		}
		if preparer, ok := adapter.(domain.VMCreatePreparer); ok && !boolValue(task.Payload, "providerIdentityPrepared") {
			prepared, err := preparer.PrepareVMCreate(ctx, providerConnection, input)
			if err != nil {
				return input, task, err
			}
			input = prepared
		}
		var err error
		input.CloudInit, err = s.vmBootstrapContent(task.Payload)
		if err != nil {
			return input, task, err
		}
		if err := s.authorizeQueuedVMTask(ctx, task); err != nil {
			return input, task, err
		}
		checkpointed, err := s.checkpointVMCreate(ctx, task, input, vmCreateConnectionIdentity(connection))
		if !errors.Is(err, domain.ErrProviderIdentityClaimed) || !boolValue(task.Payload, "providerIdentityDynamic") || boolValue(task.Payload, "providerIdentityPrepared") || attempt >= 4 {
			return input, checkpointed, err
		}
		// GET nextid is advisory. Another Soha worker may have reserved that free ID
		// before its first provider request. Re-read only; a frozen ID never changes.
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return input, task, ctx.Err()
		case <-timer.C:
		}
	}
}
