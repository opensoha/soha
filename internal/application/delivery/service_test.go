package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainrelease "github.com/opensoha/soha/internal/domain/release"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type stubApplicationReader struct {
	app                domainapp.App
	apps               []domainapp.App
	services           []domainapp.Service
	createErr          error
	createCount        *int
	updateCount        *int
	createServiceCount *int
	updateServiceCount *int
}

func (s stubApplicationReader) List(context.Context, domainidentity.Principal, domainapp.Filter) ([]domainapp.App, error) {
	if len(s.apps) > 0 {
		return s.apps, nil
	}
	return []domainapp.App{s.app}, nil
}

func (s stubApplicationReader) Get(context.Context, domainidentity.Principal, string) (domainapp.App, error) {
	return s.app, nil
}

func (s stubApplicationReader) Create(context.Context, domainidentity.Principal, domainapp.UpsertInput) (domainapp.App, error) {
	if s.createCount != nil {
		*s.createCount = *s.createCount + 1
	}
	if s.createErr != nil {
		return domainapp.App{}, s.createErr
	}
	return s.app, nil
}

func (s stubApplicationReader) Update(context.Context, domainidentity.Principal, string, domainapp.UpsertInput) (domainapp.App, error) {
	if s.updateCount != nil {
		*s.updateCount = *s.updateCount + 1
	}
	return s.app, nil
}

func (s stubApplicationReader) ListServices(context.Context, domainidentity.Principal, string) ([]domainapp.Service, error) {
	return s.services, nil
}

func (s stubApplicationReader) CreateService(_ context.Context, _ domainidentity.Principal, applicationID string, input domainapp.ServiceInput) (domainapp.Service, error) {
	if s.createServiceCount != nil {
		*s.createServiceCount = *s.createServiceCount + 1
	}
	return domainapp.Service{
		ID:            firstNonEmpty(input.ID, "service-created"),
		ApplicationID: applicationID,
		Key:           input.Key,
		Name:          input.Name,
		ServiceKind:   input.ServiceKind,
		Containers:    nil,
		Enabled:       input.Enabled,
	}, nil
}

func (s stubApplicationReader) UpdateService(_ context.Context, _ domainidentity.Principal, applicationID, serviceID string, input domainapp.ServiceInput) (domainapp.Service, error) {
	if s.updateServiceCount != nil {
		*s.updateServiceCount = *s.updateServiceCount + 1
	}
	return domainapp.Service{
		ID:            serviceID,
		ApplicationID: applicationID,
		Key:           input.Key,
		Name:          input.Name,
		ServiceKind:   input.ServiceKind,
		Enabled:       input.Enabled,
	}, nil
}

type stubCatalogReader struct {
	bindings    []domaincatalog.ApplicationEnvironment
	envs        []domaincatalog.Environment
	createCount *int
	updateCount *int
}

func (s stubCatalogReader) ListEnvironments(context.Context, domainidentity.Principal) ([]domaincatalog.Environment, error) {
	return s.envs, nil
}

func (s stubCatalogReader) ListApplicationEnvironments(context.Context, domainidentity.Principal) ([]domaincatalog.ApplicationEnvironment, error) {
	return s.bindings, nil
}

func (s stubCatalogReader) GetApplicationEnvironment(_ context.Context, _ domainidentity.Principal, bindingID string) (domaincatalog.ApplicationEnvironment, error) {
	for _, binding := range s.bindings {
		if binding.ID == bindingID {
			return binding, nil
		}
	}
	if len(s.bindings) == 0 {
		return domaincatalog.ApplicationEnvironment{}, nil
	}
	return s.bindings[0], nil
}

func (s stubCatalogReader) CreateApplicationEnvironment(context.Context, domainidentity.Principal, domaincatalog.ApplicationEnvironmentInput) (domaincatalog.ApplicationEnvironment, error) {
	if s.createCount != nil {
		*s.createCount = *s.createCount + 1
	}
	if len(s.bindings) == 0 {
		return domaincatalog.ApplicationEnvironment{}, nil
	}
	return s.bindings[0], nil
}

func (s stubCatalogReader) UpdateApplicationEnvironment(context.Context, domainidentity.Principal, string, domaincatalog.ApplicationEnvironmentInput) (domaincatalog.ApplicationEnvironment, error) {
	if s.updateCount != nil {
		*s.updateCount = *s.updateCount + 1
	}
	if len(s.bindings) == 0 {
		return domaincatalog.ApplicationEnvironment{}, nil
	}
	return s.bindings[0], nil
}

type stubBuildReader struct {
	record       domainbuild.Record
	listItems    []domainbuild.Record
	triggerInput *domainbuild.TriggerInput
	triggerCount *int
	triggerErr   error
}

func (s stubBuildReader) List(context.Context, domainidentity.Principal, domainbuild.Filter) ([]domainbuild.Record, error) {
	return s.listItems, nil
}

func (s stubBuildReader) Get(context.Context, domainidentity.Principal, string) (domainbuild.Record, error) {
	if s.record.ID != "" || s.record.Metadata != nil {
		return s.record, nil
	}
	if len(s.listItems) > 0 {
		return s.listItems[0], nil
	}
	return domainbuild.Record{}, nil
}

func (s stubBuildReader) Trigger(_ context.Context, _ domainidentity.Principal, input domainbuild.TriggerInput) (domainbuild.Record, error) {
	if s.triggerInput != nil {
		*s.triggerInput = input
	}
	if s.triggerCount != nil {
		*s.triggerCount = *s.triggerCount + 1
	}
	if s.triggerErr != nil {
		return domainbuild.Record{}, s.triggerErr
	}
	if s.record.ID != "" || s.record.Metadata != nil {
		return s.record, nil
	}
	return domainbuild.Record{}, nil
}

type stubWorkflowReader struct {
	record       domainworkflow.Run
	listItems    []domainworkflow.Run
	triggerInput *domainworkflow.Input
	triggerCount *int
}

func (s stubWorkflowReader) List(context.Context, domainidentity.Principal, string, int) ([]domainworkflow.Run, error) {
	return s.listItems, nil
}

func (s stubWorkflowReader) Get(context.Context, domainidentity.Principal, string) (domainworkflow.Run, error) {
	if s.record.ID != "" || s.record.Metadata != nil {
		return s.record, nil
	}
	if len(s.listItems) > 0 {
		return s.listItems[0], nil
	}
	return domainworkflow.Run{}, nil
}

func (s stubWorkflowReader) Trigger(_ context.Context, _ domainidentity.Principal, input domainworkflow.Input) (domainworkflow.Run, error) {
	if s.triggerInput != nil {
		*s.triggerInput = input
	}
	if s.triggerCount != nil {
		*s.triggerCount = *s.triggerCount + 1
	}
	if s.record.ID != "" || s.record.Metadata != nil {
		return s.record, nil
	}
	return domainworkflow.Run{}, nil
}

func (s stubWorkflowReader) TriggerValidation(_ context.Context, _ domainidentity.Principal, input domainworkflow.Input) (domainworkflow.Run, error) {
	if s.triggerInput != nil {
		*s.triggerInput = input
	}
	if s.triggerCount != nil {
		*s.triggerCount = *s.triggerCount + 1
	}
	if s.record.ID != "" || s.record.Metadata != nil {
		return s.record, nil
	}
	return domainworkflow.Run{}, nil
}

func (s stubWorkflowReader) TriggerRollback(_ context.Context, _ domainidentity.Principal, input domainworkflow.Input) (domainworkflow.Run, error) {
	if s.triggerInput != nil {
		*s.triggerInput = input
	}
	if s.triggerCount != nil {
		*s.triggerCount = *s.triggerCount + 1
	}
	if s.record.ID != "" || s.record.Metadata != nil {
		return s.record, nil
	}
	return domainworkflow.Run{}, nil
}

type stubReleaseReader struct {
	record       domainrelease.Record
	listItems    []domainrelease.Record
	triggerInput *domainrelease.TriggerInput
	triggerCount *int
	trigger      func(domainrelease.TriggerInput) domainrelease.Record
}

func (s stubReleaseReader) List(context.Context, domainidentity.Principal, domainrelease.Filter) ([]domainrelease.Record, error) {
	return s.listItems, nil
}

func (s stubReleaseReader) Get(context.Context, domainidentity.Principal, string) (domainrelease.Record, error) {
	if s.record.ID != "" || s.record.Metadata != nil {
		return s.record, nil
	}
	if len(s.listItems) > 0 {
		return s.listItems[0], nil
	}
	return domainrelease.Record{}, nil
}

func (s stubReleaseReader) Trigger(_ context.Context, _ domainidentity.Principal, input domainrelease.TriggerInput) (domainrelease.Record, error) {
	if s.triggerInput != nil {
		*s.triggerInput = input
	}
	if s.triggerCount != nil {
		*s.triggerCount = *s.triggerCount + 1
	}
	if s.trigger != nil {
		return s.trigger(input), nil
	}
	if s.record.ID != "" || s.record.Metadata != nil {
		return s.record, nil
	}
	return domainrelease.Record{}, nil
}

type stubTargetReader struct {
	deployments map[string][]domainresource.DeploymentView
}

func (s stubTargetReader) ListPods(context.Context, domainidentity.Principal, string, string) ([]domainresource.PodView, error) {
	return nil, nil
}

func (s stubTargetReader) ListDeployments(_ context.Context, _ domainidentity.Principal, clusterID, namespace string) ([]domainresource.DeploymentView, error) {
	return s.deployments[clusterID+"/"+namespace], nil
}

func (s stubTargetReader) GetDeploymentDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.DeploymentDetailView, error) {
	return domainresource.DeploymentDetailView{}, nil
}

func (s stubTargetReader) ListServices(context.Context, domainidentity.Principal, string, string) ([]domainresource.ServiceView, error) {
	return nil, nil
}

func (s stubTargetReader) ListIngresses(context.Context, domainidentity.Principal, string, string) ([]domainresource.IngressView, error) {
	return nil, nil
}

type stubDeliveryRolePermissionReader struct {
	matrix map[string][]string
}

func (s stubDeliveryRolePermissionReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return s.matrix, nil
}

