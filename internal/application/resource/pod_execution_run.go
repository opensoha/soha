package resource

import (
	"context"
	"errors"
	"fmt"
	"time"

	contractresource "github.com/opensoha/soha-contracts/resource"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (w *Workloads) runPodExecution(ctx context.Context, principal domainidentity.Principal, access domainidentity.AccessContext, route PodExecutionAgent, record domainresource.PodExecutionRecord) (contractresource.PodExecutionReceipt, error) {
	item := record.Receipt
	_ = w.recordAudit(ctx, principal, item.ClusterID, item.Namespace, "Pod", item.PodName, "exec", "started", "bounded Pod execution attempt admitted")
	w.recordOperation(ctx, principal, "platform.pod.exec", item.ClusterID, item.Namespace, "Pod", item.PodName, "bounded Pod execution attempt admitted", map[string]any{"executionId": item.ID, "status": "running"})
	deadline := item.DeadlineAt
	if !access.ExpiresAt.IsZero() && access.ExpiresAt.Before(deadline) {
		deadline = access.ExpiresAt
	}
	runCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	check := func(checkCtx context.Context) error {
		current, err := w.currentPodExecutionPrincipal(checkCtx, principal, access)
		if err != nil {
			return err
		}
		_, err = w.authorizePodExecutionConnection(checkCtx, current, item.ClusterID, item.Namespace, record.ConnectionHash)
		return err
	}
	var result contractresource.PodArgvResult
	authErr := check(runCtx)
	var executeErr error
	if authErr == nil {
		stop := watchPodExecutionAuthorization(runCtx, cancel, check)
		input := record.Input
		input.Container = item.Container
		result, executeErr = route.ExecutePodArgv(runCtx, item.Namespace, item.PodName, input)
		authErr = stop()
		if authErr == nil && ctx.Err() == nil {
			authErr = check(ctx)
		}
		if executeErr == nil && result.Validate(item.Namespace, item.PodName, input) != nil {
			executeErr = apperrors.ErrClusterUnready
		}
	}
	now := time.Now().UTC()
	record.Receipt.FinishedAt = &now
	record.Receipt.Status = "unknown"
	if authErr == nil && executeErr == nil {
		record.Receipt.Status = result.Output.Status
		record.Receipt.Output, record.Receipt.OutputAvailable = &result.Output, true
	}
	// Losing the HTTP response must not lose the one-attempt fence or result.
	// This bounded persistence context cannot start another provider operation.
	saveCtx, saveCancel := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Second)
	defer saveCancel()
	if err := w.executions.Finish(saveCtx, record); err != nil {
		return contractresource.PodExecutionReceipt{}, fmt.Errorf("%w: execution outcome could not be persisted; query the original key", apperrors.ErrServiceUnavailable)
	}
	_ = w.recordAudit(saveCtx, principal, item.ClusterID, item.Namespace, "Pod", item.PodName, "exec", record.Receipt.Status, "bounded Pod execution attempt finished")
	w.recordOperation(saveCtx, principal, "platform.pod.exec", item.ClusterID, item.Namespace, "Pod", item.PodName, "bounded Pod execution attempt finished", map[string]any{"executionId": item.ID, "status": record.Receipt.Status})
	if authErr != nil {
		return contractresource.PodExecutionReceipt{}, authErr
	}
	if errors.Is(executeErr, apperrors.ErrAccessDenied) || errors.Is(executeErr, apperrors.ErrConflict) || errors.Is(executeErr, apperrors.ErrUnsupportedOperation) {
		return contractresource.PodExecutionReceipt{}, executeErr
	}
	return record.Receipt, nil
}

func watchPodExecutionAuthorization(ctx context.Context, cancelRun context.CancelFunc, check func(context.Context) error) func() error {
	guardCtx, stopGuard := context.WithCancel(ctx)
	finished := make(chan struct{})
	failures := make(chan error, 1)
	go func() {
		defer close(finished)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-guardCtx.Done():
				return
			case <-ticker.C:
				run, cancel := context.WithTimeout(guardCtx, 2*time.Second)
				err := check(run)
				cancel()
				if err != nil && !errors.Is(err, context.Canceled) {
					failures <- apperrors.ErrAccessDenied
					cancelRun()
					return
				}
			}
		}
	}()
	return func() error {
		stopGuard()
		<-finished
		select {
		case err := <-failures:
			return err
		default:
			return nil
		}
	}
}
