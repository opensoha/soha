package connectors

import (
	"context"
	"testing"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

type gatewayToolInvokerStub struct {
	request domainaigateway.ToolInvocationRequest
}

func (s *gatewayToolInvokerStub) InvokeTool(_ context.Context, _ domainidentity.Principal, request domainaigateway.ToolInvocationRequest) (domainaigateway.ToolInvocationResult, error) {
	s.request = request
	return domainaigateway.ToolInvocationResult{Output: map[string]any{"documents": []any{map[string]any{"externalId": "doc", "content": "bounded"}}, "cursor": "v2"}}, nil
}

func TestGatewayFetcherRoutesThroughFixedPluginCapability(t *testing.T) {
	invoker := &gatewayToolInvokerStub{}
	fetcher, err := NewGatewayFetcher(invoker)
	if err != nil {
		t.Fatal(err)
	}
	result, err := fetcher.Fetch(t.Context(), FetchRequest{Kind: domainknowledge.SourceKindGit, Principal: domainidentity.Principal{UserID: "u-1"}, SecretRef: "secret:git", Config: map[string]any{"repositoryUrl": "https://git.example.com/org/repo"}, AllowedHosts: []string{"git.example.com"}, DenyPrivateNetworks: true, MaxRedirects: 3, MaxItems: 100, MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if invoker.request.ToolName != "knowledge.connector.git.fetch" || invoker.request.SkillID != "knowledge-connector-operator" || len(result.Documents) != 1 || result.Cursor != "v2" {
		t.Fatalf("request=%#v result=%#v", invoker.request, result)
	}
	if invoker.request.Input["secretRef"] != "secret:git" {
		t.Fatalf("secret ref not preserved: %#v", invoker.request.Input)
	}
}
