package catalog

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appservice "github.com/opensoha/soha/internal/application/app"
	catalogservice "github.com/opensoha/soha/internal/application/catalog"
	manifestservice "github.com/opensoha/soha/internal/application/manifest"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/infrastructure/manifestruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
	apprepo "github.com/opensoha/soha/internal/repository/application"
	manifestrepo "github.com/opensoha/soha/internal/repository/manifest"
)

type deploymentSetupAccess struct{}

func (deploymentSetupAccess) Authorize(context.Context, domainaccess.Request) (domainaccess.Decision, error) {
	return domainaccess.Decision{Allowed: true}, nil
}

func (deploymentSetupAccess) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"template-test": {
		appaccess.PermDeliveryApplicationsView, appaccess.PermDeliveryApplicationsUpdate,
		appaccess.PermDeliveryApplicationServicesManage, appaccess.PermDeliveryApplicationServicesView, appaccess.PermDeliveryApplicationEnvView,
		appaccess.PermDeliveryDeploymentTemplatesView, appaccess.PermDeliveryDeploymentTemplatesCreate,
		appaccess.PermDeliveryDeploymentTemplatesUpdate,
	}}, nil
}

func (deploymentSetupAccess) GetConnection(_ context.Context, id string) (domaincluster.Connection, error) {
	return domaincluster.Connection{Summary: domaincluster.Summary{ID: id}}, nil
}

