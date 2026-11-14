package proxydeployment

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/domain/delivery"
	proxy "github.com/opensoha/soha/internal/domain/networkproxy"
	domain "github.com/opensoha/soha/internal/domain/proxydeployment"
	worker "github.com/opensoha/soha/internal/infrastructure/proxydeployment"
	"github.com/opensoha/soha/internal/networkidentity"
)

// Each callback persists the real repository transition, then waits for the
// test to inspect concurrent survivor behavior before the worker can continue.
type pausedShrinkExecution struct {
	mu        sync.Mutex
	state     domain.State
	task      delivery.ExecutionTask
	command   domain.Command
	ctx       context.Context
	persisted chan domain.State
	resume    chan struct{}
}

func (f *pausedShrinkExecution) Claim(context.Context) (delivery.ExecutionTask, error) {
	return f.task, nil
}
func (f *pausedShrinkExecution) Status(context.Context, string) (delivery.ExecutionTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now().UTC()
	until := now.Add(30 * time.Second)
	views := []api.NetworkProxyDeploymentMember{}
	for _, m := range f.state.Members {
		view := api.NetworkProxyDeploymentMember{InstanceID: m.InstanceID, RuntimeID: m.RuntimeID, CredentialID: "cert", AckRevision: f.state.ControlRevision, ObservedAt: now, ControlExpiresAt: &until, ActiveCountKnown: true}
		if m.Ordinal < f.state.DesiredReplicas {
			view.ActiveConnections, view.AcceptingRequests, view.Ready = 9, true, true
		}
		views = append(views, view)
	}
	task := f.task
	task.Result = map[string]any{"proxyDeploymentDrain": aggregateDrainStatus(f.state, views, now)}
	return task, nil
}
func (f *pausedShrinkExecution) Callback(_ context.Context, input delivery.ExecutionCallbackInput) (delivery.ExecutionTask, error) {
	f.mu.Lock()
	var receipt domain.Receipt
	raw, _ := json.Marshal(input.Payload["proxyDeployment"])
	_ = json.Unmarshal(raw, &receipt)
	f.task.Status = input.Status
	applyResult(&f.state, f.task, f.command, &receipt)
	state, task := f.state, f.task
	f.mu.Unlock()
	select {
	case f.persisted <- state:
	case <-f.ctx.Done():
		return task, f.ctx.Err()
	}
	select {
	case <-f.resume:
		return task, nil
	case <-f.ctx.Done():
		return task, f.ctx.Err()
	}
}

type convergingShrinkRuntime struct {
	writes, observes int
	slot             domain.Slot
}

func (f *convergingShrinkRuntime) Observe(context.Context, domain.Slot) (domain.Observation, error) {
	f.observes++
	replicas := int32(2)
	if f.writes > 0 {
		replicas = 1
	}
	return domain.Observation{UID: f.slot.UID, TemplateDigest: f.slot.TemplateDigest, ResourceVersion: "fixture-rv", Replicas: replicas, DesiredReplicas: replicas, ReadyReplicas: replicas, ObservedAt: time.Now().UTC()}, nil
}
func (f *convergingShrinkRuntime) Scale(context.Context, domain.Slot, domain.Observation, int32) error {
	f.writes++
	return nil
}

