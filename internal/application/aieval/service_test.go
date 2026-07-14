package aieval

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceComputesDeterministicMetrics(t *testing.T) {
	ctx := context.Background()
	service := MustNewService(NewMemoryStore())
	dataset := Dataset{ID: "rag-regression", Name: "RAG regression", Version: "v1", Samples: []DatasetSample{{ID: "s1", Input: "question", ExpectedSources: []string{"doc:1", "doc:2"}, ExpectedFacts: []string{"fact:a"}, ForbiddenActions: []string{"delete"}}}}
	if err := service.PutDataset(ctx, dataset); err != nil {
		t.Fatal(err)
	}
	run, err := service.StartRun(ctx, Run{ID: "eval-1", DatasetID: dataset.ID, DatasetVersion: dataset.Version, CandidateRefs: map[string]string{"prompt": "p1"}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	completed, err := service.CompleteRun(ctx, run.ID, []SampleOutput{{SampleID: "s1", RetrievedSources: []string{"doc:1", "doc:1"}, ProducedFacts: []string{"fact:a"}}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != "completed" || completed.AggregateScores["source_recall"] != 0.5 || completed.AggregateScores["fact_recall"] != 1 || completed.Results[0].Passed {
		t.Fatalf("completed run = %#v", completed)
	}
}

func TestEvaluateSampleFailsForbiddenAction(t *testing.T) {
	result := EvaluateSample(DatasetSample{ID: "s", Input: "q", ForbiddenActions: []string{"delete"}}, SampleOutput{SampleID: "s", Actions: []string{"delete"}})
	if result.Passed || result.Scores["action_safety"] != 0 || len(result.FailureReasons) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestRankingMetricsAreDeterministic(t *testing.T) {
	expected := []string{"doc:a", "doc:b"}
	ranked := []string{"doc:x", "doc:a", "doc:b"}
	if got := ReciprocalRank(expected, ranked); got != 0.5 {
		t.Fatalf("ReciprocalRank() = %v", got)
	}
	if got := NDCG(expected, ranked, 3); got != 0.693426 {
		t.Fatalf("NDCG() = %v", got)
	}
}

func TestServiceListsClonedDatasetsAndRuns(t *testing.T) {
	ctx := context.Background()
	service := MustNewService(NewMemoryStore())
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	dataset := Dataset{ID: "rag-regression", Name: "RAG regression", Version: "v1", CreatedAt: now, Samples: []DatasetSample{{ID: "s1", Input: "question"}}}
	if err := service.PutDataset(ctx, dataset); err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartRun(ctx, Run{ID: "eval-1", DatasetID: dataset.ID, DatasetVersion: dataset.Version, CandidateRefs: map[string]string{"prompt": "p1"}}, now); err != nil {
		t.Fatal(err)
	}

	datasets, err := service.ListDatasets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	runs, err := service.ListRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	datasets[0].Samples[0].Input = "changed"
	runs[0].CandidateRefs["prompt"] = "changed"
	reloadedDatasets, _ := service.ListDatasets(ctx)
	reloadedRuns, _ := service.ListRuns(ctx)
	if reloadedDatasets[0].Samples[0].Input != "question" || reloadedRuns[0].CandidateRefs["prompt"] != "p1" {
		t.Fatal("list methods exposed mutable service state")
	}
	if _, err := service.GetRun(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetRun missing error = %v", err)
	}
}

func TestNewServiceRejectsMissingStore(t *testing.T) {
	if _, err := NewService(nil); err == nil {
		t.Fatal("NewService(nil) error = nil")
	}
	var store *MemoryStore
	if _, err := NewService(store); err == nil {
		t.Fatal("NewService(typed nil) error = nil")
	}
}

func TestServiceRestoresCompletedRunFromExistingStore(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	first := MustNewService(store)
	dataset := Dataset{ID: "rag-regression", Name: "RAG regression", Version: "v1", Samples: []DatasetSample{{ID: "s1", Input: "question"}}}
	if err := first.PutDataset(ctx, dataset); err != nil {
		t.Fatal(err)
	}
	if _, err := first.StartRun(ctx, Run{ID: "eval-1", DatasetID: dataset.ID, DatasetVersion: dataset.Version}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := first.CompleteRun(ctx, "eval-1", []SampleOutput{{SampleID: "s1"}}, time.Now()); err != nil {
		t.Fatal(err)
	}

	restarted := MustNewService(store)
	run, err := restarted.GetRun(ctx, "eval-1")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "completed" || len(run.Results) != 1 || run.Results[0].SampleID != "s1" {
		t.Fatalf("restored run = %#v", run)
	}
}
