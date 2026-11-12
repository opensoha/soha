package proxydeployment

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	app "github.com/opensoha/soha/internal/application/proxydeployment"
	"github.com/opensoha/soha/internal/domain/delivery"
	proxy "github.com/opensoha/soha/internal/domain/networkproxy"
	domain "github.com/opensoha/soha/internal/domain/proxydeployment"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
	"time"
)

func TestProxyDeploymentPublicationFenceRejectsOldEpochAndMemberFallback(t *testing.T) {
	for _, name := range []string{"approved", "wrong epoch", "wrong member", "duplicate member", "unbound master", "bound child fallback"} {
		t.Run(name, func(t *testing.T) {
			repo, mock := sqlRepository(t)
			mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WillReturnResult(sqlmock.NewResult(0, 1))
			query := mock.ExpectQuery(`SELECT owner_epoch,member_set_hash,members`).WithArgs("master")
			batch := proxy.DesiredBatch{MasterID: "master", OwnerEpoch: 2, MemberSetHash: "hash", Members: []proxy.DesiredMember{{InstanceID: "a"}, {InstanceID: "b"}}}
			if name == "unbound master" || name == "bound child fallback" {
				query.WillReturnRows(sqlmock.NewRows([]string{"owner_epoch", "member_set_hash", "members"}))
				mock.ExpectQuery(`SELECT EXISTS.*network_proxy_deployment_members`).WithArgs("master").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(name == "bound child fallback"))
				batch.OwnerEpoch = 0
				batch.MemberSetHash = ""
				batch.Members = []proxy.DesiredMember{{InstanceID: "master"}}
			} else {
				raw, _ := json.Marshal([]domain.Member{{RuntimeID: "a"}, {RuntimeID: "b"}})
				query.WillReturnRows(sqlmock.NewRows([]string{"owner_epoch", "member_set_hash", "members"}).AddRow(2, "hash", raw))
				switch name {
				case "wrong epoch":
					batch.OwnerEpoch = 1
				case "wrong member":
					batch.Members[1].InstanceID = "c"
				case "duplicate member":
					batch.Members[1].InstanceID = "a"
				}
			}
			err := repo.LockDesiredBatch(context.Background(), repo.db, batch)
			allowed := name == "approved" || name == "unbound master"
			if allowed && err != nil || !allowed && !errors.Is(err, apperrors.ErrConflict) {
				t.Fatal(err)
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestProxyDeploymentSingleWriteFenceRejectsBoundMember(t *testing.T) {
	for _, bound := range []bool{false, true} {
		repo, mock := sqlRepository(t)
		mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(`SELECT EXISTS.*network_proxy_deployments`).WithArgs("member", "member").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(bound))
		err := repo.LockSingleDesired(context.Background(), repo.db, "member")
		if bound && !errors.Is(err, apperrors.ErrConflict) || !bound && err != nil {
			t.Fatal(err)
		}
		if err = mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	}
}
func TestProxyDeploymentUnknownRejectsAllButReadonlyReconcile(t *testing.T) {
	state := domain.State{Phase: "unknown"}
	if !errors.Is(requireReconciledOutcome(state, mutationForAction("stop")), apperrors.ErrConflict) {
		t.Fatal("unknown accepted mutation")
	}
	if err := requireReconciledOutcome(state, mutationForAction("reconcile")); err != nil {
		t.Fatal(err)
	}
}
func mutationForAction(action string) app.Mutation {
	return app.Mutation{Task: &delivery.ExecutionTask{TaskKind: domain.TaskKind, ProviderKind: "proxy_deployment.exec", Payload: map[string]any{"proxyDeployment": domain.Command{ProxyID: "master", Generation: 2, Slot: domain.Slot{ExecutorID: "exec", UID: "uid"}, Action: action}}}}
}

func TestProxyDeploymentDrainPinsExistingConfigurationLeaseWithoutRenewing(t *testing.T) {
	repo, mock := sqlRepository(t)
	now := time.Now().UTC()
	oldLease := now.Add(20 * time.Second)
	active := int32(1)
	config := proxy.Configuration{Enabled: true, ContentHash: "content"}
	mock.ExpectQuery(`SELECT .*network_proxy_instances.*id IN`).WithArgs("p1").WillReturnRows(sqlmock.NewRows([]string{"id", "enabled", "desired_revision", "desired_hash", "configuration_valid_until", "configuration_lease_revision", "configuration_lease_hash"}).AddRow("p1", true, 1, "content", oldLease, 1, config.Digest()))
	state := domain.State{ActualReplicas: &active, Members: []domain.Member{{RuntimeID: "p1", Ordinal: 0}}}
	command := domain.Command{DrainUntil: now.Add(120 * time.Second), Deadline: now.Add(240 * time.Second)}
	if err := pinDrainLease(context.Background(), repo.db, &state, command); err != nil {
		t.Fatal(err)
	}
	if state.DrainUntil == nil || !state.DrainUntil.Equal(oldLease) {
		t.Fatalf("not bounded by initial configuration lease: %+v", state.DrainUntil)
	}
	// No second SQL read is expected: later configuration polls cannot extend it.
	if err := pinDrainLease(context.Background(), repo.db, &state, domain.Command{DrainUntil: now.Add(time.Hour), Deadline: now.Add(2 * time.Hour)}); err != nil || !state.DrainUntil.Equal(oldLease) {
		t.Fatal("fixed drain bound renewed")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
