package proxypublication

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"github.com/DATA-DOG/go-sqlmock"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"testing"
)

type rejectedReceiptJSON struct{}

func (rejectedReceiptJSON) Match(value driver.Value) bool {
	raw, ok := value.(string)
	if !ok {
		return false
	}
	var receipt api.NetworkProxyProjectionApplied
	return json.Unmarshal([]byte(raw), &receipt) == nil && receipt.Status == "rejected" && receipt.ReasonCode == "projection_apply_rejected"
}
func TestRejectedProjectionReceiptStoredWithoutAppliedForgery(t *testing.T) {
	r, m := testRepository(t)
	m.ExpectQuery("SELECT count.*network_proxy_employee_credentials").WithArgs("endpoint-cert", "profile-1", "boot-1", "bundle-1", int64(1)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	m.ExpectExec("INSERT INTO network_proxy_projection_receipts").WithArgs("endpoint-cert", "profile-1", "boot-1", rejectedReceiptJSON{}).WillReturnResult(sqlmock.NewResult(0, 1))
	err := r.SaveProjectionApplied(context.Background(), domain.EndpointContext{CredentialID: "endpoint-cert", ProfileID: "profile-1"}, "boot-1", api.NetworkProxyProjectionApplied{BundleID: "bundle-1", Revision: 1, ProcessEpoch: "boot-1", Status: "rejected", ReasonCode: "projection_apply_rejected"})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
