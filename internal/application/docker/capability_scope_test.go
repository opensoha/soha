package docker

import (
	"context"
	"errors"
	"reflect"
	"testing"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestDockerCapabilityScopesUseDomainResources(t *testing.T) {
	repo := newMemoryDockerRepo()
	repo.projects["project-1"] = domaindocker.Project{ID: "project-1", HostID: "host-1", ComposeContent: "services: {}"}
	repo.services["service-1"] = domaindocker.Service{ID: "service-1", ProjectID: "project-1", HostID: "host-1"}
	repo.operations["operation-1"] = domaindocker.Operation{ID: "operation-1", HostID: "host-1", ProjectID: "project-1", ServiceID: "service-1", Status: "queued"}
	service := New(repo, dockerTestPermissions(), nil)
	for _, name := range []string{"docker.projects.deploy.plan", "docker.projects.deploy.trigger", "docker.services.action.trigger", "docker.operations.get", "docker.operations.cancel", "docker.projects.runtime.assess"} {
		input := map[string]any{"projectId": "project-1", "serviceId": "service-1", "operationId": "operation-1", "afterOperationId": "operation-1", "action": "restart", "idempotencyKey": "scoped-operation-1"}
		scopes, err := service.ToolInvocationScopes(context.Background(), dockerTestPrincipal(), domainaigateway.ToolCapability{Name: name}, input)
		if err != nil || len(scopes) != 1 || scopes[0]["hostId"] != "host-1" || scopes[0]["projectId"] != "project-1" {
			t.Fatalf("%s scopes=%v err=%v", name, scopes, err)
		}
	}
	if len(repo.operations) != 1 {
		t.Fatal("scope discovery enqueued work")
	}
	op := repo.operations["operation-1"]
	op.HostID = "other-host"
	repo.operations[op.ID] = op
	if _, err := service.ToolInvocationScopes(context.Background(), dockerTestPrincipal(), domainaigateway.ToolCapability{Name: "docker.projects.runtime.assess"}, map[string]any{"projectId": "project-1", "afterOperationId": op.ID}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("mixed project and operation accepted: %v", err)
	}
}

func TestDockerScopeGuardStopsDispatchAndReceiptRetargeting(t *testing.T) {
	repo := newMemoryDockerRepo()
	repo.projects["project-1"] = domaindocker.Project{ID: "project-1", HostID: "host-1", ComposeContent: "services: {}"}
	repo.services["service-1"] = domaindocker.Service{ID: "service-1", ProjectID: "project-1", HostID: "host-1", Name: "api"}
	service := New(repo, dockerTestPermissions(), nil)
	request := domaindocker.ProjectDeployInput{IdempotencyKey: "scope-receipt-1"}
	op, err := service.DeployProject(context.Background(), dockerTestPrincipal(), "project-1", request)
	if err != nil {
		t.Fatal(err)
	}
	before := repo.operations[op.ID]
	ctx := domaindocker.WithScopeCheck(context.Background(), func(scope map[string]string) error {
		if scope["hostId"] != "host-2" {
			return apperrors.ErrConflict
		}
		return nil
	})
	if _, err := service.GetOperation(ctx, dockerTestPrincipal(), op.ID); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("unauthorized read: %v", err)
	}
	if _, err := service.CancelOperation(ctx, dockerTestPrincipal(), op.ID); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("unauthorized cancel: %v", err)
	}
	project := repo.projects["project-1"]
	project.HostID = "host-2"
	repo.projects[project.ID] = project
	if _, err := service.DeployProject(ctx, dockerTestPrincipal(), project.ID, request); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("old receipt escaped new scope: %v", err)
	}
	if _, err := service.FindProjectDeployOperation(ctx, dockerTestPrincipal(), project.ID, request); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("recovery escaped scope: %v", err)
	}
	if _, err := service.ServiceAction(context.Background(), dockerTestPrincipal(), "service-1", domaindocker.ServiceActionInput{Action: "restart", IdempotencyKey: "service-scope-1"}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("service and project host mismatch accepted: %v", err)
	}
	delete(repo.projects, project.ID)
	if _, err := service.ServiceAction(context.Background(), dockerTestPrincipal(), "service-1", domaindocker.ServiceActionInput{Action: "restart", IdempotencyKey: "service-scope-1"}); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("missing project enqueued: %v", err)
	}
	if len(repo.operations) != 1 || !reflect.DeepEqual(repo.operations[op.ID], before) {
		t.Fatal("denied operation changed durable state")
	}
}

func TestDockerQuickCreateScopesPinFutureHost(t *testing.T) {
	repo := newMemoryDockerRepo()
	service := New(repo, dockerTestPermissions(), nil)
	input := map[string]any{"name": "docker-dev", "virtualizationConnectionId": "pve-1", "cpuCoreCount": 2, "memoryBytes": 2147483648, "diskBytes": 21474836480, "cloudInit": dockerQuickCreateTestCloudInit, "idempotencyKey": "create-scoped-host"}
	plan, err := service.ToolInvocationScopes(context.Background(), dockerTestPrincipal(), domainaigateway.ToolCapability{Name: "docker.hosts.quick_create.plan"}, input)
	if err != nil || plan[0]["virtualizationConnectionId"] != "pve-1" || plan[0]["hostId"] != "" {
		t.Fatalf("plan=%v %v", plan, err)
	}
	trigger, err := service.ToolInvocationScopes(context.Background(), dockerTestPrincipal(), domainaigateway.ToolCapability{Name: "docker.hosts.quick_create.trigger"}, input)
	if err != nil || trigger[0]["hostId"] == "" {
		t.Fatalf("trigger=%v %v", trigger, err)
	}
	again, err := service.ToolInvocationScopes(context.Background(), dockerTestPrincipal(), domainaigateway.ToolCapability{Name: "docker.hosts.quick_create.trigger"}, input)
	if err != nil || !reflect.DeepEqual(trigger, again) || len(repo.hosts) != 0 || len(repo.operations) != 0 {
		t.Fatalf("discovery did not remain stable/read-only: %v %v", again, err)
	}
}

func TestDockerQuickCreateWithoutVirtualizationHasCreationScope(t *testing.T) {
	service := New(newMemoryDockerRepo(), dockerTestPermissions(), nil)
	input := map[string]any{"name": "manual-host", "idempotencyKey": "manual-create-1"}
	for _, name := range []string{"docker.hosts.quick_create.plan", "docker.hosts.quick_create.trigger"} {
		scopes, err := service.ToolInvocationScopes(context.Background(), dockerTestPrincipal(), domainaigateway.ToolCapability{Name: name}, input)
		if err != nil || len(scopes) != 1 || scopes[0]["resourceKind"] != "docker.host" {
			t.Fatalf("%s: %v %v", name, scopes, err)
		}
		for key, value := range scopes[0] {
			if value == "" {
				t.Fatalf("empty %s scope in %s", key, name)
			}
		}
	}
}
