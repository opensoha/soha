package docker

import (
	"testing"
	"time"
)

func TestBuildOperationStateDerivesDockerOperationSemantics(t *testing.T) {
	now := time.Date(2026, 6, 12, 10, 30, 0, 0, time.UTC)
	startedAt := now.Add(-10 * time.Minute)
	lastHeartbeatAt := now.Add(-6 * time.Minute)
	finishedAt := now.Add(-time.Minute)

	cases := []struct {
		name               string
		operation          Operation
		wantPhase          string
		wantTerminal       bool
		wantCancelable     bool
		wantRetryable      bool
		wantHeartbeatStale bool
		wantNextAction     string
		wantFailureReason  string
		wantFailureMessage string
	}{
		{
			name: "queued operation can be canceled",
			operation: Operation{
				Status:    "queued",
				CreatedAt: now.Add(-time.Minute),
			},
			wantPhase:      "pending",
			wantCancelable: true,
			wantNextAction: "wait_for_worker_claim",
		},
		{
			name: "running operation reports stale heartbeat",
			operation: Operation{
				Status:            "running",
				ClaimedByWorkerID: "worker-1",
				StartedAt:         &startedAt,
				LastHeartbeatAt:   &lastHeartbeatAt,
				TimeoutSeconds:    300,
				CreatedAt:         now.Add(-20 * time.Minute),
			},
			wantPhase:          "running",
			wantCancelable:     true,
			wantHeartbeatStale: true,
			wantNextAction:     "inspect_worker_or_cancel",
		},
		{
			name: "completed operation is terminal but not retryable",
			operation: Operation{
				Status:     "completed",
				FinishedAt: &finishedAt,
				CreatedAt:  now.Add(-20 * time.Minute),
			},
			wantPhase:      "succeeded",
			wantTerminal:   true,
			wantNextAction: "inspect_result",
		},
		{
			name: "failed operation exposes failure evidence",
			operation: Operation{
				Status:     "failed",
				FinishedAt: &finishedAt,
				Result: map[string]any{
					"failureReason": "compose_failed",
					"error":         "docker compose exited 1",
				},
				CreatedAt: now.Add(-20 * time.Minute),
			},
			wantPhase:          "failed",
			wantTerminal:       true,
			wantRetryable:      true,
			wantNextAction:     "inspect_failure_or_retry",
			wantFailureReason:  "compose_failed",
			wantFailureMessage: "docker compose exited 1",
		},
		{
			name: "canceled operation exposes cancel reason",
			operation: Operation{
				Status:     "canceled",
				FinishedAt: &finishedAt,
				Result:     map[string]any{"cancelReason": "operator stopped it"},
				CreatedAt:  now.Add(-20 * time.Minute),
			},
			wantPhase:          "canceled",
			wantTerminal:       true,
			wantRetryable:      true,
			wantNextAction:     "retry_or_close",
			wantFailureReason:  "canceled",
			wantFailureMessage: "operator stopped it",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildOperationState(tc.operation, now)
			if got.Phase != tc.wantPhase || got.Terminal != tc.wantTerminal || got.Cancelable != tc.wantCancelable || got.Retryable != tc.wantRetryable {
				t.Fatalf("unexpected state: %#v", got)
			}
			if got.HeartbeatStale != tc.wantHeartbeatStale {
				t.Fatalf("heartbeat stale = %v, want %v in %#v", got.HeartbeatStale, tc.wantHeartbeatStale, got)
			}
			if got.RecommendedNextAction != tc.wantNextAction {
				t.Fatalf("next action = %q, want %q", got.RecommendedNextAction, tc.wantNextAction)
			}
			if got.FailureReason != tc.wantFailureReason || got.FailureMessage != tc.wantFailureMessage {
				t.Fatalf("failure evidence = %q/%q, want %q/%q", got.FailureReason, got.FailureMessage, tc.wantFailureReason, tc.wantFailureMessage)
			}
			if tc.operation.ClaimedByWorkerID != "" && got.ClaimedByWorkerID != tc.operation.ClaimedByWorkerID {
				t.Fatalf("claimed worker = %q, want %q", got.ClaimedByWorkerID, tc.operation.ClaimedByWorkerID)
			}
		})
	}
}
