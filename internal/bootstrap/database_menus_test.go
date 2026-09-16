package bootstrap

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestUpsertMenusCleansDeprecatedPathOwnersFirst(t *testing.T) {
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

	deprecatedIDs := []string{"plugins-marketplace", "plugins-installed"}
	mock.ExpectExec(`DELETE FROM menu_role_bindings WHERE menu_id IN`).
		WithArgs(deprecatedIDs[0], deprecatedIDs[1]).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`DELETE FROM menus WHERE id IN`).
		WithArgs(deprecatedIDs[0], deprecatedIDs[1]).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`INSERT INTO menus`).WillReturnResult(sqlmock.NewResult(0, 1))

	err = upsertMenusAfterDeprecatedCleanup(context.Background(), db, []menuSeed{{
		ID:      "settings-extensions-marketplace",
		Path:    "/plugins/marketplace",
		LabelZH: "插件市场",
		LabelEN: "Marketplace",
		Enabled: true,
	}}, deprecatedIDs, time.Now())
	if err != nil {
		t.Fatalf("upsertMenusAfterDeprecatedCleanup returned error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("menu replacement order was not preserved: %v", err)
	}
}

func TestCleanupDeprecatedMenusDeletesMenuBindingsAndMenus(t *testing.T) {
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

	deprecatedIDs := []string{"identity-sessions", "identity-audit"}
	mock.ExpectExec(`DELETE FROM menu_role_bindings WHERE menu_id IN`).WithArgs(deprecatedIDs[0], deprecatedIDs[1]).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`DELETE FROM menus WHERE id IN`).WithArgs(deprecatedIDs[0], deprecatedIDs[1]).WillReturnResult(sqlmock.NewResult(0, 2))

	if err := cleanupDeprecatedMenus(context.Background(), db, deprecatedIDs); err != nil {
		t.Fatalf("cleanupDeprecatedMenus returned error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestSyncPlatformMenuSeedUpgradesMovesUntouchedApplicationManifests(t *testing.T) {
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

	now := time.Now().UTC()
	mock.ExpectExec(`UPDATE menus`).
		WithArgs(100, now, "platform-manifests", 15).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := syncPlatformMenuSeedUpgrades(context.Background(), db, now); err != nil {
		t.Fatalf("syncPlatformMenuSeedUpgrades returned error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

//nolint:dupl // Independent expected seed values must not be shared with the migration under test.
func TestSyncNetworkAccessMenuSeedUpgradesOnlyMovesUntouchedDefaults(t *testing.T) {
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

	now := time.Now().UTC()
	tests := []struct {
		id         string
		parent     string
		path       string
		labelZH    string
		labelEN    string
		icon       string
		section    string
		sort       int
		newParent  string
		newSection string
		newSort    int
		enabled    bool
	}{
		{id: "network-access-wifi", parent: "identity", path: "/network-access/wifi", labelZH: "Wi-Fi 入网", labelEN: "Wi-Fi Access", icon: "wifi", section: "network", sort: 22, newParent: "identity", newSection: "network", newSort: 22, enabled: false},
		{id: "network-access-wired", parent: "identity", path: "/network-access/wired", labelZH: "有线入网", labelEN: "Wired Access", icon: "network", section: "network", sort: 24, newParent: "identity", newSection: "network", newSort: 24, enabled: false},
		{id: "network-access-radius-services", parent: "identity", path: "/network-access/radius-services", labelZH: "RADIUS 服务", labelEN: "RADIUS Services", icon: "key", section: "network", sort: 32, newParent: "identity", newSection: "network", newSort: 32, enabled: false},
		{id: "network-access-radius-services", parent: "network-access-settings", path: "/network-access/radius-services", labelZH: "RADIUS 服务", labelEN: "RADIUS Services", icon: "key", section: "network", sort: 10, newParent: "network-access-settings", newSection: "network", newSort: 10, enabled: false},
		{id: "network-access-ssids", parent: "network-access-settings", path: "/network-access/ssids", labelZH: "SSID", labelEN: "SSIDs", icon: "wifi", section: "network", sort: 20, newParent: "network-access-settings", newSection: "network", newSort: 20, enabled: false},
		{id: "network-access-nas-bindings", parent: "identity", path: "/network-access/nas-bindings", labelZH: "网络设备", labelEN: "Network Devices", icon: "radio-tower", section: "network", sort: 30, newParent: "identity", newSection: "network", newSort: 30, enabled: false},
		{id: "network-access-nas-bindings", parent: "network-access-settings", path: "/network-access/nas-bindings", labelZH: "网络设备", labelEN: "Network Devices", icon: "radio-tower", section: "network", sort: 30, newParent: "network-access-settings", newSection: "network", newSort: 30, enabled: false},
		{id: "network-access-sites", parent: "identity", path: "/network-access/sites", labelZH: "站点", labelEN: "Sites", icon: "globe", section: "network", sort: 20, newParent: "identity", newSection: "vpn", newSort: 10, enabled: true},
		{id: "network-access-sites", parent: "identity", path: "/network-access/sites", labelZH: "站点", labelEN: "Sites", icon: "globe", section: "network", sort: 30, newParent: "identity", newSection: "vpn", newSort: 10, enabled: true},
		{id: "network-access-spaces", parent: "identity", path: "/network-access/spaces", labelZH: "网络空间", labelEN: "Network Spaces", icon: "network", section: "vpn", sort: 10, newParent: "identity", newSection: "vpn", newSort: 20, enabled: true},
		{id: "network-access-resources", parent: "identity", path: "/network-access/resources", labelZH: "资源", labelEN: "Resources", icon: "blocks", section: "vpn", sort: 20, newParent: "identity", newSection: "vpn", newSort: 30, enabled: true},
		{id: "network-access-gateways", parent: "identity", path: "/network-access/gateways", labelZH: "网关", labelEN: "Gateways", icon: "network", section: "vpn", sort: 30, newParent: "identity", newSection: "vpn", newSort: 40, enabled: true},
		{id: "network-access-site-profile-bindings", parent: "identity", path: "/network-access/site-profile-bindings", labelZH: "接入等级", labelEN: "Access Levels", icon: "shield", section: "network", sort: 40, newParent: "identity", newSection: "vpn", newSort: 50, enabled: true},
		{id: "network-access-access-grants", parent: "identity", path: "/network-access/access-grants", labelZH: "访问授权", labelEN: "Access Grants", icon: "shield", section: "vpn", sort: 50, newParent: "identity", newSection: "vpn", newSort: 60, enabled: true},
		{id: "network-access-policy", parent: "identity", path: "/network-access/policy", labelZH: "策略", labelEN: "Policies", icon: "shield", section: "vpn", sort: 60, newParent: "identity", newSection: "vpn", newSort: 70, enabled: true},
		{id: "network-access-sessions", parent: "identity", path: "/network-access/sessions", labelZH: "会话", labelEN: "Sessions", icon: "activity", section: "network", sort: 50, newParent: "identity", newSection: "vpn", newSort: 80, enabled: true},
		{id: "network-access-enrollments", parent: "identity", path: "/network-access/enrollments", labelZH: "运行时注册", labelEN: "Runtime Enrollment", icon: "key", section: "vpn", sort: 40, newParent: "identity", newSection: "vpn", newSort: 90, enabled: true},
		{id: "network-access-telemetry", parent: "identity", path: "/network-access/telemetry", labelZH: "遥测", labelEN: "Telemetry", icon: "gauge", section: "network", sort: 60, newParent: "identity", newSection: "vpn", newSort: 100, enabled: true},
	}
	for _, test := range tests {
		mock.ExpectExec(`UPDATE menus`).
			WithArgs(test.newParent, test.newSection, test.newSort, test.enabled, now, test.id, test.parent, test.path, test.labelZH, test.labelEN, test.icon, test.section, test.sort).
			WillReturnResult(sqlmock.NewResult(0, 1))
	}

	if err := syncNetworkAccessMenuSeedUpgrades(context.Background(), db, now); err != nil {
		t.Fatalf("syncNetworkAccessMenuSeedUpgrades returned error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestDefaultComputeWorkbenchSectionsDoNotUseObservabilityGrouping(t *testing.T) {
	for _, item := range defaultMenuSeeds() {
		switch item.ID {
		case "compute-workbench", "compute-workbench-overview", "compute-workbench-tasks-operations":
			if item.Section != "" {
				t.Fatalf("compute menu %s section = %q, want empty", item.ID, item.Section)
			}
		}
	}
}

func TestDefaultDeliveryMenuSeedsUseTaskSpecificIcons(t *testing.T) {
	want := map[string]string{
		"delivery-overview":              "gauge",
		"builds":                         "blocks",
		"release-board":                  "activity",
		"delivery-testing":               "shield",
		"delivery-analysis":              "inspect",
		"release-bundles":                "clipboard-list",
		"releases":                       "file-clock",
		"delivery-blueprints":            "puzzle",
		"build-templates":                "code",
		"workflow-templates":             "sync",
		"delivery-environment-directory": "cluster",
		"registries":                     "storage",
	}
	used := make(map[string]string, len(want))
	for _, item := range defaultMenuSeeds() {
		expected, ok := want[item.ID]
		if !ok {
			continue
		}
		if item.IconKey != expected {
			t.Fatalf("delivery menu %s icon = %q, want %q", item.ID, item.IconKey, expected)
		}
		if previousID, exists := used[item.IconKey]; exists {
			t.Fatalf("delivery menus %s and %s reuse icon %q", previousID, item.ID, item.IconKey)
		}
		used[item.IconKey] = item.ID
		delete(want, item.ID)
	}
	if len(want) != 0 {
		t.Fatalf("delivery menu seeds missing: %v", want)
	}
}

func TestDefaultDeliveryMenuSeedsRemoveRedundantWorkflowLists(t *testing.T) {
	removed := map[string]bool{"execution-tasks": false, "workflows": false}
	for _, id := range obsoleteMenuIDsForCleanup() {
		if _, ok := removed[id]; ok {
			removed[id] = true
		}
	}
	for _, item := range defaultMenuSeeds() {
		if _, ok := removed[item.ID]; ok {
			t.Fatalf("redundant delivery menu remains seeded: %s", item.ID)
		}
	}
	for id, cleaned := range removed {
		if !cleaned {
			t.Fatalf("redundant delivery menu is not cleaned: %s", id)
		}
	}
}

func TestWorkflowCenterReplacesDeliveryBatchMenuSeeds(t *testing.T) {
	found := false
	for _, item := range defaultMenuSeeds() {
		if item.ID == "delivery-workflows" || item.ID == "delivery-batches" {
			t.Fatalf("obsolete delivery menu is still seeded: %#v", item)
		}
		if item.ID == "release-board" {
			found = item.Path == "/release-board" && item.LabelZH == "工作流中心" && item.Enabled
		}
	}
	if !found {
		t.Fatal("workflow center menu is missing")
	}
	for _, id := range []string{"delivery-workflows", "delivery-batches"} {
		found = false
		for _, obsoleteID := range obsoleteMenuIDsForCleanup() {
			found = found || obsoleteID == id
		}
		if !found {
			t.Fatalf("obsolete delivery menu is not cleaned: %s", id)
		}
	}
}

func TestInternalWorkbenchOverviewSeedUsesCanonicalPath(t *testing.T) {
	paths := map[string]string{}
	for _, item := range builtinMenuSeeds {
		if item.ID == "identity" || item.ID == "identity-overview" {
			paths[item.ID] = item.Path
		}
	}
	if paths["identity"] != "/internal-workbench" {
		t.Fatalf("internal workbench seed path = %q", paths["identity"])
	}
	if paths["identity-overview"] != "/internal-workbench/overview" {
		t.Fatalf("internal workbench overview seed path = %q", paths["identity-overview"])
	}
}

func TestNetworkAccessMenuSeedsLiveUnderInternalWorkbench(t *testing.T) {
	want := map[string]struct {
		parent  string
		path    string
		section string
		labelZH string
		labelEN string
		sort    int
	}{
		"network-access-devices":               {parent: "identity", path: "/network-access/devices", section: "endpoint", labelZH: "终端资产", labelEN: "Endpoint Assets", sort: 10},
		"network-access-user-admission":        {parent: "identity", path: "/network-access/user-admission", section: "network", labelZH: "用户入网", labelEN: "User Admission", sort: 10},
		"network-access-settings":              {parent: "identity", path: "/network-access/settings", section: "network", labelZH: "入网设置", labelEN: "Admission Settings", sort: 20},
		"network-access-sites":                 {parent: "identity", path: "/network-access/sites", section: "vpn", labelZH: "站点", labelEN: "Sites", sort: 10},
		"network-access-spaces":                {parent: "identity", path: "/network-access/spaces", section: "vpn", labelZH: "网络空间", labelEN: "Network Spaces", sort: 20},
		"network-access-resources":             {parent: "identity", path: "/network-access/resources", section: "vpn", labelZH: "资源", labelEN: "Resources", sort: 30},
		"network-access-gateways":              {parent: "identity", path: "/network-access/gateways", section: "vpn", labelZH: "网关", labelEN: "Gateways", sort: 40},
		"network-access-site-profile-bindings": {parent: "identity", path: "/network-access/site-profile-bindings", section: "vpn", labelZH: "接入等级", labelEN: "Access Levels", sort: 50},
		"network-access-access-grants":         {parent: "identity", path: "/network-access/access-grants", section: "vpn", labelZH: "访问授权", labelEN: "Access Grants", sort: 60},
		"network-access-policy":                {parent: "identity", path: "/network-access/policy", section: "vpn", labelZH: "策略", labelEN: "Policies", sort: 70},
		"network-access-sessions":              {parent: "identity", path: "/network-access/sessions", section: "vpn", labelZH: "会话", labelEN: "Sessions", sort: 80},
		"network-access-enrollments":           {parent: "identity", path: "/network-access/enrollments", section: "vpn", labelZH: "运行时注册", labelEN: "Runtime Enrollment", sort: 90},
		"network-access-telemetry":             {parent: "identity", path: "/network-access/telemetry", section: "vpn", labelZH: "遥测", labelEN: "Telemetry", sort: 100},
		"network-access-mihomo-profiles":       {parent: "identity", path: "/network-access/mihomo-profiles", section: "proxy", labelZH: "代理隧道", labelEN: "Proxy Tunnels", sort: 10},
		"network-access-proxy-overview":        {parent: "identity", path: "/network-access/proxy-overview", section: "proxy", labelZH: "流量概览", labelEN: "Traffic Overview", sort: 20},
		"network-access-proxy-connections":     {parent: "identity", path: "/network-access/proxy-connections", section: "proxy", labelZH: "用户连接", labelEN: "User Connections", sort: 30},
	}

	for _, item := range builtinMenuSeeds {
		expected, ok := want[item.ID]
		if !ok {
			continue
		}
		if item.ParentID != expected.parent || item.Path != expected.path || item.Section != expected.section || item.LabelZH != expected.labelZH || item.LabelEN != expected.labelEN || item.SortOrder != expected.sort {
			t.Fatalf("network access seed %s = parent %q path %q section %q labels %q/%q sort %d", item.ID, item.ParentID, item.Path, item.Section, item.LabelZH, item.LabelEN, item.SortOrder)
		}
		delete(want, item.ID)
	}
	if len(want) != 0 {
		t.Fatalf("network access menu seeds missing: %v", want)
	}
	for _, item := range builtinMenuSeeds {
		switch item.ID {
		case "network-access-wifi", "network-access-wired", "network-access-radius-services", "network-access-ssids", "network-access-nas-bindings":
			t.Fatalf("legacy or tab child menu remains seeded: %s", item.ID)
		}
	}
}
