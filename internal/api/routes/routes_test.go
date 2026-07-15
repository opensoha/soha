package routes

import (
	"slices"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	apiHandlers "github.com/opensoha/soha/internal/api/handlers"
	accesshandler "github.com/opensoha/soha/internal/api/handlers/access"
	providerportalhandler "github.com/opensoha/soha/internal/api/handlers/providerportal"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
)

func routeMethodPaths(routes []gin.RouteInfo) []string {
	items := make([]string, 0, len(routes))
	for _, route := range routes {
		items = append(items, route.Method+" "+route.Path)
	}
	return items
}

func TestRegisterPlatformRoutesKeepsCoreOperationalSurface(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	group := router.Group("/api/v1")
	registerPlatformRoutes(group, Dependencies{Platform: &apiHandlers.PlatformHandler{}})

	registered := make(map[string]struct{})
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = struct{}{}
	}

	for _, route := range []string{
		"GET /api/v1/clusters/capabilities",
		"GET /api/v1/clusters/:clusterID/workloads/pods",
		"PUT /api/v1/clusters/:clusterID/workloads/pods/:podName/yaml",
		"POST /api/v1/clusters/:clusterID/workloads/deployments/restart",
		"POST /api/v1/clusters/:clusterID/workloads/statefulsets/restart",
		"POST /api/v1/clusters/:clusterID/workloads/statefulsets/scale",
		"POST /api/v1/clusters/:clusterID/workloads/daemonsets/restart",
		"DELETE /api/v1/clusters/:clusterID/workloads/replicasets/:replicaSetName",
		"GET /api/v1/clusters/:clusterID/workloads/replicasets/:replicaSetName/yaml",
		"PUT /api/v1/clusters/:clusterID/workloads/replicasets/:replicaSetName/yaml",
		"GET /api/v1/clusters/:clusterID/network/services",
		"GET /api/v1/clusters/:clusterID/network/topology",
		"GET /api/v1/clusters/:clusterID/network/gatewayclasses",
		"GET /api/v1/clusters/:clusterID/network/gateways",
		"GET /api/v1/clusters/:clusterID/network/httproutes",
		"GET /api/v1/clusters/:clusterID/network/backendtlspolicies",
		"GET /api/v1/clusters/:clusterID/network/grpcroutes",
		"GET /api/v1/clusters/:clusterID/network/referencegrants",
		"GET /api/v1/clusters/:clusterID/helm/releases/:releaseName/values",
		"POST /api/v1/clusters/:clusterID/helm/charts/install",
		"GET /api/v1/clusters/:clusterID/extensions/crds/:crdName/resources",
	} {
		if _, ok := registered[route]; !ok {
			t.Fatalf("missing route %s", route)
		}
	}

	for _, route := range []string{
		"GET /api/v1/clusters/:clusterID/network/httproutes/:name/yaml",
		"PUT /api/v1/clusters/:clusterID/network/httproutes/:name/yaml",
		"DELETE /api/v1/clusters/:clusterID/network/httproutes/:name",
		"GET /api/v1/clusters/:clusterID/network/backendtlspolicies/:name/yaml",
		"PUT /api/v1/clusters/:clusterID/network/backendtlspolicies/:name/yaml",
		"DELETE /api/v1/clusters/:clusterID/network/backendtlspolicies/:name",
		"GET /api/v1/clusters/:clusterID/network/grpcroutes/:name/yaml",
		"PUT /api/v1/clusters/:clusterID/network/grpcroutes/:name/yaml",
		"DELETE /api/v1/clusters/:clusterID/network/grpcroutes/:name",
		"GET /api/v1/clusters/:clusterID/network/referencegrants/:name/yaml",
		"PUT /api/v1/clusters/:clusterID/network/referencegrants/:name/yaml",
		"DELETE /api/v1/clusters/:clusterID/network/referencegrants/:name",
	} {
		if _, ok := registered[route]; ok {
			t.Fatalf("raw Gateway API route should not be exposed: %s", route)
		}
	}
}

func TestRegisterCopilotRoutesExposesAgentRunCancel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	group := router.Group("/api/v1")
	registerCopilotRoutes(group, cfgpkg.Config{Modules: cfgpkg.ModulesConfig{AI: cfgpkg.ModuleToggleConfig{Enabled: true}}}, Dependencies{Copilot: &apiHandlers.CopilotHandler{}})

	registered := make(map[string]struct{})
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = struct{}{}
	}
	for _, route := range []string{
		"POST /api/v1/copilot/agent-runs/:runID/cancel",
		"POST /api/v1/copilot/sessions/:sessionID/messages/stream",
	} {
		if _, ok := registered[route]; !ok {
			t.Fatalf("missing route %s", route)
		}
	}
}

