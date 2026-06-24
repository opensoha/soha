package bootstrap

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

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

	deprecatedIDs := []string{"ai-workbench-gateway", "assistant"}
	mock.ExpectExec(`DELETE FROM menu_role_bindings WHERE menu_id IN`).WithArgs(deprecatedIDs[0], deprecatedIDs[1]).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`DELETE FROM menus WHERE id IN`).WithArgs(deprecatedIDs[0], deprecatedIDs[1]).WillReturnResult(sqlmock.NewResult(0, 2))

	if err := cleanupDeprecatedMenus(context.Background(), db, deprecatedIDs); err != nil {
		t.Fatalf("cleanupDeprecatedMenus returned error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
