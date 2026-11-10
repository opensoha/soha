package docker

import (
	"context"
	"strings"
	"testing"

	dockerruntime "github.com/opensoha/soha-contracts/docker/runtime"
	domain "github.com/opensoha/soha/internal/domain/docker"
)

type inventoryMemoryRepo struct {
	*memoryDockerRepo
	calls int
	last  []domain.ServiceInput
}

func (r *inventoryMemoryRepo) ReconcileRuntimeServices(_ context.Context, _, _ string, values []domain.ServiceInput) error {
	r.calls++
	r.last = values
	return nil
}

func TestOnlyCompleteConfirmedInventoryMayReconcile(t *testing.T) {
	binding, _ := dockerruntime.NewProjectBinding("11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222")
	item := domain.Operation{HostID: "host", ProjectID: "project", Payload: map[string]any{"action": "down"}}
	repo := &inventoryMemoryRepo{memoryDockerRepo: newMemoryDockerRepo()}
	service := New(repo, dockerTestPermissions(), nil)
	for _, payload := range []map[string]any{nil, {"services": []any{}}, {"runtimeInventoryConfirmed": false, "services": []any{}}} {
		if err := service.reconcileBoundInventory(context.Background(), item, OperationStatusCompleted, payload, binding); err != nil || repo.calls != 0 {
			t.Fatal("unconfirmed observation changed inventory", err)
		}
	}
	if err := service.reconcileBoundInventory(context.Background(), item, OperationStatusCompleted, map[string]any{"runtimeInventoryConfirmed": true, "services": []any{}}, binding); err != nil || repo.calls != 1 || len(repo.last) != 0 {
		t.Fatal("confirmed empty inventory was not applied", err)
	}
	valid := map[string]any{"name": "web", "containerId": strings.Repeat("a", 64), "status": "running"}
	for _, records := range []any{nil, "[]", []any{nil}, []any{map[string]any{"name": "web", "containerId": "short"}}, []any{valid, valid}} {
		if err := service.reconcileBoundInventory(context.Background(), item, OperationStatusCompleted, map[string]any{"runtimeInventoryConfirmed": true, "services": records}, binding); err == nil || repo.calls != 1 {
			t.Fatal("malformed confirmed inventory was applied", records, err)
		}
	}
	if err := service.reconcileBoundInventory(context.Background(), item, OperationStatusFailed, map[string]any{"runtimeInventoryConfirmed": true, "services": []any{}}, binding); err != nil || repo.calls != 1 {
		t.Fatal("failed operation cleared inventory", err)
	}
}
