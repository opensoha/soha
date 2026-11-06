package api

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	k8sagent "github.com/opensoha/soha-agent/internal/agent/kubernetes"
	contractresource "github.com/opensoha/soha-contracts/resource"
)

func TestNewRegistersRouteFamilies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := New(
		cfgpkg.Config{HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"}},
		nil,
		&k8sagent.Client{},
		&fakeRuntimeController{},
	)
	router, ok := server.httpServer.Handler.(*gin.Engine)
	if !ok {
		t.Fatalf("handler type = %T, want *gin.Engine", server.httpServer.Handler)
	}
	registered := make(map[string]struct{}, len(router.Routes()))
	signatures := make([]string, 0, len(router.Routes()))
	observedMutations := map[string]struct{}{
		"DELETE /api/v1/platform/ownership-v2/resources/observed":              {},
		"POST /api/v1/platform/ownership-v2/resources/yaml/observed/preflight": {},
		"PUT /api/v1/platform/ownership-v2/resources/yaml/observed":            {},
		"PUT /api/v1/platform/helm/releases/:name/values/conditional":          {},
	}
	for _, route := range router.Routes() {
		signature := route.Method + " " + route.Path
		registered[signature] = struct{}{}
		if _, observed := observedMutations[signature]; observed {
			delete(observedMutations, signature)
			continue
		}
		if signature != http.MethodGet+" /api/v1/diagnostics/actions" && signature != http.MethodPost+" /api/v1/platform/network/topology/snapshot" && signature != http.MethodGet+" /api/v1/platform/infrastructure/resource-monitoring" {
			signatures = append(signatures, signature)
		}
	}
	if len(observedMutations) != 0 {
		t.Fatalf("missing guarded mutation routes: %v", observedMutations)
	}
	sort.Strings(signatures)
	const graphPage = http.MethodPost + " /api/v1/platform/extensions/custom-resources/graph-page"
	const graphObject = http.MethodPost + " /api/v1/platform" + contractresource.CustomResourceGraphObjectPath
	const graphProbe = http.MethodHead + " /api/v1/platform/resources/graph"
	beforeGraphPage := make([]string, 0, len(signatures))
	beforeGraphObject := make([]string, 0, len(signatures))
	baseline := make([]string, 0, len(signatures))
	for _, signature := range signatures {
		if signature == graphObject {
			continue
		}
		beforeGraphObject = append(beforeGraphObject, signature)
		if signature == graphPage {
			continue
		}
		beforeGraphPage = append(beforeGraphPage, signature)
		if signature != graphProbe {
			baseline = append(baseline, signature)
		}
	}
	if len(baseline) != 282 || fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(baseline, "\n")))) != "762dc83cf6723af593906534d7fbf648e3bbb57fe9e89dc46a716d8bff403cef" {
		t.Fatal("routes beyond the storage graph capability changed")
	}
	if len(beforeGraphPage) != 283 || fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(beforeGraphPage, "\n")))) != "2a6a069a13120f708f1b496841a4dafd08fb6305f183799e5e56a61ab4f424b3" {
		t.Fatal("routes beyond the custom resource graph page changed")
	}
	routeDigest := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(beforeGraphObject, "\n"))))
	if len(signatures) != 285 {
		t.Fatalf("route count = %d, want 285", len(signatures))
	}
	const expectedRouteDigest = "082f34bdfe52bc997817a9ce9c50eafe9729bc63567968818307a030c290155f"
	if routeDigest != expectedRouteDigest {
		t.Fatalf("route digest = %s, want %s", routeDigest, expectedRouteDigest)
	}

	expected := []string{
		graphProbe,
		graphPage,
		graphObject,
		http.MethodGet + " /api/v1/platform/ownership-v2/configuration/secrets/:name/certificate",
		http.MethodGet + " /api/v1/platform/workloads/daemonsets/:name/rollouts",
		http.MethodGet + " /api/v1/platform/workloads/daemonsets/:name/rollouts/:revisionName/yaml",
		http.MethodGet + " /api/v1/platform/ownership-v1/workloads/daemonsets/:name/rollouts",
		http.MethodGet + " /api/v1/platform/ownership-v1/workloads/daemonsets/:name/rollouts/:revisionName/yaml",
		http.MethodGet + " /api/v1/platform/ownership-v2/workloads/daemonsets/:name/rollouts",
		http.MethodGet + " /api/v1/platform/ownership-v2/workloads/daemonsets/:name/rollouts/:revisionName/yaml",
		http.MethodGet + " /api/v1/platform/workloads/statefulsets/:name/rollouts",
		http.MethodGet + " /api/v1/platform/workloads/statefulsets/:name/rollouts/:revisionName/yaml",
		http.MethodGet + " /api/v1/platform/ownership-v1/workloads/statefulsets/:name/rollouts",
		http.MethodGet + " /api/v1/platform/ownership-v1/workloads/statefulsets/:name/rollouts/:revisionName/yaml",
		http.MethodGet + " /api/v1/platform/ownership-v2/workloads/statefulsets/:name/rollouts",
		http.MethodGet + " /api/v1/platform/ownership-v2/workloads/statefulsets/:name/rollouts/:revisionName/yaml",
		http.MethodGet + " /api/v1/platform/infrastructure/resource-monitoring",
		http.MethodPost + " /api/v1/platform/network/topology/snapshot",
		http.MethodGet + " /api/v1/diagnostics/actions",
		http.MethodGet + " /api/v1/ssh/capability",
		http.MethodPost + " /api/v1/ssh/sessions",
		http.MethodGet + " /api/v1/ssh/sessions/:id/stream",
		http.MethodDelete + " /api/v1/platform/extensions/crds/:name",
		http.MethodPost + " /api/v1/platform/extensions/custom-resources/access",
		http.MethodPost + " /api/v1/platform/metrics/prometheus/query",
		http.MethodGet + " /api/v1/platform/ownership-v2/configuration/secrets/:name/detail",
		http.MethodPut + " /api/v1/platform/ownership-v2/configuration/configmaps/:name/data",
		http.MethodPost + " /api/v1/platform/ownership-v2/namespaces",
		http.MethodPost + " /api/v1/platform/ownership-v2/infrastructure/nodes/:name/drain",
		http.MethodPost + " /api/v1/platform/ownership-v2/workloads/cronjobs/:name/suspend",
		http.MethodPost + " /api/v1/platform/ownership-v1/resources/yaml/preflight",
		http.MethodPost + " /api/v1/platform/ownership-v2/resources/yaml/preflight",
		http.MethodPost + " /api/v1/platform/ownership-v2/manifests/rollout/observe",
		http.MethodPost + " /api/v1/platform/ownership-v2/manifests/rollout/control",
		http.MethodPost + " /api/v1/platform/extensions/custom-resources/delete-observed",
		http.MethodPost + " /api/v1/platform/helm/delivery/prepare",
		http.MethodPost + " /api/v1/platform/helm/delivery/preflight",
		http.MethodPost + " /api/v1/platform/helm/delivery/observe",
		http.MethodGet + " /healthz",
		http.MethodGet + " /api/v1/diagnostics",
		http.MethodGet + " /api/v1/platform/workloads/pods",
		http.MethodPost + " /api/v1/platform/logs/query",
		http.MethodPost + " /api/v1/platform/logs/stream",
		http.MethodGet + " /api/v1/platform/configuration/configmaps",
		http.MethodGet + " /api/v1/platform/access-control/roles",
		http.MethodPost + " /api/v1/platform/access-control/access-reviews",
		http.MethodGet + " /api/v1/platform/network/services",
		http.MethodGet + " /api/v1/platform/network/services/:name/detail",
		http.MethodPost + " /api/v1/platform/resources/yaml/preflight",
		http.MethodGet + " /api/v1/platform/resources/graph",
		http.MethodGet + " /api/v1/platform/resources/stream",
		http.MethodGet + " /api/v1/platform/security/posture",
		http.MethodGet + " /api/v1/platform/helm/releases/:name/manifest",
		http.MethodPost + " /api/v1/platform/helm/releases/:name/rollback/preflight",
		http.MethodPost + " /api/v1/platform/helm/releases/:name/rollback",
		http.MethodGet + " /api/v1/platform/storage/persistentvolumes",
		http.MethodGet + " /api/v1/platform/helm/releases",
		http.MethodGet + " /api/v1/runtime/execution-tasks",
		http.MethodPost + " /api/v1/docker/runtime/logs",
	}
	for _, route := range expected {
		if _, ok := registered[route]; !ok {
			t.Errorf("route %q is not registered", route)
		}
	}
}