func TestShrinkEveryPersistedCallbackKeepsSurvivorServing(t *testing.T) {
	now := time.Now().UTC()
	state := memberShrinkState(now, 2, 1)
	command := domain.Command{ProxyID: state.ProxyID, Slot: *state.Slot, Generation: state.Generation, OwnerEpoch: state.OwnerEpoch, Action: "scale", Replicas: 1, DrainUntil: *state.DrainUntil, Deadline: now.Add(time.Minute)}
	task := delivery.ExecutionTask{ID: state.LatestTaskID, TaskKind: domain.TaskKind, ProviderKind: state.Slot.ProviderKind(), ClaimedByAgentID: "exec", Status: "dispatching", CallbackToken: "callback", Payload: map[string]any{"proxyDeployment": command}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := &pausedShrinkExecution{state: state, task: task, command: command, ctx: ctx, persisted: make(chan domain.State), resume: make(chan struct{})}
	runtime := &convergingShrinkRuntime{slot: *state.Slot}
	w, err := worker.NewWorker(client, runtime, "exec", []domain.Slot{*state.Slot})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); w.Execute(ctx, task) }()
	convergedCallbacks := 0
	for {
		select {
		case snapshot := <-client.persisted:
			repo, mock := sqlRepository(t)
			issued := time.Now().UTC()
			if snapshot.RequiresDrain() && !memberRequiresDrain(snapshot, "p0", "pool-0") {
				expectShrinkTask(mock, snapshot, command.Deadline)
			}
			grant, err := grantMemberControl(context.Background(), repo.db, &snapshot, credential{ID: "cert", Generation: 2, ExpiresAt: now.Add(time.Hour)}, "p0", "pool-0", "original-survivor-boot", issued)
			if err != nil || !grant.AcceptingRequests || grant.StopRequired {
				t.Fatalf("persisted callback revoked survivor: phase=%s held=%v actual=%v grant=%+v err=%v", snapshot.Phase, snapshot.DrainHeld, snapshot.ActualReplicas, grant, err)
			}
			ack := domain.InstanceACK{InstanceID: grant.InstanceID, RuntimeID: grant.RuntimeID, ProcessEpoch: grant.ProcessEpoch, CredentialID: grant.CredentialID, CredentialEpoch: grant.CredentialEpoch, OwnerEpoch: grant.OwnerEpoch, ControlRevision: grant.ControlRevision, AcceptingRequests: true, ActiveCountKnown: true, ActiveConnections: 9, ExpiresAt: grant.ExpiresAt, ObservedAt: issued}
			if !currentACKMatches(snapshot, credential{ID: "cert", Generation: 2}, "p0", grant, ack, issued) || !deploymentReady(true, true, true, memberRequiresDrain(snapshot, "p0", "pool-0")) {
				t.Fatal("persisted callback invalidated survivor ACK/readiness")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
			if snapshot.ActualReplicas != nil && *snapshot.ActualReplicas == 1 {
				convergedCallbacks++
				if snapshot.DrainHeld || snapshot.StopConfirmUntil != nil {
					t.Fatal("converged observation persisted before atomic resume")
				}
			}
			client.resume <- struct{}{}
		case <-done:
			if runtime.writes != 1 || convergedCallbacks == 0 {
				t.Fatalf("writes=%d converged callbacks=%d", runtime.writes, convergedCallbacks)
			}
			return
		case <-ctx.Done():
			t.Fatal("worker did not finish paused callback test")
		}
	}
}

func expectShrinkTask(mock sqlmock.Sqlmock, state domain.State, deadline time.Time) {
	command := domain.Command{ProxyID: state.ProxyID, Slot: *state.Slot, Generation: state.Generation, OwnerEpoch: state.OwnerEpoch, Action: "scale", Replicas: state.DesiredReplicas, Deadline: deadline}
	payload, _ := json.Marshal(map[string]any{"proxyDeployment": command})
	mock.ExpectQuery(`SELECT task_kind,provider_kind,target_kind,status,payload FROM execution_tasks`).WithArgs(state.LatestTaskID).WillReturnRows(sqlmock.NewRows([]string{"task_kind", "provider_kind", "target_kind", "status", "payload"}).AddRow(domain.TaskKind, state.Slot.ProviderKind(), "proxy_slot", "running", payload))
}

