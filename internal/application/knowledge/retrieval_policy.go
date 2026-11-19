package knowledge

import (
	"context"
	"sort"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

// Selected active bases share the minimum default and maximum; scoring stays per base.
func (s *Service) applyRetrievalPolicies(ctx context.Context, principal domainidentity.Principal, request *domainknowledge.SearchRequest) (int, error) {
	policies, err := s.repo.ListRetrievalPolicies(ctx, principalScope(principal), request.KnowledgeBaseIDs)
	if err != nil {
		return 0, err
	}
	selected := make(map[string]domainknowledge.RetrievalPolicy, len(policies))
	ids := make([]string, 0, len(policies))
	defaultTopK, maxTopK := 50, 50
	for _, id := range request.KnowledgeBaseIDs {
		policy, ok := policies[id]
		if !ok {
			continue
		}
		policy = domainknowledge.EffectiveRetrievalPolicy(policy)
		selected[id] = policy
		ids = append(ids, id)
		defaultTopK, maxTopK = min(defaultTopK, policy.DefaultTopK), min(maxTopK, policy.MaxTopK)
	}
	request.KnowledgeBaseIDs, request.Policies = ids, selected
	topK := request.TopK
	if topK <= 0 {
		topK = defaultTopK
	}
	return min(topK, maxTopK), nil
}

// The model can order tied hits; the policy score controls thresholds, citations and TopK.
func (s *Service) rerankRetrievalTies(ctx context.Context, principal domainidentity.Principal, query string, hits []domainknowledge.SearchHit) {
	if s.reranker == nil {
		return
	}
	var tied []domainknowledge.SearchHit
	for i, hit := range hits {
		tiesPrevious := i > 0 && hit.Score == hits[i-1].Score
		tiesNext := i+1 < len(hits) && hit.Score == hits[i+1].Score
		if tiesPrevious || tiesNext {
			tied = append(tied, hit)
		}
	}
	if len(tied) == 0 {
		return
	}
	reranked, err := s.reranker.Rerank(ctx, principal, query, tied)
	if err != nil {
		return
	}
	ranks := make(map[string]int, len(reranked))
	for i, hit := range reranked {
		if ranks[hit.ChunkID] == 0 {
			ranks[hit.ChunkID] = i + 1
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		left, right := ranks[hits[i].ChunkID], ranks[hits[j].ChunkID]
		return left > 0 && (right == 0 || left < right)
	})
}

func retrievalNoAnswer(hits []domainknowledge.SearchHit, policies map[string]domainknowledge.RetrievalPolicy) bool {
	for _, hit := range hits {
		minimum := policies[hit.KnowledgeBaseID].MinScore
		if minimum <= 0 {
			minimum = .05
		}
		if hit.Score >= minimum {
			return false
		}
	}
	return true
}
