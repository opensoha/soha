package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

func (r *Repository) ListRetrievalPolicies(ctx context.Context, p domainknowledge.PrincipalScope, ids []string) (map[string]domainknowledge.RetrievalPolicy, error) {
	policies := make(map[string]domainknowledge.RetrievalPolicy, len(ids))
	if len(ids) == 0 {
		return policies, nil
	}
	clause, args := accessClause("b", p)
	selected, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	query := `SELECT b.id,b.retrieval_policy FROM ai_knowledge_bases b WHERE b.status='active' AND b.deleted_at IS NULL AND (` + clause + `) AND b.id IN (SELECT jsonb_array_elements_text(?::jsonb))`
	args = append(args, string(selected))
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("read knowledge retrieval policies: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		var raw []byte
		var policy domainknowledge.RetrievalPolicy
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		if err := unmarshal(raw, &policy); err != nil {
			return nil, err
		}
		policies[id] = policy
	}
	return policies, rows.Err()
}

func retrievalPolicyValues(policies map[string]domainknowledge.RetrievalPolicy) (string, []any, error) {
	type policyRow struct {
		ID            string  `json:"knowledge_base_id"`
		LexicalWeight float64 `json:"lexical_weight"`
		VectorWeight  float64 `json:"vector_weight"`
		MinScore      float64 `json:"min_score"`
	}
	ids := make([]string, 0, len(policies))
	for id := range policies {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	rows := make([]policyRow, 0, len(ids))
	for _, id := range ids {
		policy := policies[id]
		rows = append(rows, policyRow{id, policy.LexicalWeight, policy.VectorWeight, policy.MinScore})
	}
	encoded, err := json.Marshal(rows)
	return `SELECT * FROM jsonb_to_recordset(?::jsonb) AS p(knowledge_base_id text,lexical_weight float8,vector_weight float8,min_score float8)`, []any{string(encoded)}, err
}

func (r *Repository) retrievalPolicyQuery(ctx context.Context, p domainknowledge.PrincipalScope, request domainknowledge.SearchRequest) (string, []any, error) {
	policies := request.Policies
	if policies == nil {
		var err error
		policies, err = r.ListRetrievalPolicies(ctx, p, request.KnowledgeBaseIDs)
		if err != nil {
			return "", nil, err
		}
		for id, policy := range policies {
			policies[id] = domainknowledge.EffectiveRetrievalPolicy(policy)
		}
	}
	return retrievalPolicyValues(policies)
}

func knowledgeVectorAvailability(batch domainknowledge.EmbeddingBatch) (string, []any) {
	if len(batch.Vectors) != 1 || !batch.Source.Known() {
		return `SELECT ''::text AS knowledge_base_id WHERE false`, nil
	}
	return `SELECT p.knowledge_base_id FROM retrieval_policies p WHERE EXISTS (SELECT 1 FROM authorized_current c WHERE c.knowledge_base_id=p.knowledge_base_id AND c.embedding IS NOT NULL AND c.embedding_dimension=? AND c.embedding_model=? AND c.embedding_route=? AND c.embedding_upstream_id=? AND c.embedding_provider_kind=? AND c.embedding_upstream_model=?)`,
		[]any{len(batch.Vectors[0]), batch.Model, batch.Route, batch.Source.UpstreamID, batch.Source.ProviderKind, batch.Source.UpstreamModel}
}
