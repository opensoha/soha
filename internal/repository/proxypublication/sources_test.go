package proxypublication

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"regexp"
	"testing"
	"time"
)

func testRepository(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	g, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return New(g), m
}

func sourceRows(revision, floor int64, material map[string]string, deleted any) *sqlmock.Rows {
	raw, _ := json.Marshal(material)
	return sqlmock.NewRows([]string{"instance_id", "source_id", "revision", "compatibility_floor", "node_material", "deleted_at", "public"}).AddRow("instance", "source", revision, floor, raw, deleted, []byte(`{"sourceId":"source","revision":1}`))
}
func expectPolicy(m sqlmock.Sqlmock) {
	m.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).WithArgs("network-access-policy-snapshot:default:default").WillReturnResult(sqlmock.NewResult(0, 1))
}
func sourceInput(material map[string]string) domain.SourceRecord {
	return domain.SourceRecord{InstanceID: "instance", Public: api.NetworkProxySource{SourceID: "source", Revision: 2, Name: "Renamed"}, EncryptedInput: "encrypted-input", EncryptedIR: "encrypted-ir", NodeMaterial: material}
}
func TestSourceCASFailureDoesNotRevokeOtherCredentials(t *testing.T) {
	r, m := testRepository(t)
	m.ExpectBegin()
	expectPolicy(m)
	m.ExpectQuery("SELECT .*network_proxy_sources.*FOR UPDATE").WillReturnRows(sourceRows(3, 1, map[string]string{"node-b": "b"}, nil))
	m.ExpectRollback()
	if err := r.SaveSource(context.Background(), sourceInput(map[string]string{"node-b": "b"}), 1); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestSourceRenameAndAdditionKeepImmutableBundles(t *testing.T) {
	for _, add := range []bool{false, true} {
		t.Run(map[bool]string{false: "rename", true: "add-c"}[add], func(t *testing.T) {
			r, m := testRepository(t)
			before := map[string]string{"node-a": "a", "node-b": "b"}
			next := map[string]string{"node-a": "a", "node-b": "b"}
			if add {
				next["node-c"] = "c"
			}
			m.ExpectBegin()
			expectPolicy(m)
			m.ExpectQuery("SELECT .*network_proxy_sources.*FOR UPDATE").WillReturnRows(sourceRows(1, 1, before, nil))
			m.ExpectExec("UPDATE .*network_proxy_sources").WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectQuery("SELECT .*network_proxy_bundles").WillReturnRows(sqlmock.NewRows([]string{"record"}))
			m.ExpectCommit()
			if err := r.SaveSource(context.Background(), sourceInput(next), 1); err != nil {
				t.Fatal(err)
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal("rename/add must not insert invalidations or delete all credentials", err)
			}
		})
	}
}
func TestOldBundleNodeInvalidationSurvivesRestore(t *testing.T) {
	for _, test := range []struct {
		name     string
		current  int64
		material map[string]string
		invalid  []string
		want     []string
	}{
		{"add-c", 2, map[string]string{"node-a": "a", "node-b": "b", "node-c": "c"}, nil, []string{"node-a", "node-b"}},
		{"delete-a", 2, map[string]string{"node-b": "b"}, []string{"node-a"}, []string{"node-b"}},
		{"restore-a", 3, map[string]string{"node-a": "a", "node-b": "b"}, []string{"node-a"}, []string{"node-b"}},
		{"auth-change-a", 2, map[string]string{"node-a": "new-auth", "node-b": "b"}, []string{"node-a"}, []string{"node-b"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			r, m := testRepository(t)
			m.ExpectQuery("SELECT .*network_proxy_sources").WillReturnRows(sourceRows(test.current, 1, test.material, nil))
			rows := sqlmock.NewRows([]string{"node_id"})
			for _, id := range test.invalid {
				rows.AddRow(id)
			}
			m.ExpectQuery("SELECT .*network_proxy_node_invalidations").WithArgs("instance", "source", "node-a", "node-b", int64(1)).WillReturnRows(rows)
			got, err := r.AllowedSourceNodes(context.Background(), "instance", "source", 1, []string{"node-a", "node-b"})
			if err != nil || fmtNodes(got) != fmtNodes(test.want) {
				t.Fatal(got, err)
			}
			if err = m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func fmtNodes(nodes []string) string { raw, _ := json.Marshal(nodes); return string(raw) }
func TestDeletedSourceAndAmbiguousLegacyCannotGrant(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		r, m := testRepository(t)
		var at any
		if deleted {
			at = time.Now()
		}
		m.ExpectQuery("SELECT .*network_proxy_sources").WillReturnRows(sourceRows(2, 2, map[string]string{}, at))
		if _, err := r.AllowedSourceNodes(context.Background(), "instance", "source", 1, []string{"node-a"}); !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatal(err)
		}
		if err := m.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	}
}

type scopedCredentialJSON struct {
	id     string
	nodes  []string
	secret string
}

func (a scopedCredentialJSON) Match(value driver.Value) bool {
	raw, ok := value.(string)
	if !ok {
		return false
	}
	var c domain.Credential
	return json.Unmarshal([]byte(raw), &c) == nil && c.ID == a.id && fmtNodes(c.NodeIDs) == fmtNodes(a.nodes) && c.EncryptedSecret == a.secret
}
func TestSourceNodeDeletionNarrowsCredentialInSameTransaction(t *testing.T) {
	r, m := testRepository(t)
	bundle := domain.BundleRecord{InstanceID: "instance", SourceNodeIDs: []string{"node-a", "node-b"}, Scope: api.NetworkProxyScope{NodeIDs: []string{"node-a", "node-b"}}, Public: api.NetworkProxyBundle{BundleID: "bundle", SourceID: "source", SourceRevision: 1, DeliveryMode: "mapped_nodes"}}
	rawBundle, _ := json.Marshal(bundle)
	credential := domain.Credential{ID: "credential", InstanceID: "instance", BundleID: "bundle", NodeIDs: []string{"node-a", "node-b"}, EncryptedSecret: "encrypted-existing"}
	rawCredential, _ := json.Marshal(credential)
	m.ExpectBegin()
	expectPolicy(m)
	m.ExpectQuery("SELECT .*network_proxy_sources.*FOR UPDATE").WillReturnRows(sourceRows(1, 1, map[string]string{"node-a": "a", "node-b": "b"}, nil))
	m.ExpectExec("INSERT INTO network_proxy_node_invalidations").WithArgs("instance", "source", "node-a", int64(2)).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("UPDATE .*network_proxy_sources").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectQuery("SELECT .*network_proxy_bundles").WillReturnRows(sqlmock.NewRows([]string{"record"}).AddRow(rawBundle))
	m.ExpectQuery("SELECT .*network_proxy_sources").WillReturnRows(sourceRows(2, 1, map[string]string{"node-b": "b"}, nil))
	m.ExpectQuery("SELECT .*network_proxy_node_invalidations").WillReturnRows(sqlmock.NewRows([]string{"node_id"}).AddRow("node-a"))
	m.ExpectQuery("SELECT .*network_proxy_employee_credentials").WillReturnRows(sqlmock.NewRows([]string{"id", "record"}).AddRow("credential", rawCredential))
	m.ExpectQuery("SELECT .*network_proxy_credential_scopes.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"node_ids"}))
	m.ExpectExec("INSERT INTO network_proxy_credential_scopes").WithArgs(domain.CredentialIdentity(credential), `["node-b"]`).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("UPDATE .*network_proxy_employee_credentials").WithArgs(scopedCredentialJSON{"credential", []string{"node-b"}, "encrypted-existing"}, "credential").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	if err := r.SaveSource(context.Background(), sourceInput(map[string]string{"node-b": "b"}), 1); err != nil {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestInvalidationFailureRollsBackSourceCAS(t *testing.T) {
	r, m := testRepository(t)
	m.ExpectBegin()
	expectPolicy(m)
	m.ExpectQuery("SELECT .*network_proxy_sources.*FOR UPDATE").WillReturnRows(sourceRows(1, 1, map[string]string{"node-a": "a", "node-b": "b"}, nil))
	m.ExpectExec("INSERT INTO network_proxy_node_invalidations").WillReturnError(errors.New("storage unavailable"))
	m.ExpectRollback()
	if err := r.SaveSource(context.Background(), sourceInput(map[string]string{"node-b": "b"}), 1); err == nil {
		t.Fatal("partial mutation accepted")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestDeletedSourceIDCannotBeRecreated(t *testing.T) {
	r, m := testRepository(t)
	record := sourceInput(map[string]string{"node-a": "a"})
	record.Public.Revision = 1
	m.ExpectBegin()
	expectPolicy(m)
	m.ExpectExec("INSERT INTO network_proxy_sources.*ON CONFLICT DO NOTHING").WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectRollback()
	if err := r.SaveSource(context.Background(), record, 0); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("tombstoned ID recreated", err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
