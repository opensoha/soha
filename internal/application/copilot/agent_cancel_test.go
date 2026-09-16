package copilot

import (
	"context"
	"errors"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	"testing"
)

type cancellationTestRepository struct {
	*specialistTestRepository
	childReads int
}

func (r *cancellationTestRepository) GetAgentRun(ctx context.Context, owner, id string) (domaincopilot.AgentRun, error) {
	run, err := r.specialistTestRepository.GetAgentRun(ctx, owner, id)
	if id == r.child.ID {
		r.childReads++
		run.Output = map[string]any{"cancellationPending": r.childReads < 2}
	} else {
		run.Output = map[string]any{"cancellationPending": false}
	}
	return run, err
}
func TestCancellationWaitsForParentAndChildAcknowledgements(t *testing.T) {
	parent := domaincopilot.AgentRun{ID: "parent", CreatedBy: "owner", Status: "canceled", Output: map[string]any{"cancellationPending": true}, ToolExecutions: []domaincopilot.ToolExecution{{ToolName: "agent.delegate"}}}
	repo := &cancellationTestRepository{specialistTestRepository: &specialistTestRepository{parent: parent, child: domaincopilot.AgentRun{ID: "parent:specialist", Status: "canceled"}}}
	service := &Service{agentRuns: repo}
	got, err := service.waitForAgentCancellation(context.Background(), parent)
	if err != nil || got.Output["cancellationPending"] != false || repo.childReads < 2 {
		t.Fatalf("result=%+v reads=%d error=%v", got, repo.childReads, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = service.waitForAgentCancellation(ctx, parent)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("context cancellation: %v", err)
	}
}
