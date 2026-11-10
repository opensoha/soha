package bootstrap

import (
	"context"
	"github.com/stretchr/testify/require"
	"reflect"
	"testing"
	"time"
)

func TestFlatKubernetesNetworkMenusWithPostgres(t *testing.T) {
	t.Run("preserves-customization-bindings-and-idempotency", func(t *testing.T) {
		store := networkNavigationTestStore(t)
		db, ctx := store.DB(), context.Background()
		items := []menuSeed{
			{ID: "network", Path: "/network", Enabled: true},
			{ID: "network-routing", ParentID: "network", Path: "/network/routing", Enabled: true},
			{ID: "network-backends", ParentID: "network", Path: "/network/backends", Enabled: true},
			{ID: "network-services", ParentID: "network-backends", Path: "/network/services", LabelZH: "自定义服务", LabelEN: "Custom services", SortOrder: 87, Enabled: true},
			{ID: "custom-network", ParentID: "network-routing", Path: "/custom-network", LabelZH: "自定义子菜单", SortOrder: 94, Enabled: true},
		}
		require.NoError(t, upsertMenus(ctx, db, items, time.Now()))
		require.NoError(t, db.Exec(`INSERT INTO roles (id,name,scope,capabilities,permission_keys) VALUES ('custom','Custom','workspace','[]','["custom.permission"]')`).Error)
		require.NoError(t, db.Exec(`INSERT INTO menu_role_bindings (id,menu_id,role_id) VALUES ('service-binding','network-services','custom'), ('group-binding','network-routing','custom')`).Error)
		require.NoError(t, writeSeedVersion(ctx, db, kubernetesWorkbenchSeedVersionKey, kubernetesWorkbenchSeedVersion))
		require.NoError(t, upgradeKubernetesWorkbenchSeeds(ctx, db))
		var actual []menuSeed
		require.NoError(t, db.Table("menus").Where("id IN ?", []string{"network-services", "custom-network"}).Order("id").Find(&actual).Error)
		if len(actual) != 2 || actual[0].ParentID != "network" || actual[0].LabelZH != "自定义子菜单" || actual[0].SortOrder != 94 || actual[1].ParentID != "network" || actual[1].LabelZH != "自定义服务" || actual[1].SortOrder != 87 || actual[1].Path != "/network/services" {
			t.Fatalf("customized children changed: %+v", actual)
		}
		var enabled, bindings int64
		{
			err := db.Table("menus").Where("id IN ? AND enabled=TRUE", []string{"network-routing", "network-backends"}).Count(&enabled).Error
			require.NoError(t, err, "legacy groups visible: %d, %v", enabled, err)
			require.Equal(t, int64(0), enabled, "legacy groups visible: %d, %v", enabled, err)
		}
		{
			err := db.Table("menu_role_bindings").Where("role_id='custom'").Count(&bindings).Error
			require.NoError(t, err, "bindings lost: %d, %v", bindings, err)
			require.Equal(t, int64(2), bindings, "bindings lost: %d, %v", bindings, err)
		}
		var permissions string
		{
			err := db.Raw(`SELECT permission_keys::text FROM roles WHERE id='custom'`).Scan(&permissions).Error
			require.NoError(t, err, "custom permissions changed: %s, %v", permissions, err)
			require.Equal(t, `["custom.permission"]`, permissions, "custom permissions changed: %s, %v", permissions, err)
		}
		ids := []string{"network-services", "custom-network", "network-routing", "network-backends"}
		first := navigationRows(t, db, ids)
		require.NoError(t, upgradeKubernetesWorkbenchSeeds(ctx, db))
		if !reflect.DeepEqual(first, navigationRows(t, db, ids)) {
			t.Fatal("second startup changed flattened rows")
		}
	})
	t.Run("rolls-back-children-and-marker-on-error", func(t *testing.T) {
		store := networkNavigationTestStore(t)
		db, ctx := store.DB(), context.Background()
		if err := upsertMenus(ctx, db, []menuSeed{
			{ID: "network", Path: "/network", Enabled: true},
			{ID: "network-routing", ParentID: "network", Path: "/network/routing", Enabled: true},
			{ID: "network-services", ParentID: "network-routing", Path: "/network/services", Enabled: true},
		}, time.Now()); err != nil {
			t.Fatal(err)
		}
		require.NoError(t, db.Exec(`CREATE FUNCTION reject_group_update() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.id='network-routing' AND NEW.enabled=FALSE THEN RAISE EXCEPTION 'test-only failure'; END IF; RETURN NEW; END $$`).Error)
		require.NoError(t, db.Exec(`CREATE TRIGGER reject_group_update BEFORE UPDATE ON menus FOR EACH ROW EXECUTE FUNCTION reject_group_update()`).Error)
		before := navigationRows(t, db, []string{"network-services", "network-routing"})
		if err := upgradeFlatKubernetesNetworkMenus(ctx, db); err == nil {
			t.Fatal("injected failure must stop the upgrade")
		}
		if !reflect.DeepEqual(before, navigationRows(t, db, []string{"network-services", "network-routing"})) {
			t.Fatal("failed upgrade changed menus")
		}
		{
			version, err := readSeedVersion(ctx, db, flatKubernetesNetworkMenuVersionKey)
			require.NoError(t, err, "failed upgrade advanced marker: %q, %v", version, err)
			require.Empty(t, version, "failed upgrade advanced marker: %q, %v", version, err)
		}
	})
}
