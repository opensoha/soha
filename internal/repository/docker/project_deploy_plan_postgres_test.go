package docker_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appdocker "github.com/opensoha/soha/internal/application/docker"
	domain "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	dockerrepo "github.com/opensoha/soha/internal/repository/docker"
)

type reviewedDeployRoles struct{}

func (reviewedDeployRoles) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"reviewed": {appaccess.PermDockerProjectsView, appaccess.PermDockerTemplatesView, appaccess.PermDockerProjectsDeploy, appaccess.PermDockerOperationsView}}, nil
}

func reviewedDeployWithPostgres(t *testing.T, store *dbstore.Store) {
	for _, version := range []int{0, 1} {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			repo := dockerrepo.New(store.DB())
			service := appdocker.New(repo, appaccess.NewPermissionResolver(reviewedDeployRoles{}), nil)
			actor := domainidentity.Principal{UserID: "reviewed-deploy-fixture", Roles: []string{"reviewed"}}
			host, err := repo.CreateHost(t.Context(), domain.HostInput{ID: uuid.NewString(), Name: "reviewed-owned"})
			if err != nil {
				t.Fatal(err)
			}
			project, err := repo.CreateProject(t.Context(), domain.ProjectInput{ID: uuid.NewString(), HostID: host.ID, Name: "reviewed-owned", Slug: "reviewed-owned", RuntimeIdentityVersion: version, ComposeContent: "services:\n  web:\n    image: alpine:3.21\n"})
			if err != nil {
				t.Fatal(err)
			}
			preview, err := service.PlanProjectDeploy(t.Context(), actor, project.ID, domain.ProjectDeployInput{Action: "deploy"})
			if err != nil {
				t.Fatal(err)
			}
			input := domain.ProjectDeployInput{Action: "deploy", IdempotencyKey: uuid.NewString(), Preconditions: preview.DockerProjectDeploy}
			assertReviewedDeployLocks(t, store, repo, project, *input.Preconditions)
			failure := errors.New("owned fixture rollback")
			var rolledBack domain.Operation
			err = repo.WithinProjectDeployPlan(t.Context(), project.ID, *input.Preconditions, func(txCtx context.Context) error {
				var err error
				rolledBack, err = service.DeployProject(txCtx, actor, project.ID, input)
				if err != nil {
					return err
				}
				return failure
			})
			if !errors.Is(err, failure) || rolledBack.ID == "" {
				t.Fatalf("rollback setup: %v", err)
			}
			if _, err := repo.GetOperation(t.Context(), rolledBack.ID); !errors.Is(err, apperrors.ErrNotFound) {
				t.Fatalf("operation escaped transaction rollback: %v", err)
			}
			first := concurrentReviewedDeploy(t, service, actor, project.ID, input)
			if first.Payload["composeContent"] != project.ComposeContent {
				t.Fatal("queued payload did not use the reviewed definition")
			}
			if err := store.DB().Exec("UPDATE docker_projects SET compose_content=? WHERE id=?", "services: {changed: {image: alpine:3.21}}", project.ID).Error; err != nil {
				t.Fatal(err)
			}
			recovered, err := service.GetProjectDeploymentRequest(t.Context(), actor, project.ID, input.IdempotencyKey, input.Action)
			if err != nil || recovered.ID != first.ID || recovered.Payload != nil || recovered.Result != nil {
				t.Fatalf("read-only original receipt: %v", err)
			}
			input.IdempotencyKey = uuid.NewString()
			if _, err := service.DeployProject(t.Context(), actor, project.ID, input); !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("stale plan executed: %v", err)
			}
			var count int64
			if err := store.DB().Table("docker_operations").Where("project_id = ?", project.ID).Count(&count).Error; err != nil || count != 1 {
				t.Fatalf("expected exactly one operation: count=%d error=%v", count, err)
			}
			assertReviewedDeployAllowsLegacyDefinitionWriter(t, store, service, actor, project)
		})
	}
}

