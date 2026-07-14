package aieval

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	appaieval "github.com/opensoha/soha/internal/application/aieval"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) CreateDataset(ctx context.Context, dataset appaieval.Dataset) error {
	samples, err := json.Marshal(dataset.Samples)
	if err != nil {
		return fmt.Errorf("encode evaluation dataset samples: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		INSERT INTO ai_evaluation_datasets(schema_version,id,version,name,samples,created_at)
		VALUES(?,?,?,?,?,?) ON CONFLICT (id,version) DO NOTHING`,
		dataset.SchemaVersion, dataset.ID, dataset.Version, dataset.Name, samples, dataset.CreatedAt)
	if result.Error != nil {
		return fmt.Errorf("create evaluation dataset: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: evaluation dataset %s@%s already exists", appaieval.ErrConflict, dataset.ID, dataset.Version)
	}
	return nil
}

func (r *Repository) ListDatasets(ctx context.Context) ([]appaieval.Dataset, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT schema_version,id,name,version,samples,created_at
		FROM ai_evaluation_datasets
		ORDER BY created_at DESC,id ASC,version ASC`).Rows()
	if err != nil {
		return nil, fmt.Errorf("list evaluation datasets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := []appaieval.Dataset{}
	for rows.Next() {
		item, err := scanDataset(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate evaluation datasets: %w", err)
	}
	return items, nil
}

func (r *Repository) GetDataset(ctx context.Context, id, version string) (appaieval.Dataset, error) {
	item, err := scanDataset(r.db.WithContext(ctx).Raw(`
		SELECT schema_version,id,name,version,samples,created_at
		FROM ai_evaluation_datasets WHERE id=? AND version=?`, id, version).Row())
	if errors.Is(err, sql.ErrNoRows) {
		return appaieval.Dataset{}, fmt.Errorf("%w: evaluation dataset %s@%s", appaieval.ErrNotFound, id, version)
	}
	return item, err
}

func (r *Repository) CreateRun(ctx context.Context, run appaieval.Run) error {
	candidateRefs, err := json.Marshal(run.CandidateRefs)
	if err != nil {
		return fmt.Errorf("encode evaluation candidate references: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		INSERT INTO ai_evaluation_runs(schema_version,id,dataset_id,dataset_version,candidate_refs,status,started_at)
		VALUES(?,?,?,?,?,?,?) ON CONFLICT (id) DO NOTHING`,
		run.SchemaVersion, run.ID, run.DatasetID, run.DatasetVersion, candidateRefs, run.Status, run.StartedAt)
	if result.Error != nil {
		return fmt.Errorf("create evaluation run: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: evaluation run %q already exists", appaieval.ErrConflict, run.ID)
	}
	return nil
}

func (r *Repository) ListRuns(ctx context.Context) ([]appaieval.Run, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT schema_version,id,dataset_id,dataset_version,candidate_refs,status,started_at,completed_at,aggregate_scores
		FROM ai_evaluation_runs ORDER BY started_at DESC,id ASC`).Rows()
	if err != nil {
		return nil, fmt.Errorf("list evaluation runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := []appaieval.Run{}
	for rows.Next() {
		item, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate evaluation runs: %w", err)
	}
	return items, nil
}

func (r *Repository) GetRun(ctx context.Context, id string) (appaieval.Run, error) {
	run, err := scanRun(r.db.WithContext(ctx).Raw(`
		SELECT schema_version,id,dataset_id,dataset_version,candidate_refs,status,started_at,completed_at,aggregate_scores
		FROM ai_evaluation_runs WHERE id=?`, id).Row())
	if errors.Is(err, sql.ErrNoRows) {
		return appaieval.Run{}, fmt.Errorf("%w: evaluation run %q", appaieval.ErrNotFound, id)
	}
	if err != nil {
		return appaieval.Run{}, err
	}
	results, err := r.listResults(ctx, id)
	if err != nil {
		return appaieval.Run{}, err
	}
	run.Results = results
	return run, nil
}

func (r *Repository) CompleteRun(ctx context.Context, run appaieval.Run) error {
	aggregateScores, err := json.Marshal(run.AggregateScores)
	if err != nil {
		return fmt.Errorf("encode evaluation aggregate scores: %w", err)
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Exec(`
			UPDATE ai_evaluation_runs
			SET status=?,completed_at=?,aggregate_scores=?
			WHERE id=? AND status='running'`, run.Status, run.CompletedAt, aggregateScores, run.ID)
		if result.Error != nil {
			return fmt.Errorf("complete evaluation run: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("%w: evaluation run %q is missing or terminal", appaieval.ErrConflict, run.ID)
		}
		for ordinal, evaluationResult := range run.Results {
			if err := insertResult(tx, run.ID, ordinal, evaluationResult); err != nil {
				return err
			}
		}
		return nil
	})
}

type rowScanner interface {
	Scan(...any) error
}

func scanDataset(row rowScanner) (appaieval.Dataset, error) {
	var item appaieval.Dataset
	var samples []byte
	if err := row.Scan(&item.SchemaVersion, &item.ID, &item.Name, &item.Version, &samples, &item.CreatedAt); err != nil {
		return item, err
	}
	if err := json.Unmarshal(samples, &item.Samples); err != nil {
		return item, fmt.Errorf("decode evaluation dataset samples: %w", err)
	}
	return item, nil
}

func scanRun(row rowScanner) (appaieval.Run, error) {
	var item appaieval.Run
	var candidateRefs []byte
	var aggregateScores []byte
	var completedAt *time.Time
	if err := row.Scan(
		&item.SchemaVersion, &item.ID, &item.DatasetID, &item.DatasetVersion,
		&candidateRefs, &item.Status, &item.StartedAt, &completedAt, &aggregateScores,
	); err != nil {
		return item, err
	}
	if err := json.Unmarshal(candidateRefs, &item.CandidateRefs); err != nil {
		return item, fmt.Errorf("decode evaluation candidate references: %w", err)
	}
	if len(aggregateScores) > 0 {
		if err := json.Unmarshal(aggregateScores, &item.AggregateScores); err != nil {
			return item, fmt.Errorf("decode evaluation aggregate scores: %w", err)
		}
	}
	if completedAt != nil {
		item.CompletedAt = completedAt.UTC()
	}
	return item, nil
}

func (r *Repository) listResults(ctx context.Context, runID string) ([]appaieval.Result, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT schema_version,sample_id,retrieved_sources,produced_facts,actions,scores,passed,failure_reasons
		FROM ai_evaluation_results WHERE run_id=? ORDER BY ordinal ASC`, runID).Rows()
	if err != nil {
		return nil, fmt.Errorf("list evaluation results: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := []appaieval.Result{}
	for rows.Next() {
		var item appaieval.Result
		var retrievedSources, producedFacts, actions, scores, failureReasons []byte
		if err := rows.Scan(&item.SchemaVersion, &item.SampleID, &retrievedSources, &producedFacts, &actions, &scores, &item.Passed, &failureReasons); err != nil {
			return nil, fmt.Errorf("scan evaluation result: %w", err)
		}
		if err := unmarshalResult(&item, retrievedSources, producedFacts, actions, scores, failureReasons); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate evaluation results: %w", err)
	}
	return items, nil
}

func insertResult(tx *gorm.DB, runID string, ordinal int, result appaieval.Result) error {
	retrievedSources, producedFacts, actions, scores, failureReasons, err := marshalResult(result)
	if err != nil {
		return err
	}
	if err := tx.Exec(`
		INSERT INTO ai_evaluation_results(
			run_id,sample_id,ordinal,schema_version,retrieved_sources,produced_facts,actions,scores,passed,failure_reasons
		) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		runID, result.SampleID, ordinal, result.SchemaVersion, retrievedSources, producedFacts,
		actions, scores, result.Passed, failureReasons).Error; err != nil {
		return fmt.Errorf("create evaluation result: %w", err)
	}
	return nil
}

func marshalResult(result appaieval.Result) ([]byte, []byte, []byte, []byte, []byte, error) {
	values := []any{result.RetrievedSources, result.ProducedFacts, result.Actions, result.Scores, result.FailureReasons}
	encoded := make([][]byte, len(values))
	for index, value := range values {
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, nil, nil, nil, nil, fmt.Errorf("encode evaluation result: %w", err)
		}
		encoded[index] = raw
	}
	return encoded[0], encoded[1], encoded[2], encoded[3], encoded[4], nil
}

func unmarshalResult(result *appaieval.Result, retrievedSources, producedFacts, actions, scores, failureReasons []byte) error {
	targets := []struct {
		raw    []byte
		target any
	}{
		{retrievedSources, &result.RetrievedSources},
		{producedFacts, &result.ProducedFacts},
		{actions, &result.Actions},
		{scores, &result.Scores},
		{failureReasons, &result.FailureReasons},
	}
	for _, target := range targets {
		if err := json.Unmarshal(target.raw, target.target); err != nil {
			return fmt.Errorf("decode evaluation result: %w", err)
		}
	}
	return nil
}
