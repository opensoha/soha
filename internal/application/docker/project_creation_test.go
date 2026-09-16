package docker

import (
	"context"
	"errors"
	"testing"

	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestProjectCreationReceiptRestoresConfigurationWithoutExecution(t *testing.T) {
	repo := newMemoryDockerRepo()
	repo.hosts["host"] = domaindocker.Host{ID: "host"}
	service := New(repo, dockerTestPermissions(), nil)
	principal := dockerTestPrincipal()
	input := domaindocker.ProjectInput{IdempotencyKey: "project-intent-1", HostID: "host", Name: "api", ComposeContent: "services:\n  api:\n    image: image:tag\n", EnvContent: "TOKEN=private"}
	first, err := service.CreateProject(context.Background(), principal, input)
	if err != nil || first.ID == "" || first.EnvContent != "" || first.ComposeContent != "" {
		t.Fatalf("receipt: %+v %v", first, err)
	}
	stored := repo.projects[first.ID]
	stored.Name = "changed"
	repo.projects[first.ID] = stored
	recreated := New(repo, dockerTestPermissions(), nil)
	again, err := recreated.CreateProject(context.Background(), principal, input)
	if err != nil || again.Name != first.Name || again.ID != first.ID || len(repo.projects) != 1 || len(repo.operations) != 0 || len(repo.services) != 1 {
		t.Fatalf("duplicate or altered receipt: %+v %v", again, err)
	}
	input.Name = "conflicting"
	if _, err := recreated.CreateProject(context.Background(), principal, input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("conflict: %v", err)
	}
	input.Name = "api"
	principal.UserID = "another-actor"
	if _, err := recreated.FindProjectCreation(context.Background(), principal, input); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("cross actor recovery: %v", err)
	}
	principal.Roles = nil
	if _, err := recreated.FindProjectCreation(context.Background(), principal, input); err == nil {
		t.Fatal("revoked creation receipt exposed")
	}
	input.SourceKind, input.SourceRef = "url", "https://example.test/compose.yaml"
	if _, err := projectCreationClaim(dockerTestPrincipal(), input); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("remote mutable input accepted: %v", err)
	}
}
