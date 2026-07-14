package agentharness

import (
	"errors"
	"testing"
	"time"
)

func TestRunBudgetAndTerminalProtection(t *testing.T) {
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	run, err := NewRun(Run{ID: "run-1", ProviderID: "hermes", ProviderVersion: "1.0.0", AdapterProtocolVersion: "v1", CatalogRevision: 2, Budget: Budget{MaxSteps: 2, MaxTokens: 100, MaxToolCalls: 2, MaxCost: 1, Deadline: now.Add(time.Minute)}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Transition(RunPreparing, "", now); err != nil {
		t.Fatal(err)
	}
	if err := run.Transition(RunRunning, "", now); err != nil {
		t.Fatal(err)
	}
	if err := run.ApplyUsage(Usage{Steps: 2}, now); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("ApplyUsage() error = %v", err)
	}
	if run.State != RunBudgetExceeded || run.StopReason != StopMaxSteps {
		t.Fatalf("run = %#v", run)
	}
	if err := run.Transition(RunCompleted, StopAnswerAccepted, now); !errors.Is(err, ErrTerminalRun) {
		t.Fatalf("late callback error = %v", err)
	}
}

func TestResumeCreatesNewAttempt(t *testing.T) {
	now := time.Now().UTC()
	original := Run{ID: "run-1", Attempt: 1, State: RunPaused, ProviderID: "provider", ProviderVersion: "v1", CatalogRevision: 1, Usage: Usage{Steps: 2}}
	resumed, err := ResumeFromCheckpoint(original, Checkpoint{ID: "checkpoint-1", RunID: original.ID, StateHash: "sha256:state"}, "run-2", now)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.ID != "run-2" || resumed.Attempt != 2 || resumed.State != RunQueued || resumed.Usage.Steps != 0 || resumed.CheckpointRef != "checkpoint-1" {
		t.Fatalf("resumed = %#v", resumed)
	}
}

func TestProgressTrackerStopsRepeatedAction(t *testing.T) {
	tracker := ProgressTracker{MaxIdenticalActions: 3}
	for i := 0; i < 2; i++ {
		if reason, err := tracker.Observe("tool:logs.query:hash"); err != nil || reason != "" {
			t.Fatalf("Observe() = %q, %v", reason, err)
		}
	}
	if reason, err := tracker.Observe("tool:logs.query:hash"); err != nil || reason != StopNoProgress {
		t.Fatalf("Observe() = %q, %v, want %q", reason, err, StopNoProgress)
	}
}
