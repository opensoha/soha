package bootstrap

import (
	"context"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, admin.Exec(context.Background(), "CREATE DATABASE "+name))
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
	require.NoError(t, store.MigrateFromFile(ctx, filepath.Join("..", "..", "migrations", "postgres")))
	return store
}

func navigationRows(t *testing.T, db *gorm.DB, ids []string) []string {
	t.Helper()
	var rows []string
	require.NoError(t, db.Raw(`SELECT row_to_json(m)::text FROM menus m WHERE id IN ? ORDER BY id`, ids).Scan(&rows).Error)
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
	require.NoError(t, upsertMenus(ctx, db, items, time.Now()))
	require.NoError(t, db.Exec(`INSERT INTO roles (id,name,permission_keys) VALUES ('navigation-role','Retained role','["retained.permission"]')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO menu_role_bindings (id,menu_id,role_id) VALUES ('navigation-binding','network-access-sites','navigation-role')`).Error)
	require.NoError(t, writeSeedVersion(ctx, db, bootstrapSeedVersionKey, "2026-09-22-oncall-settings-menu"))
	// Keep this fixture focused on its independent network-access upgrade.
	require.NoError(t, writeSeedVersion(ctx, db, kubernetesWorkbenchSeedVersionKey, kubernetesWorkbenchSeedVersion))
	customIDs := []string{"network-access-spaces", "network-access-gateways", "network-access-policy", "custom-operator-page"}
	before := navigationRows(t, db, customIDs)
	var initialMenuCount int64
	require.NoError(t, db.Table("menus").Count(&initialMenuCount).Error)
	cfg := cfgpkg.Config{Modules: cfgpkg.ModulesConfig{Security: cfgpkg.ModuleToggleConfig{Enabled: true}}}

	t.Run("disabled_upgrade_stays_pending", func(t *testing.T) {
		require.NoError(t, seedDefaults(ctx, store, cfgpkg.Config{}))
		version, err := readSeedVersion(ctx, db, bootstrapSeedVersionKey)
		{
			require.NoError(t, err, "disabled module advanced upgrade: %q, %v", version, err)
			require.Equal(t, "2026-09-22-oncall-settings-menu", version, "disabled module advanced upgrade: %q, %v", version, err)
		}
		marker, err := readSeedVersion(ctx, db, networkNavigationSeedVersionKey)
		{
			require.NoError(t, err, "disabled module marked upgrade: %q, %v", marker, err)
			require.Empty(t, marker, "disabled module marked upgrade: %q, %v", marker, err)
		}
		var count int64
		{
			err := db.Table("menus").Count(&count).Error
			require.NoError(t, err, "disabled module inserted containers: %d, %v", count, err)
			require.Equal(t, initialMenuCount, count, "disabled module inserted containers: %d, %v", count, err)
		}
		var parent string
		{
			err := db.Raw(`SELECT parent_id FROM menus WHERE id='network-access-sites'`).Scan(&parent).Error
			require.NoError(t, err, "disabled module moved a leaf: %q, %v", parent, err)
			require.Equal(t, "identity", parent, "disabled module moved a leaf: %q, %v", parent, err)
		}
	})
	t.Run("failure_rolls_back", func(t *testing.T) {
		collision := menuSeed{ID: "custom-path-owner", Path: "/network-access/controls", LabelZH: "Custom", LabelEN: "Custom", Enabled: true}
		require.NoError(t, upsertMenus(ctx, db, []menuSeed{collision}, time.Now()))
		if err := seedDefaults(ctx, store, cfg); err == nil {
			t.Fatal("expected unique-path collision")
		}
		version, err := readSeedVersion(ctx, db, bootstrapSeedVersionKey)
		{
			require.NoError(t, err, "failed upgrade advanced version: %q, %v", version, err)
			require.Equal(t, "2026-09-22-oncall-settings-menu", version, "failed upgrade advanced version: %q, %v", version, err)
		}
		marker, err := readSeedVersion(ctx, db, networkNavigationSeedVersionKey)
		{
			require.NoError(t, err, "failed upgrade left marker: %q, %v", marker, err)
			require.Empty(t, marker, "failed upgrade left marker: %q, %v", marker, err)
		}
		var groups int64
		{
			err := db.Table("menus").Where("id = ?", "network-access-infrastructure").Count(&groups).Error
			require.NoError(t, err, "failed upgrade left a container: %d, %v", groups, err)
			require.Equal(t, int64(0), groups, "failed upgrade left a container: %d, %v", groups, err)
		}
		require.NoError(t, db.Exec(`DELETE FROM menus WHERE id = 'custom-path-owner'`).Error)
	})
	t.Run("upgrade_preserves_custom_data_and_bindings", func(t *testing.T) {
		verifyNetworkNavigationUpgrade(t, ctx, store, cfg, customIDs, before, initialMenuCount)
	})
	t.Run("previous_task52_marker_recovers_without_reseed", func(t *testing.T) {
		verifyNetworkNavigationLegacyMarker(t, ctx, store, cfg, initialMenuCount)
	})
	t.Run("second_start_is_idempotent", func(t *testing.T) {
		ids := append(customIDs, "network-access-sites", "network-access-resources", "network-access-infrastructure", "network-access-controls", "network-access-vpn", "network-access-monitoring")
		before := navigationRows(t, db, ids)
		require.NoError(t, seedDefaults(ctx, store, cfg))
		if after := navigationRows(t, db, ids); !reflect.DeepEqual(before, after) {
			t.Fatalf("second start changed menus: before %v after %v", before, after)
		}
	})
}

