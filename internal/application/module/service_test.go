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

func moduleStatusByID(items []domainmodule.Status, id string) (domainmodule.Status, bool) {
	for _, item := range items {
		if item.Descriptor.ID == id {
			return item, true
		}
	}
	return domainmodule.Status{}, false
}
