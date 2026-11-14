package proxypublication

import (
	"context"
	"encoding/json"
	"github.com/DATA-DOG/go-sqlmock"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"testing"
	"time"
)

func credentialCandidate() domain.Credential {
	return domain.Credential{ID: "new-id", InstanceID: "instance", BundleID: "bundle", EndpointCredentialID: "endpoint-cert", RuntimeID: "endpoint", ProfileID: "profile", ProcessEpoch: "boot", Revision: 1, Epoch: 1, UserID: "user", DeviceID: "device", NodeIDs: []string{"node-a", "node-b"}, EncryptedSecret: "new-secret", SecretHash: "new-hash", ExpiresAt: time.Now().UTC().Add(time.Minute)}
}
func expectCredentialAuthorization(m sqlmock.Sqlmock, c domain.Credential, revoked bool) {
	bundle := domain.BundleRecord{InstanceID: c.InstanceID, SourceNodeIDs: []string{"node-a", "node-b"}, Scope: api.NetworkProxyScope{NodeIDs: []string{"node-a", "node-b"}}, Public: api.NetworkProxyBundle{BundleID: c.BundleID, Revision: c.Revision, ActiveRevision: c.Revision, CredentialEpoch: c.Epoch, State: "activated", SourceID: "source", SourceRevision: 1, OrgRevision: 0, DeliveryMode: "mapped_nodes"}}
	raw, _ := json.Marshal(bundle)
	m.ExpectQuery("SELECT .*network_proxy_bundles").WillReturnRows(sqlmock.NewRows([]string{"record"}).AddRow(raw))
	material := map[string]string{"node-a": "a", "node-b": "b"}
	if revoked {
		material = map[string]string{"node-b": "b"}
	}
	m.ExpectQuery("SELECT .*network_proxy_sources").WillReturnRows(sourceRows(2, 1, material, nil))
	events := sqlmock.NewRows([]string{"node_id"})
	if revoked {
		events.AddRow("node-a")
	}
	m.ExpectQuery("SELECT .*network_proxy_node_invalidations").WillReturnRows(events)
	m.ExpectQuery("SELECT .*network_proxy_rule_sets").WillReturnRows(sqlmock.NewRows([]string{"revision", "rules"}))
	m.ExpectQuery("SELECT count.*network_runtime_credentials").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
}
func TestCredentialLegacyScopeKeysMergeByIntersection(t *testing.T) {
	r, m := testRepository(t)
	c := credentialCandidate()
	first := c
	first.ID = "old-first"
	first.RenewalKey = "legacy-a-b"
	first.EncryptedSecret = "retained-secret"
	first.SecretHash = "retained-hash"
	second := first
	second.ID = "old-second"
	second.RenewalKey = "legacy-b"
	second.NodeIDs = []string{"node-b"}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	key := domain.CredentialIdentity(c)
	m.ExpectBegin()
	expectPolicy(m)
	expectCredentialAuthorization(m, c, false)
	m.ExpectQuery("SELECT .*network_proxy_employee_credentials.*FOR UPDATE").WithArgs(c.InstanceID, c.BundleID, c.EndpointCredentialID, c.RuntimeID, c.ProfileID, c.ProcessEpoch, c.Revision, c.Epoch).WillReturnRows(sqlmock.NewRows([]string{"id", "renewal_key", "record", "renewable"}).AddRow(first.ID, first.RenewalKey, a, true).AddRow(second.ID, second.RenewalKey, b, true))
	m.ExpectQuery("SELECT .*network_proxy_credential_scopes.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"node_ids"}))
	m.ExpectExec("INSERT INTO network_proxy_credential_scopes").WithArgs(key, `["node-b"]`).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("UPDATE .*network_proxy_employee_credentials").WithArgs(scopedCredentialJSON{first.ID, []string{"node-b"}, first.EncryptedSecret}, first.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("UPDATE .*network_proxy_employee_credentials").WithArgs(scopedCredentialJSON{second.ID, []string{"node-b"}, second.EncryptedSecret}, second.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("DELETE FROM .*network_proxy_employee_credentials").WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectExec("UPDATE .*network_proxy_employee_credentials").WithArgs(c.ExpiresAt, scopedCredentialJSON{first.ID, []string{"node-b"}, first.EncryptedSecret}, key, first.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	issued, err := r.IssueCredential(context.Background(), c)
	if err != nil || issued.ID != first.ID || issued.SecretHash != first.SecretHash || fmtNodes(issued.NodeIDs) != `["node-b"]` {
		t.Fatal("legacy scope widened or token rotated", issued.ID, err)
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestCredentialExpiredIDNeverRevivesAndKeepsCeiling(t *testing.T) {
	r, m := testRepository(t)
	c := credentialCandidate()
	old := c
	old.ID = "expired-id"
	old.RenewalKey = domain.CredentialIdentity(c)
	old.NodeIDs = []string{"node-b"}
	old.EncryptedSecret = "expired-secret"
	old.ExpiresAt = time.Now().Add(-time.Minute)
	raw, _ := json.Marshal(old)
	key := domain.CredentialIdentity(c)
	m.ExpectBegin()
	expectPolicy(m)
	expectCredentialAuthorization(m, c, false)
	m.ExpectQuery("SELECT .*network_proxy_employee_credentials.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"id", "renewal_key", "record", "renewable"}).AddRow(old.ID, key, raw, false))
	m.ExpectQuery("SELECT .*network_proxy_credential_scopes.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"node_ids"}).AddRow([]byte(`["node-b"]`)))
	m.ExpectExec("INSERT INTO network_proxy_credential_scopes").WithArgs(key, `["node-b"]`).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("UPDATE .*network_proxy_employee_credentials").WithArgs(scopedCredentialJSON{old.ID, []string{"node-b"}, old.EncryptedSecret}, old.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("DELETE FROM .*network_proxy_employee_credentials").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("INSERT INTO .*network_proxy_employee_credentials").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	issued, err := r.IssueCredential(context.Background(), c)
	if err != nil || issued.ID != c.ID || issued.ID == old.ID || issued.EncryptedSecret != c.EncryptedSecret || fmtNodes(issued.NodeIDs) != `["node-b"]` {
		t.Fatal("expired credential revived or widened", issued.ID, err)
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestCredentialSourceMutationIsRecheckedBeforeRenewal(t *testing.T) {
	r, m := testRepository(t)
	c := credentialCandidate()
	key := domain.CredentialIdentity(c)
	m.ExpectBegin()
	expectPolicy(m)
	expectCredentialAuthorization(m, c, true)
	m.ExpectQuery("SELECT .*network_proxy_employee_credentials.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"id", "record", "renewable"}))
	m.ExpectQuery("SELECT .*network_proxy_credential_scopes.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"node_ids"}))
	m.ExpectExec("INSERT INTO network_proxy_credential_scopes").WithArgs(key, `["node-b"]`).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("DELETE FROM .*network_proxy_employee_credentials").WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectExec("INSERT INTO .*network_proxy_employee_credentials").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	issued, err := r.IssueCredential(context.Background(), c)
	if err != nil || fmtNodes(issued.NodeIDs) != `["node-b"]` {
		t.Fatal("stale application A scope survived transaction check", err)
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
