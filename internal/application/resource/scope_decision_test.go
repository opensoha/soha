package resource

import (
	"context"
	"testing"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

type scopeDecisionAuthorizer struct {
	decision domainaccess.Decision
	request  domainaccess.Request
}

type namespaceLabelResolverStub struct {
	labels map[string]string
	calls  int
}

func (s *namespaceLabelResolverStub) Resolve(context.Context, domaincluster.Connection, string) (map[string]string, error) {
	s.calls++
	return s.labels, nil
}

type namespaceLabelAuthorizer struct{}

func (namespaceLabelAuthorizer) Authorize(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	allowed := request.Namespace.Labels["tenant"] == "retail"
	return domainaccess.Decision{Allowed: allowed, Reason: "namespace selector", AllowedActions: []domainaccess.Action{domainaccess.ActionCreate}}, nil
}

func (s *scopeDecisionAuthorizer) Authorize(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	s.request = request
	return s.decision, nil
}

func TestDecideCreateScopeReturnsActionScopeAndDirectCapability(t *testing.T) {
	authorizer := &scopeDecisionAuthorizer{decision: domainaccess.Decision{
		Allowed: true, Reason: "allowed", AllowedActions: []domainaccess.Action{domainaccess.ActionCreate},
		ResourceScope: &domainaccess.ResourceScope{
			Clusters: []string{"cluster-a"}, Namespaces: []string{"minio"},
			ResourceGroups: []string{"configuration"}, ResourceKinds: []string{"ConfigMap"},
		},
	}}
	creation := &ResourceCreation{resourceAccess: &resourceAccess{
		resolver: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{
			ID: "cluster-a", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig,
		}}},
		authorizer: authorizer,
	}}
	result, err := creation.DecideCreateScope(context.Background(), domainidentity.Principal{UserID: "user-1"}, "cluster-a", domainresource.ResourceCreateScopeDecisionRequest{
		Namespace: "minio", ResourceGroup: "configuration", APIVersion: "v1", Kind: "ConfigMap",
	})
	if err != nil {
		t.Fatalf("DecideCreateScope() error = %v", err)
	}
	if !result.Allowed || result.Capability.Status != "available" || result.Capability.Mode != "direct" {
		t.Fatalf("result = %#v", result)
	}
	if authorizer.request.Action != domainaccess.ActionCreate || authorizer.request.Resource.Group != "configuration" || authorizer.request.Namespace.Namespace != "minio" {
		t.Fatalf("authorization request = %#v", authorizer.request)
	}
}

func TestDecideCreateScopeDistinguishesDeniedAuthorizationFromPublishedAgentCapability(t *testing.T) {
	tests := []struct {
		name         string
		mode         domaincluster.ConnectionMode
		capabilities []string
		decision     domainaccess.Decision
		wantStatus   string
		wantAllow    bool
	}{
		{name: "permission denied", mode: domaincluster.ConnectionModeDirectKubeconfig, decision: domainaccess.Decision{Allowed: false, Reason: "scope denied"}, wantStatus: "available", wantAllow: false},
		{name: "agent capability missing", mode: domaincluster.ConnectionModeAgent, decision: domainaccess.Decision{Allowed: true, AllowedActions: []domainaccess.Action{domainaccess.ActionCreate}}, wantStatus: "unsupported", wantAllow: true},
		{name: "agent capability published", mode: domaincluster.ConnectionModeAgent, capabilities: []string{"resource.creation"}, decision: domainaccess.Decision{Allowed: true, AllowedActions: []domainaccess.Action{domainaccess.ActionCreate}}, wantStatus: "available", wantAllow: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creation := &ResourceCreation{resourceAccess: &resourceAccess{
				resolver:   stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a", ConnectionMode: tt.mode, Capabilities: tt.capabilities}}},
				authorizer: &scopeDecisionAuthorizer{decision: tt.decision},
			}}
			result, err := creation.DecideCreateScope(context.Background(), domainidentity.Principal{UserID: "user-1"}, "cluster-a", domainresource.ResourceCreateScopeDecisionRequest{
				ResourceGroup: "workloads", Kind: "Deployment",
			})
			if err != nil {
				t.Fatalf("DecideCreateScope() error = %v", err)
			}
			if result.Allowed != tt.wantAllow || result.Capability.Status != tt.wantStatus {
				t.Fatalf("result = %#v, want allowed=%v capability=%s", result, tt.wantAllow, tt.wantStatus)
			}
		})
	}
}

func TestDecideCreateScopeResolvesTrustedNamespaceLabelsBeforeFinalDenial(t *testing.T) {
	labels := &namespaceLabelResolverStub{labels: map[string]string{"tenant": "retail"}}
	creation := &ResourceCreation{resourceAccess: &resourceAccess{
		resolver: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{
			ID: "cluster-a", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig,
		}}},
		authorizer: namespaceLabelAuthorizer{}, namespaceLabels: labels,
	}}
	result, err := creation.DecideCreateScope(context.Background(), domainidentity.Principal{UserID: "user-1"}, "cluster-a", domainresource.ResourceCreateScopeDecisionRequest{
		Namespace: "minio", ResourceGroup: "configuration", Kind: "ConfigMap",
	})
	if err != nil {
		t.Fatalf("DecideCreateScope() error = %v", err)
	}
	if !result.Allowed || labels.calls != 1 {
		t.Fatalf("result = %#v, resolver calls=%d", result, labels.calls)
	}
}
