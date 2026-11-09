package proxydeployment

import (
	"context"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/proxydeployment"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
	"time"
)

type memoryRepository struct {
	state    domain.State
	mutation Mutation
	commits  int
	grants   int
}

func (r *memoryRepository) Get(context.Context, string) (domain.State, error) { return r.state, nil }
func (r *memoryRepository) GetForMember(context.Context, string, string) (domain.State, error) {
	return r.state, nil
}
func (r *memoryRepository) Commit(_ context.Context, m Mutation) (domain.State, error) {
	r.commits++
	r.mutation = m
	r.state = m.State
	return m.State, nil
}
func (r *memoryRepository) RecordResult(context.Context, delivery.ExecutionTask, domain.Command, *domain.Receipt) error {
	return nil
}
func (r *memoryRepository) TaskByToken(context.Context, string) (delivery.ExecutionTask, error) {
	return delivery.ExecutionTask{}, nil
}
func (r *memoryRepository) Members(context.Context, string) ([]api.NetworkProxyDeploymentMember, error) {
	return []api.NetworkProxyDeploymentMember{}, nil
}
func (r *memoryRepository) DrainStatus(context.Context, string, int64, time.Time) (domain.DrainStatus, error) {
	return domain.DrainStatus{}, nil
}
func (r *memoryRepository) GrantControl(context.Context, networkidentity.Identity, string, string, string) (domain.InstanceControl, error) {
	r.grants++
	return domain.InstanceControl{}, nil
}
func (r *memoryRepository) RecordACK(context.Context, networkidentity.Identity, domain.InstanceACK) error {
	return nil
}

type scope struct{}

