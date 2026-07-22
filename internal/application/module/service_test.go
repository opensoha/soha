package module

import (
	"context"
	"slices"
	"testing"

	domainmodule "github.com/opensoha/soha/internal/domain/module"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
)

type fixedRuntimeModules map[string]bool

func (s fixedRuntimeModules) ModuleEnabled(id string) bool       { return s[id] }
func (s fixedRuntimeModules) FeatureEnabled(string, string) bool { return false }

func TestListIncludesEnabledHomeWorkbench(t *testing.T) {
	service := New(cfgpkg.ModulesConfig{Home: cfgpkg.ModuleToggleConfig{Enabled: true}})

	items, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	status, ok := moduleStatusByID(items, "home")
	if !ok || !status.Enabled {
		t.Fatalf("home module = %#v", status)
	}
	if status.Descriptor.DefaultPath != "/portal" || status.Descriptor.EnabledConfigKey != "modules.home.enabled" {
		t.Fatalf("home descriptor = %#v", status.Descriptor)
	}
	if !slices.Contains(status.Descriptor.SeedMenus, "home-workbench") {
		t.Fatalf("home seed menus = %v", status.Descriptor.SeedMenus)
	}
}

func TestListIncludesVirtualizationDescriptor(t *testing.T) {
	service := New(cfgpkg.ModulesConfig{Virtualization: cfgpkg.ModuleToggleConfig{Enabled: true}})

	items, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}

	status, ok := moduleStatusByID(items, "virtualization")
	if !ok {
		t.Fatalf("virtualization module descriptor missing")
	}
	if !status.Enabled {
		t.Fatalf("virtualization module should be enabled from config")
	}
	if status.Descriptor.Name != "虚拟化资源" {
		t.Fatalf("virtualization name = %q", status.Descriptor.Name)
	}
	if status.Descriptor.DefaultPath != "/compute/virtualization" {
		t.Fatalf("virtualization default path = %q", status.Descriptor.DefaultPath)
	}
	if status.Descriptor.EnabledConfigKey != "modules.virtualization.enabled" {
		t.Fatalf("virtualization enabled config key = %q", status.Descriptor.EnabledConfigKey)
	}
	for _, permission := range []string{
		"virtualization.overview.view",
		"virtualization.vms.view",
		"virtualization.clusters.view",
		"virtualization.images.view",
		"virtualization.flavors.view",
		"virtualization.operations.view",
		"virtualization.sync.view",
		"virtualization.sync.manage",
	} {
		if !slices.Contains(status.Descriptor.VisiblePermissions, permission) {
			t.Fatalf("virtualization visible permissions = %v, missing %s", status.Descriptor.VisiblePermissions, permission)
		}
	}
	for _, menuID := range []string{
		"virtualization-workbench",
		"virtualization-workbench-vms",
		"virtualization-workbench-clusters",
		"virtualization-workbench-images",
		"virtualization-workbench-flavors",
		"virtualization-workbench-operations",
		"virtualization-workbench-sync",
	} {
		if !slices.Contains(status.Descriptor.SeedMenus, menuID) {
			t.Fatalf("virtualization seed menus = %v, missing %s", status.Descriptor.SeedMenus, menuID)
		}
	}
}

func TestListIncludesUnifiedComputeDescriptor(t *testing.T) {
	service := New(cfgpkg.ModulesConfig{Docker: cfgpkg.ModuleToggleConfig{Enabled: true}})
	items, err := service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	status, ok := moduleStatusByID(items, "compute")
	if !ok || !status.Enabled || status.Descriptor.DefaultPath != "/compute/overview" {
		t.Fatalf("compute module = %#v", status)
	}
	for _, permission := range []string{"virtualization.images.view", "virtualization.flavors.view", "virtualization.sync.view", "docker.templates.view", "docker.operations.view"} {
		if !slices.Contains(status.Descriptor.VisiblePermissions, permission) {
			t.Fatalf("compute permissions missing %s: %v", permission, status.Descriptor.VisiblePermissions)
		}
	}
	for _, menuID := range []string{"compute-workbench-tasks-operations"} {
		if !slices.Contains(status.Descriptor.SeedMenus, menuID) {
			t.Fatalf("compute seed menus missing %s: %v", menuID, status.Descriptor.SeedMenus)
		}
	}
	for _, menuID := range []string{"compute-workbench-tasks-sync", "compute-workbench-tasks-build"} {
		if slices.Contains(status.Descriptor.SeedMenus, menuID) {
			t.Fatalf("compute seed menus retained legacy %s: %v", menuID, status.Descriptor.SeedMenus)
		}
	}
}

func TestListReflectsDisabledVirtualizationModule(t *testing.T) {
	service := New(cfgpkg.ModulesConfig{Virtualization: cfgpkg.ModuleToggleConfig{Enabled: false}})

	items, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}

	status, ok := moduleStatusByID(items, "virtualization")
	if !ok {
		t.Fatalf("virtualization module descriptor missing")
	}
	if status.Enabled {
		t.Fatalf("virtualization module should be disabled from config")
	}
}

