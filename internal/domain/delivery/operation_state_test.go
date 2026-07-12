package delivery

import (
	"testing"
	"time"
)

type operationStateCase struct {
	name                                                            string
	task                                                            ExecutionTask
	wantPhase                                                       string
	wantTerminal, wantCancelable, wantRetryable, wantHeartbeatStale bool
	wantNextAction, wantFailureReason, wantFailureMessage           string
	wantRuntimeEndpoint                                             bool
}

func TestBuildOperationStateDerivesDurableTaskSemantics(t *testing.T) {
	now := time.Date(2026, 6, 12, 10, 30, 0, 0, time.UTC)
	startedAt := now.Add(-10 * time.Minute)
	lastHeartbeatAt := now.Add(-6 * time.Minute)
	finishedAt := now.Add(-time.Minute)

	cases := []operationStateCase{
		{
			name: "queued task can be canceled",
			task: ExecutionTask{
				Status:    "queued",
				CreatedAt: now.Add(-time.Minute),
			},
			wantPhase:      "pending",
			wantCancelable: true,
			wantNextAction: "wait_for_runner_claim",
		},
		{
			name: "running task reports stale heartbeat",
			task: ExecutionTask{
				Status:          "running",
				StartedAt:       &startedAt,
				LastHeartbeatAt: &lastHeartbeatAt,
				TimeoutSeconds:  300,
				CreatedAt:       now.Add(-20 * time.Minute),
				Result:          map[string]any{"runtimeEndpoint": "https://agent.example"},
			},
			wantPhase:           "running",
			wantCancelable:      true,
			wantHeartbeatStale:  true,
			wantNextAction:      "inspect_runtime_or_cancel",
			wantRuntimeEndpoint: true,
		},
		{
			name: "completed task is terminal but not retryable",
			task: ExecutionTask{
				Status:     "completed",
				FinishedAt: &finishedAt,
				CreatedAt:  now.Add(-20 * time.Minute),
			},
			wantPhase:      "succeeded",
			wantTerminal:   true,
			wantNextAction: "inspect_artifacts",
		},
		{
			name: "failed task exposes failure evidence",
			task: ExecutionTask{
				Status:     "failed",
				FinishedAt: &finishedAt,
				Result: map[string]any{
					"failureReason": "provider_disabled",
					"error":         "k8s job provider is disabled",
				},
				CreatedAt: now.Add(-20 * time.Minute),
			},
			wantPhase:          "failed",
			wantTerminal:       true,
			wantRetryable:      true,
			wantNextAction:     "inspect_failure_or_retry",
			wantFailureReason:  "provider_disabled",
			wantFailureMessage: "k8s job provider is disabled",
		},
		{
			name: "canceled task exposes cancel reason",
			task: ExecutionTask{
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
			assertOperationState(t, BuildOperationState(tc.task, now), tc)
		})
	}
}

func assertOperationState(t *testing.T, got *OperationState, tc operationStateCase) {
	t.Helper()
	if got.Phase != tc.wantPhase || got.Terminal != tc.wantTerminal || got.Cancelable != tc.wantCancelable || got.Retryable != tc.wantRetryable {
		t.Fatalf("unexpected state: %#v", got)
	}
	if got.HeartbeatStale != tc.wantHeartbeatStale || got.RecommendedNextAction != tc.wantNextAction {
		t.Fatalf("unexpected heartbeat/action state: %#v", got)
	}
	if got.FailureReason != tc.wantFailureReason || got.FailureMessage != tc.wantFailureMessage || got.RuntimeEndpointPresent != tc.wantRuntimeEndpoint {
		t.Fatalf("unexpected failure/runtime evidence: %#v", got)
	}
}
