package agenticrag

import (
	"context"
	"fmt"
	"slices"
	"strings"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

type QueryPlan struct {
	Queries    []string `json:"queries"`
	StopReason string   `json:"stopReason,omitempty"`
}

type TraceStep struct {
	Round      int      `json:"round"`
	Queries    []string `json:"queries"`
	TraceRefs  []string `json:"traceRefs"`
	HitCount   int      `json:"hitCount"`
	NewSources int      `json:"newSources"`
}

type Result struct {
	Hits       []domainknowledge.SearchHit `json:"hits"`
	Citations  []domainknowledge.Citation  `json:"citations"`
	Steps      []TraceStep                 `json:"steps"`
	StopReason string                      `json:"stopReason"`
	NoAnswer   bool                        `json:"noAnswer"`
}

type Retriever interface {
	Search(context.Context, domainidentity.Principal, domainknowledge.SearchRequest) (domainknowledge.SearchResult, error)
}

type Planner interface {
	Plan(context.Context, string, int, []domainknowledge.Citation) (QueryPlan, error)
}

type Service struct {
	retriever Retriever
	planner   Planner
}

func NewService(retriever Retriever, planner Planner) (*Service, error) {
	if retriever == nil || planner == nil {
		return nil, fmt.Errorf("agentic rag retriever and planner are required")
	}
	return &Service{retriever: retriever, planner: planner}, nil
}

func (s *Service) Execute(ctx context.Context, principal domainidentity.Principal, baseIDs []string, goal string, maxRounds, topK int) (Result, error) {
	goal = strings.TrimSpace(goal)
	if goal == "" || len(baseIDs) == 0 || len(baseIDs) > 50 {
		return Result{}, fmt.Errorf("agentic rag goal and knowledge bases are required")
	}
	if maxRounds <= 0 || maxRounds > 5 {
		maxRounds = 3
	}
	if topK <= 0 || topK > 20 {
		topK = 8
	}
	result := Result{Hits: []domainknowledge.SearchHit{}, Citations: []domainknowledge.Citation{}, Steps: []TraceStep{}}
	seenChunks := map[string]struct{}{}
	seenSources := map[string]struct{}{}
	for round := 1; round <= maxRounds; round++ {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		plan, err := s.planner.Plan(ctx, goal, round, slices.Clone(result.Citations))
		if err != nil {
			return Result{}, fmt.Errorf("planning agentic rag round %d: %w", round, err)
		}
		plan.Queries = normalizeQueries(plan.Queries)
		if len(plan.Queries) == 0 {
			result.StopReason = firstNonEmpty(plan.StopReason, "planner_stopped")
			break
		}
		step := TraceStep{Round: round, Queries: slices.Clone(plan.Queries), TraceRefs: []string{}}
		for _, query := range plan.Queries {
			search, err := s.retriever.Search(ctx, principal, domainknowledge.SearchRequest{KnowledgeBaseIDs: slices.Clone(baseIDs), Query: query, TopK: topK})
			if err != nil {
				return Result{}, fmt.Errorf("retrieving agentic rag query: %w", err)
			}
			step.TraceRefs = append(step.TraceRefs, search.TraceID)
			for _, hit := range search.Hits {
				if _, duplicate := seenChunks[hit.ChunkID]; duplicate {
					continue
				}
				seenChunks[hit.ChunkID] = struct{}{}
				result.Hits = append(result.Hits, hit)
				result.Citations = append(result.Citations, hit.Citation)
				step.HitCount++
				if _, exists := seenSources[hit.DocumentID]; !exists {
					seenSources[hit.DocumentID] = struct{}{}
					step.NewSources++
				}
			}
		}
		result.Steps = append(result.Steps, step)
		if step.NewSources == 0 {
			result.StopReason = "no_progress"
			break
		}
		if plan.StopReason != "" {
			result.StopReason = plan.StopReason
			break
		}
	}
	if result.StopReason == "" {
		result.StopReason = "round_budget_exhausted"
	}
	result.NoAnswer = len(result.Hits) == 0
	return result, nil
}

func normalizeQueries(queries []string) []string {
	out := make([]string, 0, min(len(queries), 4))
	seen := map[string]struct{}{}
	for _, query := range queries {
		query = strings.TrimSpace(query)
		if query == "" || len(query) > 2_000 {
			continue
		}
		key := strings.ToLower(query)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, query)
		if len(out) == 4 {
			break
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
