package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"gorm.io/gorm"
)

func knowledgeRetrievalGroupedWindows(t *testing.T, repo *Repository, store *dbstore.Store, ctx context.Context) {
	query, args := captureKnowledgeSearchQuery(t, repo)
	t.Run("lexical_group_cap", func(t *testing.T) {
		high, heavy, reader := seedRetrievalDocument(t, repo, ctx, 600)
		low, light, _ := seedRetrievalDocument(t, repo, ctx, 1)
		republishRetrievalFixture(t, repo, ctx, high, heavy, "ancient deployment needle", []float64{0, 1, 0})
		republishRetrievalFixture(t, repo, ctx, low, light, "ancient instructions for deployment with a needle recorded in the operational runbook", nil)
		request := domainknowledge.SearchRequest{KnowledgeBaseIDs: []string{high.KnowledgeBaseID, low.KnowledgeBaseID}, Query: "ancient deployment needle",
			Policies: map[string]domainknowledge.RetrievalPolicy{high.KnowledgeBaseID: {VectorWeight: 1}, low.KnowledgeBaseID: {LexicalWeight: 1}}}
		batch := domainknowledge.EmbeddingBatch{Model: "fixture", Source: fixtureEmbeddingSource, Vectors: [][]float64{{1, 0, 0}}}
		rows, err := repo.SearchAuthorizedChunks(ctx, reader, request, batch, 10)
		if err != nil || len(rows) == 0 || rows[0].DocumentID != low.ID || math.Abs(rows[0].LexicalScore-61.0/561) > 1e-9 {
			t.Fatalf("bounded mixed lexical pool = %+v, %v", rows, err)
		}
		plan := explainGroupedKnowledge(t, ctx, store.DB(), *query, *args, false)
		lexical := requiredGroupPlan(t, plan, "CTE lexical_raw")
		if lexical.ActualRows != 501 || !groupPlanContains(lexical, func(n groupedKnowledgePlan) bool { return n.NodeType == "Limit" && n.ActualRows <= 500 }) || groupPlanContains(lexical, func(n groupedKnowledgePlan) bool { return n.NodeType == "WindowAgg" }) {
			t.Fatalf("lexical group cap is not before RRF: %+v", lexical)
		}
	})
	t.Run("no_vectors", func(t *testing.T) {
		first, a, reader := seedRetrievalDocument(t, repo, ctx, 12)
		second, b, _ := seedRetrievalDocument(t, repo, ctx, 1)
		republishRetrievalFixture(t, repo, ctx, first, a, "ancient deployment needle", nil)
		republishRetrievalFixture(t, repo, ctx, second, b, "ancient deployment needle", nil)
		request := domainknowledge.SearchRequest{KnowledgeBaseIDs: []string{first.KnowledgeBaseID, second.KnowledgeBaseID}, Query: "ancient deployment needle",
			Policies: map[string]domainknowledge.RetrievalPolicy{first.KnowledgeBaseID: {VectorWeight: 1}, second.KnowledgeBaseID: {LexicalWeight: 1}}}
		rows, err := repo.SearchAuthorizedChunks(ctx, reader, request, domainknowledge.EmbeddingBatch{}, 10)
		if err != nil || len(rows) != 10 {
			t.Fatalf("missing-vector fallback returned %d: %v", len(rows), err)
		}
		for _, row := range rows {
			if row.VectorAvailable || row.VectorScore != 0 || row.LexicalScore <= 0 {
				t.Fatalf("missing-vector group did not use lexical fallback: %+v", row)
			}
		}
		plan := explainGroupedKnowledge(t, ctx, store.DB(), *query, *args, false)
		if got := requiredGroupPlan(t, plan, "CTE lexical_raw").ActualRows; got != 10 {
			t.Fatalf("equal fallback weights changed the request window: %g", got)
		}
	})
	for _, dimension := range []int{1536, 3072} {
		t.Run(fmt.Sprintf("hnsw_%d", dimension), func(t *testing.T) {
			first, a, reader := seedRetrievalDocument(t, repo, ctx, 11)
			second, b, _ := seedRetrievalDocument(t, repo, ctx, 1)
			vector := make([]float64, dimension)
			vector[0] = 1
			republishRetrievalFixture(t, repo, ctx, first, a, "different topic", vector)
			republishRetrievalFixture(t, repo, ctx, second, b, "different topic", vector)
			request := domainknowledge.SearchRequest{KnowledgeBaseIDs: []string{first.KnowledgeBaseID, second.KnowledgeBaseID}, Query: "no-lexical-match",
				Policies: map[string]domainknowledge.RetrievalPolicy{first.KnowledgeBaseID: {LexicalWeight: .75, VectorWeight: .25}, second.KnowledgeBaseID: {LexicalWeight: .25, VectorWeight: .75}}}
			batch := domainknowledge.EmbeddingBatch{Model: "fixture", Source: fixtureEmbeddingSource, Vectors: [][]float64{vector}}
			withKnowledgeANN(t, store, ctx, func(indexed *Repository) {
				rows, err := indexed.SearchAuthorizedChunks(ctx, reader, request, batch, 10)
				if err != nil || len(rows) != 10 || rows[0].DocumentID != second.ID {
					t.Fatalf("weighted group result = %+v, %v", rows, err)
				}
				plan := explainGroupedKnowledge(t, ctx, indexed.db, *query, *args, true)
				assertGroupANNPlan(t, plan, dimension)
				request.Policies[first.KnowledgeBaseID] = domainknowledge.RetrievalPolicy{LexicalWeight: 1}
				rows, err = indexed.SearchAuthorizedChunks(ctx, reader, request, batch, 10)
				if err != nil || len(rows) != 10 || rows[0].DocumentID != second.ID {
					t.Fatalf("zero-weight group consumed positive global results: %+v, %v", rows, err)
				}
			})
		})
	}
}

