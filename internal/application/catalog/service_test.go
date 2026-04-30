package catalog

import (
	"context"
	"errors"
	"testing"

	appaccess "github.com/kubecrux/kubecrux/internal/application/access"
	domainaccess "github.com/kubecrux/kubecrux/internal/domain/access"
	domainapp "github.com/kubecrux/kubecrux/internal/domain/application"
	domaincatalog "github.com/kubecrux/kubecrux/internal/domain/catalog"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	domainscopegrant "github.com/kubecrux/kubecrux/internal/domain/scopegrant"
	"github.com/kubecrux/kubecrux/internal/platform/apperrors"
	"github.com/kubecrux/kubecrux/internal/policy"
	apprepo "github.com/kubecrux/kubecrux/internal/repository/application"
)

type stubCatalogRepository struct {
	lastWorkflowTemplate    domaincatalog.WorkflowTemplateInput
	applicationEnvironments map[string]domaincatalog.ApplicationEnvironment
	environments            map[string]domaincatalog.Environment
}

func (s *stubCatalogRepository) ListBusinessLines(context.Context) ([]domaincatalog.BusinessLine, error) {
	return nil, nil
}

func (s *stubCatalogRepository) GetBusinessLine(context.Context, string) (domaincatalog.BusinessLine, error) {
	return domaincatalog.BusinessLine{}, nil
}

func (s *stubCatalogRepository) CreateBusinessLine(context.Context, domaincatalog.BusinessLineInput) (domaincatalog.BusinessLine, error) {
	return domaincatalog.BusinessLine{}, nil
}

func (s *stubCatalogRepository) UpdateBusinessLine(context.Context, string, domaincatalog.BusinessLineInput) (domaincatalog.BusinessLine, error) {
	return domaincatalog.BusinessLine{}, nil
}

func (s *stubCatalogRepository) DeleteBusinessLine(context.Context, string) error { return nil }
func (s *stubCatalogRepository) ListEnvironments(context.Context) ([]domaincatalog.Environment, error) {
	return nil, nil
}

func (s *stubCatalogRepository) GetEnvironment(context.Context, string) (domaincatalog.Environment, error) {
	if s.environments == nil {
		return domaincatalog.Environment{}, nil
	}
	for _, item := range s.environments {
		return item, nil
	}
	return domaincatalog.Environment{}, nil
}

func (s *stubCatalogRepository) CreateEnvironment(context.Context, domaincatalog.EnvironmentInput) (domaincatalog.Environment, error) {
	return domaincatalog.Environment{}, nil
}

func (s *stubCatalogRepository) UpdateEnvironment(context.Context, string, domaincatalog.EnvironmentInput) (domaincatalog.Environment, error) {
	return domaincatalog.Environment{}, nil
}

func (s *stubCatalogRepository) DeleteEnvironment(context.Context, string) error { return nil }
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
	service := New(repo, nil, nil, catalogPermissions(appaccess.PermDeliveryWorkflowTemplatesManage))
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
	service := New(repo, nil, nil, catalogPermissions(appaccess.PermDeliveryWorkflowTemplatesManage))
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
	service := New(repo, nil, nil, catalogPermissions(appaccess.PermDeliveryWorkflowTemplatesManage))
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
		environments: map[string]domaincatalog.Environment{
			"env-prod": {ID: "env-prod", Key: "prod"},
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
	}}, nil)

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
	}))

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
