package workflow

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"regexp"
	"testing"
)

func TestDefinitionFiltersPrecedeWorkflowLimits(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sqlDB.Close() }()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	query := `SELECT ` + workflowColumns + ` FROM workflow_runs WHERE scope = 'application' AND application_id = $1 AND metadata->>'bindingId' = $2 ORDER BY created_at DESC, id DESC LIMIT $3`
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs("app", "env", 10).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, err := New(db).List(context.Background(), "app", "env", 10); err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id FROM delivery_batches WHERE TRUE AND snapshot->>'workflowId' = $1 ORDER BY created_at DESC, id DESC LIMIT $2`)).WithArgs("definition", 10).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("batch"))
	ids, err := New(db).ListDeliveryBatchIDs(context.Background(), "", "", "definition", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "batch" {
		t.Fatalf("unexpected batch IDs: %v", ids)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
