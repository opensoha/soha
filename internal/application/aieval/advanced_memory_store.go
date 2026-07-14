package aieval

import (
	"context"
	"maps"
	"slices"
	"sync"
)

type AdvancedMemoryStore struct {
	mu        sync.RWMutex
	profiles  map[string]ExecutorProfile
	attempts  map[string][]SampleAttempt
	replays   map[string]ReplayPlan
	policies  map[string]GatePolicy
	decisions map[string]GateDecision
	feedback  map[string]FeedbackSample
}

func NewAdvancedMemoryStore() *AdvancedMemoryStore {
	return &AdvancedMemoryStore{
		profiles: map[string]ExecutorProfile{}, attempts: map[string][]SampleAttempt{},
		replays: map[string]ReplayPlan{}, policies: map[string]GatePolicy{},
		decisions: map[string]GateDecision{}, feedback: map[string]FeedbackSample{},
	}
}

func (s *AdvancedMemoryStore) PutExecutorProfile(_ context.Context, item ExecutorProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.profiles[item.ID] = item
	return nil
}

func (s *AdvancedMemoryStore) ListExecutorProfiles(context.Context) ([]ExecutorProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sortedMapValues(s.profiles, func(item ExecutorProfile) string { return item.ID }), nil
}

func (s *AdvancedMemoryStore) PutAttempt(_ context.Context, item SampleAttempt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts[item.RunID] = append(s.attempts[item.RunID], cloneAttempt(item))
	return nil
}

func (s *AdvancedMemoryStore) ListAttempts(_ context.Context, runID string) ([]SampleAttempt, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]SampleAttempt, len(s.attempts[runID]))
	for i, item := range s.attempts[runID] {
		items[i] = cloneAttempt(item)
	}
	return items, nil
}

func (s *AdvancedMemoryStore) PutReplayPlan(_ context.Context, item ReplayPlan) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replays[item.ID] = item
	return nil
}
func (s *AdvancedMemoryStore) ListReplayPlans(context.Context) ([]ReplayPlan, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sortedMapValues(s.replays, func(item ReplayPlan) string { return item.ID }), nil
}
func (s *AdvancedMemoryStore) PutGatePolicy(_ context.Context, item GatePolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item.MinimumScores = maps.Clone(item.MinimumScores)
	item.MaximumRegression = maps.Clone(item.MaximumRegression)
	s.policies[item.ID+":"+item.Version] = item
	return nil
}
func (s *AdvancedMemoryStore) ListGatePolicies(context.Context) ([]GatePolicy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sortedMapValues(s.policies, func(item GatePolicy) string { return item.ID + ":" + item.Version }), nil
}
func (s *AdvancedMemoryStore) PutGateDecision(_ context.Context, item GateDecision) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item.Reasons = slices.Clone(item.Reasons)
	item.EvidenceRefs = slices.Clone(item.EvidenceRefs)
	s.decisions[item.ID] = item
	return nil
}
func (s *AdvancedMemoryStore) ListGateDecisions(context.Context) ([]GateDecision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sortedMapValues(s.decisions, func(item GateDecision) string { return item.ID }), nil
}
func (s *AdvancedMemoryStore) PutFeedback(_ context.Context, item FeedbackSample) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.feedback[item.ID] = item
	return nil
}
func (s *AdvancedMemoryStore) ListFeedback(context.Context) ([]FeedbackSample, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sortedMapValues(s.feedback, func(item FeedbackSample) string { return item.ID }), nil
}

func sortedMapValues[M ~map[string]V, V any](items M, key func(V) string) []V {
	out := make([]V, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	slices.SortFunc(out, func(a, b V) int { return stringsCompare(key(a), key(b)) })
	return out
}

func stringsCompare(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func cloneAttempt(item SampleAttempt) SampleAttempt {
	item.CandidateRefs = maps.Clone(item.CandidateRefs)
	item.Scores = maps.Clone(item.Scores)
	item.Usage = maps.Clone(item.Usage)
	return item
}
