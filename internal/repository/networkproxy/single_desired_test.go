package networkproxy

import (
	"context"
	"errors"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

type poolOwnedFence struct{ calls int }

func (*poolOwnedFence) LockDesiredBatch(context.Context, *gorm.DB, domain.DesiredBatch) error {
	return nil
}
func (f *poolOwnedFence) LockSingleDesired(_ context.Context, _ *gorm.DB, _ string) error {
	f.calls++
	return apperrors.ErrConflict
}

func TestSingleDesiredCannotOverwritePoolMember(t *testing.T) {
	r, mock := commandTestRepository(t)
	fence := &poolOwnedFence{}
	r.SetBatchFence(fence)
	mock.ExpectBegin()
	mock.ExpectRollback()
	_, err := r.UpdateConfiguration(context.Background(), "member-1", 3, false, "encrypted", "hash", nil, false, time.Now())
	if !errors.Is(err, apperrors.ErrConflict) || fence.calls != 1 {
		t.Fatalf("pool-owned update was not fenced: err=%v calls=%d", err, fence.calls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSingleDesiredRequiresInstalledOwnershipFence(t *testing.T) {
	r, mock := commandTestRepository(t)
	_, err := r.UpdateConfiguration(context.Background(), "member-1", 3, false, "encrypted", "hash", nil, false, time.Now())
	if !errors.Is(err, apperrors.ErrServiceUnavailable) {
		t.Fatalf("missing fence: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
