package resource

import (
	"context"
	"encoding/json"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type replicaSetMetricsDirect struct {
	DirectWorkloads
	calls int
	pods  []domainresource.PodView
}

func (d *replicaSetMetricsDirect) GetReplicaSetDetail(_ context.Context, cluster, namespace, name string) (domainresource.ReplicaSetDetailView, error) {
	d.calls++
	return domainresource.ReplicaSetDetailView{Name: name, Namespace: namespace, Pods: d.pods}, nil
}
func TestReplicaSetMetricsUsesAuthorizedOwnedDetailAndDistinguishesMissingPods(t *testing.T) {
	queries := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query().Get("query"))
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": []any{map[string]any{"values": [][]any{{float64(1713225600), "0.25"}}}}}})
	}))
	defer server.Close()
	service := workloadRoutingTestService(&workloadAuditRecorder{}, nil)
	resolver := stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "direct-cluster", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig}, Metadata: map[string]any{"prometheus_url": server.URL}}}
	service.resourceAccess.resolver = resolver
	service.metricsSupport = &metricsSupport{resourceAccess: service.resourceAccess, resolver: resolver, httpClient: server.Client()}
	direct := &replicaSetMetricsDirect{pods: []domainresource.PodView{{Name: "owned-1", Namespace: "team-a"}, {Name: "foreign-pod", Namespace: "other"}}}
	service.direct = direct
	view, err := service.GetReplicaSetMetrics(t.Context(), domainidentity.Principal{UserID: "user-1"}, "direct-cluster", "team-a", "job", 15, 60)
	if err != nil || !view.Configured || view.ResourceKind != "ReplicaSet" || len(view.Series) == 0 || direct.calls != 1 {
		t.Fatalf("metrics failed: %#v %v", view, err)
	}
	for _, q := range queries {
		if !strings.Contains(q, "owned-1") || strings.Contains(q, "foreign-pod") {
			t.Fatalf("metrics escaped ReplicaSet Pod scope: %s", q)
		}
	}
	queries = nil
	direct.pods = nil
	view, err = service.GetReplicaSetMetrics(t.Context(), domainidentity.Principal{UserID: "user-1"}, "direct-cluster", "team-a", "job", 15, 60)
	if err != nil || !view.Configured || !strings.Contains(view.Message, "no matching pods") || len(queries) != 0 {
		t.Fatalf("missing Pods confused with missing monitoring: %#v %v", view, err)
	}
	service.authorizer = &statefulHistoryAuthorizer{allowed: false}
	direct.calls = 0
	if _, err = service.GetReplicaSetMetrics(t.Context(), domainidentity.Principal{UserID: "user-1"}, "direct-cluster", "team-a", "job", 15, 60); err == nil || direct.calls != 0 {
		t.Fatal("denied ReplicaSet metrics reached provider")
	}
}
