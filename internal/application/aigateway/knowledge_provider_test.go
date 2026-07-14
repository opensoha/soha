package aigateway

import (
	"context"
	"testing"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

type knowledgeSearchStub struct {
	principal domainidentity.Principal
	request   domainknowledge.SearchRequest
}

func (s *knowledgeSearchStub) Search(_ context.Context, principal domainidentity.Principal, request domainknowledge.SearchRequest) (domainknowledge.SearchResult, error) {
	s.principal, s.request = principal, request
	return domainknowledge.SearchResult{Query: request.Query, TraceID: "trace-1"}, nil
}

func TestKnowledgeCapabilityProviderInvokesScopedSearch(t *testing.T) {
	t.Parallel()
	search := &knowledgeSearchStub{}
	provider := NewKnowledgeCapabilityProvider(search)
	tools := provider.Tools()
	if len(tools) != 1 || tools[0].Name != knowledgeSearchToolName {
		t.Fatalf("tools = %#v", tools)
	}
	invoker, ok := provider.(ToolCapabilityInvoker)
	if !ok {
		t.Fatal("knowledge provider does not implement tool invocation")
	}
	principal := domainidentity.Principal{UserID: "user-1"}
	output, related, err := invoker.InvokeTool(context.Background(), principal, tools[0], map[string]any{
		"knowledgeBaseIds": []any{"kb-1"},
		"query":            "deployment failure",
		"topK":             7,
		"filters":          map[string]any{"sourceIds": []any{"source-1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, ok := output.(domainknowledge.SearchResult)
	if !ok || result.TraceID != "trace-1" {
		t.Fatalf("output = %#v", output)
	}
	if search.principal.UserID != principal.UserID || search.request.TopK != 7 || len(search.request.Filters.SourceIDs) != 1 {
		t.Fatalf("captured search = %#v principal=%#v", search.request, search.principal)
	}
	if related["traceId"] != "trace-1" {
		t.Fatalf("related = %#v", related)
	}
}