func TestRegisterComputeRoutesFollowsModuleAvailability(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name string
		cfg  cfgpkg.Config
		want int
	}{
		{name: "disabled", cfg: cfgpkg.Config{}, want: 0},
		{name: "virtualization", cfg: cfgpkg.Config{Modules: cfgpkg.ModulesConfig{Virtualization: cfgpkg.ModuleToggleConfig{Enabled: true}}}, want: 6},
		{name: "docker", cfg: cfgpkg.Config{Modules: cfgpkg.ModulesConfig{Docker: cfgpkg.ModuleToggleConfig{Enabled: true}}}, want: 6},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			group := router.Group("/api/v1")
			registerComputeRoutes(group, test.cfg, Dependencies{Compute: &apiHandlers.ComputeHandler{}})
			count := 0
			for _, route := range router.Routes() {
				if strings.HasPrefix(route.Path, "/api/v1/compute") {
					count++
				}
			}
			if count != test.want {
				t.Fatalf("compute routes = %d, want %d", count, test.want)
			}
		})
	}
}

func TestRegisterKnowledgeRoutesExposesKnowledgeAndContextSurface(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	group := router.Group("/api/v1")
	apiHandlers.RegisterKnowledgeRoutes(group, &apiHandlers.KnowledgeHandler{})

	registered := make(map[string]struct{})
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = struct{}{}
	}
	for _, route := range []string{
		"GET /api/v1/ai/knowledge-bases",
		"POST /api/v1/ai/knowledge-bases",
		"GET /api/v1/ai/knowledge-bases/:baseID",
		"PATCH /api/v1/ai/knowledge-bases/:baseID",
		"DELETE /api/v1/ai/knowledge-bases/:baseID",
		"GET /api/v1/ai/knowledge-bases/:baseID/sources",
		"POST /api/v1/ai/knowledge-bases/:baseID/sources",
		"POST /api/v1/ai/knowledge-bases/:baseID/sources/:sourceID/sync",
		"GET /api/v1/ai/knowledge-bases/:baseID/documents",
		"GET /api/v1/ai/knowledge-bases/:baseID/sync-runs",
		"GET /api/v1/ai/knowledge-bases/:baseID/index-revisions",
		"POST /api/v1/ai/knowledge/search",
		"GET /api/v1/ai/knowledge/connectors",
		"POST /api/v1/ai/knowledge/connectors",
		"POST /api/v1/ai/knowledge/connectors/:connectorID/validate",
		"POST /api/v1/ai/knowledge-bases/:baseID/sync-jobs",
		"GET /api/v1/ai/knowledge/sync-jobs/:jobID",
		"POST /api/v1/ai/knowledge/sync-jobs/:jobID/cancel",
		"POST /api/v1/ai/knowledge/sync-jobs/:jobID/retry",
		"POST /api/v1/ai/context/inspect",
	} {
		if _, ok := registered[route]; !ok {
			t.Fatalf("missing route %s", route)
		}
	}
}

func TestRegisterAgentProviderRoutesExposesConsoleAndRunnerSurfaces(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/api/v1")
	handler := &apiHandlers.AgentProviderHandler{}
	apiHandlers.RegisterProtectedAgentProviderRoutes(group, handler)
	apiHandlers.RegisterRunnerAgentProviderRoutes(group, handler)

	registered := make(map[string]struct{})
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = struct{}{}
	}
	for _, route := range []string{
		"GET /api/v1/ai/agent-providers/catalog",
		"GET /api/v1/ai/agent-providers/registry-snapshot",
		"POST /api/v1/ai/agent-providers/registry-acks",
		"GET /api/v1/ai/agent-providers/runtime-status",
	} {
		if _, ok := registered[route]; !ok {
			t.Fatalf("missing route %s", route)
		}
	}
}

func TestRegisterEvaluationRoutesExposesDatasetRunAndResultSurface(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	group := router.Group("/api/v1")
	apiHandlers.RegisterEvaluationRoutes(group, &apiHandlers.EvaluationHandler{})

	registered := make(map[string]struct{})
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = struct{}{}
	}
	for _, route := range []string{
		"GET /api/v1/ai/evaluations/datasets",
		"POST /api/v1/ai/evaluations/datasets",
		"GET /api/v1/ai/evaluations/runs",
		"POST /api/v1/ai/evaluations/runs",
		"GET /api/v1/ai/evaluations/runs/:runID",
		"GET /api/v1/ai/evaluations/runs/:runID/results",
		"POST /api/v1/ai/evaluations/runs/:runID/complete",
	} {
		if _, ok := registered[route]; !ok {
			t.Fatalf("missing route %s", route)
		}
	}
}

func TestAdvancedAIRoutesRespectIndependentFeatureFlags(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/api/v1")
	apiHandlers.RegisterAIAdvancedRoutes(group, apiHandlers.NewAIAdvancedHandler(nil, nil, nil, nil, nil, nil, nil, map[string]bool{
		"evaluation.release_gate": true,
	}))
	paths := routeMethodPaths(router.Routes())
	if !slices.Contains(paths, "POST /api/v1/ai/evaluations/gates/evaluate") {
		t.Fatalf("release gate route missing: %#v", paths)
	}
	if slices.Contains(paths, "GET /api/v1/ai/memory") || slices.Contains(paths, "POST /api/v1/ai/agent-runs/multi-agent") {
		t.Fatalf("default-off advanced routes exposed: %#v", paths)
	}
}

