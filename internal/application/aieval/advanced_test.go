package aieval

import (
	"context"
	"errors"
	"testing"
	"time"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type candidateExecutorStub struct {
	result ExecutionResult
	err    error
}

type gateDecisionSinkStub struct{ err error }

func (s gateDecisionSinkStub) RecordGateDecision(context.Context, GateDecision) error { return s.err }

func (s candidateExecutorStub) Execute(context.Context, ExecutionRequest) (ExecutionResult, error) {
	return s.result, s.err
}

func TestAdvancedServiceExecutesDatasetAndPersistsAttempts(t *testing.T) {
	base := MustNewService(NewMemoryStore())
	dataset := Dataset{ID: "dataset-1", Name: "RAG", Version: "v1", Samples: []DatasetSample{{ID: "sample-1", Input: "question", ExpectedFacts: []string{"fact"}}}}
	if err := base.PutDataset(t.Context(), dataset); err != nil {
		t.Fatal(err)
	}
	run, err := base.StartRun(t.Context(), Run{ID: "run-1", DatasetID: dataset.ID, DatasetVersion: dataset.Version, CandidateRefs: map[string]string{"prompt": "v2"}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	store := NewAdvancedMemoryStore()
	service, err := NewAdvancedService(base, store, candidateExecutorStub{result: ExecutionResult{Output: SampleOutput{SampleID: "sample-1", ProducedFacts: []string{"fact"}}, TraceRef: "trace-1"}})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := service.ExecuteRun(t.Context(), domainidentity.Principal{}, run.ID, ExecutorProfile{ID: "isolated", EnvironmentPolicy: "eval-readonly", IsolationMode: "read-only", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != "completed" || completed.AggregateScores["fact_recall"] != 1 {
		t.Fatalf("unexpected run %#v", completed)
	}
	attempts, err := service.ListAttempts(t.Context(), run.ID)
	if err != nil || len(attempts) != 1 || attempts[0].TraceRef != "trace-1" {
		t.Fatalf("attempts = %#v, %v", attempts, err)
	}
}

func TestAdvancedServiceFailsClosedOnExecutorError(t *testing.T) {
	base := MustNewService(NewMemoryStore())
	dataset := Dataset{ID: "dataset-1", Name: "RAG", Version: "v1", Samples: []DatasetSample{{ID: "sample-1", Input: "question"}}}
	if err := base.PutDataset(t.Context(), dataset); err != nil {
		t.Fatal(err)
	}
	run, err := base.StartRun(t.Context(), Run{ID: "run-1", DatasetID: dataset.ID, DatasetVersion: dataset.Version, CandidateRefs: map[string]string{"prompt": "v2"}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	store := NewAdvancedMemoryStore()
	service, err := NewAdvancedService(base, store, candidateExecutorStub{err: errors.New("provider unavailable")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ExecuteRun(t.Context(), domainidentity.Principal{}, run.ID, ExecutorProfile{ID: "isolated", EnvironmentPolicy: "eval-readonly", IsolationMode: "read-only", Timeout: time.Second}); err == nil {
		t.Fatal("expected executor error")
	}
	attempts, _ := service.ListAttempts(t.Context(), run.ID)
	if len(attempts) != 1 || attempts[0].Status != "failed" {
		t.Fatalf("attempts = %#v", attempts)
	}
}

func TestAdvancedServiceGateBlocksRegressionAndErrorsOnInvalidInput(t *testing.T) {
	base := MustNewService(NewMemoryStore())
	store := NewAdvancedMemoryStore()
	service, err := NewAdvancedService(base, store, candidateExecutorStub{})
	if err != nil {
		t.Fatal(err)
	}
	policy := GatePolicy{ID: "release", Version: "v1", Enabled: true, MinimumScores: map[string]float64{"fact_recall": .8}, MaximumRegression: map[string]float64{"fact_recall": .1}}
	baseline := Run{ID: "base", Status: "completed", AggregateScores: map[string]float64{"fact_recall": 1}}
	candidate := Run{ID: "candidate", Status: "completed", AggregateScores: map[string]float64{"fact_recall": .7}}
	decision, err := service.EvaluateGate(t.Context(), "decision-1", policy, baseline, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision != "block" || len(decision.Reasons) != 2 {
		t.Fatalf("decision = %#v", decision)
	}
	decision, err = service.EvaluateGate(t.Context(), "decision-2", GatePolicy{}, baseline, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision != "error" {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestAdvancedServiceGateFailsClosedWhenReleaseIntegrationFails(t *testing.T) {
	service, err := NewAdvancedService(MustNewService(NewMemoryStore()), NewAdvancedMemoryStore(), candidateExecutorStub{})
	if err != nil {
		t.Fatal(err)
	}
	service.SetGateDecisionSink(gateDecisionSinkStub{err: errors.New("operation store unavailable")})
	decision, err := service.EvaluateGate(t.Context(), "decision", GatePolicy{ID: "release", Version: "v1", Enabled: true, MinimumScores: map[string]float64{"quality": .8}}, Run{ID: "base", Status: "completed", AggregateScores: map[string]float64{"quality": .9}}, Run{ID: "candidate", Status: "completed", AggregateScores: map[string]float64{"quality": .9}})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision != "error" || decision.Reasons[len(decision.Reasons)-1].Code != "release_integration_failed" {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestAdvancedServiceGateEnforcesLatencyAndCostEvidence(t *testing.T) {
	store := NewAdvancedMemoryStore()
	service, err := NewAdvancedService(MustNewService(NewMemoryStore()), store, candidateExecutorStub{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutAttempt(t.Context(), SampleAttempt{RunID: "candidate", SampleID: "sample", Attempt: 1, LatencyMillis: 250, Usage: map[string]float64{"costUsd": 2}}); err != nil {
		t.Fatal(err)
	}
	decision, err := service.EvaluateGate(
		t.Context(),
		"decision",
		GatePolicy{ID: "release", Version: "v1", Enabled: true, MinimumScores: map[string]float64{"quality": .8}, MaximumCost: 1, MaximumLatencyMS: 100},
		Run{ID: "baseline", Status: "completed", AggregateScores: map[string]float64{"quality": .9}},
		Run{ID: "candidate", Status: "completed", AggregateScores: map[string]float64{"quality": .9}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision != "block" || len(decision.Reasons) != 2 {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestReplayRequiresDisposableIsolationForWrites(t *testing.T) {
	service, err := NewAdvancedService(MustNewService(NewMemoryStore()), NewAdvancedMemoryStore(), candidateExecutorStub{})
	if err != nil {
		t.Fatal(err)
	}
	err = service.PutReplayPlan(t.Context(), ReplayPlan{ID: "replay-1", SourceTraceRefs: []string{"trace-1"}, Profile: ExecutorProfile{IsolationMode: "read-only"}})
	if err == nil {
		t.Fatal("expected isolation error")
	}
}
