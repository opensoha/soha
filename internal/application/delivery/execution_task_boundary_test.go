package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsecret "github.com/opensoha/soha/internal/domain/secret"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type taskBoundaryRepository struct {
	stubRepository
	task domaindelivery.ExecutionTask
}

func (r taskBoundaryRepository) GetExecutionTask(context.Context, string) (domaindelivery.ExecutionTask, error) {
	return r.task, nil
}

type taskBoundaryController struct {
	ExecutionController
	task  domaindelivery.ExecutionTask
	calls int
}

func (c *taskBoundaryController) CancelExecutionTask(context.Context, string, domaindelivery.ExecutionTaskActionInput) (domaindelivery.ExecutionTask, error) {
	c.calls++
	return c.task, nil
}

func (c *taskBoundaryController) RetryExecutionTask(context.Context, string, domaindelivery.ExecutionTaskActionInput) (domaindelivery.ExecutionTask, error) {
	c.calls++
	return c.task, nil
}

func taskBoundaryFixture() domaindelivery.ExecutionTask {
	return domaindelivery.ExecutionTask{ID: "task-1", ApplicationID: "app-1", ApplicationEnvironmentID: "binding-1", TaskKind: "build", Status: "queued", ProviderKind: "fixture", CallbackToken: "synthetic-runner-callback-secret", SecretLease: &domainsecret.LeaseGrant{ID: "lease-1", Token: "synthetic-lease-secret"}}
}

func taskBoundaryService(task domaindelivery.ExecutionTask, catalog CatalogReader, action string) (*Service, *taskBoundaryController) {
	controller := &taskBoundaryController{task: task}
	repo := taskBoundaryRepository{stubRepository: stubRepository{tasks: []domaindelivery.ExecutionTask{task}}, task: task}
	service := New(stubApplicationReader{}, catalog, nil, nil, nil, repo, controller, nil,
		deliveryActionPermissions(appaccess.PermDeliveryExecutionTasksView, appaccess.ManagedActionPermission(appaccess.PermDeliveryExecutionTasksManage, action)))
	return service, controller
}

type taskActionCatalog struct {
	stubCatalogReader
	permission string
}

func (c *taskActionCatalog) AuthorizeApplicationEnvironmentPermission(_ context.Context, _ domainidentity.Principal, _ string, stringKey string) (domaincatalog.ApplicationEnvironment, error) {
	c.permission = stringKey
	return domaincatalog.ApplicationEnvironment{}, apperrors.ErrAccessDenied
}

func TestExecutionTaskControlsRejectViewOnlyScopeGrant(t *testing.T) {
	for _, action := range []string{"cancel", "retry"} {
		t.Run(action, func(t *testing.T) {
			task := taskBoundaryFixture()
			catalog := &taskActionCatalog{stubCatalogReader: stubCatalogReader{bindings: []domaincatalog.ApplicationEnvironment{{ID: task.ApplicationEnvironmentID, ApplicationID: task.ApplicationID}}}}
			service, controller := taskBoundaryService(task, catalog, action)
			var err error
			if action == "cancel" {
				_, err = service.CancelExecutionTask(t.Context(), deliveryActionPrincipal(), task.ID, domaindelivery.ExecutionTaskActionInput{})
			} else {
				_, err = service.RetryExecutionTask(t.Context(), deliveryActionPrincipal(), task.ID, domaindelivery.ExecutionTaskActionInput{})
			}
			permission := appaccess.ManagedActionPermission(appaccess.PermDeliveryExecutionTasksManage, action)
			if !errors.Is(err, apperrors.ErrAccessDenied) || controller.calls != 0 || catalog.permission != permission {
				t.Fatalf("control reused view scope instead of %s: calls=%d checked=%q error=%v", action, controller.calls, catalog.permission, err)
			}
		})
	}
}

type taskActionApplication struct {
	stubApplicationReader
	permission, application string
	err                     error
}

func (a *taskActionApplication) AuthorizeApplicationPermission(_ context.Context, _ domainidentity.Principal, application, permission string) error {
	a.application, a.permission = application, permission
	return a.err
}

func TestExecutionTaskWithoutEnvironmentStillRequiresActionScope(t *testing.T) {
	for _, action := range []string{"cancel", "retry"} {
		for _, allowed := range []bool{false, true} {
			task := taskBoundaryFixture()
			task.ApplicationEnvironmentID = ""
			service, controller := taskBoundaryService(task, nil, action)
			application := &taskActionApplication{}
			if !allowed {
				application.err = apperrors.ErrAccessDenied
			}
			service.applications = application
			var err error
			if action == "cancel" {
				_, err = service.CancelExecutionTask(t.Context(), deliveryActionPrincipal(), task.ID, domaindelivery.ExecutionTaskActionInput{})
			} else {
				_, err = service.RetryExecutionTask(t.Context(), deliveryActionPrincipal(), task.ID, domaindelivery.ExecutionTaskActionInput{})
			}
			wantCalls := 0
			if allowed {
				wantCalls = 1
			}
			if allowed && err != nil || !allowed && !errors.Is(err, apperrors.ErrAccessDenied) || controller.calls != wantCalls || application.permission != appaccess.ManagedActionPermission(appaccess.PermDeliveryExecutionTasksManage, action) || application.application != task.ApplicationID {
				t.Fatalf("application-only action scope mismatch: action=%s allowed=%v calls=%d error=%v", action, allowed, controller.calls, err)
			}
		}
	}
}