func TestRegisterProtectedRoutesExposesFirstClassOpenAICompatibleRelayRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	group := router.Group("/api/v1")
	registerProtectedRoutes(group, allRoutesEnabledConfig(), routeTestDependencies())

	registered := make(map[string]struct{})
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = struct{}{}
	}
	for _, route := range []string{
		"POST /api/v1/ai-gateway/llm/openai/v1/images/generations",
		"POST /api/v1/ai-gateway/llm/openai/v1/images/edits",
		"POST /api/v1/ai-gateway/llm/openai/v1/images/variations",
		"POST /api/v1/ai-gateway/llm/openai/v1/audio/speech",
		"POST /api/v1/ai-gateway/llm/openai/v1/audio/transcriptions",
		"POST /api/v1/ai-gateway/llm/openai/v1/audio/translations",
		"GET /api/v1/ai-gateway/llm/openai/v1/realtime",
		"GET /api/v1/ai-gateway/llm/deepseek/v1/models",
		"POST /api/v1/ai-gateway/llm/deepseek/v1/chat/completions",
		"POST /api/v1/ai-gateway/llm/deepseek/v1/responses",
		"POST /api/v1/ai-gateway/llm/deepseek/v1/embeddings",
		"POST /api/v1/ai-gateway/llm/deepseek/v1/images/generations",
		"POST /api/v1/ai-gateway/llm/deepseek/v1/images/edits",
		"POST /api/v1/ai-gateway/llm/deepseek/v1/images/variations",
		"POST /api/v1/ai-gateway/llm/deepseek/v1/audio/speech",
		"POST /api/v1/ai-gateway/llm/deepseek/v1/audio/transcriptions",
		"POST /api/v1/ai-gateway/llm/deepseek/v1/audio/translations",
		"GET /api/v1/ai-gateway/llm/qwen/v1/models",
		"POST /api/v1/ai-gateway/llm/qwen/v1/chat/completions",
		"POST /api/v1/ai-gateway/llm/qwen/v1/responses",
		"POST /api/v1/ai-gateway/llm/qwen/v1/embeddings",
		"POST /api/v1/ai-gateway/llm/qwen/v1/images/generations",
		"POST /api/v1/ai-gateway/llm/qwen/v1/images/edits",
		"POST /api/v1/ai-gateway/llm/qwen/v1/images/variations",
		"POST /api/v1/ai-gateway/llm/qwen/v1/audio/speech",
		"POST /api/v1/ai-gateway/llm/qwen/v1/audio/transcriptions",
		"POST /api/v1/ai-gateway/llm/qwen/v1/audio/translations",
		"GET /api/v1/ai-gateway/llm/openrouter/v1/models",
		"POST /api/v1/ai-gateway/llm/openrouter/v1/chat/completions",
		"POST /api/v1/ai-gateway/llm/openrouter/v1/responses",
		"POST /api/v1/ai-gateway/llm/openrouter/v1/embeddings",
		"POST /api/v1/ai-gateway/llm/openrouter/v1/images/generations",
		"POST /api/v1/ai-gateway/llm/openrouter/v1/images/edits",
		"POST /api/v1/ai-gateway/llm/openrouter/v1/images/variations",
		"POST /api/v1/ai-gateway/llm/openrouter/v1/audio/speech",
		"POST /api/v1/ai-gateway/llm/openrouter/v1/audio/transcriptions",
		"POST /api/v1/ai-gateway/llm/openrouter/v1/audio/translations",
		"GET /api/v1/ai-gateway/llm/azure-openai/v1/models",
		"POST /api/v1/ai-gateway/llm/azure-openai/v1/chat/completions",
		"POST /api/v1/ai-gateway/llm/azure-openai/v1/responses",
		"POST /api/v1/ai-gateway/llm/azure-openai/v1/embeddings",
		"POST /api/v1/ai-gateway/llm/azure-openai/v1/images/generations",
		"POST /api/v1/ai-gateway/llm/azure-openai/v1/images/edits",
		"POST /api/v1/ai-gateway/llm/azure-openai/v1/images/variations",
		"POST /api/v1/ai-gateway/llm/azure-openai/v1/audio/speech",
		"POST /api/v1/ai-gateway/llm/azure-openai/v1/audio/transcriptions",
		"POST /api/v1/ai-gateway/llm/azure-openai/v1/audio/translations",
		"GET /api/v1/ai-gateway/llm/gemini/v1beta/models",
		"POST /api/v1/ai-gateway/llm/gemini/v1beta/interactions",
		"POST /api/v1/ai-gateway/llm/gemini/v1beta/models/*modelAction",
		"POST /api/v1/ai-gateway/llm/cohere/v2/rerank",
	} {
		if _, ok := registered[route]; !ok {
			t.Fatalf("missing route %s", route)
		}
	}
}

func TestRegisterOperationalAuditRoutesExposeAuditOperations(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	group := router.Group("/api/v1")
	registerOperationalAuditRoutes(group, Dependencies{Platform: &apiHandlers.PlatformHandler{}})

	registered := make(map[string]struct{})
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = struct{}{}
	}
	for _, route := range []string{
		"GET /api/v1/audit/logs",
		"GET /api/v1/audit/logs/export",
		"GET /api/v1/audit/summary",
		"GET /api/v1/operations/logs",
		"GET /api/v1/operations/logs/export",
		"GET /api/v1/operations/summary",
	} {
		if _, ok := registered[route]; !ok {
			t.Fatalf("missing route %s", route)
		}
	}
}

