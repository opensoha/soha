package bootstrap

import (
	"context"
	"database/sql/driver"
	"slices"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	appdocker "github.com/opensoha/soha/internal/application/docker"
	appvirtualization "github.com/opensoha/soha/internal/application/virtualization"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainvirtualization "github.com/opensoha/soha/internal/domain/virtualization"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestDefaultMenuSeedsValidate(t *testing.T) {
	if err := validateMenuSeeds(defaultMenuSeeds()); err != nil {
		t.Fatalf("default menu seeds must stay internally consistent: %v", err)
	}
}

func appdockerHostProvisionInput(connectionID string) appdocker.HostProvisionInput {
	return appdocker.HostProvisionInput{
		ConnectionID:     connectionID,
		Name:             "docker-dev",
		CPU:              2,
		MemoryMiB:        4096,
		DiskGiB:          40,
		ImageID:          "image-1",
		CloudInit:        "#cloud-config\npackages:\n  - docker.io",
		StartAfterCreate: true,
	}
}

type captureDockerProvisionVirtualization struct {
	createPrincipal domainidentity.Principal
	createInput     appvirtualization.CreateVMInput
	cancelPrincipal domainidentity.Principal
	retryPrincipal  domainidentity.Principal
}

func (c *captureDockerProvisionVirtualization) CreateVM(_ context.Context, principal domainidentity.Principal, input appvirtualization.CreateVMInput) (domainvirtualization.Task, error) {
	c.createPrincipal = principal
	c.createInput = input
	return domainvirtualization.Task{
		ID:           "task-1",
		Provider:     appvirtualization.ProviderPVE,
		ConnectionID: input.ConnectionID,
		Status:       appvirtualization.TaskStatusQueued,
	}, nil
}

func (c *captureDockerProvisionVirtualization) GetOperation(_ context.Context, _ domainidentity.Principal, taskID string) (domainvirtualization.Task, error) {
	return domainvirtualization.Task{ID: taskID, Provider: appvirtualization.ProviderPVE, ConnectionID: "conn-pve", Status: appvirtualization.TaskStatusQueued}, nil
}

func (c *captureDockerProvisionVirtualization) GetVM(_ context.Context, _ domainidentity.Principal, vmID string) (domainvirtualization.VM, error) {
	return domainvirtualization.VM{ID: vmID, Name: "docker-dev"}, nil
}

func (c *captureDockerProvisionVirtualization) CancelOperation(_ context.Context, principal domainidentity.Principal, taskID string) (domainvirtualization.Task, error) {
	c.cancelPrincipal = principal
	return domainvirtualization.Task{ID: taskID, Provider: appvirtualization.ProviderPVE, ConnectionID: "conn-pve", Status: appvirtualization.TaskStatusCanceled}, nil
}

func (c *captureDockerProvisionVirtualization) RetryOperation(_ context.Context, principal domainidentity.Principal, taskID string) (domainvirtualization.Task, error) {
	c.retryPrincipal = principal
	return domainvirtualization.Task{ID: taskID, Provider: appvirtualization.ProviderPVE, ConnectionID: "conn-pve", Status: appvirtualization.TaskStatusQueued}, nil
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

func TestSyncDisabledModuleMenusCleansDeprecatedMenus(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new sqlmock: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm postgres mock: %v", err)
	}

	deprecatedIDs := deprecatedMenuIDs()
	deprecatedArgs := make([]driver.Value, 0, len(deprecatedIDs))
	for _, id := range deprecatedIDs {
		deprecatedArgs = append(deprecatedArgs, id)
	}

	mock.ExpectExec(`DELETE FROM menu_role_bindings WHERE menu_id IN`).WithArgs(deprecatedArgs...).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`DELETE FROM menus WHERE id IN`).WithArgs(deprecatedArgs...).WillReturnResult(sqlmock.NewResult(0, 0))

	modules := cfgpkg.ModulesConfig{
		Delivery:       cfgpkg.ModuleToggleConfig{Enabled: true},
		Monitoring:     cfgpkg.ModuleToggleConfig{Enabled: true},
		AI:             cfgpkg.ModuleToggleConfig{Enabled: true},
		AIGateway:      cfgpkg.ModuleToggleConfig{Enabled: true},
		Virtualization: cfgpkg.ModuleToggleConfig{Enabled: true},
		Docker:         cfgpkg.ModuleToggleConfig{Enabled: true},
	}

	if err := syncDisabledModuleMenus(context.Background(), db, modules); err != nil {
		t.Fatalf("syncDisabledModuleMenus returned error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
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
		"docker-workbench-templates",
		"docker-workbench-operations",
	} {
		if !slices.ContainsFunc(items, func(item menuSeed) bool { return item.ID == id }) {
			t.Fatalf("default menu seeds missing %s", id)
		}
	}
}

func TestDockerHostProvisionerUsesPrivilegedVirtualizationBridge(t *testing.T) {
	virtualization := &captureDockerProvisionVirtualization{}
	provisioner := dockerHostProvisioner{virtualization: virtualization}
	principal := domainidentity.Principal{
		UserID:         "docker-operator",
		UserName:       "Docker Operator",
		Roles:          []string{"docker-host-admin"},
		PermissionKeys: []string{"docker.hosts.manage"},
	}

	if _, err := provisioner.ProvisionDockerHost(context.Background(), principal, appdockerHostProvisionInput("conn-pve")); err != nil {
		t.Fatalf("ProvisionDockerHost() error = %v", err)
	}
	if virtualization.createPrincipal.UserID != "docker-operator" {
		t.Fatalf("create principal user = %q, want docker-operator", virtualization.createPrincipal.UserID)
	}
	if !slices.Equal(virtualization.createPrincipal.Roles, []string{"admin"}) {
		t.Fatalf("create principal roles = %#v, want admin bridge", virtualization.createPrincipal.Roles)
	}
	if len(virtualization.createPrincipal.PermissionKeys) != 0 {
		t.Fatalf("create principal should not carry capped permission keys: %#v", virtualization.createPrincipal.PermissionKeys)
	}
	if virtualization.createInput.CloudInit == "" || virtualization.createInput.ImageID != "image-1" || virtualization.createInput.MemoryMiB != 4096 {
		t.Fatalf("create vm input = %#v", virtualization.createInput)
	}

	if _, err := provisioner.CancelProvisionTask(context.Background(), principal, "task-1"); err != nil {
		t.Fatalf("CancelProvisionTask() error = %v", err)
	}
	if virtualization.cancelPrincipal.UserID != "docker-operator" || !slices.Equal(virtualization.cancelPrincipal.Roles, []string{"admin"}) {
		t.Fatalf("cancel principal = %#v", virtualization.cancelPrincipal)
	}
}

func TestDefaultMenuSeedsIncludeAIGatewayWorkbench(t *testing.T) {
	items := defaultMenuSeeds()
	var gateway *menuSeed
	children := map[string]string{
		"ai-gateway-overview":   "/ai-gateway/overview",
		"ai-gateway-relay":      "/ai-gateway/relay",
		"ai-gateway-manifest":   "/ai-gateway/manifest",
		"ai-gateway-clients":    "/ai-gateway/clients",
		"ai-gateway-tokens":     "/ai-gateway/tokens",
		"ai-gateway-governance": "/ai-gateway/governance",
		"ai-gateway-call-logs":  "/ai-gateway/call-logs",
		"plugins":               "/plugins",
		"plugins-marketplace":   "/plugins/marketplace",
		"plugins-installed":     "/plugins/installed",
	}
	expectedParents := map[string]string{
		"plugins":             "ai-gateway",
		"plugins-marketplace": "plugins",
		"plugins-installed":   "plugins",
	}
	for i := range items {
		if items[i].ID == "ai-gateway" {
			gateway = &items[i]
			continue
		}
		if wantPath, ok := children[items[i].ID]; ok {
			wantParent := expectedParents[items[i].ID]
			if wantParent == "" {
				wantParent = "ai-gateway"
			}
			if items[i].ParentID != wantParent {
				t.Fatalf("%s parent = %q, want %s", items[i].ID, items[i].ParentID, wantParent)
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
		if item.Section != "delivery" || item.ID == applicationCenter.ID {
			continue
		}
		if item.SortOrder <= applicationCenter.SortOrder {
			t.Fatalf("application center sort order = %d, delivery menu %q sort order = %d", applicationCenter.SortOrder, item.ID, item.SortOrder)
		}
	}
}

func TestDefaultMenuSeedsGroupDeliveryWorkbenchByUserTask(t *testing.T) {
	items := defaultMenuSeeds()
	expected := map[string]string{
		"builds":                   "delivery",
		"delivery-onboarding":      "delivery",
		"release-board":            "delivery",
		"delivery-testing":         "delivery",
		"delivery-analysis":        "delivery",
		"release-bundles":          "delivery-records",
		"execution-tasks":          "delivery-records",
		"workflows":                "delivery-records",
		"releases":                 "delivery-records",
		"delivery-blueprints":      "delivery-platform",
		"build-templates":          "delivery-platform",
		"workflow-templates":       "delivery-platform",
		"application-environments": "delivery-platform",
		"registries":               "delivery-platform",
	}
	for _, item := range items {
		section, ok := expected[item.ID]
		if !ok {
			continue
		}
		if item.Section != section {
			t.Fatalf("menu seed %q section = %q, want %q", item.ID, item.Section, section)
		}
		delete(expected, item.ID)
	}
	if len(expected) > 0 {
		t.Fatalf("default menu seeds missing delivery menus: %v", expected)
	}
}

func TestDefaultMenuSeedsBindDeliveryMenusByResponsibility(t *testing.T) {
	items := defaultMenuSeeds()
	byID := make(map[string]menuSeed, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}

	expectedRoles := map[string][]string{
		"builds":            {"admin", "ops", "developer", "tester", "readonly"},
		"delivery-testing":  {"admin", "ops", "developer", "tester", "readonly"},
		"delivery-analysis": {"admin", "ops", "developer", "tester", "readonly"},
		"release-bundles":   {"admin", "ops", "developer", "tester", "readonly"},
		"execution-tasks":   {"admin", "ops", "developer", "tester", "readonly"},
		"workflows":         {"admin", "ops", "developer", "readonly"},
		"releases":          {"admin", "ops", "developer", "readonly"},
	}
	for menuID, roles := range expectedRoles {
		item, ok := byID[menuID]
		if !ok {
			t.Fatalf("default menu seeds missing %s", menuID)
		}
		for _, role := range roles {
			if !slices.Contains(item.Roles, role) {
				t.Fatalf("menu %s roles = %v, missing %s", menuID, item.Roles, role)
			}
		}
	}

	restrictedMenus := []string{
		"delivery-onboarding",
		"release-board",
		"delivery-blueprints",
		"build-templates",
		"workflow-templates",
		"application-environments",
		"registries",
	}
	for _, menuID := range restrictedMenus {
		item, ok := byID[menuID]
		if !ok {
			t.Fatalf("default menu seeds missing %s", menuID)
		}
		for _, role := range []string{"tester", "readonly"} {
			if slices.Contains(item.Roles, role) {
				t.Fatalf("menu %s roles = %v, should not include %s", menuID, item.Roles, role)
			}
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
		"ai-gateway-relay",
		"ai-gateway-manifest",
		"ai-gateway-clients",
		"ai-gateway-tokens",
		"ai-gateway-governance",
		"ai-gateway-call-logs",
		"plugins",
		"plugins-marketplace",
		"plugins-installed",
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
		"ai-gateway-relay":      false,
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
