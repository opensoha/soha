package runner

import "context"

func (r *Runner) acknowledgeAgentCancellation(ctx context.Context, run AgentRun, status string) {
	if status == "canceled" || status == "cancelled" {
		r.agentRunCallback(ctx, run, "canceled", map[string]any{"cancellationAcknowledged": true}, nil, nil, "", "")
	}
}
