package networkproxy

import (
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
	"testing"
	"time"
)

type allowBatchFence struct{}

func (allowBatchFence) LockSingleDesired(context.Context, *gorm.DB, string) error { return nil }

func (allowBatchFence) LockDesiredBatch(context.Context, *gorm.DB, domain.DesiredBatch) error {
	return nil
}
func TestDesiredBatchCASRollsBackAllMembers(t *testing.T) {
	r, m := commandTestRepository(t)
	r.SetBatchFence(allowBatchFence{})
	m.ExpectBegin()
	m.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectQuery("SELECT .*network_proxy_desired_batch_receipts").WillReturnRows(sqlmock.NewRows([]string{"receipt"}))
	m.ExpectQuery("SELECT .*network_proxy_instances.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"id", "desired_revision"}).AddRow("p1", 1))
	m.ExpectQuery("SELECT .*network_proxy_instances.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"id", "desired_revision"}).AddRow("p2", 9))
	m.ExpectRollback()
	err := r.CommitDesiredBatch(context.Background(), domain.DesiredBatch{PublicationID: "pub1", Members: []domain.DesiredMember{{InstanceID: "p2", ExpectedRevision: 1}, {InstanceID: "p1", ExpectedRevision: 1}}}, time.Now())
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("error=%v", err)
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestDesiredBatchIntentExcludesRandomCiphertext(t *testing.T) {
	batch := domain.DesiredBatch{PublicationID: "pub1", MasterID: "p1", Members: []domain.DesiredMember{{InstanceID: "p1", ExpectedRevision: 1, ContentHash: "hash", Encrypted: "cipher-one"}}}
	first := batchIntentHash(batch, batch.Members)
	batch.Members[0].Encrypted = "cipher-two"
	if batchIntentHash(batch, batch.Members) != first {
		t.Fatal("ciphertext broke idempotence")
	}
	batch.Members[0].ExpectedRevision++
	if batchIntentHash(batch, batch.Members) == first {
		t.Fatal("revision not bound to receipt")
	}
}
