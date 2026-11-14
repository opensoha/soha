package docker

import (
	"context"
	"errors"
	"strings"
	"testing"

	dockerruntime "github.com/opensoha/soha/contracts/docker/runtime"
	domain "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type identityCheckingDockerRepo struct {
	*memoryDockerRepo
	callbackCalls int
}

func (r *identityCheckingDockerRepo) WithinRuntimeIdentity(ctx context.Context, hostID, projectID, hostUID, projectUID string, run func(context.Context) error) error {
	if r.hosts[hostID].ResourceUID != hostUID || r.projects[projectID].ResourceUID != projectUID || r.projects[projectID].HostID != hostID {
		return apperrors.ErrConflict
	}
	r.callbackCalls++
	return run(ctx)
}

func TestRuntimeIdentityRechecksOriginalRowsBeforeDispatchAndCallback(t *testing.T) {
	repo := &identityCheckingDockerRepo{memoryDockerRepo: newMemoryDockerRepo()}
	host := domain.Host{ID: "host-1"}
	host.ResourceUID = "11111111-1111-4111-8111-111111111111"
	repo.hosts[host.ID] = host
	project := domain.Project{ID: "bound", HostID: host.ID, ResourceUID: "22222222-2222-4222-8222-222222222222", RuntimeIdentityVersion: 1, Status: "draft"}
	repo.projects[project.ID] = project
	service := New(repo, dockerTestPermissions(), nil)
	binding, _ := dockerruntime.NewProjectBinding(host.ResourceUID, project.ResourceUID)
	item := domain.Operation{ProjectID: project.ID, HostID: host.ID, OperationKind: OperationKindProjectDeploy, Payload: map[string]any{"runtimeBinding": binding, "runtimeContainers": []dockerruntime.ContainerBinding{}, "action": "deploy"}}
	if err := service.authorizeQueuedOperation(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	if err := service.applyCallbackRuntimeState(context.Background(), item, OperationStatusRunning, nil); err != nil || repo.callbackCalls != 1 {
		t.Fatal("original identity callback did not reach its transaction", err)
	}
	project.ResourceUID = "33333333-3333-4333-8333-333333333333"
	repo.projects[project.ID] = project
	if err := service.authorizeQueuedOperation(context.Background(), item); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("recreated target dispatched", err)
	}
	if err := service.applyCallbackRuntimeState(context.Background(), item, OperationStatusCompleted, nil); !errors.Is(err, apperrors.ErrConflict) || repo.callbackCalls != 1 {
		t.Fatal("replacement identity callback reached the transaction body", err)
	}
	legacy := New(repo.memoryDockerRepo, dockerTestPermissions(), nil)
	if err := legacy.applyCallbackRuntimeState(context.Background(), item, OperationStatusCompleted, nil); !errors.Is(err, apperrors.ErrUnsupportedOperation) {
		t.Fatal("repository without identity transactions accepted a bound callback", err)
	}
	if repo.projects[project.ID].Status != "draft" {
		t.Fatal("old callback changed replacement project")
	}
	if validateFrozenRuntimeIdentity(project, host, &binding) == nil {
		t.Fatal("old delivery snapshot adopted replacement")
	}
	project.ResourceUID = binding.ProjectUID
	if validateFrozenRuntimeIdentity(project, host, nil) == nil {
		t.Fatal("legacy snapshot adopted bound project")
	}
}

func TestRuntimeServiceDispatchRejectsReplacedContainer(t *testing.T) {
	repo := newMemoryDockerRepo()
	host := domain.Host{ID: "host", ResourceUID: "11111111-1111-4111-8111-111111111111"}
	project := domain.Project{ID: "project", HostID: host.ID, ResourceUID: "22222222-2222-4222-8222-222222222222", RuntimeIdentityVersion: 1}
	repo.hosts[host.ID], repo.projects[project.ID] = host, project
	binding, _ := dockerruntime.NewProjectBinding(host.ResourceUID, project.ResourceUID)
	target := dockerruntime.ContainerBinding{ProjectBinding: binding, ServiceName: "web", ContainerID: strings.Repeat("a", 64)}
	repo.services["service"] = domain.Service{ID: "service", HostID: host.ID, ProjectID: project.ID, Name: target.ServiceName, ContainerID: target.ContainerID}
	item := domain.Operation{HostID: host.ID, ProjectID: project.ID, ServiceID: "service", OperationKind: OperationKindServiceAction, Payload: map[string]any{"runtimeBinding": binding, "runtimeContainer": target, "runtimeContainers": []dockerruntime.ContainerBinding{target}}}
	service := New(repo, dockerTestPermissions(), nil)
	if err := service.checkRuntimeOperationIdentity(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	replacement := repo.services["service"]
	replacement.ContainerID = strings.Repeat("b", 64)
	repo.services["service"] = replacement
	if err := service.checkRuntimeOperationIdentity(context.Background(), item); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("stale service operation adopted a replacement", err)
	}
	delete(item.Payload, "runtimeContainer")
	replacement.ContainerID = target.ContainerID
	repo.services["service"] = replacement
	if err := service.checkRuntimeOperationIdentity(context.Background(), item); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("missing frozen ID fell back to service name", err)
	}
}
