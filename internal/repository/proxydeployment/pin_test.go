package proxydeployment

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	app "github.com/opensoha/soha/internal/application/proxydeployment"
	"github.com/opensoha/soha/internal/domain/delivery"
	domain "github.com/opensoha/soha/internal/domain/proxydeployment"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
	"time"
)

func stoppedBindingPolicy() domain.Slot {
	return domain.Slot{ID: "slot", ProxyID: "edge", ExecutorID: "executor", UID: "uid", Name: "edge", Namespace: "soha", WorkloadKind: "StatefulSet", TemplateDigest: "old-template", MaxReplicas: 1, Members: []domain.Member{{InstanceID: "edge-0", RuntimeID: "edge", Ordinal: 0}}}
}
func stoppedBinding(slot domain.Slot) domain.State {
	zero := int32(0)
	now := time.Now().UTC()
	return domain.State{ProxyID: "edge", SlotID: slot.ID, UID: slot.UID, Slot: &slot, Owner: "soha-managed", OwnerEpoch: 2, Generation: 4, ObservedGeneration: 4, DesiredReplicas: 0, ActualReplicas: &zero, ObservedDesiredReplicas: &zero, ObservedAt: &now, LatestTaskID: "stop-task", Phase: "converged"}
}
func isRebindRequired(err error) bool {
	var business *apperrors.BusinessError
	return errors.Is(err, apperrors.ErrConflict) && errors.As(err, &business) && business.Code() == "rebind_required"
}
func TestPinRebindTransactionRequiresMatchingCompletedStopNotJustTerminalTask(t *testing.T) {
	for _, name := range []string{"completed stop", "running", "failed", "canceled", "callback timeout", "old generation", "old owner", "different accepted pin", "readonly reconcile", "nonzero target", "wrong provider"} {
		t.Run(name, func(t *testing.T) {
			repo, mock := sqlRepository(t)
			old := stoppedBindingPolicy()
			state := stoppedBinding(old)
			next := old
			next.TemplateDigest = "new-template"
			mutation := app.Mutation{State: state}
			mutation.State.Slot = &next
			command := domain.Command{ProxyID: "edge", Slot: old, Generation: 4, OwnerEpoch: 2, Action: "stop", Replicas: 0}
			status, provider := "completed", old.ProviderKind()
			switch name {
			case "running":
				status = "running"
			case "failed":
				status = "failed"
			case "canceled":
				status = "canceled"
			case "callback timeout":
				status = "callback_timeout"
			case "old generation":
				command.Generation--
			case "old owner":
				command.OwnerEpoch--
			case "different accepted pin":
				command.Slot.TemplateDigest = "foreign"
			case "readonly reconcile":
				command.Action = "reconcile"
			case "nonzero target":
				command.Replicas = 1
			case "wrong provider":
				provider = "proxy_deployment.other"
			}
			payload, _ := json.Marshal(map[string]any{"proxyDeployment": command})
			mock.ExpectQuery(`SELECT task_kind,provider_kind,target_kind,status,payload FROM execution_tasks.*FOR SHARE`).WithArgs("stop-task").WillReturnRows(sqlmock.NewRows([]string{"task_kind", "provider_kind", "target_kind", "status", "payload"}).AddRow(domain.TaskKind, provider, "proxy_slot", status, payload))
			err := validateMutationPin(repo.db, state, mutation)
			if name == "completed stop" && err != nil || name != "completed stop" && !isRebindRequired(err) {
				t.Fatalf("unexpected proof outcome: %v", err)
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestPinRebindRechecksChangedObservationInsideCommitLock(t *testing.T) {
	repo, mock := sqlRepository(t)
	old := stoppedBindingPolicy()
	state := stoppedBinding(old)
	next := old
	next.TemplateDigest = "new-template"
	candidate := state
	candidate.Slot = &next
	candidate.Generation++
	current := state
	n := int32(1)
	current.ActualReplicas = &n
	current.Phase = "degraded"
	expectLock(mock)
	mock.ExpectQuery(`SELECT fingerprint,response`).WillReturnRows(sqlmock.NewRows([]string{"fingerprint", "response"}))
	mock.ExpectQuery(`SELECT id FROM network_proxy_instances WHERE id =`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("edge"))
	raw, _ := json.Marshal(current)
	mock.ExpectQuery(`SELECT state FROM network_proxy_deployments`).WillReturnRows(sqlmock.NewRows([]string{"state"}).AddRow(raw))
	mock.ExpectRollback()
	_, err := repo.Commit(context.Background(), app.Mutation{RequestID: "rebind", Fingerprint: "hash", ExpectedGeneration: 4, State: candidate})
	if !isRebindRequired(err) {
		t.Fatalf("stale app stopped snapshot accepted: %v", err)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func expectGrantPool(mock sqlmock.Sqlmock, state domain.State, runtimeID, instance string) {
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT proxy_id FROM network_proxy_deployment_members`).WithArgs(runtimeID, instance).WillReturnRows(sqlmock.NewRows([]string{"proxy_id"}).AddRow("edge"))
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT proxy_id FROM network_proxy_deployments.*FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"proxy_id"}).AddRow("edge"))
	mock.ExpectQuery(`SELECT id FROM network_proxy_instances`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("edge"))
	mock.ExpectQuery(`SELECT id FROM network_proxy_instances`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("edge"))
	raw, _ := json.Marshal(state)
	mock.ExpectQuery(`SELECT state FROM network_proxy_deployments`).WillReturnRows(sqlmock.NewRows([]string{"state"}).AddRow(raw))
}
func TestRuntimeGrantRechecksExpectedPinAndMemberAfterApplicationSnapshot(t *testing.T) {
	for _, name := range []string{"changed pin", "missing pin", "old association"} {
		t.Run(name, func(t *testing.T) {
			repo, mock := sqlRepository(t)
			old := stoppedBindingPolicy()
			state := stoppedBinding(old)
			expected, _ := app.SlotPolicyHash(old)
			runtimeID := "edge"
			switch name {
			case "changed pin":
				next := old
				next.TemplateDigest = "new-template"
				state.Slot = &next
			case "missing pin":
				state.Slot = nil
			case "old association":
				runtimeID = "removed-runtime"
			}
			expectGrantPool(mock, state, runtimeID, "edge-0")
			mock.ExpectRollback()
			_, err := repo.GrantControl(context.Background(), networkidentity.Identity{ID: runtimeID}, "edge-0", "boot", expected)
			if name == "old association" {
				if !errors.Is(err, apperrors.ErrAccessDenied) {
					t.Fatal(err)
				}
			} else if !isRebindRequired(err) {
				t.Fatal("changed/missing accepted pin renewed after preflight")
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestGetForMemberUsesPersistedExactIdentityCoordinates(t *testing.T) {
	repo, mock := sqlRepository(t)
	slot := stoppedBindingPolicy()
	state := stoppedBinding(slot)
	mock.ExpectQuery(`SELECT proxy_id FROM network_proxy_deployment_members`).WithArgs("edge", "edge-0").WillReturnRows(sqlmock.NewRows([]string{"proxy_id"}).AddRow("edge"))
	mock.ExpectQuery(`SELECT id FROM network_proxy_instances`).WithArgs("edge").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("edge"))
	raw, _ := json.Marshal(state)
	mock.ExpectQuery(`SELECT state FROM network_proxy_deployments`).WithArgs("edge").WillReturnRows(sqlmock.NewRows([]string{"state"}).AddRow(raw))
	got, err := repo.GetForMember(context.Background(), "edge", "edge-0")
	if err != nil || got.ProxyID != "edge" {
		t.Fatal(err)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestTaskCannotReplaceAcceptedPinOrCreateUnderMissingPin(t *testing.T) {
	old := stoppedBindingPolicy()
	next := old
	next.TemplateDigest = "new-template"
	for _, missing := range []bool{false, true} {
		state := stoppedBinding(old)
		if missing {
			state.Slot = nil
		}
		repo, _ := sqlRepository(t)
		task := delivery.ExecutionTask{TaskKind: domain.TaskKind, ProviderKind: next.ProviderKind(), Payload: map[string]any{"proxyDeployment": domain.Command{ProxyID: "edge", Generation: 5, Slot: next, Action: "start"}}}
		if err := validateMutationPin(repo.db, state, app.Mutation{State: state, Task: &task}); !isRebindRequired(err) {
			t.Fatal("control replaced accepted policy")
		}
	}
}
