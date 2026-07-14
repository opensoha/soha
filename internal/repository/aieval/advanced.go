package aieval

import (
	"context"
	"encoding/json"
	"fmt"

	appaieval "github.com/opensoha/soha/internal/application/aieval"
)

func (r *Repository) PutExecutorProfile(ctx context.Context, item appaieval.ExecutorProfile) error {
	payload, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("encode evaluation executor profile: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`INSERT INTO ai_evaluation_executor_profiles(id,payload,updated_at) VALUES(?,?,NOW()) ON CONFLICT(id) DO UPDATE SET payload=EXCLUDED.payload,updated_at=NOW()`, item.ID, payload).Error; err != nil {
		return fmt.Errorf("put evaluation executor profile: %w", err)
	}
	return nil
}

func (r *Repository) ListExecutorProfiles(ctx context.Context) ([]appaieval.ExecutorProfile, error) {
	return listAdvancedJSON[appaieval.ExecutorProfile](ctx, r, `SELECT payload FROM ai_evaluation_executor_profiles ORDER BY id`)
}

func (r *Repository) PutAttempt(ctx context.Context, item appaieval.SampleAttempt) error {
	payload, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("encode evaluation attempt: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`INSERT INTO ai_evaluation_sample_attempts(run_id,sample_id,attempt,payload,completed_at) VALUES(?,?,?,?,?) ON CONFLICT(run_id,sample_id,attempt) DO NOTHING`, item.RunID, item.SampleID, item.Attempt, payload, item.CompletedAt).Error; err != nil {
		return fmt.Errorf("put evaluation attempt: %w", err)
	}
	return nil
}

func (r *Repository) ListAttempts(ctx context.Context, runID string) ([]appaieval.SampleAttempt, error) {
	return listAdvancedJSON[appaieval.SampleAttempt](ctx, r, `SELECT payload FROM ai_evaluation_sample_attempts WHERE run_id=? ORDER BY sample_id,attempt`, runID)
}

func (r *Repository) PutReplayPlan(ctx context.Context, item appaieval.ReplayPlan) error {
	payload, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("encode evaluation replay plan: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`INSERT INTO ai_evaluation_replay_plans(id,payload,created_at) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET payload=EXCLUDED.payload`, item.ID, payload, item.CreatedAt).Error; err != nil {
		return fmt.Errorf("put evaluation replay plan: %w", err)
	}
	return nil
}

func (r *Repository) ListReplayPlans(ctx context.Context) ([]appaieval.ReplayPlan, error) {
	return listAdvancedJSON[appaieval.ReplayPlan](ctx, r, `SELECT payload FROM ai_evaluation_replay_plans ORDER BY created_at DESC`)
}

func (r *Repository) PutGatePolicy(ctx context.Context, item appaieval.GatePolicy) error {
	payload, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("encode evaluation gate policy: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`INSERT INTO ai_evaluation_gate_policies(id,version,payload,updated_at) VALUES(?,?,?,NOW()) ON CONFLICT(id,version) DO UPDATE SET payload=EXCLUDED.payload,updated_at=NOW()`, item.ID, item.Version, payload).Error; err != nil {
		return fmt.Errorf("put evaluation gate policy: %w", err)
	}
	return nil
}

func (r *Repository) ListGatePolicies(ctx context.Context) ([]appaieval.GatePolicy, error) {
	return listAdvancedJSON[appaieval.GatePolicy](ctx, r, `SELECT payload FROM ai_evaluation_gate_policies ORDER BY id,version`)
}

func (r *Repository) PutGateDecision(ctx context.Context, item appaieval.GateDecision) error {
	payload, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("encode evaluation gate decision: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`INSERT INTO ai_evaluation_gate_decisions(id,candidate_run_id,decision,payload,evaluated_at) VALUES(?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, item.ID, item.CandidateRunID, item.Decision, payload, item.EvaluatedAt).Error; err != nil {
		return fmt.Errorf("put evaluation gate decision: %w", err)
	}
	return nil
}

func (r *Repository) ListGateDecisions(ctx context.Context) ([]appaieval.GateDecision, error) {
	return listAdvancedJSON[appaieval.GateDecision](ctx, r, `SELECT payload FROM ai_evaluation_gate_decisions ORDER BY evaluated_at DESC`)
}

func (r *Repository) PutFeedback(ctx context.Context, item appaieval.FeedbackSample) error {
	payload, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("encode evaluation feedback: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`INSERT INTO ai_evaluation_feedback_samples(id,trace_ref,decision,payload,created_at) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET decision=EXCLUDED.decision,payload=EXCLUDED.payload`, item.ID, item.TraceRef, item.Decision, payload, item.CreatedAt).Error; err != nil {
		return fmt.Errorf("put evaluation feedback: %w", err)
	}
	return nil
}

func (r *Repository) ListFeedback(ctx context.Context) ([]appaieval.FeedbackSample, error) {
	return listAdvancedJSON[appaieval.FeedbackSample](ctx, r, `SELECT payload FROM ai_evaluation_feedback_samples ORDER BY created_at DESC`)
}

func listAdvancedJSON[T any](ctx context.Context, r *Repository, query string, args ...any) ([]T, error) {
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("list advanced evaluation resources: %w", err)
	}
	defer rows.Close()
	items := make([]T, 0)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan advanced evaluation resource: %w", err)
		}
		var item T
		if err := json.Unmarshal(payload, &item); err != nil {
			return nil, fmt.Errorf("decode advanced evaluation resource: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate advanced evaluation resources: %w", err)
	}
	return items, nil
}
