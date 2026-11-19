package knowledge

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

type policyLexicalScores []float64

func (s policyLexicalScores) Score(string, []domainknowledge.Chunk) []float64 { return s }

type policyVectorScores struct {
	scores []float64
	err    error
}

func (s policyVectorScores) Score(context.Context, domainidentity.Principal, string, []domainknowledge.Chunk) ([]float64, error) {
	return s.scores, s.err
}

func policySearchService(t *testing.T, repo *searchRepository, lexical LexicalScorer, vector VectorScorer) (*Service, domainidentity.Principal) {
	t.Helper()
	permissions := appaccess.NewPermissionResolver(testRoleReader{permissions: map[string][]string{"reader": {PermKnowledgeView}}})
	service, err := New(repo, permissions, lexical, vector)
	if err != nil {
		t.Fatal(err)
	}
	return service, domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}
}

func TestSearchUsesOnlySelectedAuthorizedBaseLimits(t *testing.T) {
	for _, test := range []struct {
		name string
		ids  []string
		topK int
		want int
	}{
		{name: "single configured default", ids: []string{"a"}, want: 7},
		{name: "multi base minimum default", ids: []string{"a", "b"}, want: 3},
		{name: "multi base minimum max", ids: []string{"a", "b"}, topK: 50, want: 8},
		{name: "small requested limit", ids: []string{"a", "b"}, topK: 2, want: 2},
		{name: "legacy bounded default", ids: []string{"legacy"}, want: 5},
		{name: "legacy bounded max", ids: []string{"legacy"}, topK: 50, want: 20},
		{name: "hidden or missing does not restrict authorized", ids: []string{"hidden", "missing", "a"}, want: 7},
		{name: "no authorized bases", ids: []string{"hidden", "missing"}, want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &searchRepository{policies: map[string]domainknowledge.RetrievalPolicy{
				"a": {DefaultTopK: 7, MaxTopK: 12}, "b": {DefaultTopK: 3, MaxTopK: 8}, "legacy": {}, "unselected": {MaxTopK: 1},
			}}
			for _, id := range []string{"a", "b", "legacy", "hidden"} {
				for i := 0; i < 60; i++ {
					repo.chunks = append(repo.chunks, domainknowledge.Chunk{KnowledgeBaseID: id, Content: "query"})
				}
			}
			service, principal := policySearchService(t, repo, nil, nil)
			result, err := service.Search(t.Context(), principal, domainknowledge.SearchRequest{KnowledgeBaseIDs: test.ids, Query: "query", TopK: test.topK})
			if err != nil || len(result.Hits) != test.want || len(result.Citations) != test.want || repo.policyCalls != 1 || repo.limit > 500 {
				t.Fatalf("policy search = %d/%d, %v; reads = %d, candidate limit = %d", len(result.Hits), len(result.Citations), err, repo.policyCalls, repo.limit)
			}
			for _, hit := range result.Hits {
				if hit.KnowledgeBaseID == "hidden" || hit.KnowledgeBaseID == "unselected" {
					t.Fatalf("unselected/unauthorized hit: %+v", hit)
				}
			}
			if test.want == 0 && (!result.NoAnswer || repo.calls != 0) {
				t.Fatalf("empty search disclosed content or read candidates: %+v", result)
			}
		})
	}
}

func TestSearchUsesPerBaseWeightsMinScoreAndLexicalFallback(t *testing.T) {
	for _, test := range []struct {
		name      string
		minScore  float64
		vectorErr error
		wantID    string
		wantN     int
		wantScore float64
	}{
		{name: "weights reorder results", wantID: "lexical", wantN: 2, wantScore: .91},
		{name: "positive threshold filters", minScore: .95, wantID: "vector", wantN: 1, wantScore: .81},
		{name: "vector failure reweights lexical", vectorErr: errors.New("embedding unavailable"), wantID: "lexical", wantN: 2, wantScore: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &searchRepository{chunks: []domainknowledge.Chunk{{ID: "lexical", KnowledgeBaseID: "a"}, {ID: "vector", KnowledgeBaseID: "b"}}, policies: map[string]domainknowledge.RetrievalPolicy{
				"a": {LexicalWeight: .9, VectorWeight: .1, MinScore: test.minScore}, "b": {LexicalWeight: .1, VectorWeight: .9},
			}}
			service, principal := policySearchService(t, repo, policyLexicalScores{1, .9}, policyVectorScores{scores: []float64{.1, .8}, err: test.vectorErr})
			result, err := service.Search(t.Context(), principal, domainknowledge.SearchRequest{KnowledgeBaseIDs: []string{"a", "b"}, Query: "query"})
			if err != nil || len(result.Hits) != test.wantN || result.Hits[0].ChunkID != test.wantID || math.Abs(result.Hits[0].Score-test.wantScore) > 1e-9 || result.Citations[0].Score != result.Hits[0].Score {
				t.Fatalf("weighted result = %+v, %v", result, err)
			}
		})
	}
}