// Legacy PUT saves lock the project before inserting service definitions. The
// service FK takes KEY SHARE on its host, so a deployment's host lock must not
// invert that implicit lock order while it waits for the project.
func assertReviewedDeployAllowsLegacyDefinitionWriter(t *testing.T, store *dbstore.Store, service *appdocker.Service, actor domainidentity.Principal, project domain.Project) {
	t.Helper()
	preview, err := service.PlanProjectDeploy(t.Context(), actor, project.ID, domain.ProjectDeployInput{Action: "deploy"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	key := uuid.NewString()
	result := make(chan error, 1)
	err = dbtx.Within(ctx, store.DB(), func(legacyCtx context.Context) error {
		db := dbtx.DB(legacyCtx, store.DB())
		if err := db.Exec("SELECT id FROM docker_projects WHERE id=? FOR UPDATE", project.ID).Error; err != nil {
			return err
		}
		go func() {
			_, err := service.DeployProject(ctx, actor, project.ID, domain.ProjectDeployInput{Action: "deploy", IdempotencyKey: key, Preconditions: preview.DockerProjectDeploy})
			result <- err
		}()
		if err := waitForReviewedDeployHostLock(ctx, store, project.HostID); err != nil {
			return err
		}
		if err := db.Exec("UPDATE docker_projects SET compose_content=compose_content || E'\\n# legacy definition writer\\n' WHERE id=?", project.ID).Error; err != nil {
			return err
		}
		return db.Exec("INSERT INTO docker_services(id,project_id,host_id,name,status) VALUES(?,?,?,?,'defined')", uuid.NewString(), project.ID, project.HostID, "legacy-new-definition").Error
	})
	select {
	case deployErr := <-result:
		if err != nil || !errors.Is(deployErr, apperrors.ErrConflict) {
			t.Fatalf("legacy writer must commit and stale deployment must conflict: writer=%v deployment=%v", err, deployErr)
		}
	case <-ctx.Done():
		t.Fatalf("legacy writer/deployment did not finish: writer=%v", err)
	}
	if _, err := service.GetProjectDeploymentRequest(t.Context(), actor, project.ID, key, "deploy"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("stale deployment left an operation: %v", err)
	}
}

func waitForReviewedDeployHostLock(ctx context.Context, store *dbstore.Store, hostID string) error {
	for {
		err := dbtx.Within(ctx, store.DB(), func(probe context.Context) error {
			return dbtx.DB(probe, store.DB()).Exec("SELECT id FROM docker_hosts WHERE id=? FOR UPDATE NOWAIT", hostID).Error
		})
		if err != nil {
			if strings.Contains(err.Error(), "55P03") {
				return nil
			}
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func assertReviewedDeployLocks(t *testing.T, store *dbstore.Store, repo *dockerrepo.Repository, project domain.Project, expected domain.ProjectDeployPreconditions) {
	t.Helper()
	err := repo.WithinProjectDeployPlan(t.Context(), project.ID, expected, func(context.Context) error {
		for _, row := range []struct{ table, id string }{{"docker_hosts", project.HostID}, {"docker_projects", project.ID}} {
			err := dbtx.Within(t.Context(), store.DB(), func(ctx context.Context) error {
				db := dbtx.DB(ctx, store.DB())
				if err := db.Exec("SET LOCAL lock_timeout = '100ms'").Error; err != nil {
					return err
				}
				return db.Exec("UPDATE "+row.table+" SET name=name WHERE id=?", row.id).Error
			})
			if err == nil || !strings.Contains(err.Error(), "55P03") {
				return fmt.Errorf("%s configuration lock not retained: %v", row.table, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func concurrentReviewedDeploy(t *testing.T, service *appdocker.Service, actor domainidentity.Principal, id string, input domain.ProjectDeployInput) domain.Operation {
	t.Helper()
	var wg sync.WaitGroup
	results := make(chan domain.Operation, 2)
	failures := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			item, err := service.DeployProject(t.Context(), actor, id, input)
			results <- item
			failures <- err
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first domain.Operation
	for item := range results {
		if first.ID == "" {
			first = item
		}
		if item.ID != first.ID {
			t.Fatal("concurrent original request created different operations")
		}
	}
	return first
}