func (scope) AuthorizeDeployment(_ context.Context, p identity.Principal, _ string, _ bool) error {
	if p.UserID != "op" {
		return apperrors.ErrAccessDenied
	}
	return nil
}
func testSlot() domain.Slot {
	return domain.Slot{ID: "slot", ProxyID: "master", ExecutorID: "exec", Namespace: "soha", Name: "pool", UID: "uid", TemplateDigest: "digest", WorkloadKind: "StatefulSet", MaxReplicas: 2, AllowAdoption: true, PreviousOwnerStopped: true, Members: []domain.Member{{InstanceID: "pool-0", RuntimeID: "master", Ordinal: 0}, {InstanceID: "pool-1", RuntimeID: "member", Ordinal: 1}}}
}
func TestProxyDeploymentRequiresApprovedAdoptionAndScope(t *testing.T) {
	for _, name := range []string{"valid", "scope denied", "no adoption", "no stopped evidence", "wrong UID", "wrong owner epoch"} {
		t.Run(name, func(t *testing.T) {
			slot := testSlot()
			input := domain.BindingRequest{RequestID: "req", SlotID: slot.ID, ObjectUID: slot.UID, TemplateHash: slot.TemplateDigest, Owner: "soha-managed", Adopt: true}
			p := identity.Principal{UserID: "op"}
			switch name {
			case "scope denied":
				p.UserID = "other"
			case "no adoption":
				input.Adopt = false
			case "no stopped evidence":
				slot.PreviousOwnerStopped = false
			case "wrong UID":
				input.ObjectUID = "replacement"
			case "wrong owner epoch":
				input.OwnerEpoch = 7
			}
			repo := &memoryRepository{state: domain.State{ProxyID: "master", Owner: "external"}}
			service, err := New(repo, nil, []domain.Slot{slot}, nil, scope{})
			if err != nil {
				t.Fatal(err)
			}
			view, err := service.BindRequest(context.Background(), p, "master", input)
			if name == "valid" {
				if err != nil || view.Owner != "soha-managed" || repo.state.OwnerEpoch != 1 || repo.state.MemberSetHash == "" {
					t.Fatalf("bad adoption: %+v %v", view, err)
				}
			} else if err == nil || repo.commits != 0 {
				t.Fatalf("unapproved bind: %+v %v", view, err)
			}
		})
	}
}
func TestProxyDeploymentControlsUseDurable240SecondTaskAnd120SecondDrain(t *testing.T) {
	for _, name := range []string{"stop", "scale", "over ceiling", "over drain"} {
		t.Run(name, func(t *testing.T) {
			slot := testSlot()
			n := int32(2)
			repo := &memoryRepository{state: domain.State{ProxyID: "master", SlotID: slot.ID, Slot: &slot, UID: slot.UID, Owner: "soha-managed", OwnerEpoch: 1, Generation: 1, ControlRevision: 3, ActualReplicas: &n}}
			service, err := New(repo, nil, []domain.Slot{slot}, nil, scope{})
			if err != nil {
				t.Fatal(err)
			}
			service.executors["exec"] = true
			input := domain.ControlRequest{RequestID: "req", ExpectedRevision: 1, Action: "stop"}
			switch name {
			case "scale":
				input.Action = "scale"
				input.Replicas = 1
			case "over ceiling":
				input.Action = "scale"
				input.Replicas = 3
			case "over drain":
				input.DrainSeconds = 121
			}
			_, err = service.ControlRequest(context.Background(), identity.Principal{UserID: "op"}, "master", input)
			if name == "over ceiling" || name == "over drain" {
				if err == nil || repo.commits != 0 {
					t.Fatal("unbounded operation accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			task := repo.mutation.Task
			if task == nil || task.TimeoutSeconds != 240 || task.ApplicationID != "" {
				t.Fatalf("not a private durable task: %+v", task)
			}
			command, err := DecodeCommand(*task)
			if err != nil || command.DrainTimeoutSeconds != 120 || time.Until(command.Deadline) > 240*time.Second || time.Until(command.Deadline) < 238*time.Second {
				t.Fatalf("wrong deadline/drain: %+v %v", command, err)
			}
			if !repo.state.DrainHeld || repo.state.ControlRevision != 4 {
				t.Fatal("shrink gate not committed")
			}
		})
	}
}
func TestProxyDeploymentRegistryRejectsSharedIdentityAndTypedNil(t *testing.T) {
	slot := testSlot()
	slot.Members[1].RuntimeID = "master"
	if _, err := New(&memoryRepository{}, nil, []domain.Slot{slot}, nil, scope{}); err == nil {
		t.Fatal("shared identity accepted")
	}
	var repo *memoryRepository
	if _, err := New(repo, nil, nil, nil, scope{}); err == nil {
		t.Fatal("typed nil accepted")
	}
}

type executionTasksFake struct{ task delivery.ExecutionTask }

func (f executionTasksFake) ClaimProxyDeploymentExecutionTask(context.Context, string) (delivery.ExecutionTask, error) {
	return f.task, nil
}
func (f executionTasksFake) GetProxyDeploymentExecutionTask(context.Context, string, string) (delivery.ExecutionTask, error) {
	return f.task, nil
}
func (f executionTasksFake) RecordProxyDeploymentCallback(context.Context, string, delivery.ExecutionCallbackInput) (delivery.ExecutionTask, error) {
	return f.task, nil
}
func TestProxyDeploymentExecutorChecksServerDeadlineAndCurrentFence(t *testing.T) {
	for _, name := range []string{"valid", "expired", "old generation", "wrong workload"} {
		t.Run(name, func(t *testing.T) {
			slot := testSlot()
			repo := &memoryRepository{state: domain.State{ProxyID: "master", SlotID: slot.ID, Slot: &slot, UID: slot.UID, Owner: "soha-managed", OwnerEpoch: 1, Generation: 2, LatestTaskID: "task"}}
			service, err := New(repo, nil, []domain.Slot{slot}, nil, scope{})
			if err != nil {
				t.Fatal(err)
			}
			command := domain.Command{ProxyID: "master", Slot: slot, OwnerEpoch: 1, Generation: 2, Deadline: time.Now().Add(time.Minute)}
			switch name {
			case "expired":
				command.Deadline = time.Now().Add(-time.Second)
			case "old generation":
				command.Generation = 1
			case "wrong workload":
				command.Slot.UID = "other"
			}
			task := delivery.ExecutionTask{ID: "task", TaskKind: domain.TaskKind, ProviderKind: slot.ProviderKind(), ClaimedByAgentID: "exec", Status: "running", Payload: map[string]any{"proxyDeployment": command}}
			executor, err := NewExecutorService(service, executionTasksFake{task}, []ExecutorCredential{{ExecutorID: "exec", Token: "01234567890123456789012345678901"}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = executor.Status(context.Background(), "01234567890123456789012345678901", "task")
			if name == "valid" && err != nil || name != "valid" && err == nil {
				t.Fatalf("fence outcome %v", err)
			}
		})
	}
}

type writeOnlyScope struct{}

func (writeOnlyScope) AuthorizeDeployment(_ context.Context, _ identity.Principal, _ string, write bool) error {
	if !write {
		return apperrors.ErrAccessDenied
	}
	return nil
}
func TestProxyDeploymentBindingDoesNotRequireExtraViewPermission(t *testing.T) {
	slot := testSlot()
	repo := &memoryRepository{state: domain.State{ProxyID: "master", Owner: "external"}}
	service, err := New(repo, nil, []domain.Slot{slot}, nil, writeOnlyScope{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.BindRequest(context.Background(), identity.Principal{}, "master", domain.BindingRequest{RequestID: "req", SlotID: slot.ID, ObjectUID: slot.UID, TemplateHash: slot.TemplateDigest, Owner: "soha-managed", Adopt: true})
	if err != nil {
		t.Fatal(err)
	}
}

func TestStopConfirmationApplicationRejectsImpossibleStoppedProof(t *testing.T) {
	service, err := New(&memoryRepository{}, nil, nil, nil, scope{})
	if err != nil {
		t.Fatal(err)
	}
	identity := networkidentity.Identity{Scope: networkidentity.ScopeNetworkControl, Kind: "proxy", ID: "p0"}
	for _, name := range []string{"known stopped", "unknown count", "active process", "accepting"} {
		ack := domain.InstanceACK{RuntimeID: "p0", InstanceID: "pool-0", ProcessEpoch: "boot", ProcessStopped: true, ActiveCountKnown: true}
		switch name {
		case "unknown count":
			ack.ActiveCountKnown = false
		case "active process":
			ack.ActiveConnections = 1
		case "accepting":
			ack.AcceptingRequests = true
		}
		err = service.RuntimeACK(context.Background(), identity, ack)
		if name == "known stopped" && err != nil || name != "known stopped" && err == nil {
			t.Fatalf("%s accepted invalid stopped semantics: %v", name, err)
		}
	}
}