func TestRegisterProviderProtocolRoutesExposeProxyLogout(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	group := router.Group("/api/v1")
	registerProviderProtocolRoutes(group, Dependencies{ProviderPortal: &providerportalhandler.Handler{}})

	registered := make(map[string]struct{})
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = struct{}{}
	}
	for _, route := range []string{
		"GET /api/v1/provider/proxy/auth",
		"POST /api/v1/provider/proxy/auth",
		"GET /api/v1/provider/proxy/start",
		"GET /api/v1/provider/proxy/callback",
		"POST /api/v1/provider/proxy/logout",
		"GET /api/v1/provider/proxy/reverse/:providerID",
		"GET /api/v1/provider/proxy/reverse/:providerID/*proxyPath",
		"POST /api/v1/provider/outposts/claim",
		"POST /api/v1/provider/outposts/:outpostID/heartbeat",
		"POST /api/v1/provider/outposts/:outpostID/check",
		"POST /api/v1/provider/outposts/:outpostID/events",
	} {
		if _, ok := registered[route]; !ok {
			t.Fatalf("missing route %s", route)
		}
	}
}

func TestPlatformMutatingRoutesHaveSecuritySurface(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	group := router.Group("/api/v1")
	registerPlatformRoutes(group, Dependencies{Platform: &apiHandlers.PlatformHandler{}})

	capabilityKeys := map[string]struct{}{}
	for _, entry := range domaincluster.DefaultCapabilityMatrix() {
		capabilityKeys[entry.Key] = struct{}{}
	}

	checked := 0
	for _, route := range router.Routes() {
		if !isMutationMethod(route.Method) || !hasPlatformClusterPrefix(route.Path) {
			continue
		}
		checked++
		entry, ok := platformMutationSecuritySurface(route.Method, route.Path)
		if !ok {
			t.Fatalf("missing platform mutation security surface for %s %s", route.Method, route.Path)
		}
		if entry.ResourceKind == "" || entry.Action == "" || entry.CapabilityKey == "" {
			t.Fatalf("incomplete security surface for %s %s: %#v", route.Method, route.Path, entry)
		}
		if !entry.AuditRequired || !entry.OperationRequired {
			t.Fatalf("mutation route must require audit and operation records for %s %s: %#v", route.Method, route.Path, entry)
		}
		if _, ok := capabilityKeys[entry.CapabilityKey]; !ok {
			t.Fatalf("unknown capability key for %s %s: %#v", route.Method, route.Path, entry)
		}
	}
	if checked == 0 {
		t.Fatal("expected at least one mutating platform route")
	}
}

func TestNonPlatformMutatingRoutesHaveSecuritySurface(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	group := router.Group("/api/v1")
	registerProtectedRoutes(group, allRoutesEnabledConfig(), routeTestDependencies())

	checked := 0
	for _, route := range router.Routes() {
		if !isMutationMethod(route.Method) || hasPlatformClusterPrefix(route.Path) || hasProtectedAuthPrefix(route.Path) {
			continue
		}
		checked++
		entry, ok := nonPlatformMutationSecuritySurface(route.Method, route.Path)
		if !ok {
			t.Fatalf("missing non-platform mutation security surface for %s %s", route.Method, route.Path)
		}
		if entry.ResourceKind == "" || entry.Action == "" || entry.PermissionKey == "" {
			t.Fatalf("incomplete security surface for %s %s: %#v", route.Method, route.Path, entry)
		}
		if !entry.AuditRequired || !entry.OperationRequired {
			t.Fatalf("mutation route must require audit and operation records for %s %s: %#v", route.Method, route.Path, entry)
		}
		if !appaccess.HasPermission([]string{"admin"}, entry.PermissionKey) {
			t.Fatalf("unknown permission key for %s %s: %#v", route.Method, route.Path, entry)
		}
	}
	if checked == 0 {
		t.Fatal("expected at least one mutating non-platform route")
	}
}

func TestRegisterProtectedRoutesExposeAIGatewayRelayCacheManagement(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	group := router.Group("/api/v1")
	registerProtectedRoutes(group, allRoutesEnabledConfig(), routeTestDependencies())

	registered := make(map[string]struct{})
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = struct{}{}
	}
	for _, route := range []string{
		"GET /api/v1/ai-gateway/relay/cache/stats",
		"POST /api/v1/ai-gateway/relay/cache/purge",
	} {
		if _, ok := registered[route]; !ok {
			t.Fatalf("missing route %s", route)
		}
	}
}

