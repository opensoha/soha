package delivery

import (
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestOutpostRuntimeClaimsRejectGenericAndMismatchedIdentity(t *testing.T) {
	// A nil database deliberately proves rejection happens before any SQL access.
	repo := New(nil)
	for _, test := range []struct {
		name      string
		ctx       context.Context
		providers []string
		agent     string
	}{
		{"generic private provider", context.Background(), []string{"outpost_runtime.edge"}, "edge"},
		{"mixed providers", context.Background(), []string{"ci_agent_runner", "outpost_runtime.edge"}, "edge"},
		{"wrong agent", domain.WithOutpostRuntimeClaim(context.Background(), "edge"), []string{"outpost_runtime.edge"}, "other"},
		{"wrong provider", domain.WithOutpostRuntimeClaim(context.Background(), "edge"), []string{"outpost_runtime.other"}, "edge"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := repo.ClaimExecutionTask(test.ctx, test.providers, test.agent, ""); !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("got %v", err)
			}
		})
	}
	if _, err := repo.ClaimExecutionTask(context.Background(), nil, "edge", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty providers = %v", err)
	}
}

func TestPrivateTimeoutLostHeartbeatRaceDoesNotOverwriteTask(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	// Zero affected rows models a heartbeat/token change or a concurrent terminal
	// callback between the maintenance read and its compare-and-set update.
	mock.ExpectExec(`UPDATE execution_tasks[\s\S]*status NOT IN \('completed','failed','canceled','callback_timeout'\)[\s\S]*callback_token IS NOT DISTINCT FROM[\s\S]*last_heartbeat_at IS NOT DISTINCT FROM`).WillReturnResult(sqlmock.NewResult(0, 0))
	heartbeat := time.Now().UTC().Add(-time.Minute)
	_, err = New(db).UpdateExecutionTask(context.Background(), domain.ExecutionTask{ID: "private", TaskKind: "outpost_runtime_control", ProviderKind: "outpost_runtime.edge", Status: "callback_timeout", CallbackToken: "attempt-token", LastHeartbeatAt: &heartbeat, Payload: map[string]any{}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("lost timeout CAS returned %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
