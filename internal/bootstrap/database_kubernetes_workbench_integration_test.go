package bootstrap

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"testing"
	"time"

	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"gorm.io/gorm"
)

func TestKubernetesWorkbenchUpgradeWithPostgres(t *testing.T) {
	store := networkNavigationTestStore(t)
	db, ctx := store.DB(), context.Background()
	items := []menuSeed{
		{ID: "network", Path: "/network", LabelZH: "网络", LabelEN: "Network", IconKey: "network", SortOrder: 50, Enabled: true},
		{ID: "network-services", ParentID: "network", Path: "/network/services", LabelZH: "Services", LabelEN: "Services", IconKey: "network", SortOrder: 41, Enabled: true},
		{ID: "network-ingresses", ParentID: "network", Path: "/network/ingresses", LabelZH: "自定义入口", LabelEN: "Custom ingress", IconKey: "network", SortOrder: 42, Enabled: true},
	}
	if err := upsertMenus(ctx, db, items, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO roles (id,name,scope,capabilities,permission_keys)
		VALUES ('admin','Retained admin','system','["retained"]','["retained.permission"]'),
		('ops','Retained ops','system','[]','[]'),('readonly','Reader','system','[]','[]'),
		('custom','Custom role','workspace','[]','["custom.permission"]')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO menu_role_bindings (id,menu_id,role_id) VALUES ('retained-binding','network-services','custom')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := writeSeedVersion(ctx, db, bootstrapSeedVersionKey, bootstrapSeedVersion); err != nil {
		t.Fatal(err)
	}
	before := navigationRows(t, db, []string{"network-ingresses"})
	if err := seedDefaults(ctx, store, cfgpkg.Config{}); err != nil {
		t.Fatal(err)
	}
	if after := navigationRows(t, db, []string{"network-ingresses"}); !reflect.DeepEqual(before, after) {
		t.Fatal("custom menu was changed")
	}
	var parent string
	if err := db.Raw(`SELECT parent_id FROM menus WHERE id='network-services'`).Scan(&parent).Error; err != nil || parent != "network" {
		t.Fatalf("default service parent=%q, err=%v", parent, err)
	}
	var count int64
	if err := db.Table("menus").Where("id LIKE ?", "resource-monitoring%").Count(&count).Error; err != nil || count != 6 {
		t.Fatalf("monitoring menus=%d, err=%v", count, err)
	}
	if err := db.Table("menus").Where("id LIKE ? AND enabled=TRUE", "resource-monitoring%").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("enabled monitoring menus=%d, err=%v", count, err)
	}
	if err := db.Table("menu_role_bindings").Where("id = ?", "retained-binding").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("retained bindings=%d, err=%v", count, err)
	}
	assertWorkbenchRolePermissions(t, db)
	ids := []string{"network-services", "resource-monitoring", "resource-monitoring-cpu"}
	first := navigationRows(t, db, ids)
	if err := seedDefaults(ctx, store, cfgpkg.Config{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, navigationRows(t, db, ids)) {
		t.Fatal("second startup changed migrated menus")
	}
}

func TestResourceMonitoringDashboardUpgradeWithPostgres(t *testing.T) {
	store := networkNavigationTestStore(t)
	db, ctx := store.DB(), context.Background()
	var items []menuSeed
	for _, item := range defaultMenuSeeds() {
		if item.ID == "resource-monitoring" || item.ParentID == "resource-monitoring" {
			item.Enabled = true
			if item.ID == "resource-monitoring-memory" {
				item.LabelZH = "自定义内存入口"
			}
			items = append(items, item)
		}
	}
	if err := upsertMenus(ctx, db, items, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO roles (id,name,scope,capabilities,permission_keys) VALUES ('custom','Custom','workspace','[]','["platform.resource-monitoring.cpu.view"]')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO menu_role_bindings (id,menu_id,role_id) VALUES ('custom-cpu','resource-monitoring-cpu','custom')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := writeSeedVersion(ctx, db, kubernetesWorkbenchSeedVersionKey, kubernetesWorkbenchSeedVersion); err != nil {
		t.Fatal(err)
	}
	if err := upgradeKubernetesWorkbenchSeeds(ctx, db); err != nil {
		t.Fatal(err)
	}
	var enabled []string
	if err := db.Table("menus").Where("id LIKE ? AND enabled=TRUE", "resource-monitoring%").Order("id").Pluck("id", &enabled).Error; err != nil || !slices.Equal(enabled, []string{"resource-monitoring", "resource-monitoring-memory"}) {
		t.Fatalf("enabled=%v, err=%v", enabled, err)
	}
	var count int64
	if err := db.Table("menu_role_bindings").Where("id='custom-cpu'").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("custom binding lost: %d, %v", count, err)
	}
	first := navigationRows(t, db, []string{"resource-monitoring-cpu", "resource-monitoring-memory"})
	if err := upgradeKubernetesWorkbenchSeeds(ctx, db); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, navigationRows(t, db, []string{"resource-monitoring-cpu", "resource-monitoring-memory"})) {
		t.Fatal("second startup changed dashboard menus")
	}
}

func TestKubernetesWorkbenchUpgradeRollbackWithPostgres(t *testing.T) {
	store := networkNavigationTestStore(t)
	db, ctx := store.DB(), context.Background()
	if err := upsertMenus(ctx, db, []menuSeed{{ID: "custom-owner", Path: "/resource-monitoring", LabelZH: "Custom", LabelEN: "Custom", Enabled: true}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := writeSeedVersion(ctx, db, bootstrapSeedVersionKey, bootstrapSeedVersion); err != nil {
		t.Fatal(err)
	}
	if err := seedDefaults(ctx, store, cfgpkg.Config{}); err == nil {
		t.Fatal("conflicting custom path must stop migration")
	}
	version, err := readSeedVersion(ctx, db, kubernetesWorkbenchSeedVersionKey)
	if err != nil || version != "" {
		t.Fatalf("failed migration advanced marker=%q, err=%v", version, err)
	}
	var count int64
	if err := db.Table("menus").Where("id IN ?", []string{"network-routing", "resource-monitoring"}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("partial migration persisted %d menus, err=%v", count, err)
	}
}

func assertWorkbenchRolePermissions(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, role := range []string{"admin", "ops", "readonly", "custom"} {
		var raw string
		if err := db.Raw(`SELECT permission_keys::text FROM roles WHERE id=?`, role).Scan(&raw).Error; err != nil {
			t.Fatal(err)
		}
		var keys []string
		if err := json.Unmarshal([]byte(raw), &keys); err != nil {
			t.Fatal(err)
		}
		if role == "custom" {
			if !slices.Equal(keys, []string{"custom.permission"}) {
				t.Fatal("custom role was expanded")
			}
			continue
		}
		for _, view := range []string{"overview", "cpu", "memory", "conntrack", "pods"} {
			if !slices.Contains(keys, "platform.resource-monitoring."+view+".view") {
				t.Fatalf("%s missing %s", role, view)
			}
		}
		if role == "admin" && !slices.Contains(keys, "retained.permission") {
			t.Fatal("existing permission removed")
		}
		for _, permission := range kubernetesCRUDPermissionKeys {
			if slices.Contains(keys, permission) != (role == "admin") {
				t.Fatalf("unexpected write grant %s for %s", permission, role)
			}
		}
	}
}