func TestListIncludesDockerDescriptorWithoutRemovedDetailMenus(t *testing.T) {
	service := New(cfgpkg.ModulesConfig{Docker: cfgpkg.ModuleToggleConfig{Enabled: true}})

	items, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}

	status, ok := moduleStatusByID(items, "docker")
	if !ok {
		t.Fatalf("docker module descriptor missing")
	}
	if !status.Enabled {
		t.Fatalf("docker module should be enabled from config")
	}
	if status.Descriptor.Name != "容器运行时" {
		t.Fatalf("docker name = %q", status.Descriptor.Name)
	}
	if status.Descriptor.DefaultPath != "/compute/runtimes" {
		t.Fatalf("docker default path = %q", status.Descriptor.DefaultPath)
	}
	if len(status.Descriptor.Dependencies) != 0 {
		t.Fatalf("container runtime should be independently toggleable, dependencies = %v", status.Descriptor.Dependencies)
	}
	for _, permission := range []string{"docker.services.view", "docker.ports.view"} {
		if !slices.Contains(status.Descriptor.VisiblePermissions, permission) {
			t.Fatalf("docker visible permissions = %v, missing %s", status.Descriptor.VisiblePermissions, permission)
		}
	}
	for _, menuID := range []string{"docker-workbench", "docker-workbench-hosts", "docker-workbench-projects", "docker-workbench-templates", "docker-workbench-operations"} {
		if !slices.Contains(status.Descriptor.SeedMenus, menuID) {
			t.Fatalf("docker seed menus = %v, missing %s", status.Descriptor.SeedMenus, menuID)
		}
	}
	for _, removedMenuID := range []string{"docker-workbench-services", "docker-workbench-ports"} {
		if slices.Contains(status.Descriptor.SeedMenus, removedMenuID) {
			t.Fatalf("docker seed menus should not include removed menu %s: %v", removedMenuID, status.Descriptor.SeedMenus)
		}
	}
}

func TestListIncludesAIGatewayDescriptor(t *testing.T) {
	service := New(cfgpkg.ModulesConfig{AIGateway: cfgpkg.ModuleToggleConfig{Enabled: true}})

	items, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}

	status, ok := moduleStatusByID(items, "aiGateway")
	if !ok {
		t.Fatalf("AI Gateway module descriptor missing")
	}
	if !status.Enabled {
		t.Fatalf("AI Gateway module should be enabled from config")
	}
	if status.Descriptor.DefaultPath != "/ai-gateway/manifest" {
		t.Fatalf("AI Gateway default path = %q", status.Descriptor.DefaultPath)
	}
	if status.Descriptor.EnabledConfigKey != "modules.ai_gateway.enabled" {
		t.Fatalf("AI Gateway enabled config key = %q", status.Descriptor.EnabledConfigKey)
	}
	if len(status.Descriptor.Dependencies) != 0 {
		t.Fatalf("AI Gateway should be independently toggleable, dependencies = %v", status.Descriptor.Dependencies)
	}
	for _, permission := range []string{"ai.gateway.view", "ai.gateway.invoke", "ai.gateway.manage"} {
		if !slices.Contains(status.Descriptor.VisiblePermissions, permission) {
			t.Fatalf("AI Gateway visible permissions = %v, missing %s", status.Descriptor.VisiblePermissions, permission)
		}
	}
	for _, id := range []string{"ai-gateway-relay", "ai-gateway-manifest", "ai-gateway-clients", "ai-gateway-tokens", "ai-gateway-governance", "ai-gateway-call-logs"} {
		if !slices.Contains(status.Descriptor.SeedMenus, id) {
			t.Fatalf("AI Gateway seed menus = %v, missing %s", status.Descriptor.SeedMenus, id)
		}
	}
	if len(status.Descriptor.SeedMenus) != 6 {
		t.Fatalf("AI Gateway seed menus should contain only canonical leaves: %v", status.Descriptor.SeedMenus)
	}
}

func TestListReflectsDisabledAIGatewayModule(t *testing.T) {
	service := New(cfgpkg.ModulesConfig{AIGateway: cfgpkg.ModuleToggleConfig{Enabled: false}})

	items, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}

	status, ok := moduleStatusByID(items, "aiGateway")
	if !ok {
		t.Fatalf("AI Gateway module descriptor missing")
	}
	if status.Enabled {
		t.Fatalf("AI Gateway module should be disabled from config")
	}
}

func TestRuntimeListPreservesDisabledSecurityAndCMDB(t *testing.T) {
	service := NewRuntime(fixedRuntimeModules{"security": false, "cmdb": false})
	items, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	for _, id := range []string{"security", "cmdb"} {
		status, ok := moduleStatusByID(items, id)
		if !ok {
			t.Fatalf("%s module descriptor missing", id)
		}
		if status.Enabled {
			t.Fatalf("%s module should preserve disabled runtime baseline", id)
		}
	}
}

func moduleStatusByID(items []domainmodule.Status, id string) (domainmodule.Status, bool) {
	for _, item := range items {
		if item.Descriptor.ID == id {
			return item, true
		}
	}
	return domainmodule.Status{}, false
}
