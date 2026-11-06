package resource

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

type ownedPodMetricsDirect struct {
	DirectWorkloads
	calls []string
	pods  []domainresource.PodView
}

func (d *ownedPodMetricsDirect) GetJobDetail(_ context.Context, _, namespace, name string) (domainresource.JobDetailView, error) {
	d.calls = append(d.calls, "Job")
	return domainresource.JobDetailView{Name: name, Namespace: namespace, Pods: d.pods}, nil
}

func (d *ownedPodMetricsDirect) GetReplicaSetDetail(_ context.Context, _, namespace, name string) (domainresource.ReplicaSetDetailView, error) {
	d.calls = append(d.calls, "ReplicaSet")
	return domainresource.ReplicaSetDetailView{Name: name, Namespace: namespace, Pods: d.pods}, nil
}

func (d *ownedPodMetricsDirect) GetReplicationControllerDetail(_ context.Context, _, namespace, name string) (domainresource.ReplicationControllerDetailView, error) {
	d.calls = append(d.calls, "ReplicationController")
	return domainresource.ReplicationControllerDetailView{Name: name, Namespace: namespace, Pods: d.pods}, nil
}

func TestOwnedWorkloadMetricsUseAuthorizedDetailAndDistinguishMissingPods(t *testing.T) {
	for _, tc := range []struct {
		kind string
		get  func(*Workloads, context.Context, domainidentity.Principal, string, string, string, int, int) (domainresource.ResourceMetricsView, error)
	}{
		{"Job", (*Workloads).GetJobMetrics},
		{"ReplicaSet", (*Workloads).GetReplicaSetMetrics},
		{"ReplicationController", (*Workloads).GetReplicationControllerMetrics},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			var queries []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				queries = append(queries, r.URL.Query().Get("query"))
				_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": []any{map[string]any{"values": [][]any{{float64(1713225600), "0.25"}}}}}})
			}))
			defer server.Close()

			service := workloadRoutingTestService(&workloadAuditRecorder{}, nil)
			resolver := stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "direct-cluster", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig}, Metadata: map[string]any{"prometheus_url": server.URL}}}
			service.resourceAccess.resolver = resolver
			service.metricsSupport = &metricsSupport{resourceAccess: service.resourceAccess, resolver: resolver, httpClient: server.Client()}
			direct := &ownedPodMetricsDirect{pods: []domainresource.PodView{{Name: "owned-1", Namespace: "team-a"}, {Name: "foreign-pod", Namespace: "other"}}}
			service.direct = direct
			principal := domainidentity.Principal{UserID: "user-1"}

			view, err := tc.get(service, t.Context(), principal, "direct-cluster", "team-a", "job", 15, 60)
			if err != nil || !view.Configured || view.ResourceKind != tc.kind || len(view.Series) == 0 || !slices.Equal(direct.calls, []string{tc.kind}) {
				t.Fatalf("metrics failed: view=%#v detail calls=%v err=%v", view, direct.calls, err)
			}
			for _, q := range queries {
				if !strings.Contains(q, "owned-1") || strings.Contains(q, "foreign-pod") {
					t.Fatalf("metrics escaped %s Pod scope: %s", tc.kind, q)
				}
			}

			queries = nil
			direct.pods = nil
			direct.calls = nil
			view, err = tc.get(service, t.Context(), principal, "direct-cluster", "team-a", "job", 15, 60)
			if err != nil || !view.Configured || !strings.Contains(view.Message, "no matching pods") || len(queries) != 0 {
				t.Fatalf("missing Pods confused with missing monitoring: %#v %v", view, err)
			}
			if !slices.Equal(direct.calls, []string{tc.kind}) {
				t.Fatalf("missing Pods checked the wrong detail: %v", direct.calls)
			}

			service.authorizer = &statefulHistoryAuthorizer{allowed: false}
			direct.calls = nil
			if _, err = tc.get(service, t.Context(), principal, "direct-cluster", "team-a", "job", 15, 60); err == nil || len(direct.calls) != 0 {
				t.Fatalf("denied %s metrics reached provider", tc.kind)
			}
		})
	}
}
