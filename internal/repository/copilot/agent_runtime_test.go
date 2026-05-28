package copilot

import (
	"testing"

	domaincopilot "github.com/soha/soha/internal/domain/copilot"
)

func TestMergeAgentRunToolExecutionsAppendsAndReplacesByID(t *testing.T) {
	current := []domaincopilot.ToolExecution{{
		ID:       "tool:events",
		ToolName: "events.query",
		Status:   "success",
	}, {
		ID:       "tool:stale",
		ToolName: "logs.query",
		Status:   "running",
	}}
	patch := []domaincopilot.ToolExecution{{
		ID:       "tool:stale",
		ToolName: "logs.query",
		Status:   "failed",
	}, {
		ID:       "tool:agent-analysis",
		ToolName: "hermes.analysis",
		Status:   "completed",
	}}

	merged := mergeAgentRunToolExecutions(current, patch)

	if len(merged) != 3 {
		t.Fatalf("expected 3 merged executions, got %#v", merged)
	}
	if merged[0].ID != "tool:events" || merged[1].ID != "tool:stale" || merged[1].Status != "failed" || merged[2].ID != "tool:agent-analysis" {
		t.Fatalf("unexpected merge result: %#v", merged)
	}
}
