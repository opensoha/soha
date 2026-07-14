package aieval

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

const maxEvaluationSamples = 500

type ExecutorProfile struct {
	ID                string        `json:"id"`
	EnvironmentPolicy string        `json:"environmentPolicy"`
	ToolSnapshotRef   string        `json:"toolSnapshotRef"`
	IsolationMode     string        `json:"isolationMode"`
	Timeout           time.Duration `json:"timeout"`
	MaxCost           float64       `json:"maxCost"`
}

type ExecutionRequest struct {
	RunID         string                   `json:"runId"`
	Principal     domainidentity.Principal `json:"-"`
	Sample        DatasetSample            `json:"sample"`
	CandidateRefs map[string]string        `json:"candidateRefs"`
	Profile       ExecutorProfile          `json:"profile"`
	Attempt       int                      `json:"attempt"`
}

type ExecutionResult struct {
	Output      SampleOutput       `json:"output"`
	TraceRef    string             `json:"traceRef"`
	Usage       map[string]float64 `json:"usage"`
	Latency     time.Duration      `json:"latency"`
	CompletedAt time.Time          `json:"completedAt"`
}

type SampleAttempt struct {
	RunID         string             `json:"runId"`
	SampleID      string             `json:"sampleId"`
	Attempt       int                `json:"attempt"`
	CandidateRefs map[string]string  `json:"candidateRefs"`
	TraceRef      string             `json:"traceRef,omitempty"`
	Scores        map[string]float64 `json:"scores,omitempty"`
	Usage         map[string]float64 `json:"usage,omitempty"`
	LatencyMillis int64              `json:"latencyMillis"`
	Status        string             `json:"status"`
	ErrorCode     string             `json:"errorCode,omitempty"`
	CompletedAt   time.Time          `json:"completedAt"`
}

type ReplayPlan struct {
	ID              string            `json:"id"`
	SourceTraceRefs []string          `json:"sourceTraceRefs"`
	CandidateRefs   map[string]string `json:"candidateRefs"`
	Profile         ExecutorProfile   `json:"profile"`
	ReadOnly        bool              `json:"readOnly"`
	CreatedAt       time.Time         `json:"createdAt"`
}

type GatePolicy struct {
	ID                string             `json:"id"`
	Version           string             `json:"version"`
	MinimumScores     map[string]float64 `json:"minimumScores"`
	MaximumRegression map[string]float64 `json:"maximumRegression"`
	MaximumCost       float64            `json:"maximumCost,omitempty"`
	MaximumLatencyMS  int64              `json:"maximumLatencyMs,omitempty"`
	Enabled           bool               `json:"enabled"`
}

type GateReason struct {
	Metric   string  `json:"metric"`
	Expected float64 `json:"expected"`
	Actual   float64 `json:"actual"`
	Code     string  `json:"code"`
}

type GateDecision struct {
	ID             string       `json:"id"`
	PolicyID       string       `json:"policyId"`
	PolicyVersion  string       `json:"policyVersion"`
	BaselineRunID  string       `json:"baselineRunId"`
	CandidateRunID string       `json:"candidateRunId"`
	Decision       string       `json:"decision"`
	Reasons        []GateReason `json:"reasons"`
	EvidenceRefs   []string     `json:"evidenceRefs"`
	EvaluatedAt    time.Time    `json:"evaluatedAt"`
}

