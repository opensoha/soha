package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domain "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type privateClaimCase struct {
	name      string
	ctx       context.Context
	providers []string
	agent     string
}

func assertPrivateClaimRejections(t *testing.T, tests []privateClaimCase) {
	t.Helper()
	repo := New(nil)
	for _, test := range tests {
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
func assertPrivateTimeoutCAS(t *testing.T, kind, provider string) {
	t.Helper()
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
	_, err = New(db).UpdateExecutionTask(context.Background(), domain.ExecutionTask{ID: "private", TaskKind: kind, ProviderKind: provider, Status: "callback_timeout", CallbackToken: "attempt-token", LastHeartbeatAt: &heartbeat, Payload: map[string]any{}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("lost timeout CAS returned %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
