package catalog

import (
	"context"
	"errors"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainscopegrant "github.com/opensoha/soha/internal/domain/scopegrant"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/policy"
	apprepo "github.com/opensoha/soha/internal/repository/application"
)

type stubCatalogRepository struct {
	lastWorkflowTemplate    domaincatalog.WorkflowTemplateInput
	lastBuildTemplate       domaincatalog.BuildTemplateInput
	applicationEnvironments map[string]domaincatalog.ApplicationEnvironment
}

func (s *stubCatalogRepository) ListEnvironments(context.Context) ([]domaincatalog.Environment, error) {
	return nil, nil
}

func (s *stubCatalogRepository) ListApplicationEnvironments(context.Context) ([]domaincatalog.ApplicationEnvironment, error) {
	if len(s.applicationEnvironments) == 0 {
		return nil, nil
	}
	items := make([]domaincatalog.ApplicationEnvironment, 0, len(s.applicationEnvironments))
	for _, item := range s.applicationEnvironments {
		items = append(items, item)
	}
	return items, nil
}

func (s *stubCatalogRepository) GetApplicationEnvironment(_ context.Context, id string) (domaincatalog.ApplicationEnvironment, error) {
	if s.applicationEnvironments == nil {
		return domaincatalog.ApplicationEnvironment{}, nil
	}
	if item, ok := s.applicationEnvironments[id]; ok {
		return item, nil
	}
	return domaincatalog.ApplicationEnvironment{}, nil
}

func (s *stubCatalogRepository) CreateApplicationEnvironment(context.Context, domaincatalog.ApplicationEnvironmentInput) (domaincatalog.ApplicationEnvironment, error) {
	return domaincatalog.ApplicationEnvironment{}, nil
}

func (s *stubCatalogRepository) UpdateApplicationEnvironment(context.Context, string, domaincatalog.ApplicationEnvironmentInput) (domaincatalog.ApplicationEnvironment, error) {
	return domaincatalog.ApplicationEnvironment{}, nil
}

func (s *stubCatalogRepository) DeleteApplicationEnvironment(context.Context, string) error {
	return nil
}

func (s *stubCatalogRepository) ListBuildTemplates(context.Context) ([]domaincatalog.BuildTemplate, error) {
	return nil, nil
}

func (s *stubCatalogRepository) GetBuildTemplate(context.Context, string) (domaincatalog.BuildTemplate, error) {
	return domaincatalog.BuildTemplate{}, nil
}

func (s *stubCatalogRepository) CreateBuildTemplate(_ context.Context, input domaincatalog.BuildTemplateInput) (domaincatalog.BuildTemplate, error) {
	s.lastBuildTemplate = input
	return domaincatalog.BuildTemplate{Key: input.Key, Name: input.Name, BuilderKind: input.BuilderKind, BuildCommands: input.BuildCommands}, nil
}

func (s *stubCatalogRepository) UpdateBuildTemplate(_ context.Context, _ string, input domaincatalog.BuildTemplateInput) (domaincatalog.BuildTemplate, error) {
	s.lastBuildTemplate = input
	return domaincatalog.BuildTemplate{Key: input.Key, Name: input.Name, BuilderKind: input.BuilderKind, BuildCommands: input.BuildCommands}, nil
}

func (s *stubCatalogRepository) DeleteBuildTemplate(context.Context, string) error {
	return nil
}
func (s *stubCatalogRepository) ListWorkflowTemplates(context.Context) ([]domaincatalog.WorkflowTemplate, error) {
	return nil, nil
}

func (s *stubCatalogRepository) GetWorkflowTemplate(context.Context, string) (domaincatalog.WorkflowTemplate, error) {
	return domaincatalog.WorkflowTemplate{}, nil
}

func (s *stubCatalogRepository) CreateWorkflowTemplate(_ context.Context, input domaincatalog.WorkflowTemplateInput) (domaincatalog.WorkflowTemplate, error) {
	s.lastWorkflowTemplate = input
	return domaincatalog.WorkflowTemplate{Key: input.Key, Name: input.Name, Category: input.Category, Definition: input.Definition}, nil
}

