package knowledge

import "math"

// EffectiveRetrievalPolicy bounds legacy values and makes cross-base scores comparable.
func EffectiveRetrievalPolicy(policy RetrievalPolicy) RetrievalPolicy {
	if policy.MaxTopK <= 0 {
		policy.MaxTopK = 20
	}
	policy.MaxTopK = min(policy.MaxTopK, 50)
	if policy.DefaultTopK <= 0 {
		policy.DefaultTopK = 5
	}
	policy.DefaultTopK = min(policy.DefaultTopK, policy.MaxTopK)
	policy.LexicalWeight = boundedRetrievalWeight(policy.LexicalWeight)
	policy.VectorWeight = boundedRetrievalWeight(policy.VectorWeight)
	if total := policy.LexicalWeight + policy.VectorWeight; total > 0 {
		policy.LexicalWeight /= total
		policy.VectorWeight /= total
	} else {
		policy.LexicalWeight, policy.VectorWeight = 0.45, 0.55
	}
	policy.MinScore = boundedRetrievalWeight(policy.MinScore)
	return policy
}

func boundedRetrievalWeight(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return min(max(value, 0), 1)
}
