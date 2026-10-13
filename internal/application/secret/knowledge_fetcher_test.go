package secret

import (
	"context"
	"errors"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	domainsecret "github.com/opensoha/soha/internal/domain/secret"
	"github.com/opensoha/soha/internal/infrastructure/knowledge/connectors"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type knowledgeSecretInvoker struct {
	service  *Service
	pinned   []domainsecret.Reference
	resolved bool
}

func (i *knowledgeSecretInvoker) InvokeTool(ctx context.Context, principal domainidentity.Principal, request domainaigateway.ToolInvocationRequest) (domainaigateway.ToolInvocationResult, error) {
	target := domainsecret.Target{Type: "capability", Ref: request.ToolName}
	pinned, err := i.service.PinReferences(ctx, principal, request.SecretRefs, target)
	if err != nil {
		return domainaigateway.ToolInvocationResult{}, err
	}
	values, err := i.service.ResolvePinnedReferences(ctx, principal, pinned, target)
	if err != nil {
		return domainaigateway.ToolInvocationResult{}, err
	}
	i.pinned, i.resolved = pinned, len(values) == 1
	return domainaigateway.ToolInvocationResult{Output: map[string]any{"documents": []any{}, "cursor": "v1"}}, nil
}

func TestKnowledgeFetcherUsesCurrentSecretAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name, actor string
		kind        domainknowledge.SourceKind
		versioned   bool
		revoked     bool
		denied      bool
	}{
		{name: "current", actor: "owner", kind: domainknowledge.SourceKindHTTP},
		{name: "pinned", actor: "owner", kind: domainknowledge.SourceKindHTTP, versioned: true},
		{name: "foreign-scope", actor: "foreign", kind: domainknowledge.SourceKindHTTP, denied: true},
		{name: "wrong-capability", actor: "owner", kind: domainknowledge.SourceKindGit, denied: true},
		{name: "revoked-version", actor: "owner", kind: domainknowledge.SourceKindHTTP, versioned: true, revoked: true, denied: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMemoryRepository()
			service, err := New(repo, appaccess.NewPermissionResolver(testRoleReader{"user": {appaccess.PermSecretCreate, appaccess.PermSecretUse}}), &captureAudit{}, discardOperations{}, testSecretKeyring(t), nil)
			if err != nil {
				t.Fatal(err)
			}
			owner := domainidentity.Principal{UserID: "owner", Roles: []string{"user"}, Projects: []string{"source-project"}}
			value := "test-only-source-value"
			created, err := service.Create(t.Context(), owner, domainsecret.CreateInput{Name: "source", Value: &value, ScopeType: domainsecret.ScopeProject, ScopeID: "source-project", Bindings: []domainsecret.Binding{{TargetType: "capability", TargetRef: "knowledge.connector.http.fetch"}}})
			if err != nil {
				t.Fatal(err)
			}
			ref := "soha://secrets/" + created.ID
			if tc.versioned {
				ref += "/versions/1"
			}
			if tc.revoked {
				version := repo.versions[created.ID][1]
				version.Status = domainsecret.VersionRevoked
				repo.versions[created.ID][1] = version
			}
			invoker := &knowledgeSecretInvoker{service: service}
			fetcher, err := connectors.NewGatewayFetcher(invoker)
			if err != nil {
				t.Fatal(err)
			}
			principal := domainidentity.Principal{UserID: tc.actor, Roles: []string{"user"}}
			if tc.actor == "owner" {
				principal.Projects = owner.Projects
			}
			_, err = fetcher.Fetch(t.Context(), connectors.FetchRequest{Kind: tc.kind, Principal: principal, SecretRef: ref})
			if tc.denied {
				if !errors.Is(err, apperrors.ErrNotFound) || invoker.resolved {
					t.Fatalf("unauthorized credential reached execution: %v", err)
				}
				return
			}
			if err != nil || !invoker.resolved || len(invoker.pinned) != 1 {
				t.Fatalf("credential authorization did not complete: %v", err)
			}
			if invoker.pinned[0].Alias != "SOURCE" || invoker.pinned[0].SecretID != created.ID || invoker.pinned[0].Version != 1 {
				t.Fatal("source alias, identity or version changed")
			}
		})
	}
}