// Production application services and repositories use real PostgreSQL here;
// only access decisions and cluster connection lookup are supplied.
func verifyServiceDeploymentConfiguration(t *testing.T, ctx context.Context, repo *Repository) {
	t.Helper()
	access := deploymentSetupAccess{}
	permissions := appaccess.NewPermissionResolver(access)
	principal := domainidentity.Principal{UserID: "template-test", Roles: []string{"template-test"}}
	applications, packages := apprepo.New(repo.db), manifestrepo.New(repo.db)
	apps := appservice.New(applications, nil, access, nil, nil)
	apps.SetPermissionResolver(permissions)
	catalog := catalogservice.New(repo, access, applications, permissions, nil, nil)
	catalog.SetDeploymentTemplates(repo, manifestruntime.NewRenderer())
	apps.SetDeploymentTemplateReaders(catalog, packages)
	manifests := manifestservice.New(packages, apps, catalog, access, access, permissions, nil, nil)
	manifestservice.NewDeclarative(manifests, packages).SetDeploymentTemplateReader(catalog)
	appID, clusterID := uuid.NewString(), uuid.NewString()
	_, err := applications.Create(ctx, domainapp.UpsertInput{ID: appID, Key: appID, Name: "Template setup", Enabled: true})
	requireTemplateNoError(t, err)
	requireTemplateNoError(t, repo.db.Exec(`INSERT INTO clusters (id, name) VALUES (?, 'Template setup')`, clusterID).Error)
	t.Cleanup(func() {
		_ = repo.db.Exec(`DELETE FROM applications WHERE id = ?`, appID).Error
		_ = repo.db.Exec(`DELETE FROM clusters WHERE id = ?`, clusterID).Error
	})
	environment, err := repo.CreateApplicationEnvironment(ctx, domaincatalog.ApplicationEnvironmentInput{ApplicationID: appID, EnvironmentID: "dev", ClusterID: clusterID, Namespace: "template-setup"})
	requireTemplateNoError(t, err)
	environment = verifyEnvironmentTargetCAS(t, ctx, repo, environment)
	templateInput := domaincatalog.DeploymentTemplateInput{DeploymentTemplateSpec: domaincatalog.BuiltinDeploymentTemplates()[0]}
	templateInput.Key = uuid.NewString()
	draft, err := catalog.SaveDeploymentTemplate(ctx, principal, "", templateInput)
	requireTemplateNoError(t, err)
	template, err := catalog.PublishDeploymentTemplate(ctx, principal, draft.ID, draft.Revision)
	requireTemplateNoError(t, err)
	serviceInput := domainapp.ServiceInput{Key: "api", Name: "API", Enabled: true, ServiceKind: domainapp.ServiceKindKubernetesWorkload,
		Containers:         []domainapp.ServiceContainerInput{{Name: "main", ImageRepository: "registry.invalid/api"}},
		DeploymentTemplate: &domaincatalog.DeploymentTemplateBinding{TemplateID: template.ID, Version: 1, Parameters: map[string]any{"port": 8081}},
	}
	service, err := apps.CreateService(ctx, principal, appID, serviceInput)
	requireTemplateNoError(t, err)
	preview, err := catalog.PreviewDeploymentTemplate(ctx, principal, appID, domaincatalog.DeploymentTemplatePreviewInput{
		TemplateID: template.ID, Version: 1, ServiceKey: service.Key, ApplicationEnvironmentID: environment.ID,
		Parameters: service.DeploymentTemplate.Parameters, Overrides: map[string]any{"replicas": 2},
	})
	requireTemplateNoError(t, err)
	if !preview.ConfigurationOnly || len(preview.Source.Files) != 1 || !strings.Contains(preview.Source.Files[0].Content, "namespace: template-setup") || !strings.Contains(preview.Source.Files[0].Content, "replicas: 2") {
		t.Fatalf("preview lost environment or typed parameters: %+v", preview)
	}
	input := domainmanifest.Input{Name: "API deployment", ApplicationID: appID, ServiceID: service.ID, Renderer: template.Source.Renderer, Files: template.Source.Files,
		Bindings: []domainmanifest.Binding{{ApplicationEnvironmentID: environment.ID, ClusterID: clusterID, Namespace: environment.Namespace, TemplateParameters: map[string]any{"replicas": 2}}},
	}
	item, err := manifests.Create(ctx, principal, input)
	requireTemplateNoError(t, err)
	serviceInput.ExpectedVersion, serviceInput.DeploymentTemplate.ManifestPackageID = &service.Version, item.ID
	linked, err := apps.UpdateService(ctx, principal, appID, service.ID, serviceInput)
	requireTemplateNoError(t, err)
	if linked.DeploymentTemplate.ManifestPackageID != item.ID || linked.DeploymentTemplate.Version != 1 {
		t.Fatalf("service did not retain saved configuration: %+v", linked)
	}
	read, err := manifests.Get(ctx, principal, item.ID)
	requireTemplateNoError(t, err)
	if len(read.Bindings) != 1 || read.Bindings[0].TemplateParameters["replicas"] != float64(2) || strings.Contains(read.Files[0].Content, "preview.invalid") || read.Files[0] != template.Source.Files[0] {
		t.Fatalf("template draft or atomic environment parameters changed: %+v", read)
	}
	input.Bindings = read.Bindings
	if _, err := manifests.Update(ctx, principal, item.ID, input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("template draft accepted unversioned update: %v", err)
	}
	input.ExpectedUpdatedAt, input.Bindings[0].TemplateParameters = &read.UpdatedAt, map[string]any{"port": 9000}
	if _, err := manifests.Update(ctx, principal, item.ID, input); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("non-overridable parameter accepted: %v", err)
	}
	input.Bindings[0].TemplateParameters = map[string]any{"replicas": 3}
	results := make(chan error, 2)
	for range 2 {
		go func() { _, err := manifests.Update(ctx, principal, item.ID, input); results <- err }()
	}
	left, right := <-results, <-results
	requireTemplateCASConflict(t, left, right)
	verifyDeploymentTemplateUpgrade(t, ctx, apps, catalog, manifests, packages, principal, template, templateInput, serviceInput, linked, input)
}

