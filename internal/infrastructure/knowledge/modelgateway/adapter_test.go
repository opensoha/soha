package modelgateway

import (
	"context"
	"fmt"
	"testing"

	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

type gatewayStub struct {
	dimensionMismatch bool
	missingReceipt    bool
}

func (g gatewayStub) InvokeKnowledgeModel(
	_ context.Context,
	_ domainidentity.Principal,
	request appaigateway.KnowledgeModelRequest,
) (appaigateway.KnowledgeModelResponse, error) {
	switch request.Endpoint {
	case "embeddings":
		body := `{"data":[{"index":0,"embedding":[1,0]},{"index":1,"embedding":[1,0]}]}`
		if g.dimensionMismatch {
			body = `{"data":[{"index":0,"embedding":[1,0]},{"index":1,"embedding":[1]}]}`
		}
		response := appaigateway.KnowledgeModelResponse{Body: []byte(body), PublicModel: "embed", RouteID: "fixture-route"}
		if !g.missingReceipt {
			response.UpstreamID, response.ProviderKind, response.UpstreamModel = "fixture-upstream", "openai", "actual-model"
		}
		return response, nil
	case "rerank":
		return appaigateway.KnowledgeModelResponse{Body: []byte(`{"results":[{"index":1,"relevance_score":0.9},{"index":0,"relevance_score":0.2}]}`)}, nil
	default:
		return appaigateway.KnowledgeModelResponse{}, fmt.Errorf("unexpected endpoint %q", request.Endpoint)
	}
}

func TestAdapterEmbeddingAndRerank(t *testing.T) {
	t.Parallel()
	adapter, err := New(gatewayStub{}, Config{EmbeddingModel: "embed", RerankModel: "rerank", Dimension: 2})
	if err != nil {
		t.Fatal(err)
	}
	batch, err := adapter.Embed(t.Context(), domainidentity.Principal{}, []string{"one", "two"})
	want := domainknowledge.EmbeddingSource{UpstreamID: "fixture-upstream", ProviderKind: "openai", UpstreamModel: "actual-model"}
	if err != nil || batch.Model != "embed" || batch.Route != "fixture-route" || batch.Source != want {
		t.Fatalf("embedding receipt = %+v, %v", batch, err)
	}
	scores, err := adapter.Score(context.Background(), domainidentity.Principal{}, "query", []domainknowledge.Chunk{{Content: "document"}})
	if err != nil || len(scores) != 1 || scores[0] != 1 {
		t.Fatalf("Score() = %v, %v", scores, err)
	}
	hits, err := adapter.Rerank(context.Background(), domainidentity.Principal{}, "query", []domainknowledge.SearchHit{
		{ChunkID: "first"},
		{ChunkID: "second"},
	})
	if err != nil || len(hits) != 2 || hits[0].ChunkID != "second" || hits[0].Score != 0.9 {
		t.Fatalf("Rerank() = %#v, %v", hits, err)
	}
}

func TestAdapterRejectsEmbeddingWithoutExecutionReceipt(t *testing.T) {
	t.Parallel()
	adapter, err := New(gatewayStub{missingReceipt: true}, Config{Dimension: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Embed(t.Context(), domainidentity.Principal{}, []string{"one", "two"}); err == nil {
		t.Fatal("Embed() accepted vectors without a source")
	}
}

func TestAdapterRejectsEmbeddingDimensionMismatch(t *testing.T) {
	t.Parallel()
	adapter, err := New(gatewayStub{dimensionMismatch: true}, Config{Dimension: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Score(context.Background(), domainidentity.Principal{}, "query", []domainknowledge.Chunk{{Content: "document"}}); err == nil {
		t.Fatal("Score() accepted mismatched embedding dimension")
	}
}
