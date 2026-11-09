package proxydeployment

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/opensoha/soha/internal/domain/delivery"
	domain "github.com/opensoha/soha/internal/domain/proxydeployment"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
	"time"
)

func TestStopConfirmationDeadlineIsOnceBoundedAndDoesNotRenewFlows(t *testing.T) {
	now := time.Now().UTC()
	drain := now.Add(-time.Second)
	for _, name := range []string{"30 seconds", "task earlier", "certificate earlier"} {
		t.Run(name, func(t *testing.T) {
			state := domain.State{Owner: "soha-managed", Phase: "reconciling", DrainHeld: true, DrainLeasePinned: true, DrainUntil: &drain, ControlRevision: 4}
			task, cert, want := now.Add(time.Minute), now.Add(time.Hour), now.Add(30*time.Second)
			if name == "task earlier" {
				task = now.Add(10 * time.Second)
				want = task
			}
			if name == "certificate earlier" {
				cert = now.Add(5 * time.Second)
				want = cert
			}
			if !beginStopConfirmation(&state, task, cert, now) || state.ControlRevision != 5 || !state.StopConfirmUntil.Equal(want) || !state.DrainUntil.Equal(drain) {
				t.Fatalf("incorrect confirmation bound: %+v", state)
			}
			if beginStopConfirmation(&state, now.Add(time.Hour), now.Add(time.Hour), now.Add(time.Second)) || state.ControlRevision != 5 || !state.StopConfirmUntil.Equal(want) {
				t.Fatal("confirmation repeatedly extended")
			}
			got, err := grantExpiry(state, cert, now)
			if err != nil || !got.Equal(want) {
				t.Fatal("confirmation did not use new fixed deadline")
			}
			if _, err = grantExpiry(state, cert, want); !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatal("expired confirmation renewed")
			}
		})
	}
}
func TestStopConfirmationACKRequiresNewRevisionExactProcessAndPositiveStopped(t *testing.T) {
	now := time.Now().UTC()
	grant := domain.InstanceControl{InstanceID: "pool-0", RuntimeID: "p0", ProcessEpoch: "boot", OwnerEpoch: 2, CredentialID: "cred", CredentialEpoch: 3, ControlRevision: 8, StopRequired: true, ExpiresAt: now.Add(time.Second)}
	ack := domain.InstanceACK{InstanceID: grant.InstanceID, RuntimeID: grant.RuntimeID, ProcessEpoch: grant.ProcessEpoch, OwnerEpoch: grant.OwnerEpoch, CredentialID: grant.CredentialID, CredentialEpoch: grant.CredentialEpoch, ControlRevision: grant.ControlRevision, ProcessStopped: true, ActiveCountKnown: true, ExpiresAt: grant.ExpiresAt, ObservedAt: now}
	if !validACK(ack, grant, now) {
		t.Fatal("new stopped proof rejected")
	}
	for _, mutate := range []func(*domain.InstanceACK){func(a *domain.InstanceACK) { a.ControlRevision-- }, func(a *domain.InstanceACK) { a.ProcessEpoch = "old" }, func(a *domain.InstanceACK) { a.CredentialEpoch-- }, func(a *domain.InstanceACK) { a.ProcessStopped = false }, func(a *domain.InstanceACK) { a.ActiveCountKnown = false }, func(a *domain.InstanceACK) { a.ActiveConnections = 1 }, func(a *domain.InstanceACK) { a.AcceptingRequests = true }} {
		changed := ack
		mutate(&changed)
		if validACK(changed, grant, now) {
			t.Fatalf("unconfirmed process accepted: %+v", changed)
		}
	}
	grant.StopRequired = false
	if validACK(ack, grant, now) {
		t.Fatal("unissued stop proof accepted")
	}
}
func TestStopConfirmationPersistsOnceAndRejectsUnknownTaskFences(t *testing.T) {
	now := time.Now().UTC()
	drain := now.Add(-time.Second)
	slot := domain.Slot{ID: "slot", ProxyID: "master", ExecutorID: "exec", UID: "uid", Members: []domain.Member{{RuntimeID: "p0", InstanceID: "pool-0"}}}
	command := domain.Command{ProxyID: "master", Slot: slot, Generation: 2, OwnerEpoch: 3, Action: "stop", Deadline: now.Add(time.Minute)}
	payload, _ := json.Marshal(map[string]any{"proxyDeployment": command})
	state := domain.State{ProxyID: "master", Slot: &slot, SlotID: slot.ID, UID: slot.UID, Owner: "soha-managed", Generation: 2, OwnerEpoch: 3, ControlRevision: 4, MemberSetHash: slot.MemberSetHash(), LatestTaskID: "task", Phase: "reconciling", DrainHeld: true, DrainLeasePinned: true, DrainUntil: &drain}
	repo, mock := sqlRepository(t)
	for i := 0; i < 2; i++ {
		mock.ExpectQuery(`SELECT task_kind,provider_kind,target_kind,status,payload FROM execution_tasks`).WithArgs("task").WillReturnRows(sqlmock.NewRows([]string{"task_kind", "provider_kind", "target_kind", "status", "payload"}).AddRow(domain.TaskKind, slot.ProviderKind(), "proxy_slot", "running", payload))
		if i == 0 {
			mock.ExpectExec(`UPDATE network_proxy_deployments SET state`).WillReturnResult(sqlmock.NewResult(0, 1))
		}
		if err := ensureStopConfirmation(context.Background(), repo.db, &state, now.Add(time.Hour), now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if state.ControlRevision != 5 || !state.StopConfirmUntil.Equal(now.Add(30*time.Second)) || !state.DrainUntil.Equal(drain) {
		t.Fatal("confirmation mutation not stable")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	state.Phase = "unknown"
	if err := ensureStopConfirmation(context.Background(), repo.db, &state, now.Add(time.Hour), now); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("unknown state reused confirmation")
	}
}
func TestStopConfirmationCurrentTaskMustMatchOwnerGenerationMembersAndDeadline(t *testing.T) {
	now := time.Now()
	state := domain.State{ProxyID: "master", OwnerEpoch: 2, Generation: 3, SlotID: "slot", UID: "uid", LatestTaskID: "task"}
	slot := domain.Slot{ID: "slot", UID: "uid", ExecutorID: "exec"}
	state.Slot = &slot
	state.MemberSetHash = slot.MemberSetHash()
	for _, name := range []string{"expired", "old owner", "old generation", "different members", "terminal task"} {
		t.Run(name, func(t *testing.T) {
			repo, mock := sqlRepository(t)
			command := domain.Command{ProxyID: "master", OwnerEpoch: 2, Generation: 3, Slot: slot, Action: "stop", Deadline: now.Add(time.Minute)}
			status := "running"
			switch name {
			case "expired":
				command.Deadline = now
			case "old owner":
				command.OwnerEpoch--
			case "old generation":
				command.Generation--
			case "different members":
				command.Slot.Members = []domain.Member{{RuntimeID: "other"}}
			case "terminal task":
				status = "completed"
			}
			payload, _ := json.Marshal(map[string]any{"proxyDeployment": command})
			mock.ExpectQuery(`SELECT task_kind,provider_kind,target_kind,status,payload FROM execution_tasks`).WillReturnRows(sqlmock.NewRows([]string{"task_kind", "provider_kind", "target_kind", "status", "payload"}).AddRow(domain.TaskKind, slot.ProviderKind(), "proxy_slot", status, payload))
			if _, err := stopTaskDeadline(repo.db, state, now); !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("task fence accepted: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStopConfirmationACKIsStoredOnlyForCurrentActiveTaskWithoutReadingExpiredConfiguration(t *testing.T) {
	for _, name := range []string{"confirmed", "old revision", "unknown count", "expired task", "terminal task"} {
		t.Run(name, func(t *testing.T) {
			now := time.Now().UTC()
			drain := now.Add(-time.Second)
			until := now.Add(20 * time.Second)
			slot := domain.Slot{ID: "slot", ProxyID: "master", ExecutorID: "exec", UID: "uid", Members: []domain.Member{{InstanceID: "pool-0", RuntimeID: "p0"}}}
			state := domain.State{ProxyID: "master", Slot: &slot, SlotID: "slot", UID: "uid", Owner: "soha-managed", Generation: 3, OwnerEpoch: 2, ControlRevision: 8, MemberSetHash: slot.MemberSetHash(), LatestTaskID: "task", Phase: "reconciling", DrainHeld: true, DrainLeasePinned: true, DrainUntil: &drain, StopConfirmUntil: &until}
			grant := domain.InstanceControl{InstanceID: "pool-0", RuntimeID: "p0", ProcessEpoch: "boot", CredentialID: "cred", CredentialEpoch: 3, OwnerEpoch: 2, ControlRevision: 8, StopRequired: true, ExpiresAt: until}
			ack := domain.InstanceACK{InstanceID: grant.InstanceID, RuntimeID: grant.RuntimeID, ProcessEpoch: grant.ProcessEpoch, CredentialID: grant.CredentialID, CredentialEpoch: grant.CredentialEpoch, OwnerEpoch: grant.OwnerEpoch, ControlRevision: grant.ControlRevision, ExpiresAt: grant.ExpiresAt, ObservedAt: now, ActiveCountKnown: true, ProcessStopped: true}
			if name == "old revision" {
				ack.ControlRevision--
			}
			if name == "unknown count" {
				ack.ActiveCountKnown = false
			}
			repo, mock := sqlRepository(t)
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT proxy_id FROM network_proxy_deployment_members`).WillReturnRows(sqlmock.NewRows([]string{"proxy_id"}).AddRow("master"))
			mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery(`SELECT proxy_id FROM network_proxy_deployments.*FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"proxy_id"}).AddRow("master"))
			mock.ExpectQuery(`SELECT id FROM network_proxy_instances`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("master"))
			mock.ExpectQuery(`SELECT id FROM network_proxy_instances`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("master"))
			encoded, _ := json.Marshal(state)
			mock.ExpectQuery(`SELECT state FROM network_proxy_deployments`).WillReturnRows(sqlmock.NewRows([]string{"state"}).AddRow(encoded))
			mock.ExpectQuery(`SELECT id,generation,expires_at FROM network_runtime_credentials.*FOR SHARE`).WillReturnRows(sqlmock.NewRows([]string{"id", "generation", "expires_at"}).AddRow("cred", 3, now.Add(time.Hour)))
			record, _ := json.Marshal(memberRecord{Grant: grant})
			mock.ExpectQuery(`SELECT record FROM network_proxy_deployment_instances.*FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"record"}).AddRow(record))
			if name != "old revision" && name != "unknown count" {
				command := domain.Command{ProxyID: "master", Slot: slot, OwnerEpoch: 2, Generation: 3, Action: "stop", Deadline: now.Add(time.Minute)}
				status := "running"
				if name == "expired task" {
					command.Deadline = now.Add(-time.Second)
				}
				if name == "terminal task" {
					status = "failed"
				}
				payload, _ := json.Marshal(map[string]any{"proxyDeployment": command})
				mock.ExpectQuery(`SELECT task_kind,provider_kind,target_kind,status,payload FROM execution_tasks.*FOR SHARE`).WillReturnRows(sqlmock.NewRows([]string{"task_kind", "provider_kind", "target_kind", "status", "payload"}).AddRow(domain.TaskKind, slot.ProviderKind(), "proxy_slot", status, payload))
			}
			if name == "confirmed" {
				mock.ExpectExec(`INSERT INTO network_proxy_deployment_instances`).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			err := repo.RecordACK(context.Background(), networkidentity.Identity{ID: "p0", CertificateFingerprint: "sha256:cert"}, ack)
			if name == "confirmed" && err != nil || name != "confirmed" && err == nil {
				t.Fatalf("unexpected stop proof outcome: %v", err)
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStopConfirmationResumeClearsConfirmationAndNeedsValidatedReceipt(t *testing.T) {
	now := time.Now()
	drain := now.Add(-time.Second)
	confirm := now.Add(time.Second)
	state := domain.State{Owner: "soha-managed", UID: "uid", DesiredReplicas: 1, ControlRevision: 8, DrainHeld: true, DrainLeasePinned: true, DrainUntil: &drain, StopConfirmUntil: &confirm}
	receipt := &domain.Receipt{Drained: true, Resume: true, Observation: &domain.Observation{UID: "uid", TemplateDigest: "digest", Replicas: 1, DesiredReplicas: 1}}
	applyResult(&state, delivery.ExecutionTask{Status: "running"}, domain.Command{Slot: domain.Slot{TemplateDigest: "digest"}}, receipt)
	if state.DrainHeld || state.DrainUntil != nil || state.StopConfirmUntil != nil || state.DrainLeasePinned || state.ControlRevision != 9 {
		t.Fatalf("trusted resume retained stop confirmation: %+v", state)
	}
}
