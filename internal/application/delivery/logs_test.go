package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type deliveryLogRuntimeStub struct {
	queries     []domainresource.LogQuery
	failCluster string
}

func (s *deliveryLogRuntimeStub) QueryClusterLogs(_ context.Context, _ domainidentity.Principal, clusterID string, query domainresource.LogQuery) (domainresource.LogPage, error) {
	s.queries = append(s.queries, query)
	if clusterID == s.failCluster {
		return domainresource.LogPage{}, apperrors.ErrAccessDenied
	}
	return domainresource.LogPage{
		Entries:  []domainresource.LogEntry{{Timestamp: time.Unix(int64(len(s.queries)), 0), Message: clusterID, SourceMode: sohaapi.LogSourceModeRuntime, Source: domainresource.LogSource{Domain: sohaapi.LogSourceDomainKubernetes, ClusterID: clusterID}}},
		Coverage: &domainresource.LogCoverage{ResolvedSources: 1, SuccessfulSources: 1},
	}, nil
}

func (*deliveryLogRuntimeStub) AuthorizeClusterLogs(context.Context, domainidentity.Principal, string, domainresource.LogQuery, bool) error {
	return nil
}

func (*deliveryLogRuntimeStub) StreamClusterLogs(context.Context, domainidentity.Principal, string, domainresource.LogQuery, func(domainresource.LogStreamEvent) error) error {
	return nil
}

func TestQueryApplicationEnvironmentLogsClampsBindingScope(t *testing.T) {
	runtime := &deliveryLogRuntimeStub{}
	service := &Service{
		applications: stubApplicationReader{app: domainapp.App{ID: "app-1"}},
		catalog: stubCatalogReader{bindings: []domaincatalog.ApplicationEnvironment{{
			ID: "binding-1", ApplicationID: "app-1", EnvironmentKey: "prod",
			Targets: []domaincatalog.ReleaseTarget{
				{ClusterID: "cluster-a", Namespace: "payments", WorkloadKind: "Deployment", WorkloadName: "api", ContainerName: "server", Enabled: true},
				{ClusterID: "cluster-b", Namespace: "payments", WorkloadKind: "Deployment", WorkloadName: "worker", Enabled: false},
			},
		}}},
		logs: runtime,
	}

	page, err := service.QueryApplicationEnvironmentLogs(context.Background(), domainidentity.Principal{}, "app-1", "binding-1", domainresource.LogQuery{
		Selector: &domainresource.LogSourceSelector{AllContainers: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime.queries) != 1 {
		t.Fatalf("expected one enabled target query, got %d", len(runtime.queries))
	}
	selector := runtime.queries[0].Selector
	if selector.Namespace != "payments" || selector.WorkloadKind != "Deployment" || selector.WorkloadName != "api" || selector.AllContainers || len(selector.Containers) != 1 || selector.Containers[0] != "server" {
		t.Fatalf("binding scope was not enforced: %#v", selector)
	}
	if len(page.Entries) != 1 || page.Entries[0].Source.Domain != sohaapi.LogSourceDomainDelivery || page.Entries[0].Source.ApplicationID != "app-1" || page.Entries[0].Source.EnvironmentKey != "prod" {
		t.Fatalf("unexpected delivery source mapping: %#v", page.Entries)
	}
}

func TestQueryApplicationEnvironmentLogsReturnsNoPartialDataOnDeniedTarget(t *testing.T) {
	runtime := &deliveryLogRuntimeStub{failCluster: "cluster-b"}
	service := &Service{
		applications: stubApplicationReader{app: domainapp.App{ID: "app-1"}},
		catalog: stubCatalogReader{bindings: []domaincatalog.ApplicationEnvironment{{
			ID: "binding-1", ApplicationID: "app-1",
			Targets: []domaincatalog.ReleaseTarget{
				{ClusterID: "cluster-a", Namespace: "team-a", WorkloadKind: "Deployment", WorkloadName: "api", Enabled: true},
				{ClusterID: "cluster-b", Namespace: "team-b", WorkloadKind: "Deployment", WorkloadName: "api", Enabled: true},
			},
		}}},
		logs: runtime,
	}

	page, err := service.QueryApplicationEnvironmentLogs(context.Background(), domainidentity.Principal{}, "app-1", "binding-1", domainresource.LogQuery{Selector: &domainresource.LogSourceSelector{}})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("expected access denied, got %v", err)
	}
	if len(page.Entries) != 0 {
		t.Fatalf("denied aggregate must not expose partial entries: %#v", page.Entries)
	}
}
