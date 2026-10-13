package connectors

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"github.com/opensoha/soha/internal/platform/apperrors"
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
	ref := "soha://secrets/" + uuid.NewString() + "/versions/2"
	result, err := fetcher.Fetch(t.Context(), FetchRequest{Kind: domainknowledge.SourceKindGit, Principal: domainidentity.Principal{UserID: "u-1"}, SecretRef: ref, Config: map[string]any{"repositoryUrl": "https://git.example.com/org/repo"}, AllowedHosts: []string{"git.example.com"}, DenyPrivateNetworks: true, MaxRedirects: 3, MaxItems: 100, MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if invoker.request.ToolName != "knowledge.connector.git.fetch" || invoker.request.SkillID != "knowledge-connector-operator" || len(result.Documents) != 1 || result.Cursor != "v2" {
		t.Fatalf("request=%#v result=%#v", invoker.request, result)
	}
	if invoker.request.SecretRefs["SOURCE"] != ref {
		t.Fatalf("source credential bypassed Gateway secret authorization")
	}
	if _, present := invoker.request.Input["secretRef"]; present {
		t.Fatal("credential reference included in ordinary plugin input")
	}
}

func TestGatewayFetcherDoesNotMapLegacyOrMalformedCredentials(t *testing.T) {
	for _, ref := range []string{"secret:git", "secret:team/git", "soha://secrets/git/versions/0", "soha://secrets/git/../foreign"} {
		t.Run(ref, func(t *testing.T) {
			invoker := &gatewayToolInvokerStub{}
			fetcher, err := NewGatewayFetcher(invoker)
			if err != nil {
				t.Fatal(err)
			}
			_, err = fetcher.Fetch(t.Context(), FetchRequest{Kind: domainknowledge.SourceKindGit, SecretRef: ref})
			if err == nil || invoker.request.ToolName != "" {
				t.Fatal("unsupported credential reached Gateway")
			}
			if secretRefPattern.MatchString(ref) && !errors.Is(err, apperrors.ErrUnsupportedOperation) {
				t.Fatalf("legacy reference refusal: %v", err)
			}
		})
	}
}
