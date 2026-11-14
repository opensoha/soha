package networkproxy

import (
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
	"time"
)

func TestPublicationApplyAndMemberDigestSharePersistedIdentity(t *testing.T) {
	row := instanceRow{BundleID: "bundle-1", BundleRevision: 2, DesiredHash: "content-hash", Enabled: true}
	desired := domain.Configuration{ContentHash: row.DesiredHash, Enabled: true, Publication: &api.NetworkProxyServerPublication{BundleID: "bundle-1", Revision: 2}}
	if configurationForDigest(row).Digest() != desired.Digest() {
		t.Fatal("apply/member digest differs from runtime")
	}
	if got := row.domain(); got.BundleID != "bundle-1" || got.BundleRevision != 2 {
		t.Fatal("publication identity missing from runtime instance")
	}
}

func TestConfigurationLeaseRejectsStaleRevisionAndProtection(t *testing.T) {
	now := time.Now().UTC()
	row := instanceRow{ID: "p1", DesiredRevision: 4, DesiredHash: "content", Enabled: true}
	digest := configurationForDigest(row).Digest()
	for _, revision := range []int64{4, 5} {
		t.Run(time.Duration(revision).String(), func(t *testing.T) {
			r, m := commandTestRepository(t)
			m.ExpectBegin()
			m.ExpectQuery("SELECT .*network_proxy_instances.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"id", "desired_revision", "desired_hash", "enabled"}).AddRow("p1", revision, "content", true))
			if revision == 4 {
				m.ExpectExec("UPDATE .*network_proxy_instances.*configuration_valid_until").WillReturnResult(sqlmock.NewResult(0, 1))
				m.ExpectCommit()
			} else {
				m.ExpectRollback()
			}
			err := r.GrantConfigurationLease(context.Background(), "p1", 4, digest, now.Add(time.Minute), now)
			if revision == 4 && err != nil || revision == 5 && !errors.Is(err, apperrors.ErrConflict) {
				t.Fatal(err)
			}
			if err = m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
	lease := now.Add(time.Minute)
	row.ConfigurationValidUntil = &lease
	row.ConfigurationLeaseRevision = 4
	row.ConfigurationLeaseHash = digest
	if configurationLease(row) == nil {
		t.Fatal("matching grant hidden")
	}
	row.DesiredRevision++
	if configurationLease(row) != nil {
		t.Fatal("stale revision grant exposed")
	}
	row.DesiredRevision = 4
	row.BindingJSON = []byte(`{"siteId":"site","deviceId":"device","resourceIds":[]}`)
	if configurationLease(row) != nil {
		t.Fatal("changed enforcement reused old grant")
	}
}
