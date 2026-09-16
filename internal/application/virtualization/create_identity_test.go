package virtualization

import (
	"context"
	"fmt"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
)

type identityPreparingAdapter struct {
	recoveryAdapter
	calls int
}

func (a *identityPreparingAdapter) PrepareVMCreate(_ context.Context, _ domain.AdapterConnection, input domain.AdapterCreateVMInput) (domain.AdapterCreateVMInput, error) {
	a.calls++
	input.Node = "pve-node"
	input.ProviderParams = cloneMap(input.ProviderParams)
	if input.ProviderParams == nil {
		input.ProviderParams = map[string]any{}
	}
	if input.ProviderParams["vmid"] == nil {
		input.ProviderParams["vmid"] = fmt.Sprint(700 + a.calls)
	}
	return input, nil
}

type identityConflictRepo struct {
	*memoryRepo
	checkpoints int
}

func (r *identityConflictRepo) UpdateTask(ctx context.Context, task domain.Task) (domain.Task, error) {
	if task.Status == TaskStatusRunning && boolValue(task.Payload, "providerIdentityPrepared") {
		r.checkpoints++
		if r.checkpoints == 1 {
			return domain.Task{}, domain.ErrProviderIdentityClaimed
		}
	}
	return r.memoryRepo.UpdateTask(ctx, task)
}

func TestDynamicPVEIdentityConflictRechecksBeforeAnyProviderWrite(t *testing.T) {
	for _, fixed := range []bool{false, true} {
		repo := newMemoryRepo()
		connection := repo.addConnection(domain.Connection{Provider: ProviderPVE, Enabled: true, Endpoint: "https://pve.example"})
		adapter := &identityPreparingAdapter{}
		service := newTestService(repo, &captureOperations{}, adapter)
		conflicts := &identityConflictRepo{memoryRepo: repo}
		service.tasks = conflicts
		input := CreateVMInput{ConnectionID: connection.ID, Name: "worker"}
		if fixed {
			input.ProviderParams = map[string]any{"vmid": "701"}
		}
		created, err := service.CreateVM(context.Background(), testPrincipal(), input)
		if err != nil {
			t.Fatal(err)
		}
		writes := 0
		adapter.create = func(input domain.AdapterCreateVMInput) (domain.AdapterVM, error) {
			writes++
			if input.ProviderParams["vmid"] != "702" {
				t.Fatalf("wrong retried VMID: %+v", input.ProviderParams)
			}
			return domain.AdapterVM{ID: "702", Name: input.Name}, nil
		}
		claimed, err := repo.ClaimTask(context.Background(), "worker", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		service.executeTask(context.Background(), claimed)
		task, _ := repo.GetTask(context.Background(), created.ID)
		if fixed {
			if adapter.calls != 1 || writes != 0 || task.Status != TaskStatusFailed || task.Result["providerEffect"] != "not_started" {
				t.Fatalf("fixed ID changed or reached provider: %+v", task)
			}
		} else if adapter.calls != 2 || writes != 1 || task.Status != TaskStatusSucceeded {
			t.Fatalf("dynamic ID did not recover before dispatch: calls=%d writes=%d task=%+v", adapter.calls, writes, task)
		}
	}
}

func TestQueuedVMRejectsChangedConnectionBeforePreparation(t *testing.T) {
	repo := newMemoryRepo()
	connection := repo.addConnection(domain.Connection{Provider: ProviderPVE, Enabled: true, Endpoint: "https://original.example"})
	adapter := &identityPreparingAdapter{}
	service := newTestService(repo, &captureOperations{}, adapter)
	created, err := service.CreateVM(context.Background(), testPrincipal(), CreateVMInput{ConnectionID: connection.ID, Name: "worker"})
	if err != nil {
		t.Fatal(err)
	}
	connection.Endpoint = "https://different.example"
	repo.connections[connection.ID] = connection
	claimed, _ := repo.ClaimTask(context.Background(), "worker", time.Now())
	service.executeTask(context.Background(), claimed)
	task, _ := repo.GetTask(context.Background(), created.ID)
	if adapter.calls != 0 || task.Status != TaskStatusFailed || task.Result["providerEffect"] != "not_started" {
		t.Fatalf("queued task followed retargeted connection: %+v", task)
	}
}