func TestRegisterProviderPortalRoutesExposeIdentityProviderWorkbenchAndOIDCProtocol(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	v1 := router.Group("/api/v1")
	registerProviderPortalRoutes(v1, routeTestDependencies())
	registerProviderProtocolRoutes(v1, routeTestDependencies())
	registerStandardProviderProtocolRoutes(router, routeTestDependencies())

	registered := make(map[string]struct{})
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = struct{}{}
	}
	for _, route := range []string{
		"GET /api/v1/identity/providers",
		"POST /api/v1/identity/providers",
		"PATCH /api/v1/identity/providers/:providerID",
		"GET /api/v1/identity/policies",
		"GET /api/v1/identity/policies/:applicationID",
		"PATCH /api/v1/identity/policies/:applicationID",
		"GET /api/v1/identity/providers/:providerID/oidc-clients",
		"POST /api/v1/identity/providers/:providerID/oidc-clients",
		"PATCH /api/v1/identity/oidc-clients/:clientID",
		"DELETE /api/v1/identity/oidc-clients/:clientID",
		"GET /api/v1/identity/outposts",
		"POST /api/v1/identity/outposts",
		"PATCH /api/v1/identity/outposts/:outpostID",
		"DELETE /api/v1/identity/outposts/:outpostID",
		"GET /api/v1/identity/sessions",
		"POST /api/v1/identity/sessions/:sessionID/revoke",
		"GET /api/v1/identity/audit/events",
		"GET /.well-known/openid-configuration",
		"GET /oauth2/authorize",
		"POST /oauth2/token",
		"GET /oauth2/userinfo",
		"POST /oauth2/userinfo",
		"GET /oauth2/jwks",
		"POST /oauth2/introspect",
		"POST /oauth2/revoke",
		"POST /api/v1/provider/oidc/userinfo",
		"POST /api/v1/provider/oidc/introspect",
		"POST /api/v1/provider/oidc/revoke",
	} {
		if _, ok := registered[route]; !ok {
			t.Fatalf("missing route %s", route)
		}
	}
}

func TestRegisterAccessRoutesPreservesEndpointContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerAccessRoutes(router.Group("/api/v1"), routeTestDependencies())

	registered := make(map[string]struct{})
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = struct{}{}
	}
	for _, route := range []string{
		"GET /api/v1/access/users",
		"POST /api/v1/access/users",
		"PUT /api/v1/access/users/:userID",
		"DELETE /api/v1/access/users/:userID",
		"POST /api/v1/access/users/:userID/revoke-sessions",
		"PUT /api/v1/access/users/:userID/roles",
		"PUT /api/v1/access/users/:userID/teams",
		"GET /api/v1/access/permission-snapshot",
		"GET /api/v1/access/roles",
		"POST /api/v1/access/roles",
		"PUT /api/v1/access/roles/:roleID",
		"DELETE /api/v1/access/roles/:roleID",
		"GET /api/v1/access/teams",
		"POST /api/v1/access/teams",
		"PUT /api/v1/access/teams/:teamID",
		"DELETE /api/v1/access/teams/:teamID",
		"GET /api/v1/access/policies",
		"POST /api/v1/access/policies",
		"PUT /api/v1/access/policies/:policyID",
		"DELETE /api/v1/access/policies/:policyID",
		"GET /api/v1/access/scope-grants",
		"POST /api/v1/access/scope-grants",
		"PUT /api/v1/access/scope-grants/:scopeGrantID",
		"DELETE /api/v1/access/scope-grants/:scopeGrantID",
	} {
		if _, ok := registered[route]; !ok {
			t.Fatalf("missing route %s", route)
		}
	}
}

func TestPlatformMutationSecuritySurfaceClassifiesHighRiskRoutes(t *testing.T) {
	for _, tc := range []struct {
		name          string
		method        string
		path          string
		resourceKind  string
		action        string
		capabilityKey string
	}{
		{
			name:          "pod exec",
			method:        "POST",
			path:          "/api/v1/clusters/:clusterID/workloads/pods/:podName/exec",
			resourceKind:  "Pod",
			action:        "exec",
			capabilityKey: "pod.exec",
		},
		{
			name:          "pod yaml apply",
			method:        "PUT",
			path:          "/api/v1/clusters/:clusterID/workloads/pods/:podName/yaml",
			resourceKind:  "Pod",
			action:        "update",
			capabilityKey: "resource.yaml.apply",
		},
		{
			name:          "deployment restart",
			method:        "POST",
			path:          "/api/v1/clusters/:clusterID/workloads/deployments/restart",
			resourceKind:  "Deployment",
			action:        "restart",
			capabilityKey: "workload.mutations",
		},
		{
			name:          "statefulset scale",
			method:        "POST",
			path:          "/api/v1/clusters/:clusterID/workloads/statefulsets/scale",
			resourceKind:  "StatefulSet",
			action:        "scale",
			capabilityKey: "workload.mutations",
		},
		{
			name:          "daemonset restart",
			method:        "POST",
			path:          "/api/v1/clusters/:clusterID/workloads/daemonsets/restart",
			resourceKind:  "DaemonSet",
			action:        "restart",
			capabilityKey: "workload.mutations",
		},
		{
			name:          "replicaset delete",
			method:        "DELETE",
			path:          "/api/v1/clusters/:clusterID/workloads/replicasets/:replicaSetName",
			resourceKind:  "ReplicaSet",
			action:        "delete",
			capabilityKey: "workload.mutations",
		},
		{
			name:          "port forward",
			method:        "POST",
			path:          "/api/v1/clusters/:clusterID/network/port-forwards",
			resourceKind:  "PortForward",
			action:        "create",
			capabilityKey: "port.forward",
		},
		{
			name:          "custom resource apply",
			method:        "PUT",
			path:          "/api/v1/clusters/:clusterID/extensions/crds/:crdName/resources/:name/yaml",
			resourceKind:  "CustomResource",
			action:        "update",
			capabilityKey: "custom.resources",
		},
		{
			name:          "horizontal pod autoscaler apply",
			method:        "PUT",
			path:          "/api/v1/clusters/:clusterID/configuration/hpas/:name/yaml",
			resourceKind:  "HorizontalPodAutoscaler",
			action:        "update",
			capabilityKey: "resource.yaml.apply",
		},
		{
			name:          "helm install",
			method:        "POST",
			path:          "/api/v1/clusters/:clusterID/helm/charts/install",
			resourceKind:  "HelmRelease",
			action:        "create",
			capabilityKey: "helm.releases",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry, ok := platformMutationSecuritySurface(tc.method, tc.path)
			if !ok {
				t.Fatalf("platformMutationSecuritySurface() not found")
			}
			if entry.ResourceKind != tc.resourceKind || entry.Action != tc.action || entry.CapabilityKey != tc.capabilityKey {
				t.Fatalf("entry = %#v, want kind=%s action=%s capability=%s", entry, tc.resourceKind, tc.action, tc.capabilityKey)
			}
		})
	}
}