func deliveryActionPermissions(keys ...string) *appaccess.PermissionResolver {
	return appaccess.NewPermissionResolver(stubDeliveryRolePermissionReader{
		matrix: map[string][]string{"developer": keys},
	})
}

func deliveryActionPrincipal() domainidentity.Principal {
	return domainidentity.Principal{UserID: "dev-1", UserName: "developer", Roles: []string{"developer"}}
}

type stubRepository struct {
	blueprint domaindelivery.DeliveryBlueprint
	bundles   []domaindelivery.ReleaseBundle
	tasks     []domaindelivery.ExecutionTask
}

func (s stubRepository) ListReleaseBundles(context.Context, domaindelivery.ReleaseBundleFilter) ([]domaindelivery.ReleaseBundle, error) {
	return s.bundles, nil
}

func (stubRepository) GetReleaseBundle(context.Context, string) (domaindelivery.ReleaseBundle, error) {
	return domaindelivery.ReleaseBundle{}, nil
}

func (stubRepository) CreateReleaseBundle(context.Context, domaindelivery.ReleaseBundle) (domaindelivery.ReleaseBundle, error) {
	return domaindelivery.ReleaseBundle{}, nil
}

func (stubRepository) UpdateReleaseBundle(context.Context, domaindelivery.ReleaseBundle) (domaindelivery.ReleaseBundle, error) {
	return domaindelivery.ReleaseBundle{}, nil
}

func (s stubRepository) ListExecutionTasks(context.Context, domaindelivery.ExecutionTaskFilter) ([]domaindelivery.ExecutionTask, error) {
	return s.tasks, nil
}

func (stubRepository) GetExecutionTask(context.Context, string) (domaindelivery.ExecutionTask, error) {
	return domaindelivery.ExecutionTask{}, nil
}

func (stubRepository) GetExecutionTaskByCallbackToken(context.Context, string) (domaindelivery.ExecutionTask, error) {
	return domaindelivery.ExecutionTask{}, nil
}

func (stubRepository) ClaimExecutionTask(context.Context, []string, string, string) (domaindelivery.ExecutionTask, error) {
	return domaindelivery.ExecutionTask{}, nil
}

func (stubRepository) CreateExecutionTask(context.Context, domaindelivery.ExecutionTask) (domaindelivery.ExecutionTask, error) {
	return domaindelivery.ExecutionTask{}, nil
}

func (stubRepository) UpdateExecutionTask(context.Context, domaindelivery.ExecutionTask) (domaindelivery.ExecutionTask, error) {
	return domaindelivery.ExecutionTask{}, nil
}

func (stubRepository) ListExecutionLogs(context.Context, string, int) ([]domaindelivery.ExecutionLog, error) {
	return nil, nil
}

func (stubRepository) CreateExecutionLog(context.Context, domaindelivery.ExecutionLog) error {
	return nil
}

func (stubRepository) CreateExecutionCallback(context.Context, domaindelivery.ExecutionCallback) error {
	return nil
}

func (stubRepository) ListExecutionArtifacts(context.Context, string) ([]domaindelivery.ExecutionArtifact, error) {
	return nil, nil
}

func (stubRepository) ListExecutionArtifactsByBundle(context.Context, string) ([]domaindelivery.ExecutionArtifact, error) {
	return nil, nil
}

func (stubRepository) ListArtifacts(context.Context, domaindelivery.ArtifactFilter) ([]domaindelivery.ExecutionArtifact, error) {
	return nil, nil
}

func (stubRepository) UpsertExecutionArtifact(context.Context, domaindelivery.ExecutionArtifact) (domaindelivery.ExecutionArtifact, error) {
	return domaindelivery.ExecutionArtifact{}, nil
}

func (s stubRepository) ListDeliveryBlueprints(context.Context) ([]domaindelivery.DeliveryBlueprint, error) {
	if s.blueprint.ID != "" {
		return []domaindelivery.DeliveryBlueprint{s.blueprint}, nil
	}
	return nil, nil
}

func (s stubRepository) GetDeliveryBlueprint(context.Context, string) (domaindelivery.DeliveryBlueprint, error) {
	if s.blueprint.ID != "" {
		return s.blueprint, nil
	}
	return domaindelivery.DeliveryBlueprint{}, nil
}

func (stubRepository) CreateDeliveryBlueprint(context.Context, domaindelivery.DeliveryBlueprintInput) (domaindelivery.DeliveryBlueprint, error) {
	return domaindelivery.DeliveryBlueprint{}, nil
}

func (stubRepository) UpdateDeliveryBlueprint(context.Context, string, domaindelivery.DeliveryBlueprintInput) (domaindelivery.DeliveryBlueprint, error) {
	return domaindelivery.DeliveryBlueprint{}, nil
}

func (stubRepository) CreateDeliveryDraft(context.Context, domaindelivery.DeliveryDraftInput, string) (domaindelivery.DeliveryDraft, error) {
	return domaindelivery.DeliveryDraft{}, nil
}

func (stubRepository) GetDeliveryDraft(context.Context, string) (domaindelivery.DeliveryDraft, error) {
	return domaindelivery.DeliveryDraft{}, nil
}

func (stubRepository) UpdateDeliveryDraft(context.Context, domaindelivery.DeliveryDraft) (domaindelivery.DeliveryDraft, error) {
	return domaindelivery.DeliveryDraft{}, nil
}

func (stubRepository) CreateDeliveryPlan(context.Context, domaindelivery.DeliveryPlanInput, string) (domaindelivery.DeliveryPlan, error) {
	return domaindelivery.DeliveryPlan{}, nil
}

func (stubRepository) GetDeliveryPlan(context.Context, string) (domaindelivery.DeliveryPlan, error) {
	return domaindelivery.DeliveryPlan{}, nil
}

func (stubRepository) UpdateDeliveryPlan(context.Context, domaindelivery.DeliveryPlan) (domaindelivery.DeliveryPlan, error) {
	return domaindelivery.DeliveryPlan{}, nil
}

type draftRepository struct {
	stubRepository
	draft       domaindelivery.DeliveryDraft
	createInput *domaindelivery.DeliveryDraftInput
	createCount int
	updateCount int
	updateErr   error
}

func (r *draftRepository) CreateDeliveryDraft(_ context.Context, input domaindelivery.DeliveryDraftInput, createdBy string) (domaindelivery.DeliveryDraft, error) {
	r.createCount++
	r.createInput = &input
	draft := domaindelivery.DeliveryDraft{
		ID:                  firstNonEmpty(input.ID, "draft-created"),
		Source:              firstNonEmpty(input.Source, domaindelivery.DeliveryDraftSourceManual),
		Status:              domaindelivery.DeliveryDraftStatusDraft,
		ApplicationDraft:    input.ApplicationDraft,
		Services:            input.Services,
		BuildSources:        input.BuildSources,
		EnvironmentBindings: input.EnvironmentBindings,
		Files:               input.Files,
		ExecutionHints:      input.ExecutionHints,
		PostCreateActions:   input.PostCreateActions,
		CreatedBy:           createdBy,
	}
	r.draft = draft
	return draft, nil
}

func (r *draftRepository) GetDeliveryDraft(context.Context, string) (domaindelivery.DeliveryDraft, error) {
	return r.draft, nil
}

func (r *draftRepository) UpdateDeliveryDraft(_ context.Context, draft domaindelivery.DeliveryDraft) (domaindelivery.DeliveryDraft, error) {
	r.updateCount++
	if r.updateErr != nil {
		return domaindelivery.DeliveryDraft{}, r.updateErr
	}
	r.draft = draft
	return draft, nil
}

type planRepository struct {
	stubRepository
	plan        domaindelivery.DeliveryPlan
	createInput *domaindelivery.DeliveryPlanInput
	createCount int
	updateCount int
	updateErr   error
}

func (r *planRepository) CreateDeliveryPlan(_ context.Context, input domaindelivery.DeliveryPlanInput, createdBy string) (domaindelivery.DeliveryPlan, error) {
	r.createCount++
	r.createInput = &input
	plan := domaindelivery.DeliveryPlan{
		ID:                       firstNonEmpty(input.ID, "plan-created"),
		Source:                   firstNonEmpty(input.Source, domaindelivery.DeliveryPlanSourceManual),
		Status:                   domaindelivery.DeliveryPlanStatusDraft,
		ApplicationID:            input.ApplicationID,
		ApplicationName:          input.ApplicationName,
		ApplicationEnvironmentID: input.ApplicationEnvironmentID,
		EnvironmentKey:           input.EnvironmentKey,
		Action:                   input.Action,
		TargetID:                 input.TargetID,
		TargetSummary:            input.TargetSummary,
		BuildSourceID:            input.BuildSourceID,
		RefType:                  input.RefType,
		RefName:                  input.RefName,
		ImageTag:                 input.ImageTag,
		Reason:                   input.Reason,
		RiskLevel:                input.RiskLevel,
		RequiresApproval:         input.RequiresApproval,
		Impact:                   input.Impact,
		RollbackStrategy:         input.RollbackStrategy,
		Variables:                input.Variables,
		BuildArgs:                input.BuildArgs,
		CreatedBy:                createdBy,
	}
	r.plan = plan
	return plan, nil
}

func (r *planRepository) GetDeliveryPlan(context.Context, string) (domaindelivery.DeliveryPlan, error) {
	return r.plan, nil
}

func (r *planRepository) UpdateDeliveryPlan(_ context.Context, plan domaindelivery.DeliveryPlan) (domaindelivery.DeliveryPlan, error) {
	r.updateCount++
	if r.updateErr != nil {
		return domaindelivery.DeliveryPlan{}, r.updateErr
	}
	r.plan = plan
	return plan, nil
}

