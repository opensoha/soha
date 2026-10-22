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
	if len(signatures) != 270 {
		t.Fatalf("route count = %d, want 270", len(signatures))
	}
	// M5 adds exactly the metadata relation RPC. Preserve the preceding route
	// family's digest as a second oracle, rather than accepting unrelated drift.
	prior := []string{}
	for _, signature := range signatures {
		if signature != http.MethodPost+" /api/v1/platform/resources/relations/snapshot" {
			prior = append(prior, signature)
		}
	}
	if fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(prior, "\n")))) != "d246061fec36ccb61f873a1a7d50bebbdb610fd54b292f1c45658f49347ba478" {
		t.Fatal("unrelated route family drift")
	}
	const expectedRouteDigest = "8f8d27b18cb7cf57f31fb5996afa6bd8fea326c89d55aa3873d19627387204d4"
	if routeDigest != expectedRouteDigest {
		t.Fatalf("route digest = %s, want %s", routeDigest, expectedRouteDigest)
	}

	expected := []string{
		http.MethodPost + " /api/v1/platform/resources/relations/snapshot",
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
