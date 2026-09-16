package copilot

import (
	"context"
	"errors"
	"fmt"
	"time"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) waitForAgentCancellation(ctx context.Context, run domaincopilot.AgentRun) (domaincopilot.AgentRun, error) {
	childID := ""
	if run.ParentRunID == "" {
		for _, tool := range run.ToolExecutions {
			if tool.ToolName == "agent.delegate" {
				childID = run.ID + ":specialist"
				break
			}
		}
	}
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		pending := run.Status == domaincopilot.AgentRunStatusCanceled && run.Output["cancellationPending"] == true
		if childID != "" {
			child, err := s.agentRuns.GetAgentRun(ctx, run.CreatedBy, childID)
			if err != nil && !errors.Is(err, apperrors.ErrNotFound) {
				return run, err
			}
			pending = pending || (child.Status == domaincopilot.AgentRunStatusCanceled && child.Output["cancellationPending"] == true)
		}
		if !pending {
			return run, nil
		}
		select {
		case <-ctx.Done():
			return run, ctx.Err()
		case <-timer.C:
			return run, fmt.Errorf("%w: cancellation requested; waiting for runner confirmation", apperrors.ErrClusterUnready)
		case <-ticker.C:
			next, err := s.agentRuns.GetAgentRun(ctx, run.CreatedBy, run.ID)
			if err != nil {
				return run, err
			}
			run = next
		}
	}
}