type groupedKnowledgePlan struct {
	NodeType    string  `json:"Node Type"`
	SubplanName string  `json:"Subplan Name"`
	IndexName   string  `json:"Index Name"`
	OrderBy     string  `json:"Order By"`
	ActualRows  float64 `json:"Actual Rows"`
	ActualLoops float64 `json:"Actual Loops"`
	Plans       []groupedKnowledgePlan
}

func explainGroupedKnowledge(t *testing.T, ctx context.Context, db *gorm.DB, query string, args []any, forced bool) groupedKnowledgePlan {
	t.Helper()
	if query == "" {
		t.Fatal("actual hybrid search SQL was not captured")
	}
	var raw string
	if err := db.WithContext(ctx).Statement.ConnPool.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	t.Logf("optimizer_eligibility_forced=%t actual_hybrid_plan=%s", forced, raw)
	var plans []struct{ Plan groupedKnowledgePlan }
	if err := json.Unmarshal([]byte(raw), &plans); err != nil || len(plans) != 1 {
		t.Fatalf("invalid plan: %v", err)
	}
	return plans[0].Plan
}

func groupPlanContains(node groupedKnowledgePlan, match func(groupedKnowledgePlan) bool) bool {
	if match(node) {
		return true
	}
	for _, child := range node.Plans {
		if groupPlanContains(child, match) {
			return true
		}
	}
	return false
}

func requiredGroupPlan(t *testing.T, root groupedKnowledgePlan, name string) groupedKnowledgePlan {
	t.Helper()
	var result groupedKnowledgePlan
	if !groupPlanContains(root, func(n groupedKnowledgePlan) bool {
		if n.SubplanName != name {
			return false
		}
		result = n
		return true
	}) {
		t.Fatalf("missing plan branch %s", name)
	}
	return result
}

func assertGroupANNPlan(t *testing.T, root groupedKnowledgePlan, dimension int) {
	t.Helper()
	ann := requiredGroupPlan(t, root, "CTE vector_candidates")
	index := fmt.Sprintf("idx_knowledge_version_chunks_vector%d", dimension)
	if ann.ActualRows != 11 || !groupPlanContains(ann, func(n groupedKnowledgePlan) bool {
		return n.NodeType == "Limit" && n.ActualRows <= 10 && n.ActualLoops == 2 && groupPlanContains(n, func(child groupedKnowledgePlan) bool {
			return child.IndexName == index && strings.Contains(child.OrderBy, "<=>") && !strings.Contains(child.OrderBy, "weight")
		})
	}) {
		t.Fatalf("group KNN lacks its bounded pure-distance HNSW path: %+v", ann)
	}
	if got := requiredGroupPlan(t, root, "CTE vector_exact_candidates").ActualRows; got != 1 {
		t.Fatalf("exact fallback was not restricted to the short group: %g", got)
	}
}
