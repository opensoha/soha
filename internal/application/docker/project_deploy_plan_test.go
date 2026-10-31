package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type reviewedDeployRepository struct{ *memoryDockerRepo }

func (r *reviewedDeployRepository) WithinProjectDeployPlan(ctx context.Context, id string, expected domain.ProjectDeployPreconditions, apply func(context.Context) error) error {
	project, host := r.projects[id], r.hosts[expected.HostID]
	if project.HostID != expected.HostID || host.ResourceUID != expected.Host.ExpectedUID.String() || host.ConfigurationRevision != expected.Host.ExpectedConfigurationRevision.String() || project.ResourceUID != expected.Project.ExpectedUID.String() || project.ConfigurationRevision != expected.Project.ExpectedConfigurationRevision.String() {
		return apperrors.ErrConflict
	}
	return apply(ctx)
}

func reviewedDeployFixture(version int) (*reviewedDeployRepository, *Service) {
	repo := &reviewedDeployRepository{newMemoryDockerRepo()}
	repo.hosts["host-1"] = domain.Host{ID: "host-1", ResourceUID: uuid.NewString(), ConfigurationRevision: uuid.NewString()}
	repo.projects["project-1"] = domain.Project{ID: "project-1", HostID: "host-1", Name: "owned", Slug: "owned", ResourceUID: uuid.NewString(), ConfigurationRevision: uuid.NewString(), RuntimeIdentityVersion: version, ComposeContent: "services:\n  api:\n    image: nginx:alpine\n", EnvContent: "PRIVATE_ENV=value"}
	return repo, New(repo, dockerTestPermissions(), nil)
}

func TestReviewedDockerDeployRejectsChangedConfiguration(t *testing.T) {
	for _, version := range []int{0, 1} {
		for _, change := range []string{"compose", "host-config", "project-recreated", "host-recreated", "moved"} {
			t.Run(fmt.Sprintf("v%d/%s", version, change), func(t *testing.T) {
				repo, service := reviewedDeployFixture(version)
				preview, err := service.PlanProjectDeploy(t.Context(), dockerTestPrincipal(), "project-1", domain.ProjectDeployInput{Action: "deploy"})
				if err != nil {
					t.Fatal(err)
				}
				project, host := repo.projects["project-1"], repo.hosts["host-1"]
				switch change {
				case "compose":
					project.ComposeContent = "services: {changed: {image: alpine}}"
					project.ConfigurationRevision = uuid.NewString()
				case "host-config":
					host.ConfigurationRevision = uuid.NewString()
				case "project-recreated":
					project.ResourceUID = uuid.NewString()
				case "host-recreated":
					host.ResourceUID = uuid.NewString()
				case "moved":
					project.HostID = "host-2"
				}
				repo.projects[project.ID], repo.hosts[host.ID] = project, host
				_, err = service.DeployProject(t.Context(), dockerTestPrincipal(), project.ID, domain.ProjectDeployInput{Action: "deploy", IdempotencyKey: "reviewed-original", Preconditions: preview.DockerProjectDeploy})
				if !errors.Is(err, apperrors.ErrConflict) || len(repo.operations) != 0 {
					t.Fatalf("stale deployment enqueued: error=%v operations=%d", err, len(repo.operations))
				}
			})
		}
	}
}

func TestReviewedDockerDeployOriginalReceiptSurvivesChangedConfiguration(t *testing.T) {
	repo, service := reviewedDeployFixture(1)
	preview, err := service.PlanProjectDeploy(t.Context(), dockerTestPrincipal(), "project-1", domain.ProjectDeployInput{Action: "deploy"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(preview)
	if strings.Contains(string(encoded), "PRIVATE_ENV") || strings.Contains(string(encoded), "nginx:alpine") {
		t.Fatal("plan leaked runtime configuration")
	}
	input := domain.ProjectDeployInput{Action: "deploy", IdempotencyKey: "reviewed-original", Preconditions: preview.DockerProjectDeploy}
	original, err := service.DeployProject(t.Context(), dockerTestPrincipal(), "project-1", input)
	if err != nil || original.Payload["composeContent"] != repo.projects["project-1"].ComposeContent {
		t.Fatalf("original=%#v error=%v", original, err)
	}
	project := repo.projects["project-1"]
	project.ConfigurationRevision, project.ComposeContent = uuid.NewString(), "services: {changed: {image: alpine}}"
	repo.projects[project.ID] = project
	recovered, err := service.DeployProject(t.Context(), dockerTestPrincipal(), project.ID, input)
	if err != nil || recovered.ID != original.ID || len(repo.operations) != 1 {
		t.Fatalf("original replay: %v operations=%d", err, len(repo.operations))
	}
	lookup, err := service.GetProjectDeploymentRequest(t.Context(), dockerTestPrincipal(), project.ID, input.IdempotencyKey, input.Action)
	if err != nil || lookup.ID != original.ID || lookup.Payload != nil || lookup.Result != nil {
		t.Fatalf("unsafe original receipt: %#v %v", lookup, err)
	}
	if _, err := service.GetProjectDeploymentRequest(t.Context(), dockerTestPrincipal(), project.ID, input.IdempotencyKey, "destroy"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("action mismatch: %v", err)
	}
	other := dockerTestPrincipal()
	other.UserID = "other-actor"
	if _, err := service.GetProjectDeploymentRequest(t.Context(), other, project.ID, input.IdempotencyKey, "deploy"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("cross-actor receipt: %v", err)
	}
	next, err := service.PlanProjectDeploy(t.Context(), dockerTestPrincipal(), project.ID, domain.ProjectDeployInput{Action: "deploy"})
	if err != nil || preview.InputHash == next.InputHash {
		t.Fatalf("plan hash did not track configuration: %v", err)
	}
	input.Preconditions = next.DockerProjectDeploy
	if _, err := service.DeployProject(t.Context(), dockerTestPrincipal(), project.ID, input); !errors.Is(err, apperrors.ErrConflict) || len(repo.operations) != 1 {
		t.Fatalf("key rebound to new plan: %v", err)
	}
}

func TestReviewedDockerDeployRequiresAtomicPortAndKey(t *testing.T) {
	repo, service := reviewedDeployFixture(0)
	plan, err := service.PlanProjectDeploy(t.Context(), dockerTestPrincipal(), "project-1", domain.ProjectDeployInput{})
	if err != nil {
		t.Fatal(err)
	}
	input := domain.ProjectDeployInput{Action: "deploy", Preconditions: plan.DockerProjectDeploy}
	if _, err := service.DeployProject(t.Context(), dockerTestPrincipal(), "project-1", input); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("missing key: %v", err)
	}
	input.IdempotencyKey = "reviewed-original"
	withoutPort := New(repo.memoryDockerRepo, dockerTestPermissions(), nil)
	if _, err := withoutPort.DeployProject(t.Context(), dockerTestPrincipal(), "project-1", input); !errors.Is(err, apperrors.ErrUnsupportedOperation) || len(repo.operations) != 0 {
		t.Fatalf("non-atomic fallback: %v", err)
	}
	input.Preconditions = nil
	if _, err := withoutPort.DeployProject(t.Context(), dockerTestPrincipal(), "project-1", input); err != nil || len(repo.operations) != 1 {
		t.Fatalf("legacy caller compatibility: %v", err)
	}
}