func (s *stubCatalogRepository) UpdateWorkflowTemplate(_ context.Context, _ string, input domaincatalog.WorkflowTemplateInput) (domaincatalog.WorkflowTemplate, error) {
	s.lastWorkflowTemplate = input
	return domaincatalog.WorkflowTemplate{Key: input.Key, Name: input.Name, Category: input.Category, Definition: input.Definition}, nil
}

func (s *stubCatalogRepository) DeleteWorkflowTemplate(context.Context, string) error { return nil }

type stubCatalogApps struct {
	items map[string]domainapp.App
}

func (s stubCatalogApps) Get(_ context.Context, id string) (domainapp.App, error) {
	if item, ok := s.items[id]; ok {
		return item, nil
	}
	return domainapp.App{}, apprepo.ErrNotFound
}

type stubScopeGrantReader struct {
	items []domainscopegrant.Record
}

func (s stubScopeGrantReader) List(context.Context) ([]domainscopegrant.Record, error) {
	return s.items, nil
}

type stubCatalogReader struct {
	environments            []domaincatalog.Environment
	applicationEnvironments []domaincatalog.ApplicationEnvironment
}

func (s stubCatalogReader) ListEnvironments(context.Context) ([]domaincatalog.Environment, error) {
	return s.environments, nil
}

func (s stubCatalogReader) ListApplicationEnvironments(context.Context) ([]domaincatalog.ApplicationEnvironment, error) {
	return s.applicationEnvironments, nil
}

type stubCatalogRolePermissionReader struct {
	matrix map[string][]string
}

func (s stubCatalogRolePermissionReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return s.matrix, nil
}

func catalogPermissions(keys ...string) *appaccess.PermissionResolver {
	return appaccess.NewPermissionResolver(stubCatalogRolePermissionReader{
		matrix: map[string][]string{
			"admin": keys,
		},
	})
}

func TestCreateWorkflowTemplateRejectsUnsupportedStepType(t *testing.T) {
	repo := &stubCatalogRepository{}
	service := New(repo, nil, nil, catalogPermissions(appaccess.PermDeliveryWorkflowTemplatesManage), nil, nil)
	principal := domainidentity.Principal{Roles: []string{"admin"}}

	_, err := service.CreateWorkflowTemplate(context.Background(), principal, domaincatalog.WorkflowTemplateInput{
		Key:  "release-flow",
		Name: "release-flow",
		Definition: map[string]any{
			"stages": []any{
				map[string]any{
					"name": "deploy",
					"steps": []any{
						map[string]any{"name": "bad", "type": "shell_script"},
					},
				},
			},
		},
	})
	if err == nil {
		t.Fatalf("CreateWorkflowTemplate returned nil error, want unsupported step type error")
	}
}

func TestCreateWorkflowTemplateDefaultsReleaseDefinition(t *testing.T) {
	repo := &stubCatalogRepository{}
	service := New(repo, nil, nil, catalogPermissions(appaccess.PermDeliveryWorkflowTemplatesManage), nil, nil)
	principal := domainidentity.Principal{Roles: []string{"admin"}}

	_, err := service.CreateWorkflowTemplate(context.Background(), principal, domaincatalog.WorkflowTemplateInput{
		Key:  "release-flow",
		Name: "release-flow",
	})
	if err != nil {
		t.Fatalf("CreateWorkflowTemplate returned error: %v", err)
	}
	if repo.lastWorkflowTemplate.Category != "release" {
		t.Fatalf("Category = %q, want release", repo.lastWorkflowTemplate.Category)
	}
	if len(repo.lastWorkflowTemplate.Definition) == 0 {
		t.Fatalf("Definition = empty, want default release flow definition")
	}
}

