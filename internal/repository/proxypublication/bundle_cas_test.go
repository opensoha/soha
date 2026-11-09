package proxypublication

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	app "github.com/opensoha/soha/internal/application/proxypublication"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestOnlyBundleUpdateCASMissCarriesRetryMarker(t *testing.T) {
	for _, name := range []string{"updated", "update CAS miss", "org changed", "source revoked", "insert collision"} {
		t.Run(name, func(t *testing.T) {
			r, m := testRepository(t)
			record := domain.BundleRecord{InstanceID: "instance", Version: 3, SourceNodeIDs: []string{"node-a"}, Nodes: []string{"node-a"}, Scope: api.NetworkProxyScope{UserIDs: []string{"user"}, DeviceIDs: []string{"device"}, NodeIDs: []string{"node-a"}}, Public: api.NetworkProxyBundle{BundleID: "bundle", Revision: 1, SourceID: "source", SourceRevision: 1, OrgRevision: 1, DeliveryMode: "mapped_nodes"}}
			expected := int64(1)
			m.ExpectBegin()
			expectPolicy(m)
			if name == "insert collision" {
				expected = 0
				m.ExpectQuery(`SELECT count.*FROM users`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
				m.ExpectQuery(`SELECT count.*network_access_devices`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
				m.ExpectQuery(`SELECT .*network_proxy_sources.*FOR UPDATE`).WillReturnRows(sourceRows(1, 1, map[string]string{"node-a": "material"}, nil))
			} else {
				floor := int64(1)
				if name == "source revoked" {
					floor = 2
				}
				m.ExpectQuery(`SELECT .*network_proxy_sources`).WillReturnRows(sourceRows(1, floor, map[string]string{"node-a": "material"}, nil))
				if name != "source revoked" {
					m.ExpectQuery(`SELECT .*network_proxy_node_invalidations`).WillReturnRows(sqlmock.NewRows([]string{"node_id"}))
				}
			}
			if name != "source revoked" {
				org := int64(1)
				if name == "org changed" {
					org = 2
				}
				m.ExpectQuery(`SELECT .*network_proxy_rule_sets`).WillReturnRows(sqlmock.NewRows([]string{"revision", "rules"}).AddRow(org, []byte(`[]`)))
			}
			switch name {
			case "updated", "update CAS miss":
				rows := int64(1)
				if name == "update CAS miss" {
					rows = 0
				}
				m.ExpectExec(`UPDATE .*network_proxy_bundles.*revision = .*version = `).WithArgs(sqlmock.AnyArg(), int64(1), int64(4), "instance", "bundle", int64(1), int64(3)).WillReturnResult(sqlmock.NewResult(0, rows))
			case "insert collision":
				m.ExpectExec(`INSERT INTO network_proxy_bundles.*ON CONFLICT DO NOTHING`).WithArgs("instance", "bundle", int64(1), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 0))
			}
			if name == "updated" {
				m.ExpectCommit()
			} else {
				m.ExpectRollback()
			}
			err := r.SaveBundle(context.Background(), record, expected)
			if name == "updated" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				want := apperrors.ErrConflict
				if name == "source revoked" {
					want = apperrors.ErrAccessDenied
				}
				if !errors.Is(err, want) {
					t.Fatalf("error=%v want %v", err, want)
				}
				if errors.Is(err, app.ErrBundleCASConflict) != (name == "update CAS miss") {
					t.Fatalf("wrong retry classification: %v", err)
				}
				if name == "update CAS miss" && err.Error() != apperrors.ErrConflict.Error() {
					t.Fatal("CAS marker changed public error text")
				}
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
