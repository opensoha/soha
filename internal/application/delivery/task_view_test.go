package delivery

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
)

type callbackViewRepository struct {
	stubRepository
	task domaindelivery.ExecutionTask
}

func (r callbackViewRepository) GetExecutionTask(context.Context, string) (domaindelivery.ExecutionTask, error) {
	return r.task, nil
}

func TestPublicTaskAndAggregateViewsHideCallbackCapability(t *testing.T) {
	task := domaindelivery.ExecutionTask{ID: "task", ApplicationID: "app", ApplicationEnvironmentID: "binding", TaskKind: "manifest_apply", ProviderKind: "manifest_direct", Status: "running", CallbackToken: "private-callback-capability", Payload: map[string]any{}}
	repo := callbackViewRepository{stubRepository: stubRepository{tasks: []domaindelivery.ExecutionTask{task}}, task: task}
	service := New(stubApplicationReader{app: domainapp.App{ID: "app"}},
		stubCatalogReader{envs: []domaincatalog.Environment{{ID: "test", Key: "test"}}, bindings: []domaincatalog.ApplicationEnvironment{{ID: "binding", ApplicationID: "app", EnvironmentID: "test"}}},
		stubBuildReader{}, stubWorkflowReader{}, stubReleaseReader{}, repo, nil, nil,
		deliveryActionPermissions(appaccess.PermDeliveryApplicationsView, appaccess.PermDeliveryExecutionTasksView, appaccess.PermDeliveryReleaseBoardView))
	p := deliveryActionPrincipal()
	check := func(name string, value any, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s failed: %v", name, err)
		}
		data, err := json.Marshal(value)
		if err != nil || strings.Contains(string(data), task.CallbackToken) {
			t.Fatalf("%s exposed callback capability: %v", name, err)
		}
	}
	get, err := service.GetExecutionTask(t.Context(), p, task.ID)
	check("get", get, err)
	if get.CallbackToken != "[redacted]" {
		t.Fatal("configured-callback signal was lost")
	}
	list, err := service.ListExecutionTasks(t.Context(), p, domaindelivery.ExecutionTaskFilter{})
	check("list", list, err)
	if len(list) != 1 || list[0].CallbackToken != "[redacted]" {
		t.Fatal("list lost configured-callback signal")
	}
	app, err := service.GetApplicationDetail(t.Context(), p, "app")
	check("application detail", app, err)
	if app.LatestExecutionTask == nil || app.LatestExecutionTask.CallbackToken != "[redacted]" {
		t.Fatal("application task projection missing")
	}
	runtime, err := service.GetApplicationRuntimeDetail(t.Context(), p, "app")
	check("application runtime", runtime, err)
	environment, err := service.GetApplicationEnvironmentDetail(t.Context(), p, "binding")
	check("environment detail", environment, err)
	board, err := service.ListReleaseBoard(t.Context(), p)
	check("release board", board, err)
	detail, err := service.GetExecutionTaskRuntimeDetail(t.Context(), p, task.ID)
	check("task runtime detail", detail, err)
	runner, err := service.GetExecutionTaskForRunner(t.Context(), task.ID)
	if err != nil || runner.CallbackToken != task.CallbackToken || repo.task.CallbackToken != task.CallbackToken || repo.tasks[0].CallbackToken != task.CallbackToken {
		t.Fatal("public projection changed runner or repository capability")
	}
}
