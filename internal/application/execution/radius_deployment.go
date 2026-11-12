package execution

import (
	"context"

	"github.com/opensoha/soha/internal/domain/delivery"
)

// ClaimRadiusDeploymentExecutionTask is reachable only through the dedicated
// installation credential adapter, never generic delivery runner claims.
func (s *Service) ClaimRadiusDeploymentExecutionTask(ctx context.Context, executorID string) (delivery.ExecutionTask, error) {
	return s.claimPrivateExecutionTask(delivery.WithRadiusDeploymentClaim(ctx, executorID), executorID, "radius_deployment.")
}

func (s *Service) GetRadiusDeploymentExecutionTask(ctx context.Context, executorID, taskID string) (delivery.ExecutionTask, error) {
	return s.getPrivateExecutionTask(delivery.WithRadiusDeploymentClaim(ctx, executorID), taskID, delivery.IsRadiusDeploymentTask, delivery.AuthorizeRadiusDeploymentTask)
}

func (s *Service) RecordRadiusDeploymentCallback(ctx context.Context, executorID string, input delivery.ExecutionCallbackInput) (delivery.ExecutionTask, error) {
	return s.RecordCallback(delivery.WithRadiusDeploymentClaim(ctx, executorID), input)
}