func verifyNetworkNavigationLegacyMarker(t *testing.T, ctx context.Context, store *dbstore.Store, cfg cfgpkg.Config, initialMenuCount int64) {
	t.Helper()
	db := store.DB()
	require.NoError(t, writeSeedVersion(ctx, db, bootstrapSeedVersionKey, networkNavigationSeedVersion))
	require.NoError(t, db.Exec(`DELETE FROM app_settings WHERE setting_key = ?`, networkNavigationSeedVersionKey).Error)
	require.NoError(t, seedDefaults(ctx, store, cfg))
	version, err := readSeedVersion(ctx, db, bootstrapSeedVersionKey)
	{
		require.NoError(t, err, "legacy bootstrap version not restored: %q, %v", version, err)
		require.Equal(t, "2026-09-22-oncall-settings-menu", version, "legacy bootstrap version not restored: %q, %v", version, err)
	}
	marker, err := readSeedVersion(ctx, db, networkNavigationSeedVersionKey)
	{
		require.NoError(t, err, "menu upgrade marker = %q, %v", marker, err)
		require.Equal(t, networkNavigationSeedVersion, marker, "menu upgrade marker = %q, %v", marker, err)
	}
	var bindings, menus int64
	{
		err := db.Table("menu_role_bindings").Where("id = ?", "navigation-binding").Count(&bindings).Error
		require.NoError(t, err, "legacy marker recovery lost binding: %d, %v", bindings, err)
		require.Equal(t, int64(1), bindings, "legacy marker recovery lost binding: %d, %v", bindings, err)
	}
	{
		err := db.Table("menus").Count(&menus).Error
		require.NoError(t, err, "legacy marker recovery replayed defaults: %d, %v", menus, err)
		require.Equal(t, initialMenuCount+4, menus, "legacy marker recovery replayed defaults: %d, %v", menus, err)
	}
}

func verifyNetworkNavigationUpgrade(t *testing.T, ctx context.Context, store *dbstore.Store, cfg cfgpkg.Config, customIDs, before []string, initialMenuCount int64) {
	t.Helper()
	db := store.DB()
	require.NoError(t, seedDefaults(ctx, store, cfg))
	if after := navigationRows(t, db, customIDs); !reflect.DeepEqual(before, after) {
		t.Fatalf("custom or disabled menus changed: before %v after %v", before, after)
	}
	var parent string
	{
		err := db.Raw(`SELECT parent_id FROM menus WHERE id='network-access-sites'`).Scan(&parent).Error
		require.NoError(t, err, "default site parent = %q, %v", parent, err)
		require.Equal(t, "network-access-infrastructure", parent, "default site parent = %q, %v", parent, err)
	}
	var label string
	{
		err := db.Raw(`SELECT label_zh FROM menus WHERE id='network-access-resources'`).Scan(&label).Error
		require.NoError(t, err, "default resource label = %q, %v", label, err)
		require.Equal(t, "网络资源", label, "default resource label = %q, %v", label, err)
	}
	var bindings, menus int64
	{
		err := db.Table("menu_role_bindings").Where("id = ?", "navigation-binding").Count(&bindings).Error
		require.NoError(t, err, "role binding lost: %d, %v", bindings, err)
		require.Equal(t, int64(1), bindings, "role binding lost: %d, %v", bindings, err)
	}
	{
		err := db.Table("menus").Count(&menus).Error
		require.NoError(t, err, "unrelated defaults were reseeded: %d, %v", menus, err)
		require.Equal(t, initialMenuCount+4, menus, "unrelated defaults were reseeded: %d, %v", menus, err)
	}
	version, err := readSeedVersion(ctx, db, bootstrapSeedVersionKey)
	{
		require.NoError(t, err, "upgrade version = %q, %v", version, err)
		require.Equal(t, "2026-09-22-oncall-settings-menu", version, "upgrade version = %q, %v", version, err)
	}
	marker, err := readSeedVersion(ctx, db, networkNavigationSeedVersionKey)
	{
		require.NoError(t, err, "menu upgrade marker = %q, %v", marker, err)
		require.Equal(t, networkNavigationSeedVersion, marker, "menu upgrade marker = %q, %v", marker, err)
	}
}
