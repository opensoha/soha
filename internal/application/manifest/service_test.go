package manifest

import (
	"context"
	"errors"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type testRoleReader struct{}

func (testRoleReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{
		"admin": {
			appaccess.PermDeliveryApplicationsView,
			appaccess.PermDeliveryApplicationsUpdate,
			appaccess.PermDeliveryApplicationsDelete,
			appaccess.PermDeliveryReleasesTrigger,
		},
	}, nil
}

type testRepository struct {
	item      domainmanifest.Package
	revision  domainmanifest.Revision
	revisions []domainmanifest.Revision
	filter    domainmanifest.Filter
}

func (r *testRepository) List(_ context.Context, filter domainmanifest.Filter) (domainmanifest.Page, error) {
	r.filter = filter
	return domainmanifest.Page{Items: []domainmanifest.Package{r.item}, Total: 1, Page: 1, PageSize: 20}, nil
}
func (r *testRepository) Get(_ context.Context, _ string) (domainmanifest.Package, error) {
	return r.item, nil
}
func (r *testRepository) Create(_ context.Context, item domainmanifest.Package) (domainmanifest.Package, error) {
	r.item = item
	return item, nil
}
func (r *testRepository) Update(_ context.Context, _ string, item domainmanifest.Package) (domainmanifest.Package, error) {
	r.item = item
	return item, nil
}
func (r *testRepository) Delete(context.Context, string) error { return nil }
func (r *testRepository) Publish(_ context.Context, item domainmanifest.Package, revision domainmanifest.Revision) (domainmanifest.Package, error) {
	r.item = item
	r.revision = revision
	r.revisions = append(r.revisions, revision)
	return item, nil
}
func (r *testRepository) ListRevisions(context.Context, string) ([]domainmanifest.Revision, error) {
	return r.revisions, nil
}

func testPrincipal() domainidentity.Principal {
	return domainidentity.Principal{UserID: "admin", UserName: "Admin", Roles: []string{"admin"}}
}

type testApplications struct{}

func (testApplications) List(context.Context, domainidentity.Principal, domainapp.Filter) ([]domainapp.App, error) {
	return []domainapp.App{{ID: "payments", Key: "payments", Group: "commerce", BusinessLineID: "finance"}}, nil
}

func (testApplications) Get(_ context.Context, _ domainidentity.Principal, id string) (domainapp.App, error) {
	if id != "payments" {
		return domainapp.App{}, apperrors.ErrNotFound
	}
	return domainapp.App{ID: id, Key: "payments", Group: "commerce", BusinessLineID: "finance"}, nil
}

type testEnvironments struct{}

func (testEnvironments) GetApplicationEnvironment(_ context.Context, _ domainidentity.Principal, id string) (domaincatalog.ApplicationEnvironment, error) {
	if id != "payments-dev" {
		return domaincatalog.ApplicationEnvironment{}, apperrors.ErrNotFound
	}
	return domaincatalog.ApplicationEnvironment{ID: id, ApplicationID: "payments", EnvironmentID: "dev", EnvironmentKey: "dev"}, nil
}

type testClusters struct{}

func (testClusters) GetConnection(_ context.Context, id string) (domaincluster.Connection, error) {
	if id != "dev-1" {
		return domaincluster.Connection{}, apperrors.ErrNotFound
	}
	return domaincluster.Connection{Summary: domaincluster.Summary{ID: id, Environment: "dev"}}, nil
}

type testAuthorizer struct{ deny bool }

func (a testAuthorizer) Authorize(context.Context, domainaccess.Request) (domainaccess.Decision, error) {
	if a.deny {
		return domainaccess.Decision{Reason: "scope denied"}, nil
	}
	return domainaccess.Decision{Allowed: true}, nil
}

func newTestService(repository domainmanifest.Repository, authorizer domainaccess.Authorizer) *Service {
	return New(repository, testApplications{}, testEnvironments{}, testClusters{}, authorizer, appaccess.NewPermissionResolver(testRoleReader{}), nil, nil)
}

func TestCreateAndPublishManifestPackage(t *testing.T) {
	repository := &testRepository{}
	service := newTestService(repository, testAuthorizer{})
	created, err := service.Create(context.Background(), testPrincipal(), domainmanifest.Input{
		Name: "Payments ingress", ApplicationID: "payments", Renderer: domainmanifest.RendererRaw,
		Files: []domainmanifest.File{{Path: "base/ingress.yaml", Content: `apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: payments
`}},
		Bindings: []domainmanifest.Binding{{ApplicationEnvironmentID: "payments-dev", EnvironmentKey: "dev", ClusterID: "dev-1", Namespace: "payments"}},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Status != domainmanifest.StatusDraft || created.ID == "" {
		t.Fatalf("Create() = %#v, want identified draft", created)
	}

	published, err := service.Publish(context.Background(), testPrincipal(), created.ID, "initial release")
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if published.Status != domainmanifest.StatusPublished || published.CurrentRevision != 1 {
		t.Fatalf("Publish() = %#v, want published v1", published)
	}
	if repository.revision.Digest == "" || repository.revision.Note != "initial release" {
		t.Fatalf("revision = %#v, want immutable digest and note", repository.revision)
	}
}

func TestCreateRejectsEscapingFilePath(t *testing.T) {
	service := newTestService(&testRepository{}, testAuthorizer{})
	_, err := service.Create(context.Background(), testPrincipal(), domainmanifest.Input{
		Name: "Invalid", ApplicationID: "payments", Renderer: domainmanifest.RendererRaw,
		Files: []domainmanifest.File{{Path: "../secret.yaml", Content: "kind: Secret"}},
	})
	if err == nil {
		t.Fatal("Create() error = nil, want invalid path error")
	}
}

func TestCreateRejectsApplicationScopeDenial(t *testing.T) {
	service := newTestService(&testRepository{}, testAuthorizer{deny: true})
	_, err := service.Create(context.Background(), testPrincipal(), domainmanifest.Input{
		Name: "Denied", ApplicationID: "payments", Renderer: domainmanifest.RendererRaw,
	})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("Create() error = %v, want access denied", err)
	}
}

func TestListConstrainsRepositoryToAuthorizedApplicationsAndPage(t *testing.T) {
	repository := &testRepository{}
	service := newTestService(repository, testAuthorizer{})
	_, err := service.List(context.Background(), testPrincipal(), domainmanifest.Filter{Page: 2, PageSize: 50})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(repository.filter.ApplicationIDs) != 1 || repository.filter.ApplicationIDs[0] != "payments" {
		t.Fatalf("List() application IDs = %#v, want scoped payments application", repository.filter.ApplicationIDs)
	}
	if repository.filter.Page != 2 || repository.filter.PageSize != 50 {
		t.Fatalf("List() pagination = %d/%d, want 2/50", repository.filter.Page, repository.filter.PageSize)
	}
}

func TestPublishRejectsInvalidYAML(t *testing.T) {
	repository := &testRepository{item: domainmanifest.Package{
		ID: "manifest-1", Name: "Invalid", ApplicationID: "payments", Renderer: domainmanifest.RendererRaw,
		Status: domainmanifest.StatusDraft, Files: []domainmanifest.File{{Path: "bad.yaml", Content: "apiVersion: ["}},
	}}
	service := newTestService(repository, testAuthorizer{})
	_, err := service.Publish(context.Background(), testPrincipal(), repository.item.ID, "invalid")
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("Publish() error = %v, want invalid argument", err)
	}
}