func TestNonPlatformMutationSecuritySurfaceClassifiesScopedRoutes(t *testing.T) {
	for _, tc := range []struct {
		name         string
		method       string
		path         string
		resourceKind string
		action       string
		permission   string
		scoped       bool
	}{
		{name: "application create", method: "POST", path: "/api/v1/applications", resourceKind: "Application", action: "create", permission: appaccess.PermDeliveryApplicationsCreate, scoped: true},
		{name: "workflow trigger", method: "POST", path: "/api/v1/workflows/trigger", resourceKind: "Workflow", action: "trigger", permission: appaccess.PermDeliveryWorkflowsTrigger, scoped: true},
		{name: "gateway approval approve", method: "POST", path: "/api/v1/ai-gateway/approval-requests/:requestID/approve", resourceKind: "AIGatewayApprovalRequest", action: "approve", permission: appaccess.PermAIGatewayInvoke, scoped: true},
		{name: "gateway llm relay invoke", method: "POST", path: "/api/v1/ai-gateway/llm/openai/v1/chat/completions", resourceKind: "AIGatewayLLMRelayInvocation", action: "invoke", permission: appaccess.PermAIGatewayRelayInvoke, scoped: true},
		{name: "gateway llm relay embeddings invoke", method: "POST", path: "/api/v1/ai-gateway/llm/openai/v1/embeddings", resourceKind: "AIGatewayLLMRelayInvocation", action: "invoke", permission: appaccess.PermAIGatewayRelayInvoke, scoped: true},
		{name: "gateway llm relay manage", method: "POST", path: "/api/v1/ai-gateway/relay/upstreams", resourceKind: "AIGatewayLLMRelay", action: "create", permission: appaccess.PermAIGatewayRelayManage},
		{name: "gateway llm relay cache purge", method: "POST", path: "/api/v1/ai-gateway/relay/cache/purge", resourceKind: "AIGatewayLLMRelay", action: "create", permission: appaccess.PermAIGatewayRelayManage},
		{name: "docker project deploy", method: "POST", path: "/api/v1/docker/projects/:id/deploy", resourceKind: "DockerProject", action: "deploy", permission: appaccess.PermDockerProjectsDeploy},
		{name: "access scope grant update", method: "PUT", path: "/api/v1/access/scope-grants/:scopeGrantID", resourceKind: "ScopeGrant", action: "update", permission: appaccess.PermAccessScopeGrantsManage},
		{name: "identity policy update", method: "PATCH", path: "/api/v1/identity/policies/:applicationID", resourceKind: "IdentityPolicy", action: "update", permission: appaccess.PermIdentityPoliciesManage},
		{name: "identity session revoke", method: "POST", path: "/api/v1/identity/sessions/:sessionID/revoke", resourceKind: "IdentitySession", action: "revoke", permission: appaccess.PermIdentitySessionsManage},
		{name: "identity outpost update", method: "PATCH", path: "/api/v1/identity/outposts/:outpostID", resourceKind: "IdentityOutpost", action: "update", permission: appaccess.PermIdentityOutpostsManage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry, ok := nonPlatformMutationSecuritySurface(tc.method, tc.path)
			if !ok {
				t.Fatalf("nonPlatformMutationSecuritySurface() not found")
			}
			if entry.ResourceKind != tc.resourceKind || entry.Action != tc.action || entry.PermissionKey != tc.permission || entry.ScopeRequired != tc.scoped {
				t.Fatalf("entry = %#v, want kind=%s action=%s permission=%s scoped=%t", entry, tc.resourceKind, tc.action, tc.permission, tc.scoped)
			}
		})
	}
}

