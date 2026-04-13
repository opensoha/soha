package catalog

import (
	"context"
	"testing"

	domaincatalog "github.com/kubecrux/kubecrux/internal/domain/catalog"
)

type stubCatalogRepository struct {
	lastWorkflowTemplate domaincatalog.WorkflowTemplateInput
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
	return nil, nil
}

func (s *stubCatalogRepository) GetApplicationEnvironment(context.Context, string) (domaincatalog.ApplicationEnvironment, error) {
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

func TestCreateWorkflowTemplateRejectsUnsupportedStepType(t *testing.T) {
	repo := &stubCatalogRepository{}
	service := New(repo)

	_, err := service.CreateWorkflowTemplate(context.Background(), domaincatalog.WorkflowTemplateInput{
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
	service := New(repo)

	_, err := service.CreateWorkflowTemplate(context.Background(), domaincatalog.WorkflowTemplateInput{
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
	service := New(repo)

	_, err := service.CreateWorkflowTemplate(context.Background(), domaincatalog.WorkflowTemplateInput{
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
