package aieval

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"time"
)

var (
	ErrNotFound = errors.New("evaluation resource not found")
	ErrConflict = errors.New("evaluation resource conflict")
)

type Dataset struct {
	SchemaVersion string          `json:"schemaVersion"`
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Version       string          `json:"version"`
	Samples       []DatasetSample `json:"samples"`
	CreatedAt     time.Time       `json:"createdAt"`
}

type DatasetSample struct {
	ID               string   `json:"id"`
	Input            string   `json:"input"`
	ExpectedSources  []string `json:"expectedSources,omitempty"`
	ExpectedFacts    []string `json:"expectedFacts,omitempty"`
	ForbiddenActions []string `json:"forbiddenActions,omitempty"`
}

type Run struct {
	SchemaVersion   string             `json:"schemaVersion"`
	ID              string             `json:"id"`
	DatasetID       string             `json:"datasetId"`
	DatasetVersion  string             `json:"datasetVersion"`
	CandidateRefs   map[string]string  `json:"candidateRefs"`
	Status          string             `json:"status"`
	StartedAt       time.Time          `json:"startedAt"`
	CompletedAt     time.Time          `json:"completedAt,omitempty"`
	Results         []Result           `json:"-"`
	AggregateScores map[string]float64 `json:"aggregateScores,omitempty"`
}

type Result struct {
	SchemaVersion    string             `json:"schemaVersion"`
	SampleID         string             `json:"sampleId"`
	RetrievedSources []string           `json:"retrievedSources,omitempty"`
	ProducedFacts    []string           `json:"producedFacts,omitempty"`
	Actions          []string           `json:"actions,omitempty"`
	Scores           map[string]float64 `json:"scores"`
	Passed           bool               `json:"passed"`
	FailureReasons   []string           `json:"failureReasons,omitempty"`
}

type SampleOutput struct {
	SampleID         string   `json:"sampleId"`
	RetrievedSources []string `json:"retrievedSources,omitempty"`
	ProducedFacts    []string `json:"producedFacts,omitempty"`
	Actions          []string `json:"actions,omitempty"`
}

type Store interface {
	CreateDataset(context.Context, Dataset) error
	ListDatasets(context.Context) ([]Dataset, error)
	GetDataset(context.Context, string, string) (Dataset, error)
	CreateRun(context.Context, Run) error
	ListRuns(context.Context) ([]Run, error)
	GetRun(context.Context, string) (Run, error)
	CompleteRun(context.Context, Run) error
}

type Service struct {
	store Store
}

func NewService(store Store) (*Service, error) {
	if isNilStore(store) {
		return nil, fmt.Errorf("evaluation store is required")
	}
	return &Service{store: store}, nil
}

func isNilStore(store Store) bool {
	if store == nil {
		return true
	}
	value := reflect.ValueOf(store)
	return value.Kind() == reflect.Ptr && value.IsNil()
}

func MustNewService(store Store) *Service {
	service, err := NewService(store)
	if err != nil {
		panic(err)
	}
	return service
}

func (s *Service) PutDataset(ctx context.Context, dataset Dataset) error {
	if err := validateDataset(dataset); err != nil {
		return err
	}
	return s.store.CreateDataset(ctx, cloneDataset(dataset))
}

func (s *Service) ListDatasets(ctx context.Context) ([]Dataset, error) {
	items, err := s.store.ListDatasets(ctx)
	if err != nil {
		return nil, err
	}
	return cloneDatasets(items), nil
}

func (s *Service) ListRuns(ctx context.Context) ([]Run, error) {
	items, err := s.store.ListRuns(ctx)
	if err != nil {
		return nil, err
	}
	return cloneRuns(items), nil
}

func (s *Service) GetDataset(ctx context.Context, id, version string) (Dataset, error) {
	dataset, err := s.store.GetDataset(ctx, strings.TrimSpace(id), strings.TrimSpace(version))
	if err != nil {
		return Dataset{}, err
	}
	return cloneDataset(dataset), nil
}

func (s *Service) GetRun(ctx context.Context, id string) (Run, error) {
	run, err := s.store.GetRun(ctx, strings.TrimSpace(id))
	if err != nil {
		return Run{}, err
	}
	return cloneRun(run), nil
}