func TestGetDeliveryBlueprintUsageSummarizesCreatedApplicationRisk(t *testing.T) {
	repo := blueprintUsageRepository()
	service := New(
		stubApplicationReader{apps: []domainapp.App{
			{
				ID:             "app-1",
				Name:           "Payments API",
				Key:            "payments-api",
				Group:          "payments",
				BusinessLineID: "core",
				BuildSources: []domainapp.BuildSource{
					{ID: "source-1", Name: "Platform Build", Type: domainapp.BuildSourceTypePlatformTemplate},
				},
			},
			{ID: "app-2", Name: "Other", Key: "other"},
		}},
		stubCatalogReader{
			envs: []domaincatalog.Environment{
				{ID: "env-prod", Key: "prod", Name: "Production", IsProduction: true, RequiresApproval: true},
			},
			bindings: []domaincatalog.ApplicationEnvironment{
				{
					ID:             "binding-prod",
					ApplicationID:  "app-1",
					EnvironmentID:  "env-prod",
					EnvironmentKey: "prod",
					ReleasePolicy:  domaincatalog.ReleasePolicy{RequiresApproval: true},
					Targets:        []domaincatalog.ReleaseTarget{{ID: "target-1"}, {ID: "target-2"}},
				},
			},
		},
		stubBuildReader{listItems: []domainbuild.Record{
			{
				ID:            "build-1",
				ApplicationID: "app-1",
				SourceSystem:  "manual",
				Status:        "completed",
				Metadata:      map[string]any{"buildSourceId": "source-1"},
				CreatedAt:     time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC),
			},
		}},
		stubWorkflowReader{listItems: []domainworkflow.Run{
			{
				ID:            "workflow-1",
				ApplicationID: "app-1",
				WorkflowName:  "release",
				Status:        "running",
				Metadata:      map[string]any{"bindingId": "binding-prod"},
				CreatedAt:     "2026-05-08T10:10:00Z",
				UpdatedAt:     "2026-05-08T10:40:00Z",
			},
		}},
		stubReleaseReader{listItems: []domainrelease.Record{
			{
				ID:             "release-1",
				ApplicationID:  "app-1",
				ClusterID:      "cluster-a",
				Namespace:      "prod",
				DeploymentName: "payments",
				Status:         "failed",
				Metadata:       map[string]any{"applicationEnvironmentId": "binding-prod"},
				CreatedAt:      time.Date(2026, 5, 8, 10, 20, 0, 0, time.UTC),
			},
		}},
		repo,
		nil,
		nil,
		deliveryActionPermissions(appaccess.PermDeliveryApplicationsView),
	)

	usage, err := service.GetDeliveryBlueprintUsage(context.Background(), deliveryActionPrincipal(), "blueprint-1")
	if err != nil {
		t.Fatalf("GetDeliveryBlueprintUsage returned error: %v", err)
	}
	if usage.TemplateKind != domaincatalog.TemplateUsageKindBlueprint || usage.UsageCount != 1 || usage.ApplicationCount != 1 {
		t.Fatalf("unexpected blueprint usage counts: %#v", usage)
	}
	if usage.ProductionEnvironmentCount != 1 || usage.ApprovalBindingCount != 1 || usage.TargetCount != 2 {
		t.Fatalf("unexpected blueprint risk inputs: %#v", usage)
	}
	if usage.RiskLevel != domaincatalog.TemplateUsageRiskHigh || usage.RecommendedAction != "copy_template_before_editing" {
		t.Fatalf("expected high-risk recommendation, got %#v", usage)
	}
	if usage.FileKindCounts["dockerfile"] != 1 || usage.FileKindCounts["helm_values"] != 1 {
		t.Fatalf("unexpected file kind counts: %#v", usage.FileKindCounts)
	}
	states, ok := usage.LastExecutionSummary["stateCounts"].(map[string]int)
	if !ok {
		t.Fatalf("stateCounts has unexpected type: %T", usage.LastExecutionSummary["stateCounts"])
	}
	if states["succeeded"] != 1 || states["running"] != 3 || states["failed"] != 1 {
		t.Fatalf("unexpected blueprint runtime state counts: %#v", usage.LastExecutionSummary)
	}
}

func blueprintUsageRepository() stubRepository {
	return stubRepository{blueprint: domaindelivery.DeliveryBlueprint{
		ID: "blueprint-1", Key: "payments-standard", Name: "Payments Standard",
		ApplicationDraft: domaindelivery.BlueprintApplicationDraft{Key: "payments-api", Name: "Payments API"},
		EnvironmentBindings: []domaindelivery.BlueprintEnvironmentBindingTemplate{
			{EnvironmentKey: "dev"},
			{EnvironmentKey: "prod", ReleasePolicy: domaincatalog.ReleasePolicy{RequiresApproval: true}},
		},
		Files: []domaindelivery.BlueprintFileTemplate{
			{Path: "Dockerfile", Kind: "dockerfile", Required: true},
			{Path: "deploy/values.yaml", Kind: "helm_values", Required: true},
		},
	}, bundles: []domaindelivery.ReleaseBundle{{
		ID: "bundle-1", ApplicationID: "app-1", ApplicationEnvironmentID: "binding-prod", Version: "1.2.3", SourceType: "build", Status: "building",
		UpdatedAt: time.Date(2026, 5, 8, 10, 30, 0, 0, time.UTC),
	}}, tasks: []domaindelivery.ExecutionTask{{
		ID: "task-1", ApplicationID: "app-1", ApplicationEnvironmentID: "binding-prod", TaskKind: "release", Status: "running",
		UpdatedAt: time.Date(2026, 5, 8, 10, 45, 0, 0, time.UTC),
	}}}
}

func TestCreateDeliveryDraftDoesNotCreatePlatformObjects(t *testing.T) {
	appCreateCount := 0
	bindingCreateCount := 0
	repo := &draftRepository{}
	service := New(
		stubApplicationReader{createCount: &appCreateCount},
		stubCatalogReader{createCount: &bindingCreateCount},
		stubBuildReader{},
		stubWorkflowReader{},
		stubReleaseReader{},
		repo,
		nil,
		nil,
		deliveryActionPermissions(appaccess.PermDeliveryApplicationsUpdate),
	)

	result, err := service.CreateDeliveryDraft(context.Background(), deliveryActionPrincipal(), domaindelivery.DeliveryDraftInput{
		Source: domaindelivery.DeliveryDraftSourceManual,
		ApplicationDraft: domaindelivery.BlueprintApplicationDraft{
			Name:     "Demo API",
			Key:      "demo-api",
			Group:    "demo",
			Language: "go",
			Enabled:  true,
		},
		BuildSources: []domainapp.BuildSourceInput{
			{ID: "source-1", Name: "Repo Dockerfile", Type: domainapp.BuildSourceTypeRepoDockerfile, Enabled: true, IsDefault: true},
		},
	})
	if err != nil {
		t.Fatalf("CreateDeliveryDraft returned error: %v", err)
	}
	if result.Status != domaindelivery.DeliveryDraftStatusDraft {
		t.Fatalf("draft status = %q, want draft", result.Status)
	}
	if repo.createCount != 1 {
		t.Fatalf("draft create count = %d, want 1", repo.createCount)
	}
	if appCreateCount != 0 {
		t.Fatalf("application create count = %d, want 0 before confirmation", appCreateCount)
	}
	if bindingCreateCount != 0 {
		t.Fatalf("binding create count = %d, want 0 before confirmation", bindingCreateCount)
	}
}

func TestConfirmDeliveryDraftCreatesApplicationServicesAndBindings(t *testing.T) {
	appCreateCount := 0
	serviceCreateCount := 0
	bindingCreateCount := 0
	repo := &draftRepository{
		draft: domaindelivery.DeliveryDraft{
			ID:     "draft-1",
			Source: domaindelivery.DeliveryDraftSourceManual,
			Status: domaindelivery.DeliveryDraftStatusDraft,
			ApplicationDraft: domaindelivery.BlueprintApplicationDraft{
				Name:          "Demo API",
				Key:           "demo-api",
				Group:         "demo",
				Language:      "go",
				DefaultBranch: "main",
				Enabled:       true,
			},
			Services: []domaindelivery.DeliveryDraftService{
				{
					Key:           "api",
					Name:          "API",
					ServiceKind:   domainapp.ServiceKindKubernetesWorkload,
					BuildSourceID: "source-1",
					Enabled:       true,
					Containers: []domainapp.ServiceContainerInput{
						{Name: "api", ImageRepository: "registry.local/demo-api", DockerfilePath: "Dockerfile", BuildContextDir: "."},
					},
				},
			},
			BuildSources: []domainapp.BuildSourceInput{
				{ID: "source-1", Name: "Repo Dockerfile", Type: domainapp.BuildSourceTypeRepoDockerfile, Enabled: true, IsDefault: true},
			},
			EnvironmentBindings: []domaindelivery.BlueprintEnvironmentBindingTemplate{
				{EnvironmentKey: "dev"},
			},
		},
	}
	service := New(
		stubApplicationReader{
			app:                domainapp.App{ID: "app-created", Key: "other"},
			createCount:        &appCreateCount,
			createServiceCount: &serviceCreateCount,
		},
		stubCatalogReader{
			envs: []domaincatalog.Environment{
				{ID: "env-dev", Key: "dev", Name: "Development"},
			},
			createCount: &bindingCreateCount,
		},
		stubBuildReader{},
		stubWorkflowReader{},
		stubReleaseReader{},
		repo,
		nil,
		nil,
		deliveryActionPermissions(appaccess.PermDeliveryApplicationsUpdate),
	)

	result, err := service.ConfirmDeliveryDraft(context.Background(), deliveryActionPrincipal(), "draft-1")
	if err != nil {
		t.Fatalf("ConfirmDeliveryDraft returned error: %v", err)
	}
	if result.Draft.Status != domaindelivery.DeliveryDraftStatusConfirmed {
		t.Fatalf("draft status = %q, want confirmed", result.Draft.Status)
	}
	if result.Draft.ConfirmedAt == nil {
		t.Fatal("draft confirmedAt is nil")
	}
	if appCreateCount != 1 {
		t.Fatalf("application create count = %d, want 1", appCreateCount)
	}
	if serviceCreateCount != 1 {
		t.Fatalf("service create count = %d, want 1", serviceCreateCount)
	}
	if bindingCreateCount != 1 {
		t.Fatalf("binding create count = %d, want 1", bindingCreateCount)
	}
	if repo.updateCount != 2 {
		t.Fatalf("draft update count = %d, want 2", repo.updateCount)
	}
	if len(result.Spec.Services) != 1 {
		t.Fatalf("spec services length = %d, want 1", len(result.Spec.Services))
	}

	if _, err := service.ConfirmDeliveryDraft(context.Background(), deliveryActionPrincipal(), "draft-1"); err == nil {
		t.Fatal("ConfirmDeliveryDraft second call returned nil error, want already-confirmed error")
	}
}

