package execution

import (
	"context"

	"github.com/opensoha/soha/internal/domain/delivery"
)

// ClaimProxyDeploymentExecutionTask is reachable only through the dedicated
// installation credential adapter, never generic delivery runner claims.
func (s *Service) ClaimProxyDeploymentExecutionTask(ctx context.Context, executorID string) (delivery.ExecutionTask, error) {
	return s.claimPrivateExecutionTask(delivery.WithProxyDeploymentClaim(ctx, executorID), executorID, "proxy_deployment.")
}

func (s *Service) GetProxyDeploymentExecutionTask(ctx context.Context, executorID, taskID string) (delivery.ExecutionTask, error) {
	return s.getPrivateExecutionTask(delivery.WithProxyDeploymentClaim(ctx, executorID), taskID, delivery.IsProxyDeploymentTask, delivery.AuthorizeProxyDeploymentTask)
}

func (s *Service) RecordProxyDeploymentCallback(ctx context.Context, executorID string, input delivery.ExecutionCallbackInput) (delivery.ExecutionTask, error) {
	return s.RecordCallback(delivery.WithProxyDeploymentClaim(ctx, executorID), input)
}
