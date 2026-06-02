package bootstrap

import (
	"slices"
	"testing"

	cfgpkg "github.com/soha/soha/internal/infrastructure/config"
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

func TestDefaultMenuSeedsIncludeAIGatewayWorkbench(t *testing.T) {
	items := defaultMenuSeeds()
	var gateway *menuSeed
	children := map[string]string{
		"ai-gateway-overview":   "/ai-gateway/overview",
		"ai-gateway-manifest":   "/ai-gateway/manifest",
		"ai-gateway-clients":    "/ai-gateway/clients",
		"ai-gateway-tokens":     "/ai-gateway/tokens",
		"ai-gateway-governance": "/ai-gateway/governance",
		"ai-gateway-call-logs":  "/ai-gateway/call-logs",
	}
	for i := range items {
		if items[i].ID == "ai-gateway" {
			gateway = &items[i]
			continue
		}
		if wantPath, ok := children[items[i].ID]; ok {
			if items[i].ParentID != "ai-gateway" {
				t.Fatalf("%s parent = %q, want ai-gateway", items[i].ID, items[i].ParentID)
			}
			if items[i].Path != wantPath {
				t.Fatalf("%s path = %q, want %s", items[i].ID, items[i].Path, wantPath)
			}
			delete(children, items[i].ID)
		}
	}
	if gateway == nil {
		t.Fatal("default menu seeds missing ai-gateway")
	}
	if gateway.ParentID != "" {
		t.Fatalf("AI Gateway parent = %q, want root menu", gateway.ParentID)
	}
	if gateway.Path != "/ai-gateway" {
		t.Fatalf("AI Gateway path = %q, want /ai-gateway", gateway.Path)
	}
	if gateway.Section != "ops" {
		t.Fatalf("AI Gateway section = %q, want ops", gateway.Section)
	}
	if len(children) > 0 {
		t.Fatalf("default menu seeds missing AI Gateway child menus: %v", children)
	}
}

func TestDeprecatedMenusIncludeOldAIGatewayWorkbenchID(t *testing.T) {
	if !slices.Contains(deprecatedMenuIDs(), "ai-workbench-gateway") {
		t.Fatal("deprecated menu IDs should clean up ai-workbench-gateway")
	}
}

func TestDefaultMenuSeedsExposeAccessPagesAsDirectMenus(t *testing.T) {
	items := defaultMenuSeeds()
	expected := map[string]int{
		"access-users":    226,
		"access-roles":    227,
		"access-teams":    228,
		"access-policies": 229,
	}

	for _, item := range items {
		sortOrder, ok := expected[item.ID]
		if !ok {
			continue
		}
		if item.ParentID != "" {
			t.Fatalf("access seed menu %q parent = %q, want direct menu", item.ID, item.ParentID)
		}
		if item.Section != "admin" {
			t.Fatalf("access seed menu %q section = %q, want admin", item.ID, item.Section)
		}
		if item.SortOrder != sortOrder {
			t.Fatalf("access seed menu %q sort order = %d, want %d", item.ID, item.SortOrder, sortOrder)
		}
		delete(expected, item.ID)
	}
	if len(expected) > 0 {
		t.Fatalf("default menu seeds missing direct access menus: %v", expected)
	}
}

func TestDefaultMenuSeedsUseFullSystemLogLabels(t *testing.T) {
	items := defaultMenuSeeds()
	expected := map[string]struct {
		labelZH string
		labelEN string
	}{
		"operations": {labelZH: "操作日志", labelEN: "Operation Logs"},
		"audit":      {labelZH: "审计日志", labelEN: "Audit Logs"},
	}

	for _, item := range items {
		labels, ok := expected[item.ID]
		if !ok {
			continue
		}
		if item.LabelZH != labels.labelZH || item.LabelEN != labels.labelEN {
			t.Fatalf("system log menu %q labels = %q/%q, want %q/%q", item.ID, item.LabelZH, item.LabelEN, labels.labelZH, labels.labelEN)
		}
		delete(expected, item.ID)
	}
	if len(expected) > 0 {
		t.Fatalf("default menu seeds missing system log menus: %v", expected)
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

func TestFilterSeedMenusByModulesRemovesAIGatewayWhenDisabled(t *testing.T) {
	items := filterSeedMenusByModules(defaultMenuSeeds(), cfgpkg.ModulesConfig{
		Delivery:       cfgpkg.ModuleToggleConfig{Enabled: true},
		Monitoring:     cfgpkg.ModuleToggleConfig{Enabled: true},
		AI:             cfgpkg.ModuleToggleConfig{Enabled: true},
		AIGateway:      cfgpkg.ModuleToggleConfig{Enabled: false},
		Virtualization: cfgpkg.ModuleToggleConfig{Enabled: true},
		Docker:         cfgpkg.ModuleToggleConfig{Enabled: true},
	})

	foundAIWorkbench := false
	for _, item := range items {
		if isAIGatewayMenuSeed(item) {
			t.Fatalf("AI Gateway seed menu %q should be filtered when module is disabled", item.ID)
		}
		if isAIMenuSeed(item) && item.ID == "ai-workbench" {
			foundAIWorkbench = true
		}
	}
	if !foundAIWorkbench {
		t.Fatal("AI workbench root should remain when only AI Gateway is disabled")
	}
}

func TestDisabledModuleMenuIDsIncludesAIGatewayWhenSeedVersionIsCurrent(t *testing.T) {
	menuIDs := disabledModuleMenuIDs(defaultMenuSeeds(), cfgpkg.ModulesConfig{
		Delivery:       cfgpkg.ModuleToggleConfig{Enabled: true},
		Monitoring:     cfgpkg.ModuleToggleConfig{Enabled: true},
		AI:             cfgpkg.ModuleToggleConfig{Enabled: true},
		AIGateway:      cfgpkg.ModuleToggleConfig{Enabled: false},
		Virtualization: cfgpkg.ModuleToggleConfig{Enabled: true},
		Docker:         cfgpkg.ModuleToggleConfig{Enabled: true},
	})

	for _, id := range []string{
		"ai-gateway",
		"ai-gateway-overview",
		"ai-gateway-manifest",
		"ai-gateway-clients",
		"ai-gateway-tokens",
		"ai-gateway-governance",
		"ai-gateway-call-logs",
	} {
		if !slices.Contains(menuIDs, id) {
			t.Fatalf("disabled module menu IDs = %v, missing %s", menuIDs, id)
		}
	}
	if slices.Contains(menuIDs, "ai-workbench") {
		t.Fatalf("disabled module menu IDs should keep AI workbench when only AI Gateway is disabled: %v", menuIDs)
	}
}

func TestFilterSeedMenusByModulesKeepsAIGatewayWhenAIModuleDisabled(t *testing.T) {
	items := filterSeedMenusByModules(defaultMenuSeeds(), cfgpkg.ModulesConfig{
		Delivery:       cfgpkg.ModuleToggleConfig{Enabled: true},
		Monitoring:     cfgpkg.ModuleToggleConfig{Enabled: true},
		AI:             cfgpkg.ModuleToggleConfig{Enabled: false},
		AIGateway:      cfgpkg.ModuleToggleConfig{Enabled: true},
		Virtualization: cfgpkg.ModuleToggleConfig{Enabled: true},
		Docker:         cfgpkg.ModuleToggleConfig{Enabled: true},
	})

	foundGatewayIDs := map[string]bool{
		"ai-gateway":            false,
		"ai-gateway-overview":   false,
		"ai-gateway-manifest":   false,
		"ai-gateway-clients":    false,
		"ai-gateway-tokens":     false,
		"ai-gateway-governance": false,
		"ai-gateway-call-logs":  false,
	}
	for _, item := range items {
		if isAIMenuSeed(item) {
			t.Fatalf("AI workbench seed menu %q should be filtered when AI module is disabled", item.ID)
		}
		if _, ok := foundGatewayIDs[item.ID]; ok {
			foundGatewayIDs[item.ID] = true
		}
	}
	for id, found := range foundGatewayIDs {
		if !found {
			t.Fatalf("AI Gateway menu %s should remain when AI module is disabled but AI Gateway is enabled", id)
		}
	}
}