func TestShrinkPinsOnlyRemovedMembersOriginalConfigurationLease(t *testing.T) {
	now := time.Now().UTC()
	state := memberShrinkState(now, 2, 1)
	state.DrainLeasePinned = false
	state.DrainUntil = nil
	repo, mock := sqlRepository(t)
	until := now.Add(20 * time.Second)
	config := proxy.Configuration{Enabled: true, ContentHash: "content"}
	// No lookup of p0 is permitted: its independent lease must not pin removal.
	mock.ExpectQuery(`SELECT .*network_proxy_instances.*id IN`).WithArgs("p1").WillReturnRows(sqlmock.NewRows([]string{"id", "enabled", "desired_revision", "desired_hash", "configuration_valid_until", "configuration_lease_revision", "configuration_lease_hash"}).AddRow("p1", true, 1, "content", until, 1, config.Digest()))
	command := domain.Command{Replicas: 1, DrainUntil: now.Add(time.Minute), Deadline: now.Add(2 * time.Minute)}
	if err := pinDrainLease(context.Background(), repo.db, &state, command); err != nil || state.DrainUntil == nil || !state.DrainUntil.Equal(until) {
		t.Fatalf("pinned=%v error=%v", state.DrainUntil, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestShrinkOnlyRemovedMemberReceivesStopControl(t *testing.T) {
	now := time.Now().UTC()
	state := memberShrinkState(now, 2, 1)
	past := now.Add(-time.Second)
	state.DrainUntil = &past
	confirm := now.Add(20 * time.Second)
	state.StopConfirmUntil = &confirm
	for _, i := range []int{0, 1} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			repo, mock := sqlRepository(t)
			expectShrinkTask(mock, state, now.Add(time.Minute))
			runtimeID, instance := fmt.Sprintf("p%d", i), fmt.Sprintf("pool-%d", i)
			grant, err := grantMemberControl(context.Background(), repo.db, &state, credential{ID: "cert", Generation: 2, ExpiresAt: now.Add(time.Hour)}, runtimeID, instance, "same-boot", now)
			if err != nil || grant.StopRequired != (i == 1) || grant.AcceptingRequests != (i == 0) {
				t.Fatalf("grant=%+v error=%v", grant, err)
			}
			if i == 0 && !grant.ExpiresAt.Equal(now.Add(30*time.Second)) || i == 1 && !grant.ExpiresAt.Equal(confirm) {
				t.Fatal("incorrect member expiry")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestShrinkSurvivorAcceptingACKPersistsWithNonzeroFlows(t *testing.T) {
	now := time.Now().UTC()
	state := memberShrinkState(now, 2, 1)
	grant := domain.InstanceControl{InstanceID: "pool-0", RuntimeID: "p0", ProcessEpoch: "same-boot", CredentialID: "cert", CredentialEpoch: 2, OwnerEpoch: state.OwnerEpoch, ControlRevision: state.ControlRevision, AcceptingRequests: true, ExpiresAt: now.Add(30 * time.Second)}
	ack := domain.InstanceACK{InstanceID: grant.InstanceID, RuntimeID: grant.RuntimeID, ProcessEpoch: grant.ProcessEpoch, CredentialID: grant.CredentialID, CredentialEpoch: grant.CredentialEpoch, OwnerEpoch: grant.OwnerEpoch, ControlRevision: grant.ControlRevision, AcceptingRequests: true, ActiveCountKnown: true, ActiveConnections: 9, ExpiresAt: grant.ExpiresAt, ObservedAt: now}
	repo, mock := sqlRepository(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT proxy_id FROM network_proxy_deployment_members`).WillReturnRows(sqlmock.NewRows([]string{"proxy_id"}).AddRow("master"))
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT proxy_id FROM network_proxy_deployments.*FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"proxy_id"}).AddRow("master"))
	for i := 0; i < 2; i++ {
		mock.ExpectQuery(`SELECT id FROM network_proxy_instances`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("master"))
	}
	raw, _ := json.Marshal(state)
	mock.ExpectQuery(`SELECT state FROM network_proxy_deployments`).WillReturnRows(sqlmock.NewRows([]string{"state"}).AddRow(raw))
	mock.ExpectQuery(`SELECT id,generation,expires_at FROM network_runtime_credentials.*FOR SHARE`).WillReturnRows(sqlmock.NewRows([]string{"id", "generation", "expires_at"}).AddRow("cert", 2, now.Add(time.Hour)))
	raw, _ = json.Marshal(memberRecord{Grant: grant})
	mock.ExpectQuery(`SELECT record FROM network_proxy_deployment_instances.*FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"record"}).AddRow(raw))
	protection := &proxy.Protection{PolicyVersion: 1}
	raw, _ = json.Marshal(protection)
	config := proxy.Configuration{Enabled: true, ContentHash: "content", Protection: protection}
	mock.ExpectQuery(`SELECT .*network_proxy_instances`).WillReturnRows(sqlmock.NewRows([]string{"enabled", "desired_revision", "observed_revision", "desired_hash", "observed_hash", "desired_protection", "observed_policy_version"}).AddRow(true, 1, 1, "content", config.Digest(), raw, 1))
	mock.ExpectQuery(`SELECT COALESCE\(max\(policy_version\),0\) FROM network_access_policy_snapshots`).WillReturnRows(sqlmock.NewRows([]string{"policy_version"}).AddRow(1))
	expectShrinkTask(mock, state, now.Add(time.Minute))
	mock.ExpectExec(`INSERT INTO network_proxy_deployment_instances`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := repo.RecordACK(context.Background(), networkidentity.Identity{ID: "p0", CertificateFingerprint: "sha256:cert"}, ack); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func memberShrinkState(now time.Time, old, target int32) domain.State {
	slot := domain.Slot{ID: "slot", ProxyID: "master", ExecutorID: "exec", Namespace: "soha", Name: "pool", UID: "uid", TemplateDigest: "digest", WorkloadKind: "StatefulSet", MaxReplicas: old}
	for i := int32(0); i < old; i++ {
		slot.Members = append(slot.Members, domain.Member{RuntimeID: fmt.Sprintf("p%d", i), InstanceID: fmt.Sprintf("pool-%d", i), Ordinal: i})
	}
	drain := now.Add(5 * time.Second)
	return domain.State{ProxyID: "master", SlotID: slot.ID, UID: slot.UID, Slot: &slot, Members: slot.Members, MemberSetHash: slot.MemberSetHash(), Owner: "soha-managed", OwnerEpoch: 2, Generation: 3, ControlRevision: 8, LatestTaskID: "task", Phase: "reconciling", DrainHeld: true, DrainLeasePinned: true, DrainUntil: &drain, ActualReplicas: &old, ObservedDesiredReplicas: &old, ObservedAt: &now, DesiredReplicas: target}
}

func TestShrinkSurvivorKeepsOrdinaryGrantAndNonzeroTraffic(t *testing.T) {
	now := time.Now().UTC()
	state := memberShrinkState(now, 2, 1)
	confirmation := now.Add(20 * time.Second)
	state.StopConfirmUntil = &confirmation
	if memberRequiresDrain(state, "p0", "pool-0") || !memberRequiresDrain(state, "p1", "pool-1") {
		t.Fatal("drain was not scoped to removed member")
	}
	for _, issued := range []time.Time{now, now.Add(6 * time.Second)} {
		until, err := memberGrantExpiry(state, now.Add(time.Hour), issued, false)
		if err != nil || !until.Equal(issued.Add(30*time.Second)) {
			t.Fatalf("survivor received drain deadline %v %v", until, err)
		}
	}
	until, _ := memberGrantExpiry(state, now.Add(time.Hour), now, true)
	if until != confirmation {
		t.Fatal("removed member lost confirmation bound")
	}
	grant := domain.InstanceControl{InstanceID: "pool-0", RuntimeID: "p0", ProcessEpoch: "survivor-boot", CredentialID: "cert", CredentialEpoch: 2, OwnerEpoch: 2, ControlRevision: 8, AcceptingRequests: true, ExpiresAt: now.Add(30 * time.Second)}
	ack := domain.InstanceACK{InstanceID: grant.InstanceID, RuntimeID: grant.RuntimeID, ProcessEpoch: grant.ProcessEpoch, CredentialID: grant.CredentialID, CredentialEpoch: grant.CredentialEpoch, OwnerEpoch: grant.OwnerEpoch, ControlRevision: grant.ControlRevision, AcceptingRequests: true, ActiveCountKnown: true, ActiveConnections: 9, ExpiresAt: grant.ExpiresAt, ObservedAt: now}
	if !validACK(ack, grant, now) || !deploymentReady(true, true, ack.AcceptingRequests, memberRequiresDrain(state, "p0", "pool-0")) {
		t.Fatal("survivor active ACK lost readiness")
	}
}

func TestShrinkExemptionRejectsUntrustedStateAndTask(t *testing.T) {
	now := time.Now().UTC()
	for _, name := range []string{"unknown", "degraded", "conflict", "external", "no pin", "no lease pin", "no actual", "no desired", "no observation", "wrong roster", "wrong hash", "invalid range"} {
		t.Run(name, func(t *testing.T) {
			state := memberShrinkState(now, 2, 1)
			mutateUntrustedShrink(name, &state)
			if !memberRequiresDrain(state, "p0", "pool-0") {
				t.Fatal("untrusted survivor bypassed drain")
			}
		})
	}
	for _, name := range []string{"active", "expired", "terminal", "different target"} {
		t.Run(name, func(t *testing.T) {
			state := memberShrinkState(now, 2, 1)
			repo, mock := sqlRepository(t)
			command := domain.Command{ProxyID: state.ProxyID, Slot: *state.Slot, Generation: state.Generation, OwnerEpoch: state.OwnerEpoch, Action: "scale", Replicas: 1, Deadline: now.Add(time.Minute)}
			status := "running"
			switch name {
			case "expired":
				command.Deadline = now
			case "terminal":
				status = "failed"
			case "different target":
				command.Replicas = 0
			}
			payload, _ := json.Marshal(map[string]any{"proxyDeployment": command})
			mock.ExpectQuery(`SELECT task_kind,provider_kind,target_kind,status,payload FROM execution_tasks`).WillReturnRows(sqlmock.NewRows([]string{"task_kind", "provider_kind", "target_kind", "status", "payload"}).AddRow(domain.TaskKind, state.Slot.ProviderKind(), "proxy_slot", status, payload))
			draining, err := checkedMemberDrain(repo.db, state, "p0", "pool-0", now)
			if name == "active" && (err != nil || draining) || name != "active" && err == nil {
				t.Fatalf("draining=%v error=%v", draining, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestShrinkDrainStatusCountsOnlyExactRemovedMembers(t *testing.T) {
	now := time.Now().UTC()
	for _, target := range []int32{0, 1} {
		for _, old := range []int32{2, 3} {
			t.Run(fmt.Sprintf("%d to %d", old, target), func(t *testing.T) {
				state := memberShrinkState(now, old, target)
				until := now.Add(20 * time.Second)
				state.StopConfirmUntil = &until
				views := []api.NetworkProxyDeploymentMember{}
				for _, m := range state.Members {
					views = append(views, api.NetworkProxyDeploymentMember{InstanceID: m.InstanceID, RuntimeID: m.RuntimeID, CredentialID: "cert", AckRevision: state.ControlRevision, ObservedAt: now, ControlExpiresAt: &until, ActiveCountKnown: true, AcceptingRequests: m.Ordinal < target, ActiveConnections: 0, Ready: m.Ordinal < target})
					if m.Ordinal < target {
						views[len(views)-1].ActiveConnections = 9
					}
				}
				status := aggregateDrainStatus(state, views, now)
				if !status.CountKnown || status.Instances != int(old-target) || status.ActiveChecks != 0 || status.AcceptingRequests || !status.StopRequired {
					t.Fatalf("survivor poisoned removed proof: %+v", status)
				}
				resumed := state
				resumed.DrainHeld, resumed.StopConfirmUntil, resumed.Phase = false, nil, "resuming"
				ready := aggregateDrainStatus(resumed, views, now)
				if !ready.CountKnown || !ready.Ready || ready.Instances != int(target) || ready.ActiveChecks != int64(9*target) {
					t.Fatalf("resume did not return to target readiness: %+v", ready)
				}
				if target > 0 {
					missing := append([]api.NetworkProxyDeploymentMember(nil), views...)
					missing[target].RuntimeID = "p0"
					if aggregateDrainStatus(state, missing, now).CountKnown {
						t.Fatal("survivor substituted for removed identity")
					}
				}
				for _, name := range []string{"stale", "old revision", "unknown count", "expired", "missing"} {
					t.Run(name, func(t *testing.T) {
						assertInvalidRemovedProof(t, name, views, target, state, now)
					})
				}
			})
		}
	}
}

func TestFirstTrustedShrinkObservationScopesDrainBeforePin(t *testing.T) {
	now := time.Now().UTC()
	state := memberShrinkState(now, 2, 1)
	state.ActualReplicas = nil
	state.ObservedDesiredReplicas = nil
	state.ObservedAt = nil
	state.DrainHeld = false
	state.DrainLeasePinned = false
	command := domain.Command{ProxyID: state.ProxyID, Slot: *state.Slot, Generation: state.Generation, OwnerEpoch: state.OwnerEpoch, Action: "scale", Replicas: 1}
	applyResult(&state, delivery.ExecutionTask{Status: "running"}, command, &domain.Receipt{Observation: &domain.Observation{UID: state.UID, TemplateDigest: state.Slot.TemplateDigest, Replicas: 2, DesiredReplicas: 2, ObservedAt: now}})
	if !state.DrainHeld || !memberRequiresDrain(state, "p0", "pool-0") {
		t.Fatal("un-pinned first observation bypassed drain")
	}
	state.DrainLeasePinned = true
	if memberRequiresDrain(state, "p0", "pool-0") || !memberRequiresDrain(state, "p1", "pool-1") {
		t.Fatal("trusted first observation did not isolate removal")
	}
}

func mutateUntrustedShrink(name string, state *domain.State) {
	switch name {
	case "unknown", "degraded", "conflict":
		state.Phase = name
	case "external":
		state.Owner = name
	case "no pin":
		state.Slot = nil
	case "no lease pin":
		state.DrainLeasePinned = false
	case "no actual":
		state.ActualReplicas = nil
	case "no desired":
		state.ObservedDesiredReplicas = nil
	case "no observation":
		state.ObservedAt = nil
	case "wrong roster":
		state.Members = append([]domain.Member(nil), state.Members...)
		state.Members[0].RuntimeID = "foreign"
	case "wrong hash":
		state.MemberSetHash = "foreign"
	case "invalid range":
		x := int32(3)
		state.ActualReplicas = &x
	}
}

func assertInvalidRemovedProof(t *testing.T, name string, views []api.NetworkProxyDeploymentMember, target int32, state domain.State, now time.Time) {
	t.Helper()
	invalid := append([]api.NetworkProxyDeploymentMember(nil), views...)
	m := &invalid[target]
	switch name {
	case "stale":
		m.ObservedAt = now.Add(-11 * time.Second)
	case "old revision":
		m.AckRevision--
	case "unknown count":
		m.ActiveCountKnown = false
	case "expired":
		m.ControlExpiresAt = &now
	case "missing":
		invalid = invalid[:target]
	}
	if aggregateDrainStatus(state, invalid, now).CountKnown {
		t.Fatal("invalid removed proof counted")
	}
}
