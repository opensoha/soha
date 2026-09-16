package manifest

import (
	"context"
	"errors"
	"testing"
	"time"

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
		"editor": {appaccess.PermDeliveryApplicationsView, appaccess.PermDeliveryApplicationsUpdate},
		"admin": {
			appaccess.PermDeliveryApplicationsView,
			appaccess.PermDeliveryApplicationsUpdate,
			appaccess.PermDeliveryApplicationsDelete,
			appaccess.PermDeliveryReleasesTrigger,
			appaccess.ManagedActionPermission(appaccess.PermDeliveryManifestDeploymentsManage, "preflight"),
			appaccess.ManagedActionPermission(appaccess.PermDeliveryManifestDeploymentsManage, "trigger"),
		},
	}, nil
}

type testIntentRoleReader struct{}

func (testIntentRoleReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{
		"ai-only": {appaccess.PermObserveAIChatUse},
	}, nil
}

type testDeclarativeRepository struct{ DeclarativeRepository }

type testRepository struct {
	item      domainmanifest.Package
	source    domainmanifest.Source
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
func (r *testRepository) GetSource(_ context.Context, _ string) (domainmanifest.Source, error) {
	return r.source, nil
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

type templateTestApplications struct{ testApplications }

func (templateTestApplications) GetService(_ context.Context, _ domainidentity.Principal, applicationID, serviceID string) (domainapp.Service, error) {
	return domainapp.Service{ID: serviceID, ApplicationID: applicationID, DeploymentTemplate: &domaincatalog.DeploymentTemplateBinding{TemplateID: "http", Version: 1, ManifestPackageID: "config"}}, nil
}

func (testApplications) List(context.Context, domainidentity.Principal, domainapp.Filter) ([]domainapp.App, error) {
	return []domainapp.App{{ID: "payments", Key: "payments", Group: "commerce", BusinessLineID: "finance"}}, nil
}

func (testApplications) Get(_ context.Context, _ domainidentity.Principal, id string) (domainapp.App, error) {
	if id != "payments" {
		return domainapp.App{}, apperrors.ErrNotFound
	}
	return domainapp.App{ID: id, Key: "payments", Group: "commerce", BusinessLineID: "finance"}, nil
}

func (testApplications) GetService(_ context.Context, _ domainidentity.Principal, applicationID, serviceID string) (domainapp.Service, error) {
	if applicationID != "payments" || serviceID != "payments-api" {
		return domainapp.Service{}, apperrors.ErrNotFound
	}
	return domainapp.Service{ID: serviceID, ApplicationID: applicationID}, nil
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

type testRevisionPromoter struct {
	calls    int
	revision int
	err      error
}

func (p *testRevisionPromoter) PromoteRevision(_ context.Context, _ domainidentity.Principal, _ domainmanifest.Package, revision int) error {
	p.calls++
	p.revision = revision
	return p.err
}

func TestTemplateConfigurationUpdateRequiresCurrentVersion(t *testing.T) {
	now := time.Now().UTC()
	existing := domainmanifest.Package{ID: "config", ApplicationID: "payments", ServiceID: "payments-api", UpdatedAt: now}
	repository := &testRepository{item: existing}
	service := newTestService(repository, testAuthorizer{})
	service.applications = templateTestApplications{}
	input := domainmanifest.Input{Name: "API configuration", ApplicationID: "payments", ServiceID: "payments-api", Renderer: domainmanifest.RendererRaw,
		Files: []domainmanifest.File{{Path: "config.yaml", Content: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: api\n"}},
	}
	if _, err := service.Update(context.Background(), testPrincipal(), existing.ID, input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("missing version: %v", err)
	}
	stale := now.Add(-time.Second)
	input.ExpectedUpdatedAt = &stale
	if _, err := service.Update(context.Background(), testPrincipal(), existing.ID, input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale version: %v", err)
	}
	input.ExpectedUpdatedAt = &now
	if _, err := service.Update(context.Background(), testPrincipal(), existing.ID, input); err != nil {
		t.Fatalf("current version: %v", err)
	}
}

func TestCreateAndPublishManifestPackage(t *testing.T) {
	repository := &testRepository{}
	service := newTestService(repository, testAuthorizer{})
	promoter := &testRevisionPromoter{}
	service.SetRevisionPromoter(promoter)
	created, err := service.Create(context.Background(), testPrincipal(), domainmanifest.Input{
		Name: "Payments ingress", ApplicationID: "payments", ServiceID: " payments-api ", Renderer: domainmanifest.RendererRaw,
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
	if created.Status != domainmanifest.StatusDraft || created.ID == "" || created.ServiceID != "payments-api" {
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
	if promoter.calls != 1 || promoter.revision != 1 {
		t.Fatalf("revision promoter = %#v, want one v1 call", promoter)
	}
}

func TestPublishRetryPromotesExistingRevisionWithoutCreatingDuplicate(t *testing.T) {
	repository := &testRepository{item: domainmanifest.Package{
		ID: "manifest-1", Name: "Payments", ApplicationID: "payments", Renderer: domainmanifest.RendererRaw,
		Status: domainmanifest.StatusDraft, Files: []domainmanifest.File{{Path: "deployment.yaml", Content: "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: payments\n"}},
	}}
	promoter := &testRevisionPromoter{err: errors.New("queue unavailable")}
	service := newTestService(repository, testAuthorizer{})
	service.SetRevisionPromoter(promoter)

	if _, err := service.Publish(context.Background(), testPrincipal(), repository.item.ID, "release"); err == nil {
		t.Fatal("Publish() error = nil, want promoter failure")
	}
	if len(repository.revisions) != 1 {
		t.Fatalf("revisions after failed promotion = %d, want 1", len(repository.revisions))
	}
	promoter.err = nil
	if _, err := service.Publish(context.Background(), testPrincipal(), repository.item.ID, "release"); err != nil {
		t.Fatalf("Publish() retry error = %v", err)
	}
	if len(repository.revisions) != 1 || promoter.calls != 2 || promoter.revision != 1 {
		t.Fatalf("retry state: revisions=%d promoter=%#v", len(repository.revisions), promoter)
	}
}

func TestCreateRejectsServiceOutsideApplication(t *testing.T) {
	service := newTestService(&testRepository{}, testAuthorizer{})
	_, err := service.Create(context.Background(), testPrincipal(), domainmanifest.Input{
		Name: "Wrong service", ApplicationID: "payments", ServiceID: "orders-api", Renderer: domainmanifest.RendererRaw,
	})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("Create() error = %v, want invalid argument", err)
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

func TestUpdateRejectsFileChangesForGitSynchronizedPackage(t *testing.T) {
	repository := &testRepository{
		item: domainmanifest.Package{
			ID: "manifest-1", Name: "Payments", ApplicationID: "payments", Renderer: domainmanifest.RendererRaw,
			Status: domainmanifest.StatusDraft,
			Files:  []domainmanifest.File{{Path: "deployment.yaml", Content: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: original\n"}},
		},
		source: domainmanifest.Source{Mode: domainmanifest.SourceModeGitSynced},
	}
	service := newTestService(repository, testAuthorizer{})
	_, err := service.Update(context.Background(), testPrincipal(), repository.item.ID, domainmanifest.Input{
		Name: "Payments", ApplicationID: "payments", Renderer: domainmanifest.RendererRaw,
		Files: []domainmanifest.File{{Path: "deployment.yaml", Content: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: changed\n"}},
	})
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("Update() error = %v, want conflict", err)
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
	_, err := service.List(context.Background(), testPrincipal(), domainmanifest.Filter{ServiceID: "svc-api", Page: 2, PageSize: 50})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(repository.filter.ApplicationIDs) != 1 || repository.filter.ApplicationIDs[0] != "payments" {
		t.Fatalf("List() application IDs = %#v, want scoped payments application", repository.filter.ApplicationIDs)
	}
	if repository.filter.Page != 2 || repository.filter.PageSize != 50 {
		t.Fatalf("List() pagination = %d/%d, want 2/50", repository.filter.Page, repository.filter.PageSize)
	}
	if repository.filter.ServiceID != "svc-api" {
		t.Fatalf("List() service ID = %q, want svc-api", repository.filter.ServiceID)
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

func TestNormalizeGitSourceCleansRepositoryRelativePath(t *testing.T) {
	input, err := normalizeSourceInput(domainmanifest.SourceInput{
		Mode: domainmanifest.SourceModeGitSynced, RepositoryID: " repo-1 ",
		RefType: domainmanifest.SourceRefBranch, RefValue: " main ", Path: "deploy/../manifests",
		IncludePatterns: []string{" *.yaml ", "*.yaml"}, SyncPolicy: domainmanifest.SyncPolicyManual,
		ExpectedGeneration: 2,
	})
	if err != nil {
		t.Fatalf("normalizeSourceInput() error = %v", err)
	}
	if input.RepositoryID != "repo-1" || input.RefValue != "main" || input.Path != "manifests" {
		t.Fatalf("normalizeSourceInput() = %#v, want normalized Git fields", input)
	}
	if len(input.IncludePatterns) != 1 || input.IncludePatterns[0] != "*.yaml" {
		t.Fatalf("include patterns = %#v, want normalized unique pattern", input.IncludePatterns)
	}
}

func TestNormalizeGitSourceAllowsRepositoryRoot(t *testing.T) {
	input, err := normalizeSourceInput(domainmanifest.SourceInput{
		Mode: domainmanifest.SourceModeGitSynced, RepositoryID: "repo-1",
		RefType: domainmanifest.SourceRefCommit, RefValue: "abc123", Path: ".",
		SyncPolicy: domainmanifest.SyncPolicyWebhook, ExpectedGeneration: 1,
	})
	if err != nil || input.Path != "." {
		t.Fatalf("normalizeSourceInput() = %#v, %v, want repository root", input, err)
	}
}

func TestRepairDriftWaitsForExplicitApproval(t *testing.T) {
	status := applyObservedDriftState(domainmanifest.DeploymentStatus{}, domainmanifest.DriftPolicyRepair, 3, "task-1", time.Now().UTC())
	if status.Phase != domainmanifest.DeploymentPhaseWaitingApproval {
		t.Fatalf("phase = %q, want waiting approval", status.Phase)
	}
	if len(status.Conditions) != 1 || status.Conditions[0].Reason != "RepairApprovalRequired" {
		t.Fatalf("conditions = %#v, want repair approval condition", status.Conditions)
	}
}

func TestReportDriftDoesNotRequestRepairApproval(t *testing.T) {
	status := applyObservedDriftState(domainmanifest.DeploymentStatus{}, domainmanifest.DriftPolicyReport, 3, "task-1", time.Now().UTC())
	if status.Phase != domainmanifest.DeploymentPhaseDrifted {
		t.Fatalf("phase = %q, want drifted", status.Phase)
	}
}

func TestInventoryHealthyRequiresAtLeastOneHealthyResource(t *testing.T) {
	if inventoryHealthy(nil) {
		t.Fatal("inventoryHealthy(nil) = true, want false")
	}
	if inventoryHealthy([]domainmanifest.ResourceInventory{{Health: "progressing"}}) {
		t.Fatal("progressing inventory reported healthy")
	}
	if !inventoryHealthy([]domainmanifest.ResourceInventory{{Health: "healthy"}, {Health: "healthy"}}) {
		t.Fatal("healthy inventory reported unhealthy")
	}
}

func TestPublicTaskErrorDoesNotExposeProviderDetails(t *testing.T) {
	secret := "token=super-secret kubeconfig=/private/config"
	if got := publicTaskError(map[string]any{"error": secret}); got != "manifest task failed" {
		t.Fatalf("publicTaskError() = %q, want generic public error", got)
	}
}

func TestDecideDeliveryIntentRequiresApplicationUpdatePermissionForEveryDecision(t *testing.T) {
	base := New(
		&testRepository{}, testApplications{}, testEnvironments{}, testClusters{}, testAuthorizer{},
		appaccess.NewPermissionResolver(testIntentRoleReader{}), nil, nil,
	)
	service := NewDeclarative(base, testDeclarativeRepository{})
	principal := domainidentity.Principal{UserID: "ai-user", Roles: []string{"ai-only"}}
	for _, decision := range []string{domainmanifest.IntentStatusAccepted, domainmanifest.IntentStatusRejected} {
		t.Run(decision, func(t *testing.T) {
			_, err := service.DecideDeliveryIntent(context.Background(), principal, "intent-1", decision, domainmanifest.DeliveryIntentDecisionInput{})
			if !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("DecideDeliveryIntent() error = %v, want application update access denied", err)
			}
		})
	}
}

func TestNormalizeGitSourceRejectsPathEscapeAfterCleaning(t *testing.T) {
	_, err := normalizeSourceInput(domainmanifest.SourceInput{
		Mode: domainmanifest.SourceModeGitSynced, RepositoryID: "repo-1",
		RefType: domainmanifest.SourceRefBranch, RefValue: "main", Path: "deploy/../../secret",
		SyncPolicy: domainmanifest.SyncPolicyManual, ExpectedGeneration: 1,
	})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("normalizeSourceInput() error = %v, want invalid argument", err)
	}
}

func TestNormalizeBindingRequiresExplicitPolicies(t *testing.T) {
	_, err := normalizeBindingInput(domainmanifest.BindingInput{
		ApplicationEnvironmentID: "payments-dev", ClusterID: "dev-1", Namespace: "payments",
	})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("normalizeBindingInput() error = %v, want invalid argument", err)
	}

	item, err := normalizeBindingInput(domainmanifest.BindingInput{
		ApplicationEnvironmentID: "payments-dev", ClusterID: "dev-1", Namespace: "payments",
		DriftPolicy: domainmanifest.DriftPolicyReport, DeletionPolicy: domainmanifest.DeletionPolicyOrphan,
	})
	if err != nil {
		t.Fatalf("normalizeBindingInput() error = %v", err)
	}
	if item.Overlay == nil {
		t.Fatal("normalizeBindingInput() overlay = nil, want empty object")
	}
}

func TestSaveConfigurationRevisionDoesNotExecuteAndRejectsStaleDraft(t *testing.T) {
	repository := &testRepository{}
	service := newTestService(repository, testAuthorizer{})
	promoter := &testRevisionPromoter{}
	service.SetRevisionPromoter(promoter)
	principal := domainidentity.Principal{UserID: "editor", Roles: []string{"editor"}}
	created, err := service.Create(context.Background(), principal, domainmanifest.Input{
		Name: "Service configuration", ApplicationID: "payments", Renderer: domainmanifest.RendererRaw,
		Files: []domainmanifest.File{{Path: "config.yaml", Content: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: api\n"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := service.SaveRevision(context.Background(), principal, created.ID, domainmanifest.RevisionInput{ExpectedUpdatedAt: created.UpdatedAt})
	if err != nil {
		t.Fatal(err)
	}
	if saved.CurrentRevision != 1 || saved.Status != domainmanifest.StatusPublished || len(repository.revisions) != 1 || promoter.calls != 0 {
		t.Fatalf("configuration save: revision=%d revisions=%d executions=%d", saved.CurrentRevision, len(repository.revisions), promoter.calls)
	}
	if _, err := service.SaveRevision(context.Background(), principal, created.ID, domainmanifest.RevisionInput{ExpectedUpdatedAt: saved.UpdatedAt}); err != nil {
		t.Fatal(err)
	}
	if len(repository.revisions) != 1 {
		t.Fatal("saved identical configuration twice")
	}
	if _, err := service.SaveRevision(context.Background(), principal, created.ID, domainmanifest.RevisionInput{ExpectedUpdatedAt: created.UpdatedAt}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale draft: %v", err)
	}
	if _, err := service.SaveRevision(context.Background(), principal, created.ID, domainmanifest.RevisionInput{}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("missing token: %v", err)
	}
	service.authorizer = testAuthorizer{deny: true}
	if _, err := service.SaveRevision(context.Background(), principal, created.ID, domainmanifest.RevisionInput{ExpectedUpdatedAt: saved.UpdatedAt}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("scope denied: %v", err)
	}
	if promoter.calls != 0 {
		t.Fatal("configuration save triggered execution")
	}
}
