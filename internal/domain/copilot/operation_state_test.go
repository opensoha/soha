package copilot

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestBuildOperationStateForQueuedAgentRun(t *testing.T) {
	queuedAt := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	state := BuildOperationState(AgentRun{
		ID:             "agent:queued",
		Status:         AgentRunStatusQueued,
		TimeoutSeconds: 60,
		QueuedAt:       queuedAt,
		CreatedAt:      queuedAt,
	}, queuedAt.Add(2*time.Minute))

	if state.Phase != "pending" || !state.Cancelable || state.Terminal || !state.RunnerClaimRequired {
		t.Fatalf("unexpected queued operation state: %#v", state)
	}
	if !state.TimeoutStale || state.RecommendedNextAction != "inspect_runner_or_cancel" {
		t.Fatalf("expected queued timeout guidance, got %#v", state)
	}
}

func TestBuildOperationStateForRunningAgentRun(t *testing.T) {
	startedAt := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	heartbeatAt := startedAt.Add(30 * time.Second)
	state := BuildOperationState(AgentRun{
		ID:               "agent:running",
		Status:           AgentRunStatusRunning,
		TimeoutSeconds:   120,
		QueuedAt:         startedAt,
		StartedAt:        &startedAt,
		LastHeartbeatAt:  &heartbeatAt,
		ClaimedByAgentID: "runner-1",
		CreatedAt:        startedAt,
	}, startedAt.Add(3*time.Minute))

	if state.Phase != "running" || !state.Cancelable || !state.HeartbeatRequired || state.Terminal {
		t.Fatalf("unexpected running operation state: %#v", state)
	}
	if !state.HeartbeatStale || state.ClaimedByAgentID != "runner-1" || state.RecommendedNextAction != "inspect_runner_or_cancel" {
		t.Fatalf("expected stale heartbeat guidance, got %#v", state)
	}
}

func TestBuildOperationStateForFailedAgentRunIncludesEvidence(t *testing.T) {
	completedAt := time.Date(2026, 6, 12, 10, 5, 0, 0, time.UTC)
	evidenceAt := completedAt.Add(-30 * time.Second)
	toolCompletedAt := completedAt.Add(-20 * time.Second)
	state := BuildOperationState(AgentRun{
		ID:               "agent:failed",
		Status:           AgentRunStatusFailed,
		Output:           map[string]any{"failureReason": "provider_error", "message": "Hermes exited 1 token=raw-token"},
		ErrorMessage:     "fallback message",
		CompletedAt:      &completedAt,
		ExternalRunID:    "hermes:123",
		ClaimedByAgentID: "runner-1",
		TimeoutSeconds:   60,
		AnalysisArtifacts: []AnalysisArtifact{{
			Kind:    "root_cause",
			RunID:   "agent:failed",
			Summary: "failure artifact",
			Evidence: []RootCauseEvidence{{
				ID:        "evidence-1",
				Kind:      "log",
				Title:     "Provider stderr",
				Summary:   "provider stderr password=raw-password",
				Severity:  "error",
				Timestamp: &evidenceAt,
				Attributes: map[string]any{
					"authorization": "Bearer raw-bearer",
					"line":          "failed",
				},
			}},
		}},
		ToolExecutions: []ToolExecution{{
			ID:          "tool:logs",
			AdapterID:   "logs",
			ToolName:    "logs.tail",
			Status:      "failed",
			Summary:     "log fetch failed",
			CompletedAt: &toolCompletedAt,
		}},
		CreatedAt: completedAt.Add(-time.Minute),
	}, completedAt)

	if state.Phase != "failed" || !state.Terminal || !state.Retryable || state.Cancelable {
		t.Fatalf("unexpected failed operation state: %#v", state)
	}
	if state.FailureReason != "provider_error" || state.FailureMessage != "Hermes exited 1 token=[REDACTED]" || state.ExternalRunID != "hermes:123" {
		t.Fatalf("expected failure evidence in state, got %#v", state)
	}
	if state.ArtifactCount != 1 || state.ToolExecutionCount != 1 || !state.FinalStateRecordedAt.Equal(completedAt) {
		t.Fatalf("expected evidence counts and final timestamp, got %#v", state)
	}
	for _, want := range []string{"callback_payload", "callback_message", "tool_execution", "analysis_artifact", "log", "runner_claim"} {
		if !hasFailureEvidenceKind(state.FailureEvidence, want) {
			t.Fatalf("missing failure evidence kind %q in %#v", want, state.FailureEvidence)
		}
	}
	for _, item := range state.FailureEvidence {
		if strings.Contains(fmt.Sprint(item), "raw-token") || strings.Contains(fmt.Sprint(item), "raw-password") || strings.Contains(fmt.Sprint(item), "raw-bearer") {
			t.Fatalf("failure evidence leaked sensitive value: %#v", item)
		}
	}
}

func TestBuildOperationStateForCanceledAgentRun(t *testing.T) {
	completedAt := time.Date(2026, 6, 12, 10, 5, 0, 0, time.UTC)
	state := BuildOperationState(AgentRun{
		ID:          "agent:canceled",
		Status:      AgentRunStatusCanceled,
		Output:      map[string]any{"cancelReason": "canceled by user"},
		CompletedAt: &completedAt,
		CreatedAt:   completedAt.Add(-time.Minute),
	}, completedAt)

	if state.Phase != "canceled" || !state.Terminal || !state.Retryable || state.Cancelable {
		t.Fatalf("unexpected canceled operation state: %#v", state)
	}
	if state.FailureReason != AgentRunStatusCanceled || state.FailureMessage != "canceled by user" || state.RecommendedNextAction != "retry_or_close" {
		t.Fatalf("expected cancel evidence in operation state, got %#v", state)
	}
}

func hasFailureEvidenceKind(items []FailureEvidence, kind string) bool {
	for _, item := range items {
		if item.Kind == kind {
			return true
		}
	}
	return false
}
