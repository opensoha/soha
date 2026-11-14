package execution

import (
	"context"

	"github.com/opensoha/soha/internal/domain/delivery"
)

// ClaimOutpostRuntimeExecutionTask is reachable only through the dedicated
// installation credential adapter, never generic delivery runner claims.
func (s *Service) ClaimOutpostRuntimeExecutionTask(ctx context.Context, executorID string) (delivery.ExecutionTask, error) {
	return s.claimPrivateExecutionTask(delivery.WithOutpostRuntimeClaim(ctx, executorID), executorID, "outpost_runtime.")
}

func (s *Service) GetOutpostRuntimeExecutionTask(ctx context.Context, executorID, taskID string) (delivery.ExecutionTask, error) {
	return s.getPrivateExecutionTask(delivery.WithOutpostRuntimeClaim(ctx, executorID), taskID, delivery.IsOutpostRuntimeTask, delivery.AuthorizeOutpostRuntimeTask)
}

func (s *Service) RecordOutpostRuntimeCallback(ctx context.Context, executorID string, input delivery.ExecutionCallbackInput) (delivery.ExecutionTask, error) {
	return s.RecordCallback(delivery.WithOutpostRuntimeClaim(ctx, executorID), input)
}