func (s *Service) StartRun(ctx context.Context, run Run, now time.Time) (Run, error) {
	if strings.TrimSpace(run.ID) == "" || strings.TrimSpace(run.DatasetID) == "" || strings.TrimSpace(run.DatasetVersion) == "" {
		return Run{}, fmt.Errorf("evaluation run and dataset identity are required")
	}
	if _, err := s.store.GetDataset(ctx, run.DatasetID, run.DatasetVersion); err != nil {
		return Run{}, err
	}
	run.SchemaVersion = "opensoha.dev/evaluation-run/v1"
	run.Status = "running"
	run.StartedAt = now.UTC()
	run.CandidateRefs = cloneStringMap(run.CandidateRefs)
	if err := s.store.CreateRun(ctx, run); err != nil {
		return Run{}, err
	}
	return cloneRun(run), nil
}

func (s *Service) CompleteRun(ctx context.Context, id string, outputs []SampleOutput, now time.Time) (Run, error) {
	run, err := s.store.GetRun(ctx, strings.TrimSpace(id))
	if err != nil {
		return Run{}, err
	}
	if run.Status != "running" {
		return Run{}, fmt.Errorf("%w: evaluation run %q is terminal", ErrConflict, id)
	}
	dataset, err := s.store.GetDataset(ctx, run.DatasetID, run.DatasetVersion)
	if err != nil {
		return Run{}, err
	}
	byID := make(map[string]SampleOutput, len(outputs))
	for _, output := range outputs {
		if _, exists := byID[output.SampleID]; exists {
			return Run{}, fmt.Errorf("duplicate output for sample %q", output.SampleID)
		}
		byID[output.SampleID] = output
	}
	run.Results = make([]Result, 0, len(dataset.Samples))
	for _, sample := range dataset.Samples {
		output, exists := byID[sample.ID]
		if !exists {
			run.Results = append(run.Results, Result{SchemaVersion: "opensoha.dev/evaluation-result/v1", SampleID: sample.ID, Scores: map[string]float64{"completeness": 0}, Passed: false, FailureReasons: []string{"missing_output"}})
			continue
		}
		run.Results = append(run.Results, EvaluateSample(sample, output))
	}
	run.AggregateScores = aggregate(run.Results)
	run.Status = "completed"
	run.CompletedAt = now.UTC()
	if err := s.store.CompleteRun(ctx, run); err != nil {
		return Run{}, err
	}
	return cloneRun(run), nil
}

func EvaluateSample(sample DatasetSample, output SampleOutput) Result {
	sourceRecall := recall(sample.ExpectedSources, output.RetrievedSources)
	factRecall := recall(sample.ExpectedFacts, output.ProducedFacts)
	forbiddenCount := intersectionCount(sample.ForbiddenActions, output.Actions)
	actionSafety := 1.0
	if forbiddenCount > 0 {
		actionSafety = 0
	}
	scores := map[string]float64{
		"source_recall": sourceRecall,
		"fact_recall":   factRecall,
		"action_safety": actionSafety,
	}
	reasons := make([]string, 0, 3)
	if sourceRecall < 1 {
		reasons = append(reasons, "missing_expected_source")
	}
	if factRecall < 1 {
		reasons = append(reasons, "missing_expected_fact")
	}
	if actionSafety == 0 {
		reasons = append(reasons, "forbidden_action")
	}
	return Result{
		SchemaVersion: "opensoha.dev/evaluation-result/v1", SampleID: sample.ID,
		RetrievedSources: append([]string(nil), output.RetrievedSources...), ProducedFacts: append([]string(nil), output.ProducedFacts...), Actions: append([]string(nil), output.Actions...),
		Scores: scores, Passed: len(reasons) == 0, FailureReasons: reasons,
	}
}

func validateDataset(dataset Dataset) error {
	if strings.TrimSpace(dataset.ID) == "" || strings.TrimSpace(dataset.Name) == "" || strings.TrimSpace(dataset.Version) == "" || len(dataset.Samples) == 0 {
		return fmt.Errorf("evaluation dataset identity and samples are required")
	}
	seen := map[string]struct{}{}
	for _, sample := range dataset.Samples {
		if strings.TrimSpace(sample.ID) == "" || strings.TrimSpace(sample.Input) == "" {
			return fmt.Errorf("evaluation sample id and input are required")
		}
		if _, ok := seen[sample.ID]; ok {
			return fmt.Errorf("duplicate evaluation sample %q", sample.ID)
		}
		seen[sample.ID] = struct{}{}
	}
	return nil
}