func TestConfirmDeliveryDraftStopsWhenClaimUpdateFails(t *testing.T) {
	appCreateCount := 0
	serviceCreateCount := 0
	bindingCreateCount := 0
	repo := &draftRepository{
		draft: domaindelivery.DeliveryDraft{
			ID:     "draft-1",
			Source: domaindelivery.DeliveryDraftSourceManual,
			Status: domaindelivery.DeliveryDraftStatusDraft,
			ApplicationDraft: domaindelivery.BlueprintApplicationDraft{
				Name:          "Demo API",
				Key:           "demo-api",
				Group:         "demo",
				Language:      "go",
				DefaultBranch: "main",
				Enabled:       true,
			},
			Services: []domaindelivery.DeliveryDraftService{
				{
					Key:         "api",
					Name:        "API",
					ServiceKind: domainapp.ServiceKindKubernetesWorkload,
					Enabled:     true,
				},
			},
			EnvironmentBindings: []domaindelivery.BlueprintEnvironmentBindingTemplate{
				{EnvironmentKey: "dev"},
			},
		},
		updateErr: errors.New("claim update failed"),
	}
	service := New(
		stubApplicationReader{
			app:                domainapp.App{ID: "app-created", Key: "other"},
			createCount:        &appCreateCount,
			createServiceCount: &serviceCreateCount,
		},
		stubCatalogReader{
			envs: []domaincatalog.Environment{
				{ID: "env-dev", Key: "dev", Name: "Development"},
			},
			createCount: &bindingCreateCount,
		},
		stubBuildReader{},
		stubWorkflowReader{},
		stubReleaseReader{},
		repo,
		nil,
		nil,
		deliveryActionPermissions(appaccess.PermDeliveryApplicationsUpdate),
	)

	if _, err := service.ConfirmDeliveryDraft(context.Background(), deliveryActionPrincipal(), "draft-1"); err == nil {
		t.Fatal("ConfirmDeliveryDraft returned nil error, want claim update failure")
	}
	if repo.updateCount != 1 {
		t.Fatalf("draft update count = %d, want 1", repo.updateCount)
	}
	if appCreateCount != 0 {
		t.Fatalf("application create count = %d, want 0", appCreateCount)
	}
	if serviceCreateCount != 0 {
		t.Fatalf("service create count = %d, want 0", serviceCreateCount)
	}
	if bindingCreateCount != 0 {
		t.Fatalf("binding create count = %d, want 0", bindingCreateCount)
	}
}

func TestConfirmDeliveryDraftRestoresDraftStatusWhenApplyFails(t *testing.T) {
	createErr := errors.New("create application failed")
	appCreateCount := 0
	repo := &draftRepository{
		draft: domaindelivery.DeliveryDraft{
			ID:     "draft-1",
			Source: domaindelivery.DeliveryDraftSourceManual,
			Status: domaindelivery.DeliveryDraftStatusDraft,
			ApplicationDraft: domaindelivery.BlueprintApplicationDraft{
				Name:          "Demo API",
				Key:           "demo-api",
				Group:         "demo",
				Language:      "go",
				DefaultBranch: "main",
				Enabled:       true,
			},
		},
	}
	failingService := New(
		stubApplicationReader{
			app:         domainapp.App{ID: "app-created", Key: "other"},
			createErr:   createErr,
			createCount: &appCreateCount,
		},
		stubCatalogReader{},
		stubBuildReader{},
		stubWorkflowReader{},
		stubReleaseReader{},
		repo,
		nil,
		nil,
		deliveryActionPermissions(appaccess.PermDeliveryApplicationsUpdate),
	)

	if _, err := failingService.ConfirmDeliveryDraft(context.Background(), deliveryActionPrincipal(), "draft-1"); !errors.Is(err, createErr) {
		t.Fatalf("ConfirmDeliveryDraft error = %v, want create error", err)
	}
	if repo.draft.Status != domaindelivery.DeliveryDraftStatusDraft {
		t.Fatalf("draft status after failed confirm = %q, want draft", repo.draft.Status)
	}
	if repo.updateCount != 2 {
		t.Fatalf("draft update count after failed confirm = %d, want claim and restore updates", repo.updateCount)
	}
	if appCreateCount != 1 {
		t.Fatalf("application create count = %d, want 1", appCreateCount)
	}

	retryService := New(
		stubApplicationReader{app: domainapp.App{ID: "app-created", Key: "other"}},
		stubCatalogReader{},
		stubBuildReader{},
		stubWorkflowReader{},
		stubReleaseReader{},
		repo,
		nil,
		nil,
		deliveryActionPermissions(appaccess.PermDeliveryApplicationsUpdate),
	)
	result, err := retryService.ConfirmDeliveryDraft(context.Background(), deliveryActionPrincipal(), "draft-1")
	if err != nil {
		t.Fatalf("ConfirmDeliveryDraft retry returned error: %v", err)
	}
	if result.Draft.Status != domaindelivery.DeliveryDraftStatusConfirmed {
		t.Fatalf("draft status after retry = %q, want confirmed", result.Draft.Status)
	}
	if repo.updateCount != 4 {
		t.Fatalf("draft update count after retry = %d, want 4", repo.updateCount)
	}
}

func TestCreateDeliveryPlanRecordsRiskWithoutTriggeringAction(t *testing.T) {
	buildCount := 0
	repo := &planRepository{}
	service := New(
		stubApplicationReader{
			app: domainapp.App{ID: "app-1", Name: "Payments API", Key: "payments-api"},
		},
		stubCatalogReader{
			envs: []domaincatalog.Environment{
				{ID: "env-prod", Key: "prod", Name: "Production", IsProduction: true, RequiresApproval: true},
			},
			bindings: []domaincatalog.ApplicationEnvironment{
				{
					ID:             "binding-prod",
					ApplicationID:  "app-1",
					EnvironmentID:  "env-prod",
					EnvironmentKey: "prod",
					ReleasePolicy:  domaincatalog.ReleasePolicy{RequiresApproval: true, AutoRollback: true},
					Targets: []domaincatalog.ReleaseTarget{
						{ID: "target-1", ClusterID: "cluster-prod", Namespace: "payments", WorkloadKind: "Deployment", WorkloadName: "payments-api", Enabled: true},
					},
				},
			},
		},
		stubBuildReader{triggerCount: &buildCount},
		stubWorkflowReader{},
		stubReleaseReader{},
		repo,
		nil,
		nil,
		deliveryActionPermissions(appaccess.PermDeliveryApplicationsView),
	)

	plan, err := service.CreateDeliveryPlan(context.Background(), deliveryActionPrincipal(), domaindelivery.DeliveryPlanInput{
		ApplicationID:            "app-1",
		ApplicationEnvironmentID: "binding-prod",
		Action:                   domaindelivery.ApplicationDeliveryActionDeploy,
		TargetID:                 "target-1",
		RefType:                  "branch",
		RefName:                  "release/2026-06",
		Reason:                   "manual release plan",
	})
	if err != nil {
		t.Fatalf("CreateDeliveryPlan returned error: %v", err)
	}
	if repo.createCount != 1 {
		t.Fatalf("plan create count = %d, want 1", repo.createCount)
	}
	if buildCount != 0 {
		t.Fatalf("build trigger count = %d, want 0 before confirmation", buildCount)
	}
	if plan.RiskLevel != "high" {
		t.Fatalf("risk level = %q, want high", plan.RiskLevel)
	}
	if !plan.RequiresApproval {
		t.Fatal("requiresApproval = false, want true")
	}
	if plan.TargetSummary != "cluster-prod / payments / payments-api" {
		t.Fatalf("target summary = %q, want cluster/namespace/workload", plan.TargetSummary)
	}
}

func TestConfirmDeliveryPlanTriggersExistingAction(t *testing.T) {
	buildCount := 0
	repo := &planRepository{
		plan: domaindelivery.DeliveryPlan{
			ID:                       "plan-1",
			Source:                   domaindelivery.DeliveryPlanSourceManual,
			Status:                   domaindelivery.DeliveryPlanStatusDraft,
			ApplicationID:            "app-1",
			ApplicationEnvironmentID: "binding-1",
			Action:                   domaindelivery.ApplicationDeliveryActionBuild,
			BuildSourceID:            "source-1",
			RefType:                  "branch",
			RefName:                  "main",
			ImageTag:                 "candidate",
		},
	}
	service := New(
		stubApplicationReader{
			app: domainapp.App{
				ID:            "app-1",
				Name:          "Payments API",
				Key:           "payments-api",
				DefaultBranch: "main",
				BuildSources:  []domainapp.BuildSource{{ID: "source-1", Name: "Repo Dockerfile", IsDefault: true, DefaultTag: "candidate"}},
			},
		},
		stubCatalogReader{
			bindings: []domaincatalog.ApplicationEnvironment{
				{ID: "binding-1", ApplicationID: "app-1", EnvironmentID: "env-dev", EnvironmentKey: "dev", BuildPolicy: domaincatalog.BuildPolicy{SourceID: "source-1"}},
			},
		},
		stubBuildReader{triggerCount: &buildCount},
		stubWorkflowReader{},
		stubReleaseReader{},
		repo,
		nil,
		nil,
		deliveryActionPermissions(appaccess.PermDeliveryBuildsTrigger),
	)

	result, err := service.ConfirmDeliveryPlan(context.Background(), deliveryActionPrincipal(), "plan-1")
	if err != nil {
		t.Fatalf("ConfirmDeliveryPlan returned error: %v", err)
	}
	if buildCount != 1 {
		t.Fatalf("build trigger count = %d, want 1", buildCount)
	}
	if result.Plan.Status != domaindelivery.DeliveryPlanStatusConfirmed {
		t.Fatalf("plan status = %q, want confirmed", result.Plan.Status)
	}
	if result.Plan.ConfirmedAt == nil {
		t.Fatal("plan confirmedAt is nil")
	}
	if repo.updateCount != 2 {
		t.Fatalf("plan update count = %d, want 2", repo.updateCount)
	}
	if _, err := service.ConfirmDeliveryPlan(context.Background(), deliveryActionPrincipal(), "plan-1"); err == nil {
		t.Fatal("ConfirmDeliveryPlan second call returned nil error, want already-confirmed error")
	}
}