func verifyEnvironmentTargetCAS(t *testing.T, ctx context.Context, repo *Repository, environment domaincatalog.ApplicationEnvironment) domaincatalog.ApplicationEnvironment {
	t.Helper()
	// Both writers start from the same environment revision. Only one may replace its target set.
	cas := domaincatalog.ApplicationEnvironmentInput{ApplicationID: environment.ApplicationID, EnvironmentID: "dev", ClusterID: environment.ClusterID, Namespace: environment.Namespace, ExpectedUpdatedAt: &environment.UpdatedAt}
	casResults := make(chan error, 2)
	for _, name := range []string{"first", "second"} {
		go func() {
			input := cas
			input.Targets = []domaincatalog.ReleaseTargetInput{{ClusterID: environment.ClusterID, Namespace: environment.Namespace, WorkloadKind: "Deployment", WorkloadName: name, Enabled: true}}
			_, err := repo.UpdateApplicationEnvironment(ctx, environment.ID, input)
			casResults <- err
		}()
	}
	first, second := <-casResults, <-casResults
	requireTemplateCASConflict(t, first, second)
	environment, err := repo.GetApplicationEnvironment(ctx, environment.ID)
	requireTemplateNoError(t, err)
	if len(environment.Targets) != 1 {
		t.Fatal("environment CAS lost targets")
	}
	return environment
}

func verifyDeploymentTemplateUpgrade(t *testing.T, ctx context.Context, apps *appservice.Service, catalog *catalogservice.Service, manifests *manifestservice.Service, packages *manifestrepo.Repository, principal domainidentity.Principal, template domaincatalog.DeploymentTemplate, templateInput domaincatalog.DeploymentTemplateInput, serviceInput domainapp.ServiceInput, linked domainapp.Service, input domainmanifest.Input) {
	t.Helper()
	appID, serviceID, manifestID := linked.ApplicationID, linked.ID, linked.DeploymentTemplate.ManifestPackageID
	templateInput.ExpectedRevision, templateInput.Defaults["port"] = &template.Revision, 9090
	draft, err := catalog.SaveDeploymentTemplate(ctx, principal, template.ID, templateInput)
	requireTemplateNoError(t, err)
	_, err = catalog.PublishDeploymentTemplate(ctx, principal, draft.ID, draft.Revision)
	requireTemplateNoError(t, err)
	linked, err = apps.GetService(ctx, principal, appID, serviceID)
	requireTemplateNoError(t, err)
	if linked.DeploymentTemplate.Version != 1 || linked.DeploymentTemplate.Parameters["port"] != float64(8081) {
		t.Fatalf("published template changed an existing service: %+v", linked)
	}
	serviceInput.ExpectedVersion = &linked.Version
	serviceInput.DeploymentTemplate.Detached = true
	serviceInput.DeploymentTemplate.DetachedTemplate = &domaincatalog.DeploymentTemplate{ID: "forged"}
	linked, err = apps.UpdateService(ctx, principal, appID, serviceID, serviceInput)
	requireTemplateNoError(t, err)
	if linked.DeploymentTemplate.DetachedTemplate == nil || linked.DeploymentTemplate.DetachedTemplate.ContentDigest != template.ContentDigest {
		t.Fatal("independent configuration did not copy the selected trusted version")
	}
	// The catalog is unavailable after detaching. Both service parameters and
	// environment overrides must still use the saved schema, not caller data.
	apps.SetDeploymentTemplateReaders(nil, packages)
	manifestservice.NewDeclarative(manifests, packages).SetDeploymentTemplateReader(nil)
	serviceInput.ExpectedVersion = &linked.Version
	serviceInput.DeploymentTemplate.Parameters["replicas"] = 2
	linked, err = apps.UpdateService(ctx, principal, appID, serviceID, serviceInput)
	requireTemplateNoError(t, err)
	if linked.DeploymentTemplate.DetachedTemplate.ContentDigest != template.ContentDigest {
		t.Fatal("caller replaced the independent template snapshot")
	}
	read, err := manifests.Get(ctx, principal, manifestID)
	requireTemplateNoError(t, err)
	input.ExpectedUpdatedAt = &read.UpdatedAt
	input.Bindings[0].TemplateParameters = map[string]any{"replicas": 4}
	_, err = manifests.Update(ctx, principal, manifestID, input)
	requireTemplateNoError(t, err)
	serviceInput.ExpectedVersion = &linked.Version
	serviceInput.DeploymentTemplate.Parameters["unknown"] = true
	if _, err := apps.UpdateService(ctx, principal, appID, serviceID, serviceInput); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("independent configuration bypassed its copied schema: %v", err)
	}
}
