package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func networkNavigationTestStore(t *testing.T) *dbstore.Store {
	t.Helper()
	portText := os.Getenv("SOHA_CATALOG_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("set SOHA_CATALOG_TEST_POSTGRES_PORT to this run's isolated PostgreSQL")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	cfg := cfgpkg.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "postgres", User: "pgsql", Password: "test-only", SSLMode: "disable"}
	admin, err := dbstore.New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	name := "soha_navigation_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec(context.Background(), "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := admin.Exec(context.Background(), "DROP DATABASE "+name); err != nil {
			t.Error(err)
		}
	})
	cfg.Name = name
	store, err := dbstore.New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := store.MigrateFromFile(ctx, filepath.Join("..", "..", "migrations", "postgres")); err != nil {
		t.Fatal(err)
	}
	return store
}

func navigationRows(t *testing.T, db *gorm.DB, ids []string) []string {
	t.Helper()
	var rows []string
	if err := db.Raw(`SELECT row_to_json(m)::text FROM menus m WHERE id IN ? ORDER BY id`, ids).Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestNetworkNavigationUpgradeWithPostgres(t *testing.T) {
	store := networkNavigationTestStore(t)
	db := store.DB()
	ctx := context.Background()
	items := []menuSeed{
		{ID: "identity", Path: "/internal-workbench", LabelZH: "内网工作台", LabelEN: "Internal Workbench", Enabled: true},
		{ID: "network-access-sites", ParentID: "identity", Path: "/network-access/sites", LabelZH: "站点", LabelEN: "Sites", IconKey: "globe", Section: "vpn", SortOrder: 10, Enabled: true},
		{ID: "network-access-resources", ParentID: "identity", Path: "/network-access/resources", LabelZH: "资源", LabelEN: "Resources", IconKey: "blocks", Section: "vpn", SortOrder: 30, Enabled: true},
		{ID: "network-access-spaces", ParentID: "identity", Path: "/network-access/spaces", LabelZH: "自定义网络空间", LabelEN: "Custom Spaces", IconKey: "network", Section: "vpn", SortOrder: 20, Enabled: true},
		{ID: "network-access-gateways", ParentID: "identity", Path: "/network-access/gateways", LabelZH: "网关", LabelEN: "Gateways", IconKey: "network", Section: "vpn", SortOrder: 40, Enabled: false},
		{ID: "network-access-policy", ParentID: "identity", Path: "/network-access/policy", LabelZH: "策略", LabelEN: "Policies", IconKey: "shield", Section: "custom", SortOrder: 7, Enabled: true},
		{ID: "custom-operator-page", ParentID: "identity", Path: "/custom/operator", LabelZH: "自定义入口", LabelEN: "Custom Entry", Enabled: true},
	}
	if err := upsertMenus(ctx, db, items, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO roles (id,name,permission_keys) VALUES ('navigation-role','Retained role','["retained.permission"]')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO menu_role_bindings (id,menu_id,role_id) VALUES ('navigation-binding','network-access-sites','navigation-role')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := writeBootstrapSeedVersion(ctx, db, previousBootstrapSeedVersion); err != nil {
		t.Fatal(err)
	}
	customIDs := []string{"network-access-spaces", "network-access-gateways", "network-access-policy", "custom-operator-page"}
	before := navigationRows(t, db, customIDs)
	var initialMenuCount int64
	if err := db.Table("menus").Count(&initialMenuCount).Error; err != nil {
		t.Fatal(err)
	}
	cfg := cfgpkg.Config{Modules: cfgpkg.ModulesConfig{Security: cfgpkg.ModuleToggleConfig{Enabled: true}}}

	t.Run("disabled_upgrade_stays_pending", func(t *testing.T) {
		if err := seedDefaults(ctx, store, cfgpkg.Config{}); err != nil {
			t.Fatal(err)
		}
		version, err := readBootstrapSeedVersion(ctx, db)
		if err != nil || version != previousBootstrapSeedVersion {
			t.Fatalf("disabled module advanced upgrade: %q, %v", version, err)
		}
		var count int64
		if err := db.Table("menus").Count(&count).Error; err != nil || count != initialMenuCount {
			t.Fatalf("disabled module inserted containers: %d, %v", count, err)
		}
		var parent string
		if err := db.Raw(`SELECT parent_id FROM menus WHERE id='network-access-sites'`).Scan(&parent).Error; err != nil || parent != "identity" {
			t.Fatalf("disabled module moved a leaf: %q, %v", parent, err)
		}
	})
	t.Run("failure_rolls_back", func(t *testing.T) {
		collision := menuSeed{ID: "custom-path-owner", Path: "/network-access/controls", LabelZH: "Custom", LabelEN: "Custom", Enabled: true}
		if err := upsertMenus(ctx, db, []menuSeed{collision}, time.Now()); err != nil {
			t.Fatal(err)
		}
		if err := seedDefaults(ctx, store, cfg); err == nil {
			t.Fatal("expected unique-path collision")
		}
		version, err := readBootstrapSeedVersion(ctx, db)
		if err != nil || version != previousBootstrapSeedVersion {
			t.Fatalf("failed upgrade advanced version: %q, %v", version, err)
		}
		var groups int64
		if err := db.Table("menus").Where("id = ?", "network-access-infrastructure").Count(&groups).Error; err != nil || groups != 0 {
			t.Fatalf("failed upgrade left a container: %d, %v", groups, err)
		}
		if err := db.Exec(`DELETE FROM menus WHERE id = 'custom-path-owner'`).Error; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("upgrade_preserves_custom_data_and_bindings", func(t *testing.T) {
		if err := seedDefaults(ctx, store, cfg); err != nil {
			t.Fatal(err)
		}
		if after := navigationRows(t, db, customIDs); !reflect.DeepEqual(before, after) {
			t.Fatalf("custom or disabled menus changed: before %v after %v", before, after)
		}
		var parent string
		if err := db.Raw(`SELECT parent_id FROM menus WHERE id='network-access-sites'`).Scan(&parent).Error; err != nil || parent != "network-access-infrastructure" {
			t.Fatalf("default site parent = %q, %v", parent, err)
		}
		var label string
		if err := db.Raw(`SELECT label_zh FROM menus WHERE id='network-access-resources'`).Scan(&label).Error; err != nil || label != "网络资源" {
			t.Fatalf("default resource label = %q, %v", label, err)
		}
		var bindings, menus int64
		if err := db.Table("menu_role_bindings").Where("id = ?", "navigation-binding").Count(&bindings).Error; err != nil || bindings != 1 {
			t.Fatalf("role binding lost: %d, %v", bindings, err)
		}
		if err := db.Table("menus").Count(&menus).Error; err != nil || menus != initialMenuCount+4 {
			t.Fatalf("unrelated defaults were reseeded: %d, %v", menus, err)
		}
		version, err := readBootstrapSeedVersion(ctx, db)
		if err != nil || version != bootstrapSeedVersion {
			t.Fatalf("upgrade version = %q, %v", version, err)
		}
	})
	t.Run("second_start_is_idempotent", func(t *testing.T) {
		ids := append(customIDs, "network-access-sites", "network-access-resources", "network-access-infrastructure", "network-access-controls", "network-access-vpn", "network-access-monitoring")
		before := navigationRows(t, db, ids)
		if err := seedDefaults(ctx, store, cfg); err != nil {
			t.Fatal(err)
		}
		if after := navigationRows(t, db, ids); !reflect.DeepEqual(before, after) {
			t.Fatalf("second start changed menus: before %v after %v", before, after)
		}
	})
}
