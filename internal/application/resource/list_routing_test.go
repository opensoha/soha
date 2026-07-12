package resource

import (
	"context"
	"testing"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func TestListModeResourcesPreservesSourceScopeAndExistingActions(t *testing.T) {
	t.Parallel()

	audit := &workloadAuditRecorder{}
	access := &resourceAccess{
		resolver: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{
			ID: "direct-cluster", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig,
		}}},
		authorizer: scopedResourceAuthorizer{}, audit: audit,
	}
	items, err := listModeResources(context.Background(), access, domainidentity.Principal{UserID: "user-1"}, resourceListRequest{
		clusterID: "direct-cluster", namespace: "", kind: "Service",
		summary: func(source string) string { return "listed services via " + source },
	}, func(domaincluster.Connection) ([]domainresource.ServiceView, string, error) {
		return []domainresource.ServiceView{
			{Name: "keep", Namespace: "team-a", AllowedActions: []string{"view"}},
			{Name: "drop", Namespace: "team-b"},
		}, "cache", nil
	},
		func(item domainresource.ServiceView) string { return item.Namespace },
		func(item domainresource.ServiceView) []string { return item.AllowedActions },
		func(item *domainresource.ServiceView, actions []string) { item.AllowedActions = actions },
	)
	if err != nil {
		t.Fatalf("listModeResources() error = %v", err)
	}
	if len(items) != 1 || items[0].Name != "keep" {
		t.Fatalf("items = %#v, want scoped team-a item", items)
	}
	if len(items[0].AllowedActions) != 1 || items[0].AllowedActions[0] != "view" {
		t.Fatalf("AllowedActions = %#v, want existing actions preserved", items[0].AllowedActions)
	}
	if len(audit.entries) != 1 || audit.entries[0].Summary != "listed services via cache" {
		t.Fatalf("audit entries = %#v", audit.entries)
	}
}

type scopedResourceAuthorizer struct{}

func (scopedResourceAuthorizer) Authorize(context.Context, domainaccess.Request) (domainaccess.Decision, error) {
	return domainaccess.Decision{
		Allowed:        true,
		AllowedActions: []domainaccess.Action{domainaccess.ActionView, domainaccess.ActionList},
		ResourceScope:  &domainaccess.ResourceScope{Namespaces: []string{"team-a"}},
	}, nil
}
