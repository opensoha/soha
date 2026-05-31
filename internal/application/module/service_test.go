package module

import (
	"context"
	"slices"
	"testing"

	domainmodule "github.com/soha/soha/internal/domain/module"
	cfgpkg "github.com/soha/soha/internal/infrastructure/config"
)

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
	if status.Descriptor.Name != "虚拟化管理工作台" {
		t.Fatalf("virtualization name = %q", status.Descriptor.Name)
	}
	if status.Descriptor.DefaultPath != "/virtualization" {
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
		"virtualization-workbench-overview",
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
	if status.Descriptor.DefaultPath != "/ai-gateway/overview" {
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
	for _, id := range []string{"ai-gateway", "ai-gateway-overview", "ai-gateway-manifest", "ai-gateway-clients", "ai-gateway-tokens", "ai-gateway-governance", "ai-gateway-call-logs"} {
		if !slices.Contains(status.Descriptor.SeedMenus, id) {
			t.Fatalf("AI Gateway seed menus = %v, missing %s", status.Descriptor.SeedMenus, id)
		}
	}
	if slices.Contains(status.Descriptor.SeedMenus, "ai-workbench-gateway") {
		t.Fatalf("AI Gateway seed menus should not include deprecated ai-workbench-gateway: %v", status.Descriptor.SeedMenus)
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

func moduleStatusByID(items []domainmodule.Status, id string) (domainmodule.Status, bool) {
	for _, item := range items {
		if item.Descriptor.ID == id {
			return item, true
		}
	}
	return domainmodule.Status{}, false
}
