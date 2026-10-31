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
	for _, route := range router.Routes() {
		signature := route.Method + " " + route.Path
		registered[signature] = struct{}{}
		if signature != http.MethodGet+" /api/v1/diagnostics/actions" && signature != http.MethodPost+" /api/v1/platform/network/topology/snapshot" && signature != http.MethodGet+" /api/v1/platform/infrastructure/resource-monitoring" {
			signatures = append(signatures, signature)
		}
	}
	sort.Strings(signatures)
	routeDigest := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(signatures, "\n"))))
	if len(signatures) != 285 {
		t.Fatalf("route count = %d, want 285", len(signatures))
	}
	// Only these eleven reviewed protocol routes are new. Preserve the complete
	// previous route digest after removing exactly this addition.
	newRoutes := map[string]bool{
		"GET /api/v1/docker/runtime/v1/terminal/capabilities":          true,
		"GET /api/v1/docker/runtime/v1/terminal/stream":                true,
		"GET /api/v1/platform/resource-presence-v1/resources":          true,
		"POST /api/v1/docker/runtime/v1/files/list":                    true,
		"POST /api/v1/docker/runtime/v1/files/read":                    true,
		"POST /api/v1/docker/runtime/v1/logs":                          true,
		"POST /api/v1/docker/runtime/v1/volumes":                       true,
		"GET /api/v1/platform/pod-execution-v1/pods/:podName/terminal": true,
		"GET /api/v1/platform/pod-execution-v1/capabilities":           true,
		"GET /api/v1/platform/pod-execution-v1/pods/:podName/target":   true,
		"POST /api/v1/platform/pod-execution-v1/pods/:podName/argv":    true,
	}
	previous := make([]string, 0, 274)
	for _, signature := range signatures {
		if !newRoutes[signature] {
			previous = append(previous, signature)
		}
	}
	if len(previous) != 274 {
		t.Fatalf("previous route count changed: %d", len(previous))
	}
	previousDigest := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(previous, "\n"))))
	const expectedRouteDigest = "33de9d7b7f814ffb6133f1076daa398a16a5f32d25b471ac7b3e18835f4da205"
	if previousDigest != expectedRouteDigest {
		t.Fatalf("previous route digest = %s, want %s (new surface %s)", previousDigest, expectedRouteDigest, routeDigest)
	}

	expected := []string{
		"GET /api/v1/docker/runtime/v1/terminal/capabilities",
		"GET /api/v1/docker/runtime/v1/terminal/stream",
		"GET /api/v1/platform/resource-presence-v1/resources",
		"POST /api/v1/docker/runtime/v1/files/list",
		"POST /api/v1/docker/runtime/v1/files/read",
		"POST /api/v1/docker/runtime/v1/logs",
		"POST /api/v1/docker/runtime/v1/volumes",
		"GET /api/v1/platform/pod-execution-v1/pods/:podName/terminal",
		"GET /api/v1/platform/pod-execution-v1/capabilities",
		"GET /api/v1/platform/pod-execution-v1/pods/:podName/target",
		"POST /api/v1/platform/pod-execution-v1/pods/:podName/argv",
		http.MethodPost + " /api/v1/platform/preconditions-v1/resources/:kind/:name/actions/:action",
		http.MethodGet + " /api/v1/platform/preconditions-v1/resources/identity",
		http.MethodPost + " /api/v1/platform/preconditions-v1/resources/yaml/preflight",
		http.MethodPut + " /api/v1/platform/preconditions-v1/resources/yaml",
		http.MethodDelete + " /api/v1/platform/preconditions-v1/resources",
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
