package resource

import (
	"strings"
	"testing"

	domainresource "github.com/soha/soha/internal/domain/resource"
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

	if len(traces) != 4 {
		t.Fatalf("len(traces) = %d, want 4: %#v", len(traces), traces)
	}
	ingressAPI := findTopologyTrace(t, traces, "ingress", "service:team-a/api")
	if ingressAPI.State != "live" {
		t.Fatalf("ingress api state = %q, want live", ingressAPI.State)
	}
	if len(ingressAPI.BackendPods) != 2 {
		t.Fatalf("ingress api backend pods = %#v, want 2 pods", ingressAPI.BackendPods)
	}
	if ingressAPI.Entry.Name != "api.example.com" || ingressAPI.Route.Kind != "ingress-route" {
		t.Fatalf("ingress trace = %#v", ingressAPI)
	}

	ingressMissing := findTopologyTrace(t, traces, "ingress", "missing-service:team-a/missing")
	if ingressMissing.State != "pending" || ingressMissing.Service == nil || ingressMissing.Service.Kind != "missing-service" {
		t.Fatalf("missing service trace = %#v", ingressMissing)
	}

	httpRoute := findTopologyTrace(t, traces, "httproute", "service:team-a/api")
	if httpRoute.State != "live" || httpRoute.Entry.State != "live" || httpRoute.Route.Kind != "http-route" {
		t.Fatalf("httproute trace = %#v", httpRoute)
	}
	if httpRoute.Entry.Name != "gw.example.com" {
		t.Fatalf("httproute entry name = %q, want gw.example.com", httpRoute.Entry.Name)
	}

	pendingGateway := findTopologyTrace(t, traces, "gateway", "")
	if pendingGateway.State != "pending" || pendingGateway.Route.Kind != "pending-route" {
		t.Fatalf("pending gateway trace = %#v", pendingGateway)
	}
	if !strings.Contains(pendingGateway.Note, "no visible HTTPRoute") {
		t.Fatalf("pending gateway note = %q", pendingGateway.Note)
	}

	summary := summarizeNetworkTopologyTraces(traces)
	if summary.EntryCount != 3 || summary.RouteCount != 3 || summary.ServiceCount != 1 || summary.MissingServiceCount != 1 || summary.BackendPodCount != 2 || summary.PendingRouteCount != 1 {
		t.Fatalf("summary = %#v", summary)
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
