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

func TestMergeAgentRunAnalysisArtifactsAppendsAndReplacesByStableKey(t *testing.T) {
	current := []domaincopilot.AnalysisArtifact{{
		Kind:    "root_cause",
		RunID:   "agent:run-1",
		Title:   "Root cause",
		Summary: "old root cause",
	}, {
		Kind:    "trace",
		RunID:   "agent:run-1",
		Title:   "Trace",
		Summary: "trace summary",
	}, {
		Kind:    "inspection_review",
		Title:   "Nightly review",
		Summary: "old review",
	}}
	patch := []domaincopilot.AnalysisArtifact{{
		Kind:    "root_cause",
		RunID:   "agent:run-1",
		Title:   "Root cause",
		Summary: "updated root cause",
	}, {
		Kind:    "root_cause",
		RunID:   "agent:run-1",
		Title:   "Mitigation plan",
		Summary: "new mitigation",
	}, {
		Kind:    "inspection_review",
		Title:   "Nightly review",
		Summary: "updated review",
	}}

	merged := mergeAgentRunAnalysisArtifacts(current, patch)

	if len(merged) != 4 {
		t.Fatalf("expected 4 merged artifacts, got %#v", merged)
	}
	if merged[0].Kind != "root_cause" || merged[0].Summary != "updated root cause" {
		t.Fatalf("expected first artifact to be replaced, got %#v", merged)
	}
	if merged[1].Kind != "trace" || merged[1].Summary != "trace summary" {
		t.Fatalf("expected trace artifact to remain in place, got %#v", merged)
	}
	if merged[2].Kind != "inspection_review" || merged[2].Summary != "updated review" {
		t.Fatalf("expected no-run artifact to be replaced by kind/title, got %#v", merged)
	}
	if merged[3].Title != "Mitigation plan" || merged[3].Summary != "new mitigation" {
		t.Fatalf("expected new artifact to be appended, got %#v", merged)
	}
}
