package resource

import (
	"strings"
	"testing"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func TestBuildNetworkTopologyTracesAggregatesRoutesServicesAndBackends(t *testing.T) {
	t.Parallel()

	traces := buildNetworkTopologyTraces(
		[]domainresource.ServiceView{
			{Name: "api", Namespace: "team-a", Selector: map[string]string{"app": "api"}},
			{Name: "ui", Namespace: "team-a", Selector: map[string]string{"app": "ui"}},
		},
		[]domainresource.IngressView{
			{Name: "edge", Namespace: "team-a", ClassName: "nginx", Hosts: []string{"api.example.com"}, BackendServices: []string{"api", "missing"}},
		},
		[]domainresource.HTTPRouteView{
			{Name: "api-route", Namespace: "team-a", Hostnames: []string{"gw.example.com"}, ParentRefs: []string{"team-a/gw"}, BackendServices: []string{"api"}},
		},
		[]domainresource.GatewayView{
			{Name: "gw", Namespace: "team-a", GatewayClass: "kong", Addresses: []string{"203.0.113.10"}},
			{Name: "lonely", Namespace: "team-a", GatewayClass: "kong", Addresses: []string{"203.0.113.11"}},
		},
		[]domainresource.PodView{
			{Name: "api-1", Namespace: "team-a", Labels: map[string]string{"app": "api"}},
			{Name: "api-2", Namespace: "team-a", Labels: map[string]string{"app": "api"}},
			{Name: "other-1", Namespace: "team-a", Labels: map[string]string{"app": "other"}},
		},
	)

	expectTopology(t, len(traces) == 4, "len(traces) = %d, want 4: %#v", len(traces), traces)
	ingressAPI := findTopologyTrace(t, traces, "ingress", "service:team-a/api")
	expectTopology(t, ingressAPI.State == "live", "ingress api state = %q", ingressAPI.State)
	expectTopology(t, len(ingressAPI.BackendPods) == 2, "ingress api backend pods = %#v", ingressAPI.BackendPods)
	expectTopology(t, ingressAPI.Entry.Name == "api.example.com", "ingress entry = %#v", ingressAPI.Entry)
	expectTopology(t, ingressAPI.Route.Kind == "ingress-route", "ingress route = %#v", ingressAPI.Route)

	ingressMissing := findTopologyTrace(t, traces, "ingress", "missing-service:team-a/missing")
	expectTopology(t, ingressMissing.State == "pending", "missing service state = %#v", ingressMissing)
	expectTopology(t, ingressMissing.Service != nil, "missing service node absent: %#v", ingressMissing)
	expectTopology(t, ingressMissing.Service.Kind == "missing-service", "missing service kind = %#v", ingressMissing.Service)

	httpRoute := findTopologyTrace(t, traces, "httproute", "service:team-a/api")
	expectTopology(t, httpRoute.State == "live", "httproute state = %#v", httpRoute)
	expectTopology(t, httpRoute.Entry.State == "live", "httproute entry state = %#v", httpRoute)
	expectTopology(t, httpRoute.Route.Kind == "http-route", "httproute kind = %#v", httpRoute)
	expectTopology(t, httpRoute.Entry.Name == "gw.example.com", "httproute entry = %q", httpRoute.Entry.Name)

	pendingGateway := findTopologyTrace(t, traces, "gateway", "")
	expectTopology(t, pendingGateway.State == "pending", "pending gateway state = %#v", pendingGateway)
	expectTopology(t, pendingGateway.Route.Kind == "pending-route", "pending gateway route = %#v", pendingGateway)
	expectTopology(t, strings.Contains(pendingGateway.Note, "no visible HTTPRoute"), "pending gateway note = %q", pendingGateway.Note)

	summary := summarizeNetworkTopologyTraces(traces)
	expectTopology(t, summary.EntryCount == 3, "entry summary = %#v", summary)
	expectTopology(t, summary.RouteCount == 3, "route summary = %#v", summary)
	expectTopology(t, summary.ServiceCount == 1, "service summary = %#v", summary)
	expectTopology(t, summary.MissingServiceCount == 1, "missing service summary = %#v", summary)
	expectTopology(t, summary.BackendPodCount == 2, "pod summary = %#v", summary)
	expectTopology(t, summary.PendingRouteCount == 1, "pending route summary = %#v", summary)
}

func expectTopology(t *testing.T, condition bool, format string, args ...any) {
	t.Helper()
	if !condition {
		t.Fatalf(format, args...)
	}
}

func TestBuildNetworkTopologyTracesMarksInvisibleHTTPRouteParent(t *testing.T) {
	t.Parallel()

	traces := buildNetworkTopologyTraces(
		[]domainresource.ServiceView{{Name: "api", Namespace: "team-a", Selector: map[string]string{"app": "api"}}},
		nil,
		[]domainresource.HTTPRouteView{
			{Name: "api-route", Namespace: "team-a", ParentRefs: []string{"team-a/missing-gateway"}, BackendServices: []string{"api"}},
		},
		nil,
		[]domainresource.PodView{{Name: "api-1", Namespace: "team-a", Labels: map[string]string{"app": "api"}}},
	)

	trace := findTopologyTrace(t, traces, "httproute", "service:team-a/api")
	if trace.State != "pending" || trace.Entry.State != "pending" {
		t.Fatalf("trace state = %s/%s, want pending/pending: %#v", trace.State, trace.Entry.State, trace)
	}
	if !strings.Contains(trace.Note, "parent Gateway is not visible") {
		t.Fatalf("trace note = %q", trace.Note)
	}
	if len(trace.BackendPods) != 1 {
		t.Fatalf("backend pods = %#v, want one resolved pod", trace.BackendPods)
	}
}

func findTopologyTrace(t *testing.T, traces []domainresource.NetworkTopologyTraceView, sourceType, serviceID string) domainresource.NetworkTopologyTraceView {
	t.Helper()
	for _, trace := range traces {
		if trace.SourceType != sourceType {
			continue
		}
		if serviceID == "" {
			return trace
		}
		if trace.Service != nil && trace.Service.ID == serviceID {
			return trace
		}
	}
	t.Fatalf("missing trace sourceType=%s serviceID=%s in %#v", sourceType, serviceID, traces)
	return domainresource.NetworkTopologyTraceView{}
}