func TestDeliveryPlanApprovalBlocksExecutionUntilApproved(t *testing.T) {
	buildCount := 0
	repo := &planRepository{plan: domaindelivery.DeliveryPlan{
		ID: "plan-approval", Status: domaindelivery.DeliveryPlanStatusDraft,
		ApplicationID: "app-1", ApplicationEnvironmentID: "binding-1",
		Action: domaindelivery.ApplicationDeliveryActionBuild, BuildSourceID: "source-1",
		RefType: "branch", RefName: "main", ImageTag: "candidate", RequiresApproval: true,
		Impact: map[string]any{"requiresApproval": true},
	}}
	service := New(
		stubApplicationReader{app: domainapp.App{ID: "app-1", Name: "Payments API", Key: "payments-api", DefaultBranch: "main", BuildSources: []domainapp.BuildSource{{ID: "source-1", IsDefault: true}}}},
		stubCatalogReader{bindings: []domaincatalog.ApplicationEnvironment{{ID: "binding-1", ApplicationID: "app-1", EnvironmentID: "env-prod", BuildPolicy: domaincatalog.BuildPolicy{SourceID: "source-1"}}}},
		stubBuildReader{triggerCount: &buildCount}, stubWorkflowReader{}, stubReleaseReader{}, repo, nil, nil,
		deliveryActionPermissions(appaccess.PermDeliveryBuildsTrigger, appaccess.PermDeliveryApplicationsUpdate),
	)

	result, err := service.ConfirmDeliveryPlan(context.Background(), deliveryActionPrincipal(), repo.plan.ID)
	if err != nil {
		t.Fatalf("ConfirmDeliveryPlan request approval returned error: %v", err)
	}
	if result.Plan.Status != domaindelivery.DeliveryPlanStatusWaitingApproval || buildCount != 0 {
		t.Fatalf("approval request status/build count = %q/%d, want waiting_approval/0", result.Plan.Status, buildCount)
	}
	if _, err := service.ConfirmDeliveryPlan(context.Background(), deliveryActionPrincipal(), repo.plan.ID); err == nil {
		t.Fatal("ConfirmDeliveryPlan while waiting approval returned nil error")
	}
	approved, err := service.DecideDeliveryPlanApproval(context.Background(), deliveryActionPrincipal(), repo.plan.ID, domaindelivery.DeliveryPlanApprovalInput{Action: "approve", Comment: "change reviewed"})
	if err != nil {
		t.Fatalf("DecideDeliveryPlanApproval returned error: %v", err)
	}
	if approved.Status != domaindelivery.DeliveryPlanStatusDraft || !deliveryPlanApprovalGranted(approved) {
		t.Fatalf("approved plan state = %q impact=%#v", approved.Status, approved.Impact)
	}
	result, err = service.ConfirmDeliveryPlan(context.Background(), deliveryActionPrincipal(), repo.plan.ID)
	if err != nil {
		t.Fatalf("ConfirmDeliveryPlan after approval returned error: %v", err)
	}
	if result.Plan.Status != domaindelivery.DeliveryPlanStatusConfirmed || buildCount != 1 {
		t.Fatalf("confirmed plan status/build count = %q/%d, want confirmed/1", result.Plan.Status, buildCount)
	}
}

func TestDeliveryPlanApprovalRejectKeepsExecutionBlocked(t *testing.T) {
	repo := &planRepository{plan: domaindelivery.DeliveryPlan{ID: "plan-reject", Status: domaindelivery.DeliveryPlanStatusWaitingApproval, RequiresApproval: true, Impact: map[string]any{"approval": []any{map[string]any{"status": "requested"}}}}}
	service := New(stubApplicationReader{}, stubCatalogReader{}, stubBuildReader{}, stubWorkflowReader{}, stubReleaseReader{}, repo, nil, nil, deliveryActionPermissions(appaccess.PermDeliveryApplicationsUpdate))
	plan, err := service.DecideDeliveryPlanApproval(context.Background(), deliveryActionPrincipal(), repo.plan.ID, domaindelivery.DeliveryPlanApprovalInput{Action: "reject", Comment: "missing evidence"})
	if err != nil {
		t.Fatalf("DecideDeliveryPlanApproval reject returned error: %v", err)
	}
	if plan.Status != domaindelivery.DeliveryPlanStatusDraft || deliveryPlanApprovalGranted(plan) {
		t.Fatalf("rejected plan state = %q impact=%#v", plan.Status, plan.Impact)
	}
}

func TestConfirmDeliveryPlanStopsWhenClaimUpdateFails(t *testing.T) {
	buildCount := 0
	repo := &planRepository{
		plan: domaindelivery.DeliveryPlan{
			ID:                       "plan-1",
			Source:                   domaindelivery.DeliveryPlanSourceManual,
			Status:                   domaindelivery.DeliveryPlanStatusDraft,
			ApplicationID:            "app-1",
			ApplicationEnvironmentID: "binding-1",
			Action:                   domaindelivery.ApplicationDeliveryActionBuild,
			BuildSourceID:            "source-1",
			RefType:                  "branch",
			RefName:                  "main",
			ImageTag:                 "candidate",
		},
		updateErr: errors.New("claim update failed"),
	}
	service := New(
		stubApplicationReader{
			app: domainapp.App{
				ID:            "app-1",
				Name:          "Payments API",
				Key:           "payments-api",
				DefaultBranch: "main",
				BuildSources:  []domainapp.BuildSource{{ID: "source-1", Name: "Repo Dockerfile", IsDefault: true, DefaultTag: "candidate"}},
			},
		},
		stubCatalogReader{
			bindings: []domaincatalog.ApplicationEnvironment{
				{ID: "binding-1", ApplicationID: "app-1", EnvironmentID: "env-dev", EnvironmentKey: "dev", BuildPolicy: domaincatalog.BuildPolicy{SourceID: "source-1"}},
			},
		},
		stubBuildReader{triggerCount: &buildCount},
		stubWorkflowReader{},
		stubReleaseReader{},
		repo,
		nil,
		nil,
		deliveryActionPermissions(appaccess.PermDeliveryBuildsTrigger),
	)

	if _, err := service.ConfirmDeliveryPlan(context.Background(), deliveryActionPrincipal(), "plan-1"); err == nil {
		t.Fatal("ConfirmDeliveryPlan returned nil error, want claim update failure")
	}
	if repo.updateCount != 1 {
		t.Fatalf("plan update count = %d, want 1", repo.updateCount)
	}
	if buildCount != 0 {
		t.Fatalf("build trigger count = %d, want 0", buildCount)
	}
}

func TestConfirmDeliveryPlanRestoresDraftStatusWhenTriggerFails(t *testing.T) {
	triggerErr := errors.New("trigger build failed")
	buildCount := 0
	repo := &planRepository{
		plan: domaindelivery.DeliveryPlan{
			ID:                       "plan-1",
			Source:                   domaindelivery.DeliveryPlanSourceManual,
			Status:                   domaindelivery.DeliveryPlanStatusDraft,
			ApplicationID:            "app-1",
			ApplicationEnvironmentID: "binding-1",
			Action:                   domaindelivery.ApplicationDeliveryActionBuild,
			BuildSourceID:            "source-1",
			RefType:                  "branch",
			RefName:                  "main",
			ImageTag:                 "candidate",
		},
	}
	appReader := stubApplicationReader{
		app: domainapp.App{
			ID:            "app-1",
			Name:          "Payments API",
			Key:           "payments-api",
			DefaultBranch: "main",
			BuildSources:  []domainapp.BuildSource{{ID: "source-1", Name: "Repo Dockerfile", IsDefault: true, DefaultTag: "candidate"}},
		},
	}
	catalogReader := stubCatalogReader{
		bindings: []domaincatalog.ApplicationEnvironment{
			{ID: "binding-1", ApplicationID: "app-1", EnvironmentID: "env-dev", EnvironmentKey: "dev", BuildPolicy: domaincatalog.BuildPolicy{SourceID: "source-1"}},
		},
	}
	failingService := New(
		appReader,
		catalogReader,
		stubBuildReader{triggerCount: &buildCount, triggerErr: triggerErr},
		stubWorkflowReader{},
		stubReleaseReader{},
		repo,
		nil,
		nil,
		deliveryActionPermissions(appaccess.PermDeliveryBuildsTrigger),
	)

	if _, err := failingService.ConfirmDeliveryPlan(context.Background(), deliveryActionPrincipal(), "plan-1"); !errors.Is(err, triggerErr) {
		t.Fatalf("ConfirmDeliveryPlan error = %v, want trigger error", err)
	}
	if repo.plan.Status != domaindelivery.DeliveryPlanStatusDraft {
		t.Fatalf("plan status after failed confirm = %q, want draft", repo.plan.Status)
	}
	if repo.updateCount != 2 {
		t.Fatalf("plan update count after failed confirm = %d, want claim and restore updates", repo.updateCount)
	}
	if buildCount != 1 {
		t.Fatalf("build trigger count after failed confirm = %d, want 1", buildCount)
	}

	retryService := New(
		appReader,
		catalogReader,
		stubBuildReader{triggerCount: &buildCount},
		stubWorkflowReader{},
		stubReleaseReader{},
		repo,
		nil,
		nil,
		deliveryActionPermissions(appaccess.PermDeliveryBuildsTrigger),
	)
	result, err := retryService.ConfirmDeliveryPlan(context.Background(), deliveryActionPrincipal(), "plan-1")
	if err != nil {
		t.Fatalf("ConfirmDeliveryPlan retry returned error: %v", err)
	}
	if result.Plan.Status != domaindelivery.DeliveryPlanStatusConfirmed {
		t.Fatalf("plan status after retry = %q, want confirmed", result.Plan.Status)
	}
	if repo.updateCount != 4 {
		t.Fatalf("plan update count after retry = %d, want 4", repo.updateCount)
	}
	if buildCount != 2 {
		t.Fatalf("build trigger count after retry = %d, want 2", buildCount)
	}
}

