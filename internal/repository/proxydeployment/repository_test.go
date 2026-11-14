package proxydeployment

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/opensoha/soha/internal/networkidentity"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	app "github.com/opensoha/soha/internal/application/proxydeployment"
	"github.com/opensoha/soha/internal/domain/delivery"
	domain "github.com/opensoha/soha/internal/domain/proxydeployment"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func sqlRepository(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return New(db), mock
}

func expectLock(mock sqlmock.Sqlmock) {
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT proxy_id FROM network_proxy_deployments.*FOR UPDATE`).WithArgs("edge").WillReturnRows(sqlmock.NewRows([]string{"proxy_id"}).AddRow("edge"))
	mock.ExpectQuery(`SELECT id FROM network_proxy_instances`).WithArgs("edge").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("edge"))
}
func repositoryPin() domain.Slot {
	return domain.Slot{ID: "slot", ProxyID: "edge", ExecutorID: "executor", UID: "uid", TemplateDigest: "digest", Namespace: "soha", Name: "edge", MaxReplicas: 1, Members: []domain.Member{{InstanceID: "edge-0", RuntimeID: "edge"}}}
}
func expectCurrent(mock sqlmock.Sqlmock, generation int64) {
	slot := repositoryPin()
	mock.ExpectQuery(`SELECT id FROM network_proxy_instances WHERE id =`).WithArgs("edge").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("edge"))
	encoded, _ := json.Marshal(domain.State{ProxyID: "edge", Owner: "soha-managed", SlotID: "slot", UID: "uid", Slot: &slot, Generation: generation})
	mock.ExpectQuery(`SELECT state FROM network_proxy_deployments`).WithArgs("edge").WillReturnRows(sqlmock.NewRows([]string{"state"}).AddRow(encoded))
}

func TestCommitCASAndPersistentReplay(t *testing.T) {
	for _, test := range []struct {
		name         string
		replay       bool
		fingerprint  string
		current      int64
		wantErr      bool
		writeFailure bool
	}{
		{"target and task commit", false, "same", 1, false, false},
		{"stale generation", false, "same", 2, true, false},
		{"same request replays original", true, "same", 0, false, false},
		{"request reuse conflicts", true, "different", 0, true, false},
		{"target storage fails after task", false, "same", 1, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, mock := sqlRepository(t)
			slot := repositoryPin()
			state := domain.State{ProxyID: "edge", Slot: &slot, Owner: "soha-managed", SlotID: "slot", UID: "uid", Generation: 2, LatestTaskID: "task"}
			mutation := app.Mutation{RequestID: "request", Fingerprint: "same", ExpectedGeneration: 1, State: state, Task: &delivery.ExecutionTask{ID: "task", TaskKind: domain.TaskKind, ProviderKind: "proxy_deployment.executor", Payload: map[string]any{"proxyDeployment": domain.Command{ProxyID: "edge", Generation: 2, Slot: slot, Action: "stop"}}}}
			expectLock(mock)
			query := mock.ExpectQuery(`SELECT fingerprint,response FROM network_proxy_deployment_requests`).WithArgs("edge", "request")
			if test.replay {
				encoded, _ := json.Marshal(state)
				query.WillReturnRows(sqlmock.NewRows([]string{"fingerprint", "response"}).AddRow(test.fingerprint, encoded))
			} else {
				query.WillReturnRows(sqlmock.NewRows([]string{"fingerprint", "response"}))
				expectCurrent(mock, test.current)
				if !test.wantErr || test.writeFailure {
					mock.ExpectExec(`INSERT INTO execution_tasks`).WillReturnResult(sqlmock.NewResult(0, 1))
					if test.writeFailure {
						mock.ExpectExec(`INSERT INTO network_proxy_deployments`).WillReturnError(apperrors.ErrConflict)
					} else {
						mock.ExpectExec(`INSERT INTO network_proxy_deployments`).WillReturnResult(sqlmock.NewResult(0, 1))
					}
					if !test.writeFailure {
						mock.ExpectExec(`INSERT INTO network_proxy_deployment_requests`).WillReturnResult(sqlmock.NewResult(0, 1))
					}
				}
			}
			if test.wantErr {
				mock.ExpectRollback()
			} else {
				mock.ExpectCommit()
			}
			got, err := repo.Commit(context.Background(), mutation)
			if test.wantErr {
				if !errors.Is(err, apperrors.ErrConflict) {
					t.Fatalf("got %v", err)
				}
			} else if err != nil || got.LatestTaskID != "task" {
				t.Fatalf("got %+v %v", got, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProxyDeploymentACKIsBoundToProcessCredentialAndIssuedControl(t *testing.T) {
	now := time.Now().UTC()
	grant := domain.InstanceControl{InstanceID: "pool-0", RuntimeID: "runtime0", ProcessEpoch: "boot1", CredentialID: "cred", CredentialEpoch: 2, OwnerEpoch: 3, ControlRevision: 4, ExpiresAt: now.Add(time.Second)}
	ack := domain.InstanceACK{InstanceID: grant.InstanceID, RuntimeID: grant.RuntimeID, ProcessEpoch: grant.ProcessEpoch, CredentialID: grant.CredentialID, CredentialEpoch: grant.CredentialEpoch, OwnerEpoch: grant.OwnerEpoch, ControlRevision: grant.ControlRevision, ExpiresAt: grant.ExpiresAt, ObservedAt: now, ActiveCountKnown: false}
	if !validACK(ack, grant, now) {
		t.Fatal("valid unknown count ACK rejected")
	}
	for _, mutate := range []func(*domain.InstanceACK){func(a *domain.InstanceACK) { a.ProcessEpoch = "old" }, func(a *domain.InstanceACK) { a.CredentialEpoch-- }, func(a *domain.InstanceACK) { a.ControlRevision-- }, func(a *domain.InstanceACK) { a.OwnerEpoch-- }, func(a *domain.InstanceACK) { a.ExpiresAt = now.Add(time.Hour) }, func(a *domain.InstanceACK) { a.AcceptingRequests = true }, func(a *domain.InstanceACK) { a.ActiveConnections = 1 }, func(a *domain.InstanceACK) { a.ObservedAt = now.Add(-time.Minute) }} {
		changed := ack
		mutate(&changed)
		if validACK(changed, grant, now) {
			t.Fatalf("accepted stale/forged ACK: %+v", changed)
		}
	}
}
func TestProxyDeploymentResumeAdvancesControlOnceAndUnknownStaysClosed(t *testing.T) {
	state := domain.State{Owner: "soha-managed", Generation: 2, ControlRevision: 5, DrainHeld: true, DesiredReplicas: 1, UID: "uid"}
	task := delivery.ExecutionTask{Status: "running"}
	receipt := &domain.Receipt{Resume: true, Drained: true, Observation: &domain.Observation{UID: "uid", TemplateDigest: "digest", Replicas: 1, DesiredReplicas: 1}}
	applyResult(&state, task, domain.Command{Slot: domain.Slot{TemplateDigest: "digest"}}, receipt)
	if state.DrainHeld || state.ControlRevision != 6 {
		t.Fatalf("resume failed: %+v", state)
	}
	applyResult(&state, task, domain.Command{Slot: domain.Slot{TemplateDigest: "digest"}}, receipt)
	if state.ControlRevision != 6 {
		t.Fatal("duplicate resume changed revision")
	}
	receipt.Uncertain = true
	applyResult(&state, delivery.ExecutionTask{Status: "failed"}, domain.Command{}, receipt)
	if state.Phase != "unknown" || !state.RequiresDrain() {
		t.Fatalf("unknown serving: %+v", state)
	}
}

func TestProxyDeploymentProcessEpochCannotOverlapOrReturnAfterRetirement(t *testing.T) {
	now := time.Now().UTC()
	old := memberRecord{Grant: domain.InstanceControl{ProcessEpoch: "boot1", ExpiresAt: now.Add(time.Second)}}
	if !errors.Is(advanceProcess(&old, "boot2", now), apperrors.ErrConflict) {
		t.Fatal("overlapping boot accepted")
	}
	if err := advanceProcess(&old, "boot2", now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	old.Grant.ProcessEpoch = "boot2"
	if !errors.Is(advanceProcess(&old, "boot1", now.Add(time.Hour)), apperrors.ErrAccessDenied) {
		t.Fatal("retired boot returned")
	}
}

func TestProxyDeploymentDisabledStartCanBeReadyWithoutTraffic(t *testing.T) {
	if !deploymentReady(true, false, false, false) {
		t.Fatal("disabled configuration cannot converge")
	}
	if deploymentReady(true, true, false, false) {
		t.Fatal("enabled member falsely ready without traffic proof")
	}
	if deploymentReady(true, true, true, true) {
		t.Fatal("draining member falsely ready")
	}
	if deploymentReady(false, false, false, false) {
		t.Fatal("unapplied disabled configuration falsely ready")
	}
}

func TestProxyDeploymentGrantAuthenticatesIndependentIdentityAndFencesLiveEpoch(t *testing.T) {
	for _, name := range []string{"new member", "active previous boot", "revoked certificate", "retired boot"} {
		t.Run(name, func(t *testing.T) {
			repo, mock := sqlRepository(t)
			now := time.Now().UTC()
			identity := networkidentity.Identity{ID: "p1", CertificateFingerprint: "sha256:cert"}
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT proxy_id FROM network_proxy_deployment_members`).WithArgs("p1", "pool-0").WillReturnRows(sqlmock.NewRows([]string{"proxy_id"}).AddRow("master"))
			mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery(`SELECT proxy_id FROM network_proxy_deployments.*FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"proxy_id"}).AddRow("master"))
			mock.ExpectQuery(`SELECT id FROM network_proxy_instances`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("master"))
			mock.ExpectQuery(`SELECT id FROM network_proxy_instances`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("master"))
			slot := domain.Slot{ID: "slot", ProxyID: "master", UID: "uid", ExecutorID: "executor", Name: "pool", Namespace: "soha", TemplateDigest: "digest", MaxReplicas: 1, Members: []domain.Member{{InstanceID: "pool-0", RuntimeID: "p1", Ordinal: 0}}}
			pin, _ := app.SlotPolicyHash(slot)
			raw, _ := json.Marshal(domain.State{ProxyID: "master", SlotID: slot.ID, UID: slot.UID, Slot: &slot, Owner: "soha-managed", OwnerEpoch: 2, ControlRevision: 3, DesiredReplicas: 1, Members: []domain.Member{{InstanceID: "pool-0", RuntimeID: "p1", Ordinal: 0}}})
			mock.ExpectQuery(`SELECT state FROM network_proxy_deployments`).WillReturnRows(sqlmock.NewRows([]string{"state"}).AddRow(raw))
			cred := mock.ExpectQuery(`SELECT id,generation,expires_at FROM network_runtime_credentials.*FOR SHARE`).WithArgs("p1", "sha256:cert", sqlmock.AnyArg(), sqlmock.AnyArg())
			if name == "revoked certificate" {
				cred.WillReturnRows(sqlmock.NewRows([]string{"id", "generation", "expires_at"}))
			} else {
				cred.WillReturnRows(sqlmock.NewRows([]string{"id", "generation", "expires_at"}).AddRow("cred", 2, now.Add(time.Hour)))
				record := memberRecord{}
				if name == "active previous boot" {
					record.Grant = domain.InstanceControl{ProcessEpoch: "old", ExpiresAt: now.Add(time.Hour)}
				}
				if name == "retired boot" {
					record.Retired = []string{"boot"}
				}
				encoded, _ := json.Marshal(record)
				mock.ExpectQuery(`SELECT record FROM network_proxy_deployment_instances.*FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"record"}).AddRow(encoded))
			}
			if name == "new member" {
				mock.ExpectExec(`INSERT INTO network_proxy_deployment_instances`).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			control, err := repo.GrantControl(context.Background(), identity, "pool-0", "boot", pin)
			if name == "new member" {
				if err != nil || control.RuntimeID != "p1" || control.InstanceID != "pool-0" || !control.AcceptingRequests || control.CredentialEpoch != 2 {
					t.Fatalf("wrong independent member grant %+v %v", control, err)
				}
			} else if err == nil {
				t.Fatal("untrusted boot granted")
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProxyDeploymentFirstObservationClosesWholePoolBeforeShrink(t *testing.T) {
	state := domain.State{Owner: "soha-managed", Generation: 2, ControlRevision: 3, DesiredReplicas: 1, UID: "uid"}
	command := domain.Command{Action: "start", Replicas: 1, Slot: domain.Slot{TemplateDigest: "digest"}}
	receipt := &domain.Receipt{Observation: &domain.Observation{UID: "uid", TemplateDigest: "digest", Replicas: 2, DesiredReplicas: 2}}
	applyResult(&state, delivery.ExecutionTask{Status: "running"}, command, receipt)
	if !state.DrainHeld || !state.RequiresDrain() || state.ControlRevision != 4 {
		t.Fatalf("first observed shrink left survivor accepting: %+v", state)
	}
	applyResult(&state, delivery.ExecutionTask{Status: "running"}, command, receipt)
	if state.ControlRevision != 4 {
		t.Fatal("repeat observation reissued control")
	}
}

func TestProxyDeploymentDrainGrantHasFixedDeadlineAndUnknownCannotRenew(t *testing.T) {
	now := time.Now().UTC()
	until := now.Add(120 * time.Second)
	state := domain.State{Owner: "soha-managed", Phase: "reconciling", DrainHeld: true, DrainLeasePinned: true, DrainUntil: &until}
	for _, issued := range []time.Time{now, now.Add(time.Second), now.Add(100 * time.Second)} {
		got, err := grantExpiry(state, now.Add(time.Hour), issued)
		if err != nil || !got.Equal(until) {
			t.Fatalf("drain deadline renewed: %v %v", got, err)
		}
	}
	if _, err := grantExpiry(state, now.Add(time.Hour), until); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("expired drain extended")
	}
	for _, phase := range []string{"unknown", "degraded", "conflict"} {
		state.Phase = phase
		if _, err := grantExpiry(state, now.Add(time.Hour), now); !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("%s reused ordinary drain lease", phase)
		}
	}
}
func TestProxyDeploymentResumeClearsOldDrainDeadline(t *testing.T) {
	until := time.Now().Add(-time.Minute)
	state := domain.State{Owner: "soha-managed", DrainHeld: true, DrainUntil: &until, DrainLeasePinned: true, DesiredReplicas: 1, UID: "uid"}
	applyResult(&state, delivery.ExecutionTask{Status: "running"}, domain.Command{Slot: domain.Slot{TemplateDigest: "digest"}}, &domain.Receipt{Resume: true, Drained: true, Observation: &domain.Observation{UID: "uid", TemplateDigest: "digest", Replicas: 1, DesiredReplicas: 1}})
	if state.DrainUntil != nil || state.DrainLeasePinned || state.DrainHeld {
		t.Fatal("resume retained stale deadline")
	}
}

func TestProxyDeploymentResumeRequiresValidatedScaleObservation(t *testing.T) {
	state := domain.State{Owner: "soha-managed", DrainHeld: true, DesiredReplicas: 1}
	applyResult(&state, delivery.ExecutionTask{Status: "running"}, domain.Command{}, &domain.Receipt{Resume: true})
	if !state.DrainHeld {
		t.Fatal("empty executor resume opened pool")
	}
}
