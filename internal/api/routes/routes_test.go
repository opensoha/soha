package routes

import (
	"testing"

	"github.com/gin-gonic/gin"
	apiHandlers "github.com/opensoha/soha/internal/api/handlers"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
)

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
		"GET /api/v1/clusters/:clusterID/network/services",
		"GET /api/v1/clusters/:clusterID/network/topology",
		"GET /api/v1/clusters/:clusterID/network/gatewayclasses",
		"GET /api/v1/clusters/:clusterID/network/gateways",
		"GET /api/v1/clusters/:clusterID/helm/releases/:releaseName/values",
		"POST /api/v1/clusters/:clusterID/helm/charts/install",
		"GET /api/v1/clusters/:clusterID/extensions/crds/:crdName/resources",
	} {
		if _, ok := registered[route]; !ok {
			t.Fatalf("missing route %s", route)
		}
	}

	for _, route := range []string{
		"GET /api/v1/clusters/:clusterID/network/httproutes",
		"GET /api/v1/clusters/:clusterID/network/backendtlspolicies",
		"GET /api/v1/clusters/:clusterID/network/grpcroutes",
		"GET /api/v1/clusters/:clusterID/network/referencegrants",
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
	if _, ok := registered["POST /api/v1/copilot/agent-runs/:runID/cancel"]; !ok {
		t.Fatal("missing copilot agent run cancel route")
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
		{
			name:         "application create",
			method:       "POST",
			path:         "/api/v1/applications",
			resourceKind: "Application",
			action:       "create",
			permission:   appaccess.PermDeliveryApplicationsCreate,
			scoped:       true,
		},
		{
			name:         "workflow trigger",
			method:       "POST",
			path:         "/api/v1/workflows/trigger",
			resourceKind: "Workflow",
			action:       "trigger",
			permission:   appaccess.PermDeliveryWorkflowsTrigger,
			scoped:       true,
		},
		{
			name:         "gateway approval approve",
			method:       "POST",
			path:         "/api/v1/ai-gateway/approval-requests/:requestID/approve",
			resourceKind: "AIGatewayApprovalRequest",
			action:       "approve",
			permission:   appaccess.PermAIGatewayInvoke,
			scoped:       true,
		},
		{
			name:         "docker project deploy",
			method:       "POST",
			path:         "/api/v1/docker/projects/:id/deploy",
			resourceKind: "DockerProject",
			action:       "deploy",
			permission:   appaccess.PermDockerProjectsDeploy,
			scoped:       false,
		},
		{
			name:         "access scope grant update",
			method:       "PUT",
			path:         "/api/v1/access/scope-grants/:scopeGrantID",
			resourceKind: "ScopeGrant",
			action:       "update",
			permission:   appaccess.PermAccessScopeGrantsManage,
			scoped:       false,
		},
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
		Access:         &apiHandlers.AccessHandler{},
		ScopeGrants:    &apiHandlers.ScopeGrantHandler{},
		Menu:           &apiHandlers.MenuHandler{},
		Settings:       &apiHandlers.SettingsHandler{},
		Auth:           &apiHandlers.AuthHandler{},
	}
}