func TestGetApplicationDetailIncludesBindingTargets(t *testing.T) {
	service := New(
		stubApplicationReader{
			app: domainapp.App{
				ID:   "app-1",
				Name: "demo",
				BuildSources: []domainapp.BuildSource{
					{ID: "build-source-1", Name: "Repo Dockerfile", IsDefault: true},
				},
			},
		},
		stubCatalogReader{
			envs: []domaincatalog.Environment{
				{ID: "env-1", Name: "test"},
			},
			bindings: []domaincatalog.ApplicationEnvironment{
				{
					ID:             "binding-1",
					ApplicationID:  "app-1",
					EnvironmentID:  "env-1",
					EnvironmentKey: "test",
					BuildPolicy:    domaincatalog.BuildPolicy{SourceID: "build-source-1"},
					Targets: []domaincatalog.ReleaseTarget{
						{
							ID:           "target-1",
							ClusterID:    "cluster-a",
							Namespace:    "namespace-a",
							WorkloadKind: "Deployment",
							WorkloadName: "demo-api",
							Enabled:      true,
						},
					},
				},
			},
		},
		stubBuildReader{},
		stubWorkflowReader{},
		stubReleaseReader{},
		stubRepository{},
		nil,
		nil,
		nil,
	)

	result, err := service.GetApplicationDetail(context.Background(), domainidentity.Principal{}, "app-1")
	if err != nil {
		t.Fatalf("GetApplicationDetail returned error: %v", err)
	}
	if len(result.Bindings) != 1 {
		t.Fatalf("Bindings length = %d, want 1", len(result.Bindings))
	}
	if result.Bindings[0].TargetCount != 1 {
		t.Fatalf("TargetCount = %d, want 1", result.Bindings[0].TargetCount)
	}
	if len(result.Bindings[0].Targets) != 1 {
		t.Fatalf("Targets length = %d, want 1", len(result.Bindings[0].Targets))
	}
	target := result.Bindings[0].Targets[0]
	if target.ClusterID != "cluster-a" || target.Namespace != "namespace-a" || target.WorkloadName != "demo-api" {
		t.Fatalf("Targets = %+v, want cluster/namespace/workload summary", target)
	}
}

func TestGetApplicationWorkloadRuntimeDetailReturnsNotFoundWhenWorkloadMissing(t *testing.T) {
	service := New(
		stubApplicationReader{
			app: domainapp.App{
				ID:   "app-1",
				Name: "demo",
			},
		},
		stubCatalogReader{
			bindings: []domaincatalog.ApplicationEnvironment{
				{
					ID:             "binding-1",
					ApplicationID:  "app-1",
					EnvironmentID:  "env-1",
					EnvironmentKey: "prod",
					Targets: []domaincatalog.ReleaseTarget{
						{
							ID:           "target-1",
							ClusterID:    "cluster-a",
							Namespace:    "namespace-a",
							WorkloadKind: "Deployment",
							WorkloadName: "expected-workload",
							Enabled:      true,
						},
					},
				},
			},
		},
		stubBuildReader{},
		stubWorkflowReader{},
		stubReleaseReader{},
		stubRepository{},
		nil,
		stubTargetReader{
			deployments: map[string][]domainresource.DeploymentView{
				"cluster-a/namespace-a": {
					{
						Name: "different-workload",
					},
				},
			},
		},
		deliveryActionPermissions(appaccess.PermDeliveryApplicationsView),
	)

	_, err := service.GetApplicationWorkloadRuntimeDetail(context.Background(), domainidentity.Principal{}, "app-1", "binding-1", "expected-workload")
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("GetApplicationWorkloadRuntimeDetail error = %v, want ErrNotFound", err)
	}
}

func TestTriggerApplicationDeliveryActionBuildDeployTriggersWorkflowWithBindingDefaults(t *testing.T) {
	var workflowInput domainworkflow.Input
	buildCount := 0
	releaseCount := 0
	workflowCount := 0
	service := New(
		stubApplicationReader{
			app: domainapp.App{
				ID:            "app-1",
				Name:          "demo",
				DefaultBranch: "develop",
				DefaultTag:    "stable",
				BuildImage:    "registry.local/demo",
				BuildSources: []domainapp.BuildSource{
					{ID: "source-1", Name: "Repo Dockerfile", IsDefault: true, DefaultTag: "candidate"},
				},
			},
		},
		stubCatalogReader{
			bindings: []domaincatalog.ApplicationEnvironment{
				{
					ID:            "binding-1",
					ApplicationID: "app-1",
					BuildPolicy: domaincatalog.BuildPolicy{
						SourceID:  "source-1",
						RefType:   "branch",
						RefValue:  "release/1.0",
						BuildArgs: map[string]any{"BASE_IMAGE": "golang:1.22"},
						Variables: map[string]any{"ENV": "test"},
					},
					WorkflowTemplateID: "wf-1",
					WorkflowTemplate: &domaincatalog.WorkflowTemplate{
						ID:         "wf-1",
						Name:       "Release DAG",
						Definition: map[string]any{"mode": "release_dag", "nodes": []map[string]any{{"id": "build", "name": "Build", "type": "build"}}},
					},
					Targets: []domaincatalog.ReleaseTarget{
						{
							ID:            "target-1",
							ClusterID:     "cluster-a",
							Namespace:     "namespace-a",
							WorkloadKind:  "Deployment",
							WorkloadName:  "demo-api",
							ContainerName: "api",
							Enabled:       true,
						},
					},
				},
			},
		},
		stubBuildReader{triggerCount: &buildCount},
		stubWorkflowReader{triggerInput: &workflowInput, triggerCount: &workflowCount, record: domainworkflow.Run{ID: "workflow-1"}},
		stubReleaseReader{triggerCount: &releaseCount},
		stubRepository{},
		nil,
		nil,
		deliveryActionPermissions(appaccess.PermDeliveryBuildsTrigger, appaccess.PermDeliveryWorkflowsTrigger),
	)

	result, err := service.TriggerApplicationDeliveryAction(context.Background(), deliveryActionPrincipal(), "app-1", domaindelivery.ApplicationDeliveryActionInput{
		ApplicationEnvironmentID: "binding-1",
		BuildArgs:                map[string]any{"BASE_IMAGE": "golang:1.23"},
		Variables:                map[string]any{"COMMIT_SHA": "abc"},
	})
	if err != nil {
		t.Fatalf("TriggerApplicationDeliveryAction returned error: %v", err)
	}
	if result.Action != domaindelivery.ApplicationDeliveryActionBuildDeploy {
		t.Fatalf("Action = %q, want build_deploy", result.Action)
	}
	if workflowCount != 1 || buildCount != 0 || releaseCount != 0 {
		t.Fatalf("trigger counts workflow=%d build=%d release=%d, want 1/0/0", workflowCount, buildCount, releaseCount)
	}
	if workflowInput.WorkflowName != "Release DAG" || workflowInput.BuildSourceID != "source-1" || workflowInput.RefName != "release/1.0" || workflowInput.ImageTag != "candidate" {
		t.Fatalf("workflow input = %+v, want binding workflow/build defaults", workflowInput)
	}
	if !workflowInput.TriggerBuild || !workflowInput.TriggerRelease || workflowInput.ValidationOnly {
		t.Fatalf("workflow flags = build:%v release:%v validation:%v, want build/release workflow", workflowInput.TriggerBuild, workflowInput.TriggerRelease, workflowInput.ValidationOnly)
	}
	if workflowInput.BuildArgs["BASE_IMAGE"] != "golang:1.23" || workflowInput.Variables["ENV"] != "test" || workflowInput.Variables["COMMIT_SHA"] != "abc" {
		t.Fatalf("workflow args/variables = %+v / %+v, want binding defaults with input overrides", workflowInput.BuildArgs, workflowInput.Variables)
	}
	if result.Workflow == nil || result.Workflow.ID != "workflow-1" {
		t.Fatalf("Workflow = %+v, want triggered workflow", result.Workflow)
	}
	if result.RelatedIDs.WorkflowRunID != "workflow-1" {
		t.Fatalf("RelatedIDs.WorkflowRunID = %q, want workflow-1", result.RelatedIDs.WorkflowRunID)
	}
}

func TestTriggerApplicationDeliveryActionBuildDoesNotRequireReleaseTarget(t *testing.T) {
	buildCount := 0
	service := New(
		stubApplicationReader{app: domainapp.App{ID: "app-1", Name: "demo", DefaultBranch: "main"}},
		stubCatalogReader{
			bindings: []domaincatalog.ApplicationEnvironment{{ID: "binding-1", ApplicationID: "app-1"}},
		},
		stubBuildReader{triggerCount: &buildCount, record: domainbuild.Record{ID: "build-1"}},
		stubWorkflowReader{},
		stubReleaseReader{},
		stubRepository{},
		nil,
		nil,
		deliveryActionPermissions(appaccess.PermDeliveryBuildsTrigger),
	)

	result, err := service.TriggerApplicationDeliveryAction(context.Background(), deliveryActionPrincipal(), "app-1", domaindelivery.ApplicationDeliveryActionInput{
		Action:                   domaindelivery.ApplicationDeliveryActionBuild,
		ApplicationEnvironmentID: "binding-1",
	})
	if err != nil {
		t.Fatalf("TriggerApplicationDeliveryAction returned error: %v", err)
	}
	if buildCount != 1 {
		t.Fatalf("build trigger count = %d, want 1", buildCount)
	}
	if result.Target != nil {
		t.Fatalf("Target = %+v, want nil for build-only action without targets", result.Target)
	}
}

