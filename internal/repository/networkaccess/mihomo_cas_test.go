package networkaccess

import (
	"context"
	"database/sql/driver"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	access "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"regexp"
	"testing"
	"time"
)

func TestMihomoProfileUpdateCASRejectsConcurrentRevision(t *testing.T) {
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	g, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	repository := New(g)
	args := make([]driver.Value, 0, 23)
	for i := 0; i < 21; i++ {
		args = append(args, sqlmock.AnyArg())
	}
	args = append(args, "profile-1", 7)
	m.ExpectExec("UPDATE network_mihomo_profiles SET.*" + regexp.QuoteMeta("AND id = $22 AND revision = $23")).WithArgs(args...).WillReturnResult(sqlmock.NewResult(0, 0))
	_, err = repository.UpdateMihomoProfile(context.Background(), "profile-1", access.MihomoProfile{BundleID: "old-binding"}, 7, time.Now())
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("concurrent binding overwritten", err)
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestMihomoProfileUpdateRejectsMissingCAS(t *testing.T) {
	repository := New(nil)
	if _, err := repository.UpdateMihomoProfile(context.Background(), "profile", access.MihomoProfile{}, 0, time.Now()); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("zero revision bypassed CAS", err)
	}
}