type FeedbackSample struct {
	ID             string    `json:"id"`
	TraceRef       string    `json:"traceRef"`
	ScopeHash      string    `json:"scopeHash"`
	RedactedInput  string    `json:"redactedInput"`
	RedactedOutput string    `json:"redactedOutput"`
	LicenseRef     string    `json:"licenseRef"`
	Decision       string    `json:"decision"`
	DatasetRef     string    `json:"datasetRef,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
}

type CandidateExecutor interface {
	Execute(context.Context, ExecutionRequest) (ExecutionResult, error)
}

type AdvancedStore interface {
	PutExecutorProfile(context.Context, ExecutorProfile) error
	ListExecutorProfiles(context.Context) ([]ExecutorProfile, error)
	PutAttempt(context.Context, SampleAttempt) error
	ListAttempts(context.Context, string) ([]SampleAttempt, error)
	PutReplayPlan(context.Context, ReplayPlan) error
	ListReplayPlans(context.Context) ([]ReplayPlan, error)
	PutGatePolicy(context.Context, GatePolicy) error
	ListGatePolicies(context.Context) ([]GatePolicy, error)
	PutGateDecision(context.Context, GateDecision) error
	ListGateDecisions(context.Context) ([]GateDecision, error)
	PutFeedback(context.Context, FeedbackSample) error
	ListFeedback(context.Context) ([]FeedbackSample, error)
}

type AdvancedService struct {
	registry *Service
	store    AdvancedStore
	executor CandidateExecutor
	gateSink GateDecisionSink
	now      func() time.Time
}

type GateDecisionSink interface {
	RecordGateDecision(context.Context, GateDecision) error
}

func NewAdvancedService(registry *Service, store AdvancedStore, executor CandidateExecutor) (*AdvancedService, error) {
	if registry == nil || store == nil || executor == nil {
		return nil, fmt.Errorf("evaluation registry, advanced store, and candidate executor are required")
	}
	return &AdvancedService{registry: registry, store: store, executor: executor, now: time.Now}, nil
}

func (s *AdvancedService) SetGateDecisionSink(sink GateDecisionSink) {
	s.gateSink = sink
}

func (s *AdvancedService) PutExecutorProfile(ctx context.Context, profile ExecutorProfile) error {
	profile.ID = strings.TrimSpace(profile.ID)
	profile.EnvironmentPolicy = strings.TrimSpace(profile.EnvironmentPolicy)
	profile.IsolationMode = strings.TrimSpace(profile.IsolationMode)
	if profile.ID == "" || profile.EnvironmentPolicy == "" || profile.Timeout <= 0 || profile.Timeout > 30*time.Minute {
		return fmt.Errorf("invalid evaluation executor profile")
	}
	if profile.IsolationMode != "read-only" && profile.IsolationMode != "disposable-write" {
		return fmt.Errorf("unsupported evaluation isolation mode")
	}
	return s.store.PutExecutorProfile(ctx, profile)
}

func (s *AdvancedService) ListExecutorProfiles(ctx context.Context) ([]ExecutorProfile, error) {
	return s.store.ListExecutorProfiles(ctx)
}

func (s *AdvancedService) ExecuteRun(ctx context.Context, principal domainidentity.Principal, runID string, profile ExecutorProfile) (Run, error) {
	run, err := s.registry.GetRun(ctx, strings.TrimSpace(runID))
	if err != nil {
		return Run{}, err
	}
	if run.Status != "running" {
		return Run{}, fmt.Errorf("%w: evaluation run %q is not executable", ErrConflict, run.ID)
	}
	dataset, err := s.registry.GetDataset(ctx, run.DatasetID, run.DatasetVersion)
	if err != nil {
		return Run{}, err
	}
	if len(dataset.Samples) > maxEvaluationSamples {
		return Run{}, fmt.Errorf("evaluation dataset exceeds %d samples", maxEvaluationSamples)
	}
	outputs := make([]SampleOutput, 0, len(dataset.Samples))
	for _, sample := range dataset.Samples {
		result, executeErr := s.executeSample(ctx, principal, run, sample, profile)
		if executeErr != nil {
			return Run{}, executeErr
		}
		outputs = append(outputs, result.Output)
	}
	return s.registry.CompleteRun(ctx, run.ID, outputs, s.now())
}

func (s *AdvancedService) executeSample(ctx context.Context, principal domainidentity.Principal, run Run, sample DatasetSample, profile ExecutorProfile) (ExecutionResult, error) {
	timeout := profile.Timeout
	if timeout <= 0 || timeout > 30*time.Minute {
		timeout = 2 * time.Minute
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	startedAt := s.now()
	result, err := s.executor.Execute(execCtx, ExecutionRequest{
		RunID: run.ID, Principal: principal, Sample: sample, CandidateRefs: maps.Clone(run.CandidateRefs), Profile: profile, Attempt: 1,
	})
	attempt := SampleAttempt{
		RunID: run.ID, SampleID: sample.ID, Attempt: 1, CandidateRefs: maps.Clone(run.CandidateRefs),
		TraceRef: result.TraceRef, Usage: maps.Clone(result.Usage), LatencyMillis: s.now().Sub(startedAt).Milliseconds(),
		Status: "completed", CompletedAt: s.now().UTC(),
	}
	if err != nil {
		attempt.Status = "failed"
		attempt.ErrorCode = "executor_failed"
		if storeErr := s.store.PutAttempt(ctx, attempt); storeErr != nil {
			return ExecutionResult{}, fmt.Errorf("persisting failed evaluation attempt: %w", storeErr)
		}
		return ExecutionResult{}, fmt.Errorf("executing evaluation sample %q: %w", sample.ID, err)
	}
	evaluated := EvaluateSample(sample, result.Output)
	attempt.Scores = maps.Clone(evaluated.Scores)
	if err := s.store.PutAttempt(ctx, attempt); err != nil {
		return ExecutionResult{}, fmt.Errorf("persisting evaluation attempt: %w", err)
	}
	return result, nil
}

func (s *AdvancedService) ListAttempts(ctx context.Context, runID string) ([]SampleAttempt, error) {
	return s.store.ListAttempts(ctx, strings.TrimSpace(runID))
}

func (s *AdvancedService) PutReplayPlan(ctx context.Context, plan ReplayPlan) error {
	plan.ID = strings.TrimSpace(plan.ID)
	if plan.ID == "" || len(plan.SourceTraceRefs) == 0 || len(plan.SourceTraceRefs) > maxEvaluationSamples {
		return fmt.Errorf("invalid evaluation replay plan")
	}
	if !plan.ReadOnly && plan.Profile.IsolationMode != "disposable-write" {
		return fmt.Errorf("write replay requires disposable-write isolation")
	}
	plan.SourceTraceRefs = slices.Clone(plan.SourceTraceRefs)
	plan.CandidateRefs = maps.Clone(plan.CandidateRefs)
	plan.CreatedAt = s.now().UTC()
	return s.store.PutReplayPlan(ctx, plan)
}

func (s *AdvancedService) ListReplayPlans(ctx context.Context) ([]ReplayPlan, error) {
	return s.store.ListReplayPlans(ctx)
}

func (s *AdvancedService) PutGatePolicy(ctx context.Context, policy GatePolicy) error {
	policy.ID = strings.TrimSpace(policy.ID)
	policy.Version = strings.TrimSpace(policy.Version)
	if policy.ID == "" || policy.Version == "" || len(policy.MinimumScores) == 0 {
		return fmt.Errorf("invalid evaluation gate policy")
	}
	for name, score := range policy.MinimumScores {
		if strings.TrimSpace(name) == "" || score < 0 || score > 1 {
			return fmt.Errorf("invalid minimum score in evaluation gate policy")
		}
	}
	for name, regression := range policy.MaximumRegression {
		if strings.TrimSpace(name) == "" || regression < 0 || regression > 1 {
			return fmt.Errorf("invalid maximum regression in evaluation gate policy")
		}
	}
	if policy.MaximumCost < 0 || policy.MaximumLatencyMS < 0 {
		return fmt.Errorf("invalid evaluation gate operational limit")
	}
	policy.MinimumScores = maps.Clone(policy.MinimumScores)
	policy.MaximumRegression = maps.Clone(policy.MaximumRegression)
	return s.store.PutGatePolicy(ctx, policy)
}

func (s *AdvancedService) ListGatePolicies(ctx context.Context) ([]GatePolicy, error) {
	return s.store.ListGatePolicies(ctx)
}

func (s *AdvancedService) EvaluateGate(ctx context.Context, id string, policy GatePolicy, baseline, candidate Run) (GateDecision, error) {
	decision := GateDecision{
		ID: strings.TrimSpace(id), PolicyID: policy.ID, PolicyVersion: policy.Version,
		BaselineRunID: baseline.ID, CandidateRunID: candidate.ID, Decision: "pass", EvaluatedAt: s.now().UTC(),
		EvidenceRefs: []string{"evaluation-run:" + baseline.ID, "evaluation-run:" + candidate.ID},
	}
	if decision.ID == "" || !policy.Enabled || baseline.Status != "completed" || candidate.Status != "completed" {
		decision.Decision = "error"
		decision.Reasons = []GateReason{{Code: "invalid_gate_input"}}
	} else {
		decision.Reasons = evaluateGateReasons(policy, baseline, candidate)
		operationalReasons, err := s.evaluateOperationalGateReasons(ctx, policy, candidate.ID)
		if err != nil {
			return GateDecision{}, err
		}
		decision.Reasons = append(decision.Reasons, operationalReasons...)
		slices.SortFunc(decision.Reasons, func(a, b GateReason) int { return strings.Compare(a.Metric+a.Code, b.Metric+b.Code) })
		if len(decision.Reasons) > 0 {
			decision.Decision = "block"
		}
	}
	if s.gateSink != nil {
		if err := s.gateSink.RecordGateDecision(ctx, decision); err != nil {
			decision.Decision = "error"
			decision.Reasons = append(decision.Reasons, GateReason{Code: "release_integration_failed"})
		}
	}
	if err := s.store.PutGateDecision(ctx, decision); err != nil {
		return GateDecision{}, fmt.Errorf("persisting evaluation gate decision: %w", err)
	}
	return decision, nil
}

func (s *AdvancedService) evaluateOperationalGateReasons(ctx context.Context, policy GatePolicy, candidateRunID string) ([]GateReason, error) {
	if policy.MaximumCost == 0 && policy.MaximumLatencyMS == 0 {
		return nil, nil
	}
	attempts, err := s.store.ListAttempts(ctx, candidateRunID)
	if err != nil {
		return nil, fmt.Errorf("loading evaluation attempts for gate: %w", err)
	}
	reasons := make([]GateReason, 0, 2)
	if policy.MaximumLatencyMS > 0 {
		if len(attempts) == 0 {
			reasons = append(reasons, GateReason{Metric: "latency_ms", Expected: float64(policy.MaximumLatencyMS), Code: "latency_evidence_missing"})
		} else {
			var maximum int64
			for _, attempt := range attempts {
				maximum = max(maximum, attempt.LatencyMillis)
			}
			if maximum > policy.MaximumLatencyMS {
				reasons = append(reasons, GateReason{Metric: "latency_ms", Expected: float64(policy.MaximumLatencyMS), Actual: float64(maximum), Code: "latency_exceeded"})
			}
		}
	}
	if policy.MaximumCost > 0 {
		var cost float64
		found := false
		for _, attempt := range attempts {
			for _, key := range []string{"cost", "costUsd", "totalCost"} {
				if value, ok := attempt.Usage[key]; ok {
					cost += value
					found = true
					break
				}
			}
		}
		if !found {
			reasons = append(reasons, GateReason{Metric: "cost", Expected: policy.MaximumCost, Code: "cost_evidence_missing"})
		} else if cost > policy.MaximumCost {
			reasons = append(reasons, GateReason{Metric: "cost", Expected: policy.MaximumCost, Actual: cost, Code: "cost_exceeded"})
		}
	}
	return reasons, nil
}

func evaluateGateReasons(policy GatePolicy, baseline, candidate Run) []GateReason {
	reasons := make([]GateReason, 0)
	for metric, minimum := range policy.MinimumScores {
		actual := candidate.AggregateScores[metric]
		if actual < minimum {
			reasons = append(reasons, GateReason{Metric: metric, Expected: minimum, Actual: actual, Code: "below_minimum"})
		}
	}
	for metric, maximum := range policy.MaximumRegression {
		regression := baseline.AggregateScores[metric] - candidate.AggregateScores[metric]
		if regression > maximum {
			reasons = append(reasons, GateReason{Metric: metric, Expected: maximum, Actual: regression, Code: "regression_exceeded"})
		}
	}
	slices.SortFunc(reasons, func(a, b GateReason) int { return strings.Compare(a.Metric+a.Code, b.Metric+b.Code) })
	return reasons
}

func (s *AdvancedService) ListGateDecisions(ctx context.Context) ([]GateDecision, error) {
	return s.store.ListGateDecisions(ctx)
}

func (s *AdvancedService) PutFeedback(ctx context.Context, sample FeedbackSample) error {
	sample.ID = strings.TrimSpace(sample.ID)
	sample.TraceRef = strings.TrimSpace(sample.TraceRef)
	sample.ScopeHash = strings.TrimSpace(sample.ScopeHash)
	sample.Decision = strings.TrimSpace(sample.Decision)
	if sample.ID == "" || sample.TraceRef == "" || !strings.HasPrefix(sample.ScopeHash, "sha256:") {
		return fmt.Errorf("invalid evaluation feedback sample")
	}
	if sample.Decision != "pending" && sample.Decision != "accepted" && sample.Decision != "rejected" && sample.Decision != "deleted" {
		return fmt.Errorf("unsupported feedback decision")
	}
	sample.CreatedAt = s.now().UTC()
	return s.store.PutFeedback(ctx, sample)
}

func (s *AdvancedService) ListFeedback(ctx context.Context) ([]FeedbackSample, error) {
	return s.store.ListFeedback(ctx)
}