func TestDeliveryMutationSecuritySurfaceRules(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		path         string
		resourceKind string
		action       string
		permission   string
		scoped       bool
	}{
		{name: "application service precedes application", method: "POST", path: "/api/v1/applications/:applicationID/services", resourceKind: "ApplicationService", action: "create", permission: appaccess.PermDeliveryApplicationServicesManage, scoped: true},
		{name: "application delete permission", method: "DELETE", path: "/api/v1/applications/:applicationID", resourceKind: "Application", action: "delete", permission: appaccess.PermDeliveryApplicationsDelete, scoped: true},
		{name: "environment update", method: "PATCH", path: "/api/v1/application-environments/:environmentID", resourceKind: "ApplicationEnvironment", action: "update", permission: appaccess.PermDeliveryApplicationEnvManage, scoped: true},
		{name: "build template", method: "POST", path: "/api/v1/build-templates", resourceKind: "BuildTemplate", action: "create", permission: appaccess.PermDeliveryBuildTemplatesManage, scoped: true},
		{name: "workflow template", method: "PUT", path: "/api/v1/workflow-templates/:templateID", resourceKind: "WorkflowTemplate", action: "update", permission: appaccess.PermDeliveryWorkflowTemplatesManage, scoped: true},
		{name: "build trigger", method: "POST", path: "/api/v1/builds/trigger", resourceKind: "Build", action: "trigger", permission: appaccess.PermDeliveryBuildsTrigger, scoped: true},
		{name: "workflow approve", method: "POST", path: "/api/v1/workflows/:workflowID/approve", resourceKind: "WorkflowApproval", action: "approve", permission: appaccess.PermDeliveryWorkflowsTrigger, scoped: true},
		{name: "registry is global", method: "POST", path: "/api/v1/registries", resourceKind: "RegistryConnection", action: "create", permission: appaccess.PermDeliveryRegistriesManage},
		{name: "release trigger", method: "POST", path: "/api/v1/releases/trigger", resourceKind: "Release", action: "trigger", permission: appaccess.PermDeliveryReleasesTrigger, scoped: true},
		{name: "execution task retry", method: "POST", path: "/api/v1/delivery/execution-tasks/:taskID/retry", resourceKind: "ExecutionTask", action: "retry", permission: appaccess.PermDeliveryExecutionTasksManage, scoped: true},
		{name: "blueprint render precedes blueprint", method: "POST", path: "/api/v1/delivery/blueprints/:blueprintID/render-spec", resourceKind: "DeliveryBlueprint", action: "render", permission: appaccess.PermDeliveryApplicationsCreate, scoped: true},
		{name: "draft confirm precedes draft", method: "POST", path: "/api/v1/delivery/drafts/:draftID/confirm", resourceKind: "DeliveryDraft", action: "confirm", permission: appaccess.PermDeliveryApplicationsUpdate, scoped: true},
		{name: "plan confirm precedes plan", method: "POST", path: "/api/v1/delivery/plans/:planID/confirm", resourceKind: "DeliveryPlan", action: "confirm", permission: appaccess.PermDeliveryWorkflowsTrigger, scoped: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, ok := deliveryMutationSecuritySurface(tt.method, tt.path)
			if !ok {
				t.Fatal("deliveryMutationSecuritySurface() not found")
			}
			if entry.ResourceKind != tt.resourceKind || entry.Action != tt.action || entry.PermissionKey != tt.permission || entry.ScopeRequired != tt.scoped {
				t.Fatalf("entry = %#v, want kind=%s action=%s permission=%s scoped=%v", entry, tt.resourceKind, tt.action, tt.permission, tt.scoped)
			}
		})
	}
}

func TestPlatformMutationResourceKindMappings(t *testing.T) {
	tests := []struct {
		name string
		path string
		kind string
	}{
		{name: "cluster collection", path: "/api/v1/clusters", kind: "Cluster"},
		{name: "cluster item", path: "/api/v1/clusters/:clusterID", kind: "Cluster"},
		{name: "namespace", path: "/api/v1/clusters/:clusterID/namespaces/:namespace", kind: "Namespace"},
		{name: "node", path: "/api/v1/clusters/:clusterID/infrastructure/nodes/:nodeName", kind: "Node"},
		{name: "pod", path: "/api/v1/clusters/:clusterID/workloads/pods/:podName", kind: "Pod"},
		{name: "cron job", path: "/api/v1/clusters/:clusterID/workloads/cronjobs/:name", kind: "CronJob"},
		{name: "config map", path: "/api/v1/clusters/:clusterID/configuration/configmaps/:name", kind: "ConfigMap"},
		{name: "mutating webhook", path: "/api/v1/clusters/:clusterID/configuration/mutatingwebhookconfigurations/:name", kind: "MutatingWebhookConfiguration"},
		{name: "role binding", path: "/api/v1/clusters/:clusterID/access-control/rolebindings/:name", kind: "RoleBinding"},
		{name: "cluster role binding", path: "/api/v1/clusters/:clusterID/access-control/clusterrolebindings/:name", kind: "ClusterRoleBinding"},
		{name: "gateway class", path: "/api/v1/clusters/:clusterID/network/gatewayclasses/:name", kind: "GatewayClass"},
		{name: "port forward", path: "/api/v1/clusters/:clusterID/network/port-forwards/:sessionID", kind: "PortForward"},
		{name: "persistent volume claim", path: "/api/v1/clusters/:clusterID/storage/persistentvolumeclaims/:name", kind: "PersistentVolumeClaim"},
		{name: "custom resource", path: "/api/v1/clusters/:clusterID/extensions/crds/:crdName/resources/:name", kind: "CustomResource"},
		{name: "helm release", path: "/api/v1/clusters/:clusterID/helm/releases/:name", kind: "HelmRelease"},
		{name: "unknown cluster route", path: "/api/v1/clusters/:clusterID/read-only", kind: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := platformMutationResourceKind(tt.path); got != tt.kind {
				t.Fatalf("platformMutationResourceKind(%q) = %q, want %q", tt.path, got, tt.kind)
			}
		})
	}
}

