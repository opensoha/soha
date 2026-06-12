package virtualization

import (
	"testing"
	"time"
)

func TestBuildOperationStateDerivesVirtualizationTaskSemantics(t *testing.T) {
	now := time.Date(2026, 6, 12, 10, 30, 0, 0, time.UTC)
	startedAt := now.Add(-10 * time.Minute)
	lastHeartbeatAt := now.Add(-6 * time.Minute)
	finishedAt := now.Add(-time.Minute)

	cases := []struct {
		name               string
		task               Task
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
			name: "queued task can be canceled",
			task: Task{
				Status:    "queued",
				CreatedAt: now.Add(-time.Minute),
			},
			wantPhase:      "pending",
			wantCancelable: true,
			wantNextAction: "wait_for_worker_claim",
		},
		{
			name: "running task reports stale heartbeat",
			task: Task{
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
			name: "completed task is terminal but not retryable",
			task: Task{
				Status:     "completed",
				FinishedAt: &finishedAt,
				CreatedAt:  now.Add(-20 * time.Minute),
			},
			wantPhase:      "succeeded",
			wantTerminal:   true,
			wantNextAction: "inspect_result",
		},
		{
			name: "failed task exposes failure evidence",
			task: Task{
				Status:     "failed",
				FinishedAt: &finishedAt,
				Result: map[string]any{
					"failureReason": "provider_failed",
					"error":         "pve task exited 1",
				},
				CreatedAt: now.Add(-20 * time.Minute),
			},
			wantPhase:          "failed",
			wantTerminal:       true,
			wantRetryable:      true,
			wantNextAction:     "inspect_failure_or_retry",
			wantFailureReason:  "provider_failed",
			wantFailureMessage: "pve task exited 1",
		},
		{
			name: "canceled task exposes cancel reason",
			task: Task{
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
			got := BuildOperationState(tc.task, now)
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
			if tc.task.ClaimedByWorkerID != "" && got.ClaimedByWorkerID != tc.task.ClaimedByWorkerID {
				t.Fatalf("claimed worker = %q, want %q", got.ClaimedByWorkerID, tc.task.ClaimedByWorkerID)
			}
		})
	}
}