func TestExecutionTaskMissingApplicationActionAuthorizerFailsClosed(t *testing.T) {
	task := taskBoundaryFixture()
	task.ApplicationEnvironmentID = ""
	service, controller := taskBoundaryService(task, nil, "cancel")
	_, err := service.CancelExecutionTask(t.Context(), deliveryActionPrincipal(), task.ID, domaindelivery.ExecutionTaskActionInput{})
	if !errors.Is(err, apperrors.ErrAccessDenied) || controller.calls != 0 {
		t.Fatal("missing action authorizer fell back to application view permission")
	}
}

func TestExecutionTaskControlsEnforceScopeBeforeController(t *testing.T) {
	for _, action := range []string{"cancel", "retry"} {
		for _, target := range []string{"denied-binding", "mismatched-application", "vm-ssh"} {
			t.Run(action+"/"+target, func(t *testing.T) {
				task := taskBoundaryFixture()
				catalog := stubCatalogReader{bindings: []domaincatalog.ApplicationEnvironment{{ID: task.ApplicationEnvironmentID, ApplicationID: task.ApplicationID}}}
				want := apperrors.ErrAccessDenied
				switch target {
				case "denied-binding":
					catalog.getErrByBindingID = map[string]error{task.ApplicationEnvironmentID: apperrors.ErrAccessDenied}
				case "mismatched-application":
					catalog.bindings[0].ApplicationID = "foreign-app"
				case "vm-ssh":
					task.TaskKind = "vm_ssh_command"
					want = apperrors.ErrUnsupportedOperation
				}
				service, controller := taskBoundaryService(task, catalog, action)
				var err error
				if action == "cancel" {
					_, err = service.CancelExecutionTask(t.Context(), deliveryActionPrincipal(), task.ID, domaindelivery.ExecutionTaskActionInput{})
				} else {
					_, err = service.RetryExecutionTask(t.Context(), deliveryActionPrincipal(), task.ID, domaindelivery.ExecutionTaskActionInput{})
				}
				if !errors.Is(err, want) || controller.calls != 0 {
					t.Fatalf("scope/control boundary failed: error=%v controller calls=%d", err, controller.calls)
				}
			})
		}
	}
}

func assertPublicTaskBoundary(t *testing.T, task domaindelivery.ExecutionTask) {
	t.Helper()
	if task.CallbackToken != "" || task.SecretLease != nil || task.Payload == nil {
		t.Fatal("public task retained a runner credential or omitted its payload object")
	}
	raw, err := json.Marshal(task)
	if err != nil || bytes.Contains(raw, []byte("synthetic-")) {
		t.Fatal("public task JSON leaked a synthetic runner secret")
	}
	var wire map[string]json.RawMessage
	if json.Unmarshal(raw, &wire) != nil || string(wire["callbackToken"]) != `""` || string(wire["payload"]) != `{}` {
		t.Fatal("public task no longer satisfies required empty callbackToken/payload fields")
	}
}

func TestExecutionTaskUserViewsRemoveRunnerCredentialsWithoutMutatingTask(t *testing.T) {
	task := taskBoundaryFixture()
	catalog := stubCatalogReader{bindings: []domaincatalog.ApplicationEnvironment{{ID: task.ApplicationEnvironmentID, ApplicationID: task.ApplicationID}}}
	service, _ := taskBoundaryService(task, catalog, "cancel")
	items, err := service.ListExecutionTasks(t.Context(), deliveryActionPrincipal(), domaindelivery.ExecutionTaskFilter{})
	if err != nil || len(items) != 1 {
		t.Fatalf("authorized list failed: %v", err)
	}
	assertPublicTaskBoundary(t, items[0])
	item, err := service.GetExecutionTask(t.Context(), deliveryActionPrincipal(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertPublicTaskBoundary(t, item)
	for _, action := range []string{"cancel", "retry"} {
		service, controller := taskBoundaryService(task, catalog, action)
		if action == "cancel" {
			item, err = service.CancelExecutionTask(t.Context(), deliveryActionPrincipal(), task.ID, domaindelivery.ExecutionTaskActionInput{})
		} else {
			item, err = service.RetryExecutionTask(t.Context(), deliveryActionPrincipal(), task.ID, domaindelivery.ExecutionTaskActionInput{})
		}
		if err != nil || controller.calls != 1 || item.Status != task.Status {
			t.Fatalf("authorized %s changed lifecycle state: calls=%d error=%v", action, controller.calls, err)
		}
		assertPublicTaskBoundary(t, item)
	}
	assertPublicTaskBoundary(t, *latestExecutionTaskForApplication(task.ApplicationID, []domaindelivery.ExecutionTask{task}))
	assertPublicTaskBoundary(t, *latestExecutionTaskForBinding(catalog.bindings[0], []domaindelivery.ExecutionTask{task}))
	// Runner serialization and stored objects retain their credentials. Only
	// the explicitly selected user views may replace them with an empty string.
	raw, err := json.Marshal(task)
	if err != nil || !bytes.Contains(raw, []byte(task.CallbackToken)) || !bytes.Contains(raw, []byte(task.SecretLease.Token)) || task.Payload != nil {
		t.Fatal("public projection mutated the stored/runner task")
	}
}