func TestRegisterPublicRoutesIncludesConnectorEventSinkWhenAIGatewayEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	group := router.Group("/api/v1")
	registerPublicRoutes(group, cfgpkg.Config{
		Modules: cfgpkg.ModulesConfig{
			AIGateway: cfgpkg.ModuleToggleConfig{Enabled: true},
		},
	}, Dependencies{
		System:   &apiHandlers.SystemHandler{},
		Auth:     &apiHandlers.AuthHandler{},
		Platform: &apiHandlers.PlatformHandler{},
	})

	registered := make(map[string]struct{})
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = struct{}{}
	}
	if _, ok := registered["GET /api/v1/auth/providers/:providerID/login"]; !ok {
		t.Fatal("missing GET /api/v1/auth/providers/:providerID/login")
	}
	if _, ok := registered["POST /api/v1/connectors/events"]; !ok {
		t.Fatal("missing POST /api/v1/connectors/events")
	}
}

func TestRegisterPublicRoutesOmitsConnectorEventSinkWhenAIGatewayDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	group := router.Group("/api/v1")
	registerPublicRoutes(group, cfgpkg.Config{}, Dependencies{
		System:   &apiHandlers.SystemHandler{},
		Auth:     &apiHandlers.AuthHandler{},
		Platform: &apiHandlers.PlatformHandler{},
	})

	for _, route := range router.Routes() {
		if route.Method == "POST" && route.Path == "/api/v1/connectors/events" {
			t.Fatal("POST /api/v1/connectors/events should be omitted when AI Gateway is disabled")
		}
	}
}

func hasPlatformClusterPrefix(path string) bool {
	return len(path) >= len("/api/v1/clusters") && path[:len("/api/v1/clusters")] == "/api/v1/clusters"
}

func hasProtectedAuthPrefix(path string) bool {
	return len(path) >= len("/api/v1/auth/") && path[:len("/api/v1/auth/")] == "/api/v1/auth/"
}

func allRoutesEnabledConfig() cfgpkg.Config {
	return cfgpkg.Config{
		Modules: cfgpkg.ModulesConfig{
			Delivery:       cfgpkg.ModuleToggleConfig{Enabled: true},
			Monitoring:     cfgpkg.ModuleToggleConfig{Enabled: true},
			AI:             cfgpkg.ModuleToggleConfig{Enabled: true},
			AIGateway:      cfgpkg.ModuleToggleConfig{Enabled: true},
			Virtualization: cfgpkg.ModuleToggleConfig{Enabled: true},
			Docker:         cfgpkg.ModuleToggleConfig{Enabled: true},
		},
	}
}

func routeTestDependencies() Dependencies {
	return Dependencies{
		System:         &apiHandlers.SystemHandler{},
		Platform:       &apiHandlers.PlatformHandler{},
		Announcements:  &apiHandlers.AnnouncementHandler{},
		Module:         &apiHandlers.ModuleHandler{},
		Monitoring:     &apiHandlers.MonitoringHandler{},
		Catalog:        &apiHandlers.CatalogHandler{},
		Delivery:       &apiHandlers.DeliveryHandler{},
		Applications:   &apiHandlers.ApplicationHandler{},
		Builds:         &apiHandlers.BuildHandler{},
		Workflows:      &apiHandlers.WorkflowHandler{},
		Registries:     &apiHandlers.RegistryHandler{},
		Releases:       &apiHandlers.ReleaseHandler{},
		Copilot:        &apiHandlers.CopilotHandler{},
		AIGateway:      &apiHandlers.AIGatewayHandler{},
		Plugins:        &apiHandlers.PluginHandler{},
		Virtualization: &apiHandlers.VirtualizationHandler{},
		Docker:         &apiHandlers.DockerHandler{},
		Access:         &accesshandler.Handler{},
		ScopeGrants:    &accesshandler.ScopeGrantHandler{},
		Menu:           &apiHandlers.MenuHandler{},
		Settings:       &apiHandlers.SettingsHandler{},
		Auth:           &apiHandlers.AuthHandler{},
		ProviderPortal: &providerportalhandler.Handler{},
	}
}
