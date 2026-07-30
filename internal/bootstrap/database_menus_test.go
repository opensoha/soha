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
