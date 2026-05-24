package bootstrap

import (
	"slices"
	"testing"

	cfgpkg "github.com/kubecrux/kubecrux/internal/infrastructure/config"
)

func TestDefaultMenuSeedsValidate(t *testing.T) {
	if err := validateMenuSeeds(defaultMenuSeeds()); err != nil {
		t.Fatalf("default menu seeds must stay internally consistent: %v", err)
	}
}

func TestDefaultMenuSeedsExcludeDeprecatedIDs(t *testing.T) {
	deprecated := make(map[string]struct{}, len(deprecatedMenuIDs()))
	for _, id := range deprecatedMenuIDs() {
		deprecated[id] = struct{}{}
	}

	for _, item := range defaultMenuSeeds() {
		if _, exists := deprecated[item.ID]; exists {
			t.Fatalf("default menu seed %q is marked deprecated and must not be reintroduced", item.ID)
		}
	}
}

func TestDefaultMenuSeedsIncludeVirtualizationWorkbench(t *testing.T) {
	items := defaultMenuSeeds()
	for _, id := range []string{
		"virtualization-workbench",
		"virtualization-workbench-overview",
		"virtualization-workbench-vms",
		"virtualization-workbench-clusters",
		"virtualization-workbench-images",
		"virtualization-workbench-flavors",
		"virtualization-workbench-operations",
		"virtualization-workbench-sync",
	} {
		if !slices.ContainsFunc(items, func(item menuSeed) bool { return item.ID == id }) {
			t.Fatalf("default menu seeds missing %s", id)
		}
	}
}

func TestDefaultMenuSeedsIncludeDockerWorkbench(t *testing.T) {
	items := defaultMenuSeeds()
	for _, id := range []string{
		"docker-workbench",
		"docker-workbench-overview",
		"docker-workbench-hosts",
		"docker-workbench-projects",
		"docker-workbench-services",
		"docker-workbench-ports",
		"docker-workbench-templates",
		"docker-workbench-operations",
	} {
		if !slices.ContainsFunc(items, func(item menuSeed) bool { return item.ID == id }) {
			t.Fatalf("default menu seeds missing %s", id)
		}
	}
}

func TestDefaultMenuSeedsPlaceApplicationCenterFirstInDelivery(t *testing.T) {
	items := defaultMenuSeeds()
	var applicationCenter *menuSeed
	for i := range items {
		if items[i].ID == "builds" {
			applicationCenter = &items[i]
			break
		}
	}
	if applicationCenter == nil {
		t.Fatal("default menu seeds missing builds")
	}

	for _, item := range items {
		if item.Section != "deliver" || item.ID == applicationCenter.ID {
			continue
		}
		if item.SortOrder <= applicationCenter.SortOrder {
			t.Fatalf("application center sort order = %d, delivery menu %q sort order = %d", applicationCenter.SortOrder, item.ID, item.SortOrder)
		}
	}
}

func TestFilterSeedMenusByModulesRemovesVirtualizationWhenDisabled(t *testing.T) {
	items := filterSeedMenusByModules(defaultMenuSeeds(), cfgpkg.ModulesConfig{
		Delivery:       cfgpkg.ModuleToggleConfig{Enabled: true},
		Monitoring:     cfgpkg.ModuleToggleConfig{Enabled: true},
		AI:             cfgpkg.ModuleToggleConfig{Enabled: true},
		Virtualization: cfgpkg.ModuleToggleConfig{Enabled: false},
	})

	for _, item := range items {
		if isVirtualizationMenuSeed(item) {
			t.Fatalf("virtualization seed menu %q should be filtered when module is disabled", item.ID)
		}
	}
}

func TestFilterSeedMenusByModulesRemovesDockerWhenDisabled(t *testing.T) {
	items := filterSeedMenusByModules(defaultMenuSeeds(), cfgpkg.ModulesConfig{
		Delivery:       cfgpkg.ModuleToggleConfig{Enabled: true},
		Monitoring:     cfgpkg.ModuleToggleConfig{Enabled: true},
		AI:             cfgpkg.ModuleToggleConfig{Enabled: true},
		Virtualization: cfgpkg.ModuleToggleConfig{Enabled: true},
		Docker:         cfgpkg.ModuleToggleConfig{Enabled: false},
	})

	for _, item := range items {
		if isDockerMenuSeed(item) {
			t.Fatalf("docker seed menu %q should be filtered when module is disabled", item.ID)
		}
	}
}
