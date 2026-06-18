package catalog

import (
	"context"
	"errors"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainrelease "github.com/opensoha/soha/internal/domain/release"
	domainscopegrant "github.com/opensoha/soha/internal/domain/scopegrant"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/policy"
	apprepo "github.com/opensoha/soha/internal/repository/application"
)

type stubCatalogRepository struct {
	lastWorkflowTemplate    domaincatalog.WorkflowTemplateInput
	lastBuildTemplate       domaincatalog.BuildTemplateInput
	applicationEnvironments map[string]domaincatalog.ApplicationEnvironment
	environments            []domaincatalog.Environment
}

func (s *stubCatalogRepository) ListEnvironments(context.Context) ([]domaincatalog.Environment, error) {
	return s.environments, nil
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

func (s stubCatalogApps) List(context.Context, domainapp.Filter) ([]domainapp.App, error) {
	items := make([]domainapp.App, 0, len(s.items))
	for _, item := range s.items {
		items = append(items, item)
	}
	return items, nil
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

type stubCatalogBuildRuntimeReader struct {
	items []domainbuild.Record
}

func (s stubCatalogBuildRuntimeReader) List(context.Context, domainidentity.Principal, domainbuild.Filter) ([]domainbuild.Record, error) {
	return s.items, nil
}

type stubCatalogWorkflowRuntimeReader struct {
	items []domainworkflow.Run
}

func (s stubCatalogWorkflowRuntimeReader) List(context.Context, domainidentity.Principal, string, int) ([]domainworkflow.Run, error) {
	return s.items, nil
}

type stubCatalogReleaseRuntimeReader struct {
	items []domainrelease.Record
}

func (s stubCatalogReleaseRuntimeReader) List(context.Context, domainidentity.Principal, domainrelease.Filter) ([]domainrelease.Record, error) {
	return s.items, nil
}

type stubCatalogDeliveryRuntimeReader struct {
	bundles []domaindelivery.ReleaseBundle
	tasks   []domaindelivery.ExecutionTask
}

func (s stubCatalogDeliveryRuntimeReader) ListReleaseBundles(context.Context, domaindelivery.ReleaseBundleFilter) ([]domaindelivery.ReleaseBundle, error) {
	return s.bundles, nil
}

func (s stubCatalogDeliveryRuntimeReader) ListExecutionTasks(context.Context, domaindelivery.ExecutionTaskFilter) ([]domaindelivery.ExecutionTask, error) {
	return s.tasks, nil
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

func TestCreateWorkflowTemplateAcceptsDeliveryDAGPreviewFields(t *testing.T) {
	repo := &stubCatalogRepository{}
	service := New(repo, nil, nil, catalogPermissions(appaccess.PermDeliveryWorkflowTemplatesManage), nil, nil)
	principal := domainidentity.Principal{Roles: []string{"admin"}}

	_, err := service.CreateWorkflowTemplate(context.Background(), principal, domaincatalog.WorkflowTemplateInput{
		Key:  "delivery-flow",
		Name: "delivery-flow",
		Definition: map[string]any{
			"mode": "delivery_dag",
			"nodes": []map[string]any{
				{
					"id":              "build",
					"name":            "Build",
					"type":            "build",
					"inputs":          []any{"source"},
					"outputs":         []any{"image"},
					"serviceSelector": map[string]any{"matchLabels": map[string]any{"service": "checkout"}},
					"artifactOutputs": []any{map[string]any{"name": "image", "kind": "image", "required": true}},
					"runCondition":    "branch == main",
					"failurePolicy":   "rollback",
					"observability":   map[string]any{"events": []any{"started", "completed"}},
				},
				{"id": "deploy", "name": "Deploy", "type": "deploy_update_image", "targetSelector": map[string]any{"key": "prod"}},
			},
			"edges": []map[string]any{
				{"id": "e1", "source": "build", "target": "deploy", "condition": "success"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkflowTemplate returned error: %v", err)
	}
	if repo.lastWorkflowTemplate.Definition["mode"] != "delivery_dag" {
		t.Fatalf("mode = %v, want delivery_dag", repo.lastWorkflowTemplate.Definition["mode"])
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

func TestWorkflowTemplateUsageSummarizesProductionRisk(t *testing.T) {
	repo := &stubCatalogRepository{
		environments: []domaincatalog.Environment{
			{ID: "env-prod", Key: "prod", Name: "Production", IsProduction: true, RequiresApproval: true},
		},
		applicationEnvironments: map[string]domaincatalog.ApplicationEnvironment{
			"binding-1": {
				ID:                 "binding-1",
				ApplicationID:      "app-1",
				EnvironmentID:      "env-prod",
				EnvironmentKey:     "prod",
				WorkflowTemplateID: "wf-1",
				ReleasePolicy:      domaincatalog.ReleasePolicy{RequiresApproval: true},
				Targets:            []domaincatalog.ReleaseTarget{{ID: "target-1"}, {ID: "target-2"}},
			},
		},
	}
	service := New(repo, nil, stubCatalogApps{items: map[string]domainapp.App{
		"app-1": {ID: "app-1", Name: "Payments", Key: "payments", BusinessLineID: "bl-core"},
	}}, catalogPermissions(appaccess.PermDeliveryWorkflowTemplatesView), nil, nil)
	service.SetTemplateUsageRuntimeReaders(TemplateUsageRuntimeReaders{
		Workflows: stubCatalogWorkflowRuntimeReader{items: []domainworkflow.Run{
			{
				ID:            "workflow-1",
				ApplicationID: "app-1",
				WorkflowName:  "release-prod",
				Status:        "running",
				Metadata: map[string]any{
					"bindingId":          "binding-1",
					"workflowTemplateId": "wf-1",
				},
				CreatedAt: "2026-05-08T10:00:00Z",
				UpdatedAt: "2026-05-08T10:30:00Z",
			},
		}},
		Releases: stubCatalogReleaseRuntimeReader{items: []domainrelease.Record{
			{
				ID:             "release-1",
				ApplicationID:  "app-1",
				ClusterID:      "cluster-a",
				Namespace:      "prod",
				DeploymentName: "payments",
				Status:         "failed",
				Metadata:       map[string]any{"applicationEnvironmentId": "binding-1", "executionTaskId": "task-1"},
				CreatedAt:      time.Date(2026, 5, 8, 10, 45, 0, 0, time.UTC),
			},
		}},
		Delivery: stubCatalogDeliveryRuntimeReader{tasks: []domaindelivery.ExecutionTask{
			{
				ID:                       "task-1",
				ApplicationID:            "app-1",
				ApplicationEnvironmentID: "binding-1",
				TaskKind:                 "release",
				Status:                   "failed",
				UpdatedAt:                time.Date(2026, 5, 8, 11, 0, 0, 0, time.UTC),
			},
		}},
	})

	usage, err := service.GetWorkflowTemplateUsage(context.Background(), domainidentity.Principal{Roles: []string{"admin"}}, "wf-1")
	if err != nil {
		t.Fatalf("GetWorkflowTemplateUsage returned error: %v", err)
	}
	if usage.UsageCount != 1 || usage.ApplicationCount != 1 || usage.TargetCount != 2 {
		t.Fatalf("unexpected workflow usage counts: %#v", usage)
	}
	if usage.RiskLevel != domaincatalog.TemplateUsageRiskHigh || usage.RecommendedAction != "copy_template_before_editing" {
		t.Fatalf("expected high-risk recommendation, got %#v", usage)
	}
	states := usage.LastExecutionSummary["stateCounts"].(map[string]int)
	if states["running"] != 1 || states["failed"] != 2 {
		t.Fatalf("unexpected workflow runtime state counts: %#v", usage.LastExecutionSummary)
	}
}

func TestBuildTemplateUsageFindsApplicationBuildSources(t *testing.T) {
	repo := &stubCatalogRepository{
		environments: []domaincatalog.Environment{{ID: "env-dev", Key: "dev", Name: "Development"}},
		applicationEnvironments: map[string]domaincatalog.ApplicationEnvironment{
			"binding-1": {ID: "binding-1", ApplicationID: "app-1", EnvironmentID: "env-dev", Targets: []domaincatalog.ReleaseTarget{{ID: "target-1"}}},
		},
	}
	service := New(repo, nil, stubCatalogApps{items: map[string]domainapp.App{
		"app-1": {
			ID:   "app-1",
			Name: "Payments",
			Key:  "payments",
			BuildSources: []domainapp.BuildSource{
				{ID: "source-1", Name: "Platform Build", Type: domainapp.BuildSourceTypePlatformTemplate, Config: map[string]any{"buildTemplateId": "bt-1"}},
			},
		},
	}}, catalogPermissions(appaccess.PermDeliveryBuildTemplatesView), nil, nil)
	service.SetTemplateUsageRuntimeReaders(TemplateUsageRuntimeReaders{
		Builds: stubCatalogBuildRuntimeReader{items: []domainbuild.Record{
			{
				ID:            "build-1",
				ApplicationID: "app-1",
				SourceSystem:  "manual",
				Status:        "completed",
				Metadata:      map[string]any{"buildSourceId": "source-1", "executionTaskId": "task-1"},
				CreatedAt:     time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC),
			},
		}},
		Delivery: stubCatalogDeliveryRuntimeReader{
			bundles: []domaindelivery.ReleaseBundle{
				{
					ID:            "bundle-1",
					ApplicationID: "app-1",
					SourceType:    "build",
					Status:        "building",
					Metadata:      map[string]any{"buildSourceId": "source-1"},
					UpdatedAt:     time.Date(2026, 5, 8, 10, 30, 0, 0, time.UTC),
				},
			},
			tasks: []domaindelivery.ExecutionTask{
				{
					ID:            "task-1",
					ApplicationID: "app-1",
					TaskKind:      "build",
					Status:        "running",
					Payload:       map[string]any{"buildSourceId": "source-1"},
					UpdatedAt:     time.Date(2026, 5, 8, 10, 45, 0, 0, time.UTC),
				},
			},
		},
	})

	usage, err := service.GetBuildTemplateUsage(context.Background(), domainidentity.Principal{Roles: []string{"admin"}}, "bt-1")
	if err != nil {
		t.Fatalf("GetBuildTemplateUsage returned error: %v", err)
	}
	if usage.UsageCount != 1 || usage.ApplicationCount != 1 || len(usage.BuildSources) != 1 {
		t.Fatalf("unexpected build template usage: %#v", usage)
	}
	if usage.RiskLevel != domaincatalog.TemplateUsageRiskLow {
		t.Fatalf("expected low-risk dev usage, got %#v", usage)
	}
	states := usage.LastExecutionSummary["stateCounts"].(map[string]int)
	if states["succeeded"] != 1 || states["running"] != 2 {
		t.Fatalf("unexpected build runtime state counts: %#v", usage.LastExecutionSummary)
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
