package networkruntime

import (
	"context"
	"encoding/json"
	"github.com/DATA-DOG/go-sqlmock"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/radiusdeployment"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"testing"
	"time"
)

func contentTestDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, m, e := sqlmock.New()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, e := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	return db.WithContext(context.Background()), m
}
func contentBindingRow(now time.Time, runtime string, disconnect bool) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "nas_id", "runtime_id", "site_id", "name", "access_medium", "device_type", "ssid", "management_address", "status", "coa_supported", "disconnect_supported", "eap_server_trust", "created_at", "updated_at"}).AddRow("binding", "nas", runtime, "site", "NAS", "wired", "", "", "", "active", false, disconnect, []byte("null"), now, now)
}
func TestContentNASRouteKeepsLegacyAndUnknownFailClosed(t *testing.T) {
	for _, disconnect := range []bool{true, false} {
		db, m := contentTestDB(t)
		now := time.Now()
		m.ExpectQuery(`SELECT id, nas_id, runtime_id, site_id, name,`).WithArgs("external", "nas").WillReturnRows(contentBindingRow(now, "external", disconnect))
		if disconnect {
			m.ExpectQuery(`WITH current_control AS`).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
		}
		ok, e := contentNASEnforceable(db, "external", "nas", "site", "endpoint", "device", "subject", now)
		if e != nil || ok != disconnect {
			t.Fatalf("legacy=%v err=%v", ok, e)
		}
		if e := m.ExpectationsWereMet(); e != nil {
			t.Fatal(e)
		}
	}
	db, m := contentTestDB(t)
	m.ExpectQuery(`SELECT pool_id,record FROM network_radius_members`).WithArgs("radius-member:unknown").WillReturnRows(sqlmock.NewRows([]string{"pool_id", "record"}))
	ok, e := contentNASEnforceable(db, "radius-member:unknown", "nas", "site", "endpoint", "device", "subject", time.Now())
	if e != nil || ok {
		t.Fatalf("unknown route=%v %v", ok, e)
	}
	if e := m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
func TestContentNASManagedTLSUsesCurrentMemberAndEndpointCredential(t *testing.T) {
	db, m := contentTestDB(t)
	now := time.Now().UTC()
	zero := 0
	runtime := "radius-member:member"
	state := domain.State{SlotHash: "installed", Pool: api.NetworkRADIUSPool{PoolID: "pool", NASBindingID: "binding", LogicalRuntimeID: "radius-pool:pool", SiteID: "site", OwnerEpoch: 1, ConfigurationRevision: 1, ConfigurationHash: "hash", DesiredReplicas: 1, Enabled: true}, ControlRevision: 1}
	c := api.NetworkRADIUSRuntimeControl{PoolID: "pool", MemberID: "member", RuntimeID: runtime, CredentialID: "runtime-credential", ProcessEpoch: "epoch", OwnerEpoch: 1, ControlRevision: 1, ConfigurationRevision: 1, ConfigurationHash: "hash", RunProcess: true, AcceptNewAuthentications: true, ServeExistingUntil: now.Add(time.Hour), ControlLeaseUntil: now.Add(time.Second), ConfigurationLeaseUntil: now.Add(time.Second)} // #nosec G101 -- Synthetic runtime credential identity for the in-memory receipt fixture.
	a := api.NetworkRADIUSRuntimeACK{PoolID: c.PoolID, MemberID: c.MemberID, RuntimeID: runtime, CredentialID: c.CredentialID, ProcessEpoch: c.ProcessEpoch, OwnerEpoch: 1, ControlRevision: 1, ConfigurationRevision: 1, ConfigurationHash: c.ConfigurationHash, StateCountKnown: true, ActiveStates: &zero, Ready: true, ConfigurationApplied: true, HooksReady: true}
	record := domain.MemberRecord{Member: domain.Member{MemberID: "member", RuntimeID: runtime}, Control: &c, ACK: &a, ReceivedAt: &now}
	stateJSON, _ := json.Marshal(state)
	recordJSON, _ := json.Marshal(record)
	load := func() {
		m.ExpectQuery(`SELECT pool_id,record FROM network_radius_members`).WithArgs(runtime).WillReturnRows(sqlmock.NewRows([]string{"pool_id", "record"}).AddRow("pool", recordJSON))
		m.ExpectExec(`SELECT pg_advisory_xact_lock`).WithArgs("radius-pool:pool").WillReturnResult(sqlmock.NewResult(0, 1))
		m.ExpectQuery(`SELECT state FROM network_radius_pools`).WithArgs("pool").WillReturnRows(sqlmock.NewRows([]string{"state"}).AddRow(stateJSON))
		m.ExpectQuery(`SELECT record FROM network_radius_members`).WithArgs(runtime).WillReturnRows(sqlmock.NewRows([]string{"record"}).AddRow(recordJSON))
	}
	load()
	m.ExpectQuery(`SELECT certificate_fingerprint FROM network_runtime_credentials`).WithArgs("runtime-credential", runtime).WillReturnRows(sqlmock.NewRows([]string{"certificate_fingerprint"}).AddRow("fingerprint"))
	load()
	m.ExpectQuery(`SELECT id,expires_at,capabilities FROM network_runtime_credentials`).WithArgs(runtime, "fingerprint", now, now).WillReturnRows(sqlmock.NewRows([]string{"id", "expires_at", "capabilities"}).AddRow("runtime-credential", now.Add(time.Hour), []byte(`["radius-managed-runtime-v1"]`)))
	m.ExpectQuery(`SELECT id, nas_id, runtime_id, site_id, name,`).WithArgs("binding").WillReturnRows(contentBindingRow(now, "radius-pool:pool", true))
	m.ExpectQuery(`WITH current_control AS`).WithArgs("device", "subject", now, now, "endpoint", "endpoint", now, now).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	ok, e := contentNASEnforceable(db, runtime, "nas", "site", "endpoint", "device", "subject", now)
	if e != nil || !ok {
		t.Fatalf("managed route=%v %v", ok, e)
	}
	if e := m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
