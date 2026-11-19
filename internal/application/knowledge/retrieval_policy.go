package knowledge

import (
	"context"

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

func filterRetrievalHits(hits []domainknowledge.SearchHit, policies map[string]domainknowledge.RetrievalPolicy) []domainknowledge.SearchHit {
	filtered := hits[:0]
	for _, hit := range hits {
		if policy, ok := policies[hit.KnowledgeBaseID]; ok && hit.Score >= policy.MinScore {
			filtered = append(filtered, hit)
		}
	}
	return filtered
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
