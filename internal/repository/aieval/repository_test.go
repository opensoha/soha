package aieval

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	appaieval "github.com/opensoha/soha/internal/application/aieval"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestCreateDatasetReportsVersionConflict(t *testing.T) {
	repository, mock := newRepository(t)
	dataset := evaluationDataset()
	mock.ExpectExec(`INSERT INTO ai_evaluation_datasets`).
		WithArgs(dataset.SchemaVersion, dataset.ID, dataset.Version, dataset.Name, sqlmock.AnyArg(), dataset.CreatedAt).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := repository.CreateDataset(context.Background(), dataset)
	if !errors.Is(err, appaieval.ErrConflict) {
		t.Fatalf("CreateDataset() error = %v, want conflict", err)
	}
	assertExpectations(t, mock)
}

func TestGetRunRestoresDurableRunAndOrderedResults(t *testing.T) {
	repository, mock := newRepository(t)
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	completedAt := now.Add(time.Minute)
	mock.ExpectQuery(`SELECT schema_version,id,dataset_id,dataset_version,candidate_refs,status,started_at,completed_at,aggregate_scores`).
		WithArgs("eval-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"schema_version", "id", "dataset_id", "dataset_version", "candidate_refs", "status", "started_at", "completed_at", "aggregate_scores",
		}).AddRow("opensoha.dev/evaluation-run/v1", "eval-1", "rag-regression", "v1", []byte(`{"prompt":"p1"}`), "completed", now, completedAt, []byte(`{"fact_recall":1}`)))
	mock.ExpectQuery(`SELECT schema_version,sample_id,retrieved_sources,produced_facts,actions,scores,passed,failure_reasons`).
		WithArgs("eval-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"schema_version", "sample_id", "retrieved_sources", "produced_facts", "actions", "scores", "passed", "failure_reasons",
		}).AddRow("opensoha.dev/evaluation-result/v1", "s1", []byte(`["doc:1"]`), []byte(`["fact:a"]`), []byte(`[]`), []byte(`{"fact_recall":1}`), true, []byte(`[]`)))

	run, err := repository.GetRun(context.Background(), "eval-1")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "completed" || !run.CompletedAt.Equal(completedAt) || run.AggregateScores["fact_recall"] != 1 {
		t.Fatalf("run = %#v", run)
	}
	if len(run.Results) != 1 || run.Results[0].SampleID != "s1" || !run.Results[0].Passed {
		t.Fatalf("results = %#v", run.Results)
	}
	assertExpectations(t, mock)
}

func TestCompleteRunPersistsStateAndResultsAtomically(t *testing.T) {
	repository, mock := newRepository(t)
	run := appaieval.Run{
		ID: "eval-1", Status: "completed", CompletedAt: time.Now().UTC(),
		AggregateScores: map[string]float64{"fact_recall": 1},
		Results: []appaieval.Result{{
			SchemaVersion: "opensoha.dev/evaluation-result/v1", SampleID: "s1",
			RetrievedSources: []string{"doc:1"}, ProducedFacts: []string{"fact:a"},
			Scores: map[string]float64{"fact_recall": 1}, Passed: true,
		}},
	}
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE ai_evaluation_runs`).
		WithArgs(run.Status, run.CompletedAt, sqlmock.AnyArg(), run.ID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO ai_evaluation_results`).
		WithArgs(run.ID, "s1", 0, "opensoha.dev/evaluation-result/v1", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), true, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repository.CompleteRun(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	assertExpectations(t, mock)
}

func TestCompleteRunRollsBackWhenRunIsAlreadyTerminal(t *testing.T) {
	repository, mock := newRepository(t)
	run := appaieval.Run{ID: "eval-1", Status: "completed", CompletedAt: time.Now().UTC(), AggregateScores: map[string]float64{}}
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE ai_evaluation_runs`).
		WithArgs(run.Status, run.CompletedAt, sqlmock.AnyArg(), run.ID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	err := repository.CompleteRun(context.Background(), run)
	if !errors.Is(err, appaieval.ErrConflict) {
		t.Fatalf("CompleteRun() error = %v, want conflict", err)
	}
	assertExpectations(t, mock)
}

func evaluationDataset() appaieval.Dataset {
	return appaieval.Dataset{
		SchemaVersion: "opensoha.dev/evaluation-dataset/v1",
		ID:            "rag-regression", Name: "RAG regression", Version: "v1",
		Samples:   []appaieval.DatasetSample{{ID: "s1", Input: "question"}},
		CreatedAt: time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC),
	}
}

func newRepository(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return New(db), mock
}

func assertExpectations(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