func TestTriggerApplicationDeliveryActionBuildRequiresBuildPermission(t *testing.T) {
	buildCount := 0
	service := New(
		stubApplicationReader{app: domainapp.App{ID: "app-1", Name: "demo", DefaultBranch: "main"}},
		stubCatalogReader{
			bindings: []domaincatalog.ApplicationEnvironment{{ID: "binding-1", ApplicationID: "app-1"}},
		},
		stubBuildReader{triggerCount: &buildCount},
		stubWorkflowReader{},
		stubReleaseReader{},
		stubRepository{},
		nil,
		nil,
		deliveryActionPermissions(appaccess.PermDeliveryWorkflowsTrigger),
	)

	_, err := service.TriggerApplicationDeliveryAction(context.Background(), deliveryActionPrincipal(), "app-1", domaindelivery.ApplicationDeliveryActionInput{
		Action:                   domaindelivery.ApplicationDeliveryActionBuild,
		ApplicationEnvironmentID: "binding-1",
	})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("error = %v, want access denied", err)
	}
	if buildCount != 0 {
		t.Fatalf("build trigger count = %d, want 0", buildCount)
	}
}

func TestTriggerApplicationDeliveryActionBuildDeployRequiresWorkflowPermissionBeforeWork(t *testing.T) {
	buildCount := 0
	releaseCount := 0
	workflowCount := 0
	service := New(
		stubApplicationReader{app: domainapp.App{ID: "app-1", Name: "demo", DefaultBranch: "main"}},
		stubCatalogReader{
			bindings: []domaincatalog.ApplicationEnvironment{
				{
					ID:                 "binding-1",
					ApplicationID:      "app-1",
					WorkflowTemplateID: "wf-1",
					WorkflowTemplate:   &domaincatalog.WorkflowTemplate{ID: "wf-1", Name: "Release DAG", Definition: map[string]any{"mode": "release_dag", "nodes": []map[string]any{{"id": "build", "name": "Build", "type": "build"}}}},
					Targets: []domaincatalog.ReleaseTarget{
						{ID: "target-1", ClusterID: "cluster-a", Namespace: "namespace-a", WorkloadKind: "Deployment", WorkloadName: "demo-api", Enabled: true},
					},
				},
			},
		},
		stubBuildReader{triggerCount: &buildCount},
		stubWorkflowReader{triggerCount: &workflowCount},
		stubReleaseReader{triggerCount: &releaseCount},
		stubRepository{},
		nil,
		nil,
		deliveryActionPermissions(appaccess.PermDeliveryBuildsTrigger),
	)

	_, err := service.TriggerApplicationDeliveryAction(context.Background(), deliveryActionPrincipal(), "app-1", domaindelivery.ApplicationDeliveryActionInput{
		Action:                   domaindelivery.ApplicationDeliveryActionBuildDeploy,
		ApplicationEnvironmentID: "binding-1",
	})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("error = %v, want access denied", err)
	}
	if workflowCount != 0 || buildCount != 0 || releaseCount != 0 {
		t.Fatalf("trigger counts workflow=%d build=%d release=%d, want 0/0/0 before permission failure", workflowCount, buildCount, releaseCount)
	}
}

func TestTriggerApplicationDeliveryActionWorkflowAllowsMissingTemplate(t *testing.T) {
	workflowCount := 0
	var workflowInput domainworkflow.Input
	service := New(
		stubApplicationReader{app: domainapp.App{ID: "app-1", Name: "demo", DefaultBranch: "main", DefaultTag: "stable"}},
		stubCatalogReader{
			bindings: []domaincatalog.ApplicationEnvironment{
				{
					ID:            "binding-1",
					ApplicationID: "app-1",
					Targets: []domaincatalog.ReleaseTarget{
						{ID: "target-1", ClusterID: "cluster-a", Namespace: "namespace-a", WorkloadKind: "Deployment", WorkloadName: "demo-api", Enabled: true},
					},
				},
			},
		},
		stubBuildReader{},
		stubWorkflowReader{triggerInput: &workflowInput, triggerCount: &workflowCount, record: domainworkflow.Run{ID: "workflow-1"}},
		stubReleaseReader{},
		stubRepository{},
		nil,
		nil,
		deliveryActionPermissions(appaccess.PermDeliveryWorkflowsTrigger),
	)

	result, err := service.TriggerApplicationDeliveryAction(context.Background(), deliveryActionPrincipal(), "app-1", domaindelivery.ApplicationDeliveryActionInput{
		Action:                   domaindelivery.ApplicationDeliveryActionWorkflow,
		ApplicationEnvironmentID: "binding-1",
	})
	if err != nil {
		t.Fatalf("TriggerApplicationDeliveryAction returned error: %v", err)
	}
	if workflowCount != 1 {
		t.Fatalf("workflow trigger count = %d, want 1", workflowCount)
	}
	if workflowInput.WorkflowName != "build-release-verify" || !workflowInput.TriggerBuild || workflowInput.TriggerRelease || workflowInput.ValidationOnly {
		t.Fatalf("workflow input = %+v, want fallback workflow trigger without release/validation flags", workflowInput)
	}
	if result.Workflow == nil || result.Workflow.ID != "workflow-1" {
		t.Fatalf("Workflow = %+v, want triggered workflow", result.Workflow)
	}
}

func TestTriggerApplicationDeliveryActionVerifyRunsWorkflowOnly(t *testing.T) {
	buildCount := 0
	releaseCount := 0
	workflowCount := 0
	var workflowInput domainworkflow.Input
	service := New(
		stubApplicationReader{app: domainapp.App{ID: "app-1", Name: "demo", DefaultBranch: "main"}},
		stubCatalogReader{
			bindings: []domaincatalog.ApplicationEnvironment{
				{
					ID:                 "binding-1",
					ApplicationID:      "app-1",
					WorkflowTemplateID: "verify-template",
					WorkflowTemplate: &domaincatalog.WorkflowTemplate{
						ID:         "verify-template",
						Name:       "verify-template",
						Definition: map[string]any{"mode": "release_dag", "nodes": []map[string]any{{"id": "check", "name": "Check", "type": "check_http"}}},
					},
					Targets: []domaincatalog.ReleaseTarget{
						{ID: "target-1", ClusterID: "cluster-a", Namespace: "namespace-a", WorkloadKind: "Deployment", WorkloadName: "demo-api", Enabled: true},
					},
				},
			},
		},
		stubBuildReader{triggerCount: &buildCount},
		stubWorkflowReader{triggerInput: &workflowInput, triggerCount: &workflowCount, record: domainworkflow.Run{ID: "workflow-1"}},
		stubReleaseReader{triggerCount: &releaseCount},
		stubRepository{},
		nil,
		nil,
		deliveryActionPermissions(appaccess.PermDeliveryWorkflowsTrigger),
	)

	result, err := service.TriggerApplicationDeliveryAction(context.Background(), deliveryActionPrincipal(), "app-1", domaindelivery.ApplicationDeliveryActionInput{
		Action:                   domaindelivery.ApplicationDeliveryActionVerify,
		ApplicationEnvironmentID: "binding-1",
	})
	if err != nil {
		t.Fatalf("TriggerApplicationDeliveryAction returned error: %v", err)
	}
	if workflowCount != 1 || buildCount != 0 || releaseCount != 0 {
		t.Fatalf("trigger counts workflow=%d build=%d release=%d, want 1/0/0", workflowCount, buildCount, releaseCount)
	}
	if workflowInput.WorkflowName != "verify-template" || workflowInput.TriggerBuild || workflowInput.TriggerRelease || !workflowInput.ValidationOnly {
		t.Fatalf("workflow input = %+v, want verify workflow without build/release flags", workflowInput)
	}
	if result.Workflow == nil || result.Workflow.ID != "workflow-1" {
		t.Fatalf("Workflow = %+v, want triggered workflow", result.Workflow)
	}
}

func TestTriggerApplicationDeliveryActionRollbackRunsRollbackWorkflowOnly(t *testing.T) {
	buildCount := 0
	releaseCount := 0
	workflowCount := 0
	var workflowInput domainworkflow.Input
	service := New(
		stubApplicationReader{app: domainapp.App{ID: "app-1", Name: "demo", DefaultBranch: "main"}},
		stubCatalogReader{
			bindings: []domaincatalog.ApplicationEnvironment{
				{
					ID:                 "binding-1",
					ApplicationID:      "app-1",
					WorkflowTemplateID: "rollback-template",
					WorkflowTemplate: &domaincatalog.WorkflowTemplate{
						ID:   "rollback-template",
						Name: "rollback-template",
						Definition: map[string]any{
							"mode": "release_dag",
							"nodes": []map[string]any{
								{"id": "build", "name": "Build", "type": "build"},
								{"id": "rollback", "name": "Rollback", "type": "rollback_to_previous"},
							},
						},
					},
					Targets: []domaincatalog.ReleaseTarget{
						{ID: "target-1", ClusterID: "cluster-a", Namespace: "namespace-a", WorkloadKind: "Deployment", WorkloadName: "demo-api", Enabled: true},
					},
				},
			},
		},
		stubBuildReader{triggerCount: &buildCount},
		stubWorkflowReader{triggerInput: &workflowInput, triggerCount: &workflowCount, record: domainworkflow.Run{ID: "workflow-rollback-1"}},
		stubReleaseReader{triggerCount: &releaseCount},
		stubRepository{},
		nil,
		nil,
		deliveryActionPermissions(appaccess.PermDeliveryWorkflowsTrigger),
	)

	result, err := service.TriggerApplicationDeliveryAction(context.Background(), deliveryActionPrincipal(), "app-1", domaindelivery.ApplicationDeliveryActionInput{
		Action:                   domaindelivery.ApplicationDeliveryActionRollback,
		ApplicationEnvironmentID: "binding-1",
		ReleaseBundleID:          "bundle-prev",
		Variables:                map[string]any{"reason": "bad deploy"},
	})
	if err != nil {
		t.Fatalf("TriggerApplicationDeliveryAction returned error: %v", err)
	}
	if workflowCount != 1 || buildCount != 0 || releaseCount != 0 {
		t.Fatalf("trigger counts workflow=%d build=%d release=%d, want 1/0/0", workflowCount, buildCount, releaseCount)
	}
	if !workflowInput.RollbackOnly || workflowInput.TriggerBuild || workflowInput.TriggerRelease || workflowInput.ValidationOnly {
		t.Fatalf("workflow flags = rollback:%v build:%v release:%v validation:%v, want rollback-only workflow", workflowInput.RollbackOnly, workflowInput.TriggerBuild, workflowInput.TriggerRelease, workflowInput.ValidationOnly)
	}
	if workflowInput.Variables["releaseBundleId"] != "bundle-prev" || workflowInput.Variables["reason"] != "bad deploy" {
		t.Fatalf("workflow variables = %+v, want rollback bundle and caller reason", workflowInput.Variables)
	}
	if result.Workflow == nil || result.Workflow.ID != "workflow-rollback-1" {
		t.Fatalf("Workflow = %+v, want rollback workflow", result.Workflow)
	}
	if result.RelatedIDs.WorkflowRunID != "workflow-rollback-1" {
		t.Fatalf("RelatedIDs.WorkflowRunID = %q, want workflow-rollback-1", result.RelatedIDs.WorkflowRunID)
	}
}

