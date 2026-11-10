package delivery_test

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	repo "github.com/opensoha/soha/internal/repository/delivery"
	"gorm.io/gorm"
)

func TestServiceReferenceImpactMigrationPreservesV1Postgres(t *testing.T) {
	store := openDraftReceiptTestStore(t)
	tx := store.DB().WithContext(t.Context()).Begin()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(tx.Error)
	defer func() { must(tx.Rollback().Error) }()
	schema := "impact_upgrade_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	must(tx.Exec("CREATE SCHEMA " + schema).Error)
	must(tx.Exec("SET LOCAL search_path TO " + schema).Error)
	must(tx.Exec(`CREATE TABLE delivery_plans(id text PRIMARY KEY)`).Error)
	must(tx.Exec(`INSERT INTO delivery_plans(id) VALUES ('legacy'),('new')`).Error)

	// 0125 created this evidence table. Its v1 constraint was unchanged through 0140.
	legacyDDL, err := os.ReadFile("../../../migrations/postgres/0125_service_reference_plan_impacts.sql")
	must(err)
	must(tx.Exec(string(legacyDDL)).Error)
	snapshot := func(id string, version int) string {
		t.Helper()
		raw, err := json.Marshal(dd.ServiceReferencePlanImpact{
			Version: version, PlanID: id, ApplicationID: "application", SourceBindingID: "source",
			Revision: strings.Repeat("a", 64), DatabaseRevision: strings.Repeat("b", 64),
			Consumers: []dd.ServiceReferenceRecord{}, SourceRevisions: map[string]string{"service": strings.Repeat("c", 64)},
		})
		must(err)
		return string(raw)
	}
	insert := func(db *gorm.DB, id, raw string) error {
		return db.Exec(`INSERT INTO delivery_plan_service_reference_impacts(plan_id,snapshot) VALUES (?,?::jsonb)`, id, raw).Error
	}
	rejected := func(raw string) {
		t.Helper()
		err := tx.Transaction(func(db *gorm.DB) error { return insert(db, "new", raw) })
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("expected snapshot check rejection, got %v", err)
		}
	}
	must(insert(tx, "legacy", snapshot("legacy", 1)))
	rejected(snapshot("new", 2))
	var before, after string
	must(tx.Raw(`SELECT snapshot::text FROM delivery_plan_service_reference_impacts WHERE plan_id='legacy'`).Row().Scan(&before))
	upgradeDDL, err := os.ReadFile("../../../migrations/postgres/0141_service_reference_impact_projection.sql")
	must(err)
	must(tx.Exec(string(upgradeDDL)).Error)
	must(tx.Raw(`SELECT snapshot::text FROM delivery_plan_service_reference_impacts WHERE plan_id='legacy'`).Row().Scan(&after))
	if before != after {
		t.Fatal("0141 rewrote frozen v1 evidence")
	}
	rejected(snapshot("new", 3))
	rejected(snapshot("wrong-id", 2))
	must(insert(tx, "new", snapshot("new", 2)))
	legacy, err := repo.New(tx).GetDeliveryPlanServiceReferenceSnapshot(t.Context(), "legacy")
	must(err)
	if legacy.Version != 1 || legacy.Revision != strings.Repeat("a", 64) || legacy.DatabaseRevision != strings.Repeat("b", 64) {
		t.Fatal("v1 digest did not survive 0141")
	}
}