func TestCreateWorkflowTemplateRejectsGraphCycle(t *testing.T) {
	repo := &stubCatalogRepository{}
	service := New(repo, nil, nil, catalogPermissions(appaccess.PermDeliveryWorkflowTemplatesManage), nil, nil)
	principal := domainidentity.Principal{Roles: []string{"admin"}}

	_, err := service.CreateWorkflowTemplate(context.Background(), principal, domaincatalog.WorkflowTemplateInput{
		Key:  "release-flow",
		Name: "release-flow",
		Definition: map[string]any{
			"mode": "release_dag",
			"nodes": []map[string]any{
				{"id": "a", "name": "A", "type": "manual_approval"},
				{"id": "b", "name": "B", "type": "deploy_update_image"},
			},
			"edges": []map[string]any{
				{"id": "e1", "source": "a", "target": "b", "condition": "success"},
				{"id": "e2", "source": "b", "target": "a", "condition": "success"},
			},
		},
	})
	if err == nil {
		t.Fatalf("CreateWorkflowTemplate returned nil error, want DAG cycle validation error")
	}
}

func TestUpdateApplicationEnvironmentDeniesOutsideScopeGrant(t *testing.T) {
	repo := &stubCatalogRepository{
		applicationEnvironments: map[string]domaincatalog.ApplicationEnvironment{
			"binding-1": {
				ID:             "binding-1",
				ApplicationID:  "app-1",
				BusinessLineID: "bl-retail",
				EnvironmentID:  "env-prod",
				EnvironmentKey: "prod",
			},
		},
	}
	authorizer := accessServiceForCatalogTests([]domainscopegrant.Record{
		{
			ID:             "grant-1",
			SubjectType:    "user",
			SubjectID:      "user-1",
			BusinessLineID: "bl-retail",
			EnvironmentIDs: []string{"env-dev"},
			ApplicationIDs: []string{"app-1"},
			Role:           "developer",
			Effect:         "allow",
			Enabled:        true,
		},
	}, []domaincatalog.Environment{{ID: "env-dev", Key: "dev"}}, []domaincatalog.ApplicationEnvironment{repo.applicationEnvironments["binding-1"]})
	service := New(repo, authorizer, stubCatalogApps{items: map[string]domainapp.App{
		"app-1": {ID: "app-1", BusinessLineID: "bl-retail"},
	}}, nil, nil, nil)

	_, err := service.UpdateApplicationEnvironment(context.Background(), domainidentity.Principal{
		UserID: "user-1",
		Roles:  []string{"ops"},
	}, "binding-1", domaincatalog.ApplicationEnvironmentInput{
		ApplicationID: "app-1",
		EnvironmentID: "env-prod",
	})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("UpdateApplicationEnvironment error = %v, want ErrAccessDenied", err)
	}
}

func TestCreateWorkflowTemplateAllowsDelegatedManagePermission(t *testing.T) {
	repo := &stubCatalogRepository{}
	service := New(repo, nil, nil, appaccess.NewPermissionResolver(stubCatalogRolePermissionReader{
		matrix: map[string][]string{
			"delegated": {appaccess.PermDeliveryWorkflowTemplatesManage},
		},
	}), nil, nil)

	_, err := service.CreateWorkflowTemplate(context.Background(), domainidentity.Principal{Roles: []string{"delegated"}}, domaincatalog.WorkflowTemplateInput{
		Key:  "release-flow",
		Name: "release-flow",
	})
	if err != nil {
		t.Fatalf("CreateWorkflowTemplate returned error: %v", err)
	}
}

func accessServiceForCatalogTests(grants []domainscopegrant.Record, environments []domaincatalog.Environment, applicationEnvironments []domaincatalog.ApplicationEnvironment) domainaccess.Authorizer {
	return appaccess.New(
		policy.NewEngine(),
		nil,
		stubScopeGrantReader{items: grants},
		stubCatalogReader{
			environments:            environments,
			applicationEnvironments: applicationEnvironments,
		},
	)
}