func TestTriggerApplicationDeliveryActionRollbackRequiresWorkflowPermission(t *testing.T) {
	workflowCount := 0
	service := New(
		stubApplicationReader{app: domainapp.App{ID: "app-1", Name: "demo"}},
		stubCatalogReader{bindings: []domaincatalog.ApplicationEnvironment{
			{
				ID:                 "binding-1",
				ApplicationID:      "app-1",
				WorkflowTemplateID: "rollback-template",
				WorkflowTemplate:   &domaincatalog.WorkflowTemplate{ID: "rollback-template", Name: "rollback-template", Definition: map[string]any{"mode": "release_dag", "nodes": []map[string]any{{"id": "rollback", "name": "Rollback", "type": "rollback_to_previous"}}}},
				Targets:            []domaincatalog.ReleaseTarget{{ID: "target-1", ClusterID: "cluster-a", Namespace: "namespace-a", WorkloadName: "demo-api", Enabled: true}},
			},
		}},
		stubBuildReader{},
		stubWorkflowReader{triggerCount: &workflowCount},
		stubReleaseReader{},
		stubRepository{},
		nil,
		nil,
		deliveryActionPermissions(appaccess.PermDeliveryReleasesTrigger),
	)

	_, err := service.TriggerApplicationDeliveryAction(context.Background(), deliveryActionPrincipal(), "app-1", domaindelivery.ApplicationDeliveryActionInput{
		Action:                   domaindelivery.ApplicationDeliveryActionRollback,
		ApplicationEnvironmentID: "binding-1",
	})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("error = %v, want access denied", err)
	}
	if workflowCount != 0 {
		t.Fatalf("workflow trigger count = %d, want 0", workflowCount)
	}
}

func TestTriggerApplicationDeliveryActionDeployTriggersRelease(t *testing.T) {
	var releaseInput domainrelease.TriggerInput
	releaseCount := 0
	service := New(
		stubApplicationReader{app: domainapp.App{
			ID:            "app-1",
			Name:          "demo",
			DefaultBranch: "main",
			DefaultTag:    "stable",
			BuildImage:    "registry.local/demo",
			BuildSources: []domainapp.BuildSource{
				{ID: "source-1", Name: "Repo Dockerfile", IsDefault: true, BuildImage: "registry.local/source-demo", DefaultTag: "candidate"},
			},
		}},
		stubCatalogReader{
			bindings: []domaincatalog.ApplicationEnvironment{
				{
					ID:            "binding-1",
					ApplicationID: "app-1",
					BuildPolicy:   domaincatalog.BuildPolicy{SourceID: "source-1"},
					Targets: []domaincatalog.ReleaseTarget{
						{ID: "target-1", ClusterID: "cluster-a", Namespace: "namespace-a", WorkloadKind: "Deployment", WorkloadName: "demo-api", ContainerName: "api", Enabled: true},
					},
				},
			},
		},
		stubBuildReader{},
		stubWorkflowReader{},
		stubReleaseReader{
			triggerInput: &releaseInput,
			triggerCount: &releaseCount,
			record:       domainrelease.Record{ID: "release-1", Metadata: map[string]any{"releaseBundleId": "bundle-1", "executionTaskId": "task-1"}},
		},
		stubRepository{},
		nil,
		nil,
		deliveryActionPermissions(appaccess.PermDeliveryReleasesTrigger),
	)

	result, err := service.TriggerApplicationDeliveryAction(context.Background(), deliveryActionPrincipal(), "app-1", domaindelivery.ApplicationDeliveryActionInput{
		Action:                   domaindelivery.ApplicationDeliveryActionDeploy,
		ApplicationEnvironmentID: "binding-1",
		ReleaseName:              "release-candidate",
	})
	if err != nil {
		t.Fatalf("TriggerApplicationDeliveryAction returned error: %v", err)
	}
	if releaseCount != 1 {
		t.Fatalf("release trigger count = %d, want 1", releaseCount)
	}
	if releaseInput.ImageTag != "candidate" || releaseInput.Image != "registry.local/source-demo:candidate" || releaseInput.ContainerName != "api" || releaseInput.ReleaseName != "release-candidate" {
		t.Fatalf("release input = %+v, want target/default image values", releaseInput)
	}
	if result.Release == nil || result.Release.ID != "release-1" {
		t.Fatalf("Release = %+v, want release-1", result.Release)
	}
	if result.RelatedIDs.ReleaseBundleID != "bundle-1" || result.RelatedIDs.ExecutionTaskID != "task-1" {
		t.Fatalf("RelatedIDs = %+v, want bundle/task metadata", result.RelatedIDs)
	}
}

func TestTriggerApplicationDeliveryActionRejectsBindingFromAnotherApplication(t *testing.T) {
	service := New(
		stubApplicationReader{app: domainapp.App{ID: "app-1", Name: "demo"}},
		stubCatalogReader{bindings: []domaincatalog.ApplicationEnvironment{{ID: "binding-1", ApplicationID: "app-2"}}},
		stubBuildReader{},
		stubWorkflowReader{},
		stubReleaseReader{},
		stubRepository{},
		nil,
		nil,
		deliveryActionPermissions(appaccess.PermDeliveryBuildsTrigger),
	)

	_, err := service.TriggerApplicationDeliveryAction(context.Background(), deliveryActionPrincipal(), "app-1", domaindelivery.ApplicationDeliveryActionInput{
		Action:                   domaindelivery.ApplicationDeliveryActionBuild,
		ApplicationEnvironmentID: "binding-1",
	})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("error = %v, want invalid argument", err)
	}
}

func TestTriggerApplicationDeliveryActionDeployRequiresEnabledTarget(t *testing.T) {
	service := New(
		stubApplicationReader{app: domainapp.App{ID: "app-1", Name: "demo", DefaultTag: "stable", BuildImage: "registry.local/demo"}},
		stubCatalogReader{bindings: []domaincatalog.ApplicationEnvironment{{ID: "binding-1", ApplicationID: "app-1"}}},
		stubBuildReader{},
		stubWorkflowReader{},
		stubReleaseReader{},
		stubRepository{},
		nil,
		nil,
		deliveryActionPermissions(appaccess.PermDeliveryReleasesTrigger),
	)

	_, err := service.TriggerApplicationDeliveryAction(context.Background(), deliveryActionPrincipal(), "app-1", domaindelivery.ApplicationDeliveryActionInput{
		Action:                   domaindelivery.ApplicationDeliveryActionDeploy,
		ApplicationEnvironmentID: "binding-1",
	})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("error = %v, want invalid argument", err)
	}
}

func TestTriggerApplicationDeliveryActionBuildDeployRequiresWorkflowTemplate(t *testing.T) {
	service := New(
		stubApplicationReader{app: domainapp.App{ID: "app-1", Name: "demo", DefaultTag: "stable", BuildImage: "registry.local/demo"}},
		stubCatalogReader{bindings: []domaincatalog.ApplicationEnvironment{
			{
				ID:            "binding-1",
				ApplicationID: "app-1",
				Targets: []domaincatalog.ReleaseTarget{
					{ID: "target-1", ClusterID: "cluster-a", Namespace: "namespace-a", WorkloadKind: "Deployment", WorkloadName: "demo-api", Enabled: true},
				},
			},
		}},
		stubBuildReader{},
		stubWorkflowReader{},
		stubReleaseReader{},
		stubRepository{},
		nil,
		nil,
		deliveryActionPermissions(appaccess.PermDeliveryBuildsTrigger, appaccess.PermDeliveryWorkflowsTrigger),
	)

	_, err := service.TriggerApplicationDeliveryAction(context.Background(), deliveryActionPrincipal(), "app-1", domaindelivery.ApplicationDeliveryActionInput{
		Action:                   domaindelivery.ApplicationDeliveryActionBuildDeploy,
		ApplicationEnvironmentID: "binding-1",
	})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("error = %v, want invalid argument", err)
	}
}

func TestTriggerApplicationDeliveryActionDeployRequiresReleasePermission(t *testing.T) {
	releaseCount := 0
	service := New(
		stubApplicationReader{app: domainapp.App{ID: "app-1", Name: "demo", DefaultTag: "stable", BuildImage: "registry.local/demo"}},
		stubCatalogReader{bindings: []domaincatalog.ApplicationEnvironment{
			{
				ID:            "binding-1",
				ApplicationID: "app-1",
				Targets: []domaincatalog.ReleaseTarget{
					{ID: "target-1", ClusterID: "cluster-a", Namespace: "namespace-a", WorkloadKind: "Deployment", WorkloadName: "demo-api", Enabled: true},
				},
			},
		}},
		stubBuildReader{},
		stubWorkflowReader{},
		stubReleaseReader{triggerCount: &releaseCount},
		stubRepository{},
		nil,
		nil,
		deliveryActionPermissions(appaccess.PermDeliveryBuildsTrigger),
	)

	_, err := service.TriggerApplicationDeliveryAction(context.Background(), deliveryActionPrincipal(), "app-1", domaindelivery.ApplicationDeliveryActionInput{
		Action:                   domaindelivery.ApplicationDeliveryActionDeploy,
		ApplicationEnvironmentID: "binding-1",
	})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("error = %v, want access denied", err)
	}
	if releaseCount != 0 {
		t.Fatalf("release trigger count = %d, want 0", releaseCount)
	}
}
