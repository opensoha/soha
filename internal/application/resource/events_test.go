package resource

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	agentinfra "github.com/opensoha/soha/internal/infrastructure/agent"
)

func TestListClusterEventsFiltersScopeBeforeLimitForDirectCluster(t *testing.T) {
	t.Parallel()

	direct := &stubDirectEventReader{source: "live", items: scopedEventFixture()}
	service := New(Dependencies{
		Connections: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{
			ID: "direct-cluster", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig,
		}}},
		Authorizer: scopedResourceAuthorizer{}, Audit: discardAuditRecorder{}, DirectEvents: direct,
	})
	items, err := service.Events().ListClusterEvents(context.Background(), domainidentity.Principal{UserID: "user-1"}, "direct-cluster", "", 1)
	if err != nil {
		t.Fatalf("ListClusterEvents() error = %v", err)
	}
	if direct.namespace != "team-a" || direct.limit != 1 {
		t.Fatalf("backend query = namespace %q limit %d, want authorized namespace team-a and limit 1", direct.namespace, direct.limit)
	}
	assertNewestScopedEvent(t, items)
}

func TestListClusterEventsFiltersScopeBeforeLimitForAgentCluster(t *testing.T) {
	t.Parallel()

	requestedQuery := url.Values{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedQuery = r.URL.Query()
		_ = json.NewEncoder(w).Encode(map[string]any{"items": scopedEventFixture()})
	}))
	defer server.Close()

	registry := agentinfra.NewRegistry(time.Second)
	service := New(Dependencies{
		Connections: stubConnectionResolver{connection: agentConnection(server.URL)},
		Authorizer:  scopedResourceAuthorizer{}, Audit: discardAuditRecorder{}, Agents: testAgentClients(registry),
	})
	items, err := service.Events().ListClusterEvents(context.Background(), domainidentity.Principal{UserID: "user-1"}, "agent-cluster", "", 1)
	if err != nil {
		t.Fatalf("ListClusterEvents() error = %v", err)
	}
	if requestedQuery.Get("namespace") != "team-a" || requestedQuery.Get("limit") != "1" {
		t.Fatalf("agent query = %q, want authorized namespace team-a and limit 1", requestedQuery.Encode())
	}
	assertNewestScopedEvent(t, items)
}

func TestListClusterEventsDisablesBackendLimitForMultiNamespaceScope(t *testing.T) {
	t.Parallel()

	direct := &stubDirectEventReader{source: "live", items: scopedEventFixture()}
	service := New(Dependencies{
		Connections: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{
			ID: "direct-cluster", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig,
		}}},
		Authorizer: eventScopeAuthorizer{scope: domainaccess.ResourceScope{Namespaces: []string{"team-a", "team-c"}}},
		Audit:      discardAuditRecorder{}, DirectEvents: direct,
	})
	items, err := service.Events().ListClusterEvents(context.Background(), domainidentity.Principal{UserID: "user-1"}, "direct-cluster", "", 1)
	if err != nil {
		t.Fatalf("ListClusterEvents() error = %v", err)
	}
	if direct.namespace != "" || direct.limit != 0 {
		t.Fatalf("backend query = namespace %q limit %d, want all namespaces without early limit", direct.namespace, direct.limit)
	}
	assertNewestScopedEvent(t, items)
}

func scopedEventFixture() []domainresource.ClusterEventView {
	return []domainresource.ClusterEventView{
		{Name: "hidden-newest", Namespace: "team-b", LastTimestamp: "2026-07-18T12:00:00Z"},
		{Name: "visible-older", Namespace: "team-a", LastTimestamp: "2026-07-18T10:00:00Z"},
		{Name: "visible-newest", Namespace: "team-a", LastTimestamp: "2026-07-18T11:00:00Z"},
	}
}

func assertNewestScopedEvent(t *testing.T, items []domainresource.ClusterEventView) {
	t.Helper()
	if len(items) != 1 || items[0].Name != "visible-newest" {
		t.Fatalf("items = %#v, want newest authorized event", items)
	}
}

type eventScopeAuthorizer struct{ scope domainaccess.ResourceScope }

func (a eventScopeAuthorizer) Authorize(context.Context, domainaccess.Request) (domainaccess.Decision, error) {
	return domainaccess.Decision{Allowed: true, ResourceScope: &a.scope}, nil
}