func recall(expected, actual []string) float64 {
	if len(expected) == 0 {
		return 1
	}
	return roundMetric(float64(intersectionCount(expected, actual)) / float64(len(unique(expected))))
}

func intersectionCount(expected, actual []string) int {
	want := make(map[string]struct{}, len(expected))
	for _, item := range expected {
		want[strings.TrimSpace(item)] = struct{}{}
	}
	seen := map[string]struct{}{}
	count := 0
	for _, item := range actual {
		item = strings.TrimSpace(item)
		if _, duplicate := seen[item]; duplicate {
			continue
		}
		seen[item] = struct{}{}
		if _, ok := want[item]; ok {
			count++
		}
	}
	return count
}

func unique(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func aggregate(results []Result) map[string]float64 {
	sums := map[string]float64{}
	counts := map[string]int{}
	for _, result := range results {
		for name, score := range result.Scores {
			sums[name] += score
			counts[name]++
		}
	}
	out := map[string]float64{}
	for name, sum := range sums {
		out[name] = roundMetric(sum / float64(counts[name]))
	}
	return out
}

func roundMetric(value float64) float64 { return math.Round(value*1_000_000) / 1_000_000 }

func ReciprocalRank(expected, ranked []string) float64 {
	want := make(map[string]struct{}, len(expected))
	for _, item := range expected {
		want[strings.TrimSpace(item)] = struct{}{}
	}
	for index, item := range ranked {
		if _, ok := want[strings.TrimSpace(item)]; ok {
			return roundMetric(1 / float64(index+1))
		}
	}
	return 0
}

func NDCG(expected, ranked []string, limit int) float64 {
	if limit <= 0 || limit > len(ranked) {
		limit = len(ranked)
	}
	want := make(map[string]struct{}, len(expected))
	for _, item := range expected {
		want[strings.TrimSpace(item)] = struct{}{}
	}
	dcg := 0.0
	for index, item := range ranked[:limit] {
		if _, ok := want[strings.TrimSpace(item)]; ok {
			dcg += 1 / math.Log2(float64(index+2))
		}
	}
	idealHits := min(len(want), limit)
	idcg := 0.0
	for index := 0; index < idealHits; index++ {
		idcg += 1 / math.Log2(float64(index+2))
	}
	if idcg == 0 {
		return 1
	}
	return roundMetric(dcg / idcg)
}

func cloneDataset(input Dataset) Dataset {
	out := input
	out.Samples = append([]DatasetSample(nil), input.Samples...)
	for i := range out.Samples {
		out.Samples[i].ExpectedSources = append([]string(nil), input.Samples[i].ExpectedSources...)
		out.Samples[i].ExpectedFacts = append([]string(nil), input.Samples[i].ExpectedFacts...)
		out.Samples[i].ForbiddenActions = append([]string(nil), input.Samples[i].ForbiddenActions...)
	}
	return out
}

func cloneRun(input Run) Run {
	out := input
	out.CandidateRefs = cloneStringMap(input.CandidateRefs)
	out.AggregateScores = make(map[string]float64, len(input.AggregateScores))
	for key, value := range input.AggregateScores {
		out.AggregateScores[key] = value
	}
	out.Results = append([]Result(nil), input.Results...)
	for i := range out.Results {
		out.Results[i].RetrievedSources = append([]string(nil), input.Results[i].RetrievedSources...)
		out.Results[i].ProducedFacts = append([]string(nil), input.Results[i].ProducedFacts...)
		out.Results[i].Actions = append([]string(nil), input.Results[i].Actions...)
		out.Results[i].FailureReasons = append([]string(nil), input.Results[i].FailureReasons...)
		out.Results[i].Scores = make(map[string]float64, len(input.Results[i].Scores))
		for key, value := range input.Results[i].Scores {
			out.Results[i].Scores[key] = value
		}
	}
	return out
}

func cloneStringMap(input map[string]string) map[string]string {
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func SortedMetricNames(scores map[string]float64) []string {
	out := make([]string, 0, len(scores))
	for name := range scores {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
