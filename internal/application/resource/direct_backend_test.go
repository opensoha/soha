package resource

import (
	"context"
	"io"
	"testing"

	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func TestDirectCapabilitiesDelegateToTechnologyFreePorts(t *testing.T) {
	t.Parallel()
	connection := domaincluster.Connection{
		Summary: domaincluster.Summary{
			ID: "direct-cluster", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig,
		},
	}
	events := &stubDirectEventReader{
		items:  []domainresource.ClusterEventView{{Name: "scheduled", Namespace: "platform"}},
		source: "cache",
	}
	helm := &stubDirectHelmReleaseReader{
		items: []domainresource.HelmReleaseView{{Name: "gateway", Namespace: "platform"}},
	}
	pods := &stubDirectPods{
		items:  []domainresource.PodView{{Name: "api-0", Namespace: "platform"}},
		source: "cache",
	}
	service := New(Dependencies{
		Connections:  stubConnectionResolver{connection: connection},
		Authorizer:   allowAllResourceAuthorizer{},
		Audit:        discardAuditRecorder{},
		DirectEvents: events,
		DirectHelm:   helm,
		DirectPods:   pods,
	})
	principal := domainidentity.Principal{UserID: "user-1"}

	eventItems, err := service.Events().ListClusterEvents(context.Background(), principal, "direct-cluster", "platform", 10)
	if err != nil {
		t.Fatalf("ListClusterEvents() error = %v", err)
	}
	if len(eventItems) != 1 || eventItems[0].Name != "scheduled" {
		t.Fatalf("ListClusterEvents() = %#v", eventItems)
	}
	if events.clusterID != "direct-cluster" || events.namespace != "platform" || events.limit != 10 {
		t.Fatalf("direct event request = cluster %q namespace %q limit %d", events.clusterID, events.namespace, events.limit)
	}

	releases, err := service.Helm().ListHelmReleases(context.Background(), principal, "direct-cluster", "platform")
	if err != nil {
		t.Fatalf("ListHelmReleases() error = %v", err)
	}
	if len(releases) != 1 || releases[0].Name != "gateway" {
		t.Fatalf("ListHelmReleases() = %#v", releases)
	}
	if helm.clusterID != "direct-cluster" || helm.namespace != "platform" {
		t.Fatalf("direct helm request = cluster %q namespace %q", helm.clusterID, helm.namespace)
	}

	podItems, err := service.Workloads().ListPods(context.Background(), principal, "direct-cluster", "platform")
	if err != nil {
		t.Fatalf("ListPods() error = %v", err)
	}
	if len(podItems) != 1 || podItems[0].Name != "api-0" {
		t.Fatalf("ListPods() = %#v", podItems)
	}
	if pods.clusterID != "direct-cluster" || pods.namespace != "platform" {
		t.Fatalf("direct pod request = cluster %q namespace %q", pods.clusterID, pods.namespace)
	}
}

type stubDirectEventReader struct {
	clusterID string
	namespace string
	limit     int
	items     []domainresource.ClusterEventView
	source    string
}

func (s *stubDirectEventReader) ListClusterEvents(_ context.Context, clusterID, namespace string, limit int) ([]domainresource.ClusterEventView, string, error) {
	s.clusterID = clusterID
	s.namespace = namespace
	s.limit = limit
	return s.items, s.source, nil
}

type stubDirectHelmReleaseReader struct {
	clusterID string
	namespace string
	items     []domainresource.HelmReleaseView
}

type stubDirectPods struct {
	clusterID string
	namespace string
	selector  map[string]string
	items     []domainresource.PodView
	source    string
}

func (s *stubDirectPods) ListPodsBySelector(_ context.Context, clusterID, namespace string, selector map[string]string) ([]domainresource.PodView, error) {
	s.clusterID = clusterID
	s.namespace = namespace
	s.selector = selector
	return s.items, nil
}

func TestDirectPodRouteUsesSelectorScopedCapability(t *testing.T) {
	t.Parallel()
	backend := &stubDirectPods{items: []domainresource.PodView{{Name: "api-0"}}}
	route := directPodRoute{backend: backend, clusterID: "cluster-a"}

	items, err := route.ListPodsBySelector(context.Background(), "team-a", map[string]string{"app": "api"})
	if err != nil {
		t.Fatalf("ListPodsBySelector() error = %v", err)
	}
	if len(items) != 1 || backend.clusterID != "cluster-a" || backend.namespace != "team-a" || backend.selector["app"] != "api" {
		t.Fatalf("selector request = items %#v cluster %q namespace %q selector %#v", items, backend.clusterID, backend.namespace, backend.selector)
	}
}

func (s *stubDirectPods) ListPods(_ context.Context, clusterID, namespace string) ([]domainresource.PodView, string, error) {
	s.clusterID = clusterID
	s.namespace = namespace
	return s.items, s.source, nil
}

func (*stubDirectPods) GetPodDetail(context.Context, string, string, string) (domainresource.PodDetailView, error) {
	return domainresource.PodDetailView{}, nil
}

func (*stubDirectPods) GetPodLogs(context.Context, string, string, string, string, int64, int64, bool) (domainresource.PodLogsView, error) {
	return domainresource.PodLogsView{}, nil
}

func (*stubDirectPods) GetPodYAML(context.Context, string, string, string) (domainresource.ResourceYAMLView, error) {
	return domainresource.ResourceYAMLView{}, nil
}

func (*stubDirectPods) DeletePod(context.Context, string, string, string) error {
	return nil
}

func (*stubDirectPods) StreamPodLogs(context.Context, string, string, string, string, int64, int64, io.Writer) error {
	return nil
}

func (*stubDirectPods) ExecPod(context.Context, string, string, string, string, string, int64) (domainresource.PodExecView, error) {
	return domainresource.PodExecView{}, nil
}

func (*stubDirectPods) StreamPodTerminal(context.Context, string, string, string, string, string, io.Reader, io.Writer, io.Writer, domainresource.TerminalSizeQueue) error {
	return nil
}

func (s *stubDirectHelmReleaseReader) ListHelmReleases(_ context.Context, clusterID, namespace string) ([]domainresource.HelmReleaseView, error) {
	s.clusterID = clusterID
	s.namespace = namespace
	return s.items, nil
}

func (*stubDirectHelmReleaseReader) GetHelmReleaseDetail(context.Context, string, string, string) (domainresource.HelmReleaseDetailView, error) {
	return domainresource.HelmReleaseDetailView{}, nil
}

func (*stubDirectHelmReleaseReader) ListHelmReleaseHistory(context.Context, string, string, string) ([]domainresource.HelmReleaseHistoryView, error) {
	return nil, nil
}

func (*stubDirectHelmReleaseReader) GetHelmReleaseValues(context.Context, string, string, string, string) (domainresource.HelmValuesView, error) {
	return domainresource.HelmValuesView{}, nil
}

func (*stubDirectHelmReleaseReader) InstallHelmChart(context.Context, string, domainresource.HelmChartInstallInput) (domainresource.HelmChartInstallResult, error) {
	return domainresource.HelmChartInstallResult{}, nil
}

func (*stubDirectHelmReleaseReader) UpdateHelmReleaseValues(context.Context, string, string, string, string) (domainresource.HelmValuesView, error) {
	return domainresource.HelmValuesView{}, nil
}

func (*stubDirectHelmReleaseReader) DeleteHelmRelease(context.Context, string, string, string) error {
	return nil
}

type discardAuditRecorder struct{}

func (discardAuditRecorder) Record(context.Context, domainaudit.Entry) error {
	return nil
}
