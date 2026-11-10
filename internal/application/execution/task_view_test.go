package execution

import (
	"context"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type sharedTaskViewRepository struct {
	*executionRepoFake
	items []domaindelivery.ExecutionTask
}

func (r *sharedTaskViewRepository) ListExecutionTasks(context.Context, domaindelivery.ExecutionTaskFilter) ([]domaindelivery.ExecutionTask, error) {
	return r.items, nil
}

func (r *sharedTaskViewRepository) GetExecutionTask(_ context.Context, id string) (domaindelivery.ExecutionTask, error) {
	for _, item := range r.items {
		if item.ID == id {
			return item, nil
		}
	}
	return r.executionRepoFake.GetExecutionTask(context.Background(), id)
}

func TestTaskViewDoesNotGrantCallbackCapability(t *testing.T) {
	repo := &sharedTaskViewRepository{executionRepoFake: newExecutionRepoFake(), items: []domaindelivery.ExecutionTask{
		{ID: "active", TaskKind: "manifest_apply", ProviderKind: "manifest_direct", Status: "running", CallbackToken: "active-callback"},
		{ID: "finished", TaskKind: "manifest_apply", ProviderKind: "manifest_direct", Status: "failed", CallbackToken: "finished-callback"},
	}}
	permissions := appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{"viewer": {appaccess.PermDeliveryExecutionTasksView}}})
	service := New(repo, nil, nil, nil, "", "", "", "", 0, "", permissions)
	viewer := domainidentity.Principal{Roles: []string{"viewer"}}
	listed, err := service.ListExecutionTasks(t.Context(), viewer, domaindelivery.ExecutionTaskFilter{})
	if err != nil || len(listed) != 2 {
		t.Fatalf("task view failed: %v", err)
	}
	for _, item := range listed {
		if item.CallbackToken != "[redacted]" || item.OperationState == nil {
			t.Fatal("task list exposed callback capability or lost operation state")
		}
		got, err := service.GetExecutionTask(t.Context(), viewer, item.ID)
		if err != nil || got.CallbackToken != "[redacted]" || got.OperationState == nil {
			t.Fatalf("task get exposed callback capability or lost operation state: %v", err)
		}
		internal, err := service.GetExecutionTaskInternal(t.Context(), item.ID)
		if err != nil || internal.CallbackToken == "" {
			t.Fatalf("public projection changed internal runner state: %v", err)
		}
	}
	if items, err := service.ListExecutionTasks(t.Context(), domainidentity.Principal{}, domaindelivery.ExecutionTaskFilter{}); err == nil || len(items) != 0 {
		t.Fatal("list permission check was bypassed")
	}
	if _, err := service.GetExecutionTask(t.Context(), domainidentity.Principal{}, "active"); err == nil {
		t.Fatal("get permission check was bypassed")
	}
}