func TestSearchStopsOnPolicyReadError(t *testing.T) {
	repo := &searchRepository{policyErr: errors.New("policy read failed")}
	service, principal := policySearchService(t, repo, nil, nil)
	_, err := service.Search(t.Context(), principal, domainknowledge.SearchRequest{KnowledgeBaseIDs: []string{"a"}, Query: "query"})
	if !errors.Is(err, repo.policyErr) || repo.calls != 0 {
		t.Fatalf("policy read error = %v, candidate calls = %d", err, repo.calls)
	}
}

func TestBuildHitsUsesVectorAvailabilityPerBase(t *testing.T) {
	policies := map[string]domainknowledge.RetrievalPolicy{"legacy": {VectorWeight: 1}, "indexed": {VectorWeight: 1}}
	hits := buildHits(retrievalCandidates{
		chunks:  []domainknowledge.Chunk{{KnowledgeBaseID: "legacy"}, {KnowledgeBaseID: "indexed"}},
		lexical: []float64{.8, 1}, vector: []float64{0, .2},
		vectorAvailable: map[string]bool{"indexed": true},
	}, policies)
	if len(hits) != 2 || hits[0].Score != .8 || hits[1].Score != .2 {
		t.Fatalf("mixed base availability = %+v", hits)
	}
}

type belowThresholdReranker struct{}

func (belowThresholdReranker) Rerank(_ context.Context, _ domainidentity.Principal, _ string, hits []domainknowledge.SearchHit) ([]domainknowledge.SearchHit, error) {
	hits[0].Score = .01
	return hits, nil
}

func TestSearchMinScoreControlsAnswerAndRerankedHits(t *testing.T) {
	for _, test := range []struct {
		name     string
		minimum  float64
		rerank   bool
		wantN    int
		noAnswer bool
	}{
		{name: "zero keeps legacy answer threshold", wantN: 1, noAnswer: true},
		{name: "positive below legacy threshold", minimum: .02, wantN: 1},
		{name: "exact configured threshold", minimum: .03, wantN: 1},
		{name: "below configured threshold", minimum: .04, noAnswer: true},
		{name: "rerank cannot bypass threshold", minimum: .02, rerank: true, noAnswer: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &searchRepository{chunks: []domainknowledge.Chunk{{ID: "chunk", KnowledgeBaseID: "base"}}, policies: map[string]domainknowledge.RetrievalPolicy{
				"base": {LexicalWeight: 1, MinScore: test.minimum},
			}}
			service, principal := policySearchService(t, repo, policyLexicalScores{.03}, policyVectorScores{scores: []float64{0}})
			if test.rerank {
				service.reranker = belowThresholdReranker{}
			}
			result, err := service.Search(t.Context(), principal, domainknowledge.SearchRequest{KnowledgeBaseIDs: []string{"base"}, Query: "query"})
			if err != nil || len(result.Hits) != test.wantN || result.NoAnswer != test.noAnswer {
				t.Fatalf("threshold result = %+v, %v", result, err)
			}
		})
	}
}

func TestNormalizeBaseInputPreservesConfiguredWeights(t *testing.T) {
	policy := domainknowledge.RetrievalPolicy{DefaultTopK: 3, MaxTopK: 8, LexicalWeight: .2, VectorWeight: .2, MinScore: .03}
	base, err := normalizeBaseInput(domainknowledge.BaseInput{Name: "base", RetrievalPolicy: policy}, domainidentity.Principal{UserID: "owner"}, time.Now())
	if err != nil || base.RetrievalPolicy != policy {
		t.Fatalf("stored policy changed: %+v, %v", base.RetrievalPolicy, err)
	}
}

func TestRetrievalNoAnswerUsesEachBaseThreshold(t *testing.T) {
	hits := []domainknowledge.SearchHit{{KnowledgeBaseID: "legacy", Score: .04}, {KnowledgeBaseID: "configured", Score: .03}}
	if retrievalNoAnswer(hits, map[string]domainknowledge.RetrievalPolicy{"configured": {MinScore: .02}}) {
		t.Fatal("a configured base's answerable result was hidden by another base's legacy threshold")
	}
}
