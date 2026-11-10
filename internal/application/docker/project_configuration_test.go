package docker

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func projectConfigurationFixture() (*memoryDockerRepo, domaindocker.ProjectInput) {
	repo := newMemoryDockerRepo()
	repo.hosts["host-1"] = domaindocker.Host{ID: "host-1", Name: "fixture"}
	repo.projects["project-1"] = domaindocker.Project{ID: "project-1", HostID: "host-1", Name: "before", Status: "running", DesiredState: "running"}
	return repo, domaindocker.ProjectInput{HostID: "host-1", Name: "after", ComposeContent: "services:\n  api:\n    image: fixture:v2\n  worker:\n    image: fixture:worker\n"}
}

func TestProjectConfigurationPreservesObservedService(t *testing.T) {
	repo, input := projectConfigurationFixture()
	now := time.Now().UTC()
	observed := domaindocker.Service{ID: "project-1:api", ProjectID: "project-1", HostID: "host-1", Name: "api", Image: "fixture:v1", Status: "running", ContainerID: "fixture-container", RestartCount: 3, CPUPercent: 2.5, MemoryBytes: 8192, NetworkRxBytes: 42, NetworkTxBytes: 53, Config: map[string]any{"runtime": true}, LastSeenAt: &now, CreatedAt: now, UpdatedAt: now}
	repo.services[observed.ID] = observed
	s := New(repo, dockerTestPermissions(), nil)
	project, err := s.UpdateProject(t.Context(), dockerTestPrincipal(), "project-1", input)
	if err != nil {
		t.Fatal(err)
	}
	if project.Name != input.Name || project.Status != "running" || project.DesiredState != "running" {
		t.Fatalf("configuration or runtime state changed incorrectly: %#v", project)
	}
	if got := repo.services[observed.ID]; !reflect.DeepEqual(got, observed) {
		t.Fatalf("configuration save replaced observed service: %#v", got)
	}
	worker, ok := repo.services["project-1:worker"]
	if !ok || worker.Status != "defined" || worker.ContainerID != "" || worker.LastSeenAt != nil {
		t.Fatalf("new definition pretended to be observed: %#v", worker)
	}
}

type failingProjectConfigurationRepo struct {
	*memoryDockerRepo
	calls int
	err   error
}

func (r *failingProjectConfigurationRepo) CreateProjectConfiguration(context.Context, domaindocker.ProjectInput, []string) (domaindocker.Project, error) {
	r.calls++
	return domaindocker.Project{}, r.err
}

func (r *failingProjectConfigurationRepo) UpdateProjectConfiguration(context.Context, string, domaindocker.ProjectInput, []string) (domaindocker.Project, error) {
	r.calls++
	return domaindocker.Project{}, r.err
}

func TestProjectConfigurationUsesAtomicWriter(t *testing.T) {
	for _, create := range []bool{true, false} {
		t.Run(map[bool]string{true: "create", false: "update"}[create], func(t *testing.T) {
			base, input := projectConfigurationFixture()
			failure := errors.New("fixture configuration rollback")
			repo := &failingProjectConfigurationRepo{memoryDockerRepo: base, err: failure}
			s := New(repo, dockerTestPermissions(), nil)
			var err error
			if create {
				_, err = s.CreateProject(t.Context(), dockerTestPrincipal(), input)
			} else {
				_, err = s.UpdateProject(t.Context(), dockerTestPrincipal(), "project-1", input)
			}
			if !errors.Is(err, failure) || repo.calls != 1 || len(base.projects) != 1 || len(base.services) != 0 || base.projects["project-1"].Name != "before" {
				t.Fatalf("atomic writer was bypassed: err=%v calls=%d projects=%d services=%d", err, repo.calls, len(base.projects), len(base.services))
			}
		})
	}
}

type legacyProjectConfigurationRepo struct{ Repository }

func TestProjectConfigurationRequiresAtomicWriter(t *testing.T) {
	base, input := projectConfigurationFixture()
	s := New(legacyProjectConfigurationRepo{base}, dockerTestPermissions(), nil)
	if _, err := s.CreateProject(t.Context(), dockerTestPrincipal(), input); !errors.Is(err, apperrors.ErrUnsupportedOperation) {
		t.Fatalf("missing atomic create writer must fail closed: %v", err)
	}
	if _, err := s.UpdateProject(t.Context(), dockerTestPrincipal(), "project-1", input); !errors.Is(err, apperrors.ErrUnsupportedOperation) {
		t.Fatalf("missing atomic update writer must fail closed: %v", err)
	}
	if len(base.projects) != 1 || len(base.services) != 0 || base.projects["project-1"].Name != "before" {
		t.Fatal("missing writer performed a partial write")
	}
}

func TestProjectConfigurationPermissionCeiling(t *testing.T) {
	base, input := projectConfigurationFixture()
	repo := &failingProjectConfigurationRepo{memoryDockerRepo: base}
	principal := dockerTestPrincipal()
	principal.PermissionKeys = []string{"docker.projects.view"}
	for _, resolver := range []bool{true, false} {
		s := New(repo, nil, nil)
		if resolver {
			s = New(repo, dockerTestPermissions(), nil)
		}
		if _, err := s.CreateProject(t.Context(), principal, input); !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("create authorization changed: %v", err)
		}
		if _, err := s.UpdateProject(t.Context(), principal, "project-1", input); !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("update authorization changed: %v", err)
		}
	}
	if repo.calls != 0 {
		t.Fatal("denied configuration reached the writer")
	}
}

func (r *memoryDockerRepo) CreateProjectConfiguration(ctx context.Context, input domaindocker.ProjectInput, names []string) (domaindocker.Project, error) {
	project, err := r.CreateProject(ctx, input)
	if err == nil {
		r.saveProjectDefinitions(project, names)
	}
	return project, err
}

func (r *memoryDockerRepo) UpdateProjectConfiguration(ctx context.Context, id string, input domaindocker.ProjectInput, names []string) (domaindocker.Project, error) {
	project, err := r.UpdateProject(ctx, id, input)
	if err == nil {
		r.saveProjectDefinitions(project, names)
	}
	return project, err
}

func (r *memoryDockerRepo) saveProjectDefinitions(project domaindocker.Project, names []string) {
	for _, name := range names {
		exists := false
		for _, item := range r.services {
			if item.ProjectID == project.ID && item.Name == name {
				exists = true
				break
			}
		}
		if !exists {
			id := project.ID + ":" + name
			r.services[id] = domaindocker.Service{ID: id, ProjectID: project.ID, HostID: project.HostID, Name: name, Status: "defined"}
		}
	}
}
