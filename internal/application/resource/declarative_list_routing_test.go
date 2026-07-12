package resource

import (
	"context"
	"testing"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func TestConfigurationListCapabilityPreservesScopeActionsAndAudit(t *testing.T) {
	t.Parallel()

	audit := &declarativeAuditRecorder{}
	configuration := &Configuration{
		resourceAccess: declarativeResourceAccess(audit),
		direct: configMapDirectStub{items: []domainresource.ConfigMapView{
			{Name: "visible", Namespace: "team-a"},
			{Name: "hidden", Namespace: "team-b"},
		}},
	}
	items, err := configuration.ListConfigMaps(
		context.Background(),
		domainidentity.Principal{UserID: "user-1"},
		"cluster-1",
		"",
	)
	if err != nil {
		t.Fatalf("ListConfigMaps() error = %v", err)
	}
	if len(items) != 1 || items[0].Name != "visible" || len(items[0].AllowedActions) == 0 {
		t.Fatalf("items = %#v, want one scoped item with allowed actions", items)
	}
	if len(audit.entries) != 1 || audit.entries[0].Summary != "listed configmaps via live in namespace all-namespaces" {
		t.Fatalf("audit entries = %#v", audit.entries)
	}
}

func TestGatewayListCapabilityPreservesDirectSourceAndAudit(t *testing.T) {
	t.Parallel()

	audit := &declarativeAuditRecorder{}
	network := &Network{
		resourceAccess: declarativeResourceAccess(audit),
		gatewayReader:  gatewayClassDirectStub{items: []domainresource.GatewayClassView{{Name: "standard"}}},
	}
	items, err := network.ListGatewayClasses(
		context.Background(),
		domainidentity.Principal{UserID: "user-1"},
		"cluster-1",
	)
	if err != nil {
		t.Fatalf("ListGatewayClasses() error = %v", err)
	}
	if len(items) != 1 || len(items[0].AllowedActions) == 0 {
		t.Fatalf("items = %#v, want allowed actions", items)
	}
	if len(audit.entries) != 1 || audit.entries[0].Summary != "listed gatewayclasses via live" {
		t.Fatalf("audit entries = %#v", audit.entries)
	}
}

func declarativeResourceAccess(audit AuditRecorder) *resourceAccess {
	return &resourceAccess{
		resolver: declarativeConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{
			ID:             "cluster-1",
			ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig,
		}}},
		authorizer: declarativeAuthorizer{},
		audit:      audit,
	}
}

type declarativeConnectionResolver struct {
	connection domaincluster.Connection
}

func (r declarativeConnectionResolver) GetConnection(context.Context, string) (domaincluster.Connection, error) {
	return r.connection, nil
}

type declarativeAuthorizer struct{}

func (declarativeAuthorizer) Authorize(context.Context, domainaccess.Request) (domainaccess.Decision, error) {
	return domainaccess.Decision{
		Allowed:        true,
		AllowedActions: []domainaccess.Action{domainaccess.ActionList, domainaccess.ActionView},
		ResourceScope:  &domainaccess.ResourceScope{Namespaces: []string{"team-a"}},
	}, nil
}

type declarativeAuditRecorder struct {
	entries []domainaudit.Entry
}

func (r *declarativeAuditRecorder) Record(_ context.Context, entry domainaudit.Entry) error {
	r.entries = append(r.entries, entry)
	return nil
}

type configMapDirectStub struct {
	DirectConfiguration
	items []domainresource.ConfigMapView
}

func (s configMapDirectStub) ListConfigMaps(context.Context, string, string) ([]domainresource.ConfigMapView, error) {
	return s.items, nil
}

type gatewayClassDirectStub struct {
	DirectGatewayReader
	items []domainresource.GatewayClassView
}

func (s gatewayClassDirectStub) ListGatewayClasses(context.Context, string) ([]domainresource.GatewayClassView, error) {
	return s.items, nil
}
