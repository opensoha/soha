package modelgateway

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/google/uuid"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

const maxModelBatch = 256

type Gateway interface {
	InvokeKnowledgeModel(context.Context, domainidentity.Principal, appaigateway.KnowledgeModelRequest) (appaigateway.KnowledgeModelResponse, error)
}

type Config struct {
	EmbeddingModel string
	EmbeddingRoute string
	RerankModel    string
	RerankRoute    string
	Dimension      int
}

type Adapter struct {
	gateway Gateway
	config  Config
}

func New(gateway Gateway, config Config) (*Adapter, error) {
	if gateway == nil {
		return nil, fmt.Errorf("knowledge model gateway is required")
	}
	if config.Dimension < 0 || config.Dimension > 4096 {
		return nil, fmt.Errorf("knowledge embedding dimension must be between 0 and 4096")
	}
	return &Adapter{gateway: gateway, config: config}, nil
}

func (a *Adapter) Score(
	ctx context.Context,
	principal domainidentity.Principal,
	query string,
	chunks []domainknowledge.Chunk,
) ([]float64, error) {
	if len(chunks) == 0 {
		return []float64{}, nil
	}
	if len(chunks)+1 > maxModelBatch {
		return nil, fmt.Errorf("knowledge embedding batch exceeds %d", maxModelBatch)
	}
	inputs := make([]string, 0, len(chunks)+1)
	inputs = append(inputs, strings.TrimSpace(query))
	for _, chunk := range chunks {
		inputs = append(inputs, chunk.Content)
	}
	response, err := a.gateway.InvokeKnowledgeModel(ctx, principal, appaigateway.KnowledgeModelRequest{
		PublicModel: a.config.EmbeddingModel,
		RouteID:     a.config.EmbeddingRoute,
		Endpoint:    "embeddings",
		Payload:     map[string]any{"input": inputs},
		RequestID:   uuid.NewString(),
	})
	if err != nil {
		return nil, fmt.Errorf("invoke knowledge embedding route: %w", err)
	}
	vectors, err := decodeEmbeddings(response.Body, len(inputs), a.config.Dimension)
	if err != nil {
		return nil, err
	}
	scores := make([]float64, len(chunks))
	for index := range chunks {
		scores[index] = cosine(vectors[0], vectors[index+1])
	}
	return scores, nil
}

func (a *Adapter) Rerank(
	ctx context.Context,
	principal domainidentity.Principal,
	query string,
	hits []domainknowledge.SearchHit,
) ([]domainknowledge.SearchHit, error) {
	if len(hits) == 0 {
		return []domainknowledge.SearchHit{}, nil
	}
	if len(hits) > maxModelBatch {
		return nil, fmt.Errorf("knowledge rerank batch exceeds %d", maxModelBatch)
	}
	documents := make([]string, 0, len(hits))
	for _, hit := range hits {
		documents = append(documents, hit.Content)
	}
	response, err := a.gateway.InvokeKnowledgeModel(ctx, principal, appaigateway.KnowledgeModelRequest{
		PublicModel: a.config.RerankModel,
		RouteID:     a.config.RerankRoute,
		Endpoint:    "rerank",
		Payload: map[string]any{
			"query":     strings.TrimSpace(query),
			"documents": documents,
			"top_n":     len(hits),
		},
		RequestID: uuid.NewString(),
	})
	if err != nil {
		return nil, fmt.Errorf("invoke knowledge rerank route: %w", err)
	}
	ranked, err := decodeRerank(response.Body, hits)
	if err != nil {
		return nil, err
	}
	return ranked, nil
}

func decodeEmbeddings(body []byte, expected, dimension int) ([][]float64, error) {
	var payload struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode knowledge embeddings: %w", err)
	}
	if len(payload.Data) != expected {
		return nil, fmt.Errorf("knowledge embedding count mismatch: got %d want %d", len(payload.Data), expected)
	}
	vectors := make([][]float64, expected)
	actualDimension := dimension
	for _, item := range payload.Data {
		if item.Index < 0 || item.Index >= expected || len(item.Embedding) == 0 || vectors[item.Index] != nil {
			return nil, fmt.Errorf("knowledge embedding index is invalid")
		}
		if actualDimension == 0 {
			actualDimension = len(item.Embedding)
		}
		if len(item.Embedding) != actualDimension {
			return nil, fmt.Errorf("knowledge embedding dimension mismatch: got %d want %d", len(item.Embedding), actualDimension)
		}
		vectors[item.Index] = append([]float64{}, item.Embedding...)
	}
	return vectors, nil
}

func decodeRerank(body []byte, hits []domainknowledge.SearchHit) ([]domainknowledge.SearchHit, error) {
	var payload struct {
		Results []struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
			Score          float64 `json:"score"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode knowledge rerank response: %w", err)
	}
	if len(payload.Results) == 0 || len(payload.Results) > len(hits) {
		return nil, fmt.Errorf("knowledge rerank result count is invalid")
	}
	ranked := make([]domainknowledge.SearchHit, 0, len(payload.Results))
	seen := make(map[int]struct{}, len(payload.Results))
	for _, item := range payload.Results {
		if item.Index < 0 || item.Index >= len(hits) {
			return nil, fmt.Errorf("knowledge rerank index is invalid")
		}
		if _, exists := seen[item.Index]; exists {
			return nil, fmt.Errorf("knowledge rerank index is duplicated")
		}
		seen[item.Index] = struct{}{}
		hit := hits[item.Index]
		score := item.RelevanceScore
		if score == 0 {
			score = item.Score
		}
		hit.Score = score
		hit.Citation.Score = score
		ranked = append(ranked, hit)
	}
	sort.SliceStable(ranked, func(left, right int) bool { return ranked[left].Score > ranked[right].Score })
	return ranked, nil
}

func cosine(left, right []float64) float64 {
	var dot, leftNorm, rightNorm float64
	for index := range left {
		dot += left[index] * right[index]
		leftNorm += left[index] * left[index]
		rightNorm += right[index] * right[index]
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	return dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
}
