package delivery

import (
	"context"
	"errors"
	"testing"

	appaccess "github.com/soha/soha/internal/application/access"
	domainapp "github.com/soha/soha/internal/domain/application"
	domainbuild "github.com/soha/soha/internal/domain/build"
	domaincatalog "github.com/soha/soha/internal/domain/catalog"
	domaindelivery "github.com/soha/soha/internal/domain/delivery"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainrelease "github.com/soha/soha/internal/domain/release"
	domainworkflow "github.com/soha/soha/internal/domain/workflow"
	"github.com/soha/soha/internal/platform/apperrors"
)

type stubApplicationReader struct {
	app domainapp.App
}

func (s stubApplicationReader) List(context.Context, domainidentity.Principal, domainapp.Filter) ([]domainapp.App, error) {
	return []domainapp.App{s.app}, nil
}

func (s stubApplicationReader) Get(context.Context, domainidentity.Principal, string) (domainapp.App, error) {
	return s.app, nil
}

func (s stubApplicationReader) Create(context.Context, domainidentity.Principal, domainapp.UpsertInput) (domainapp.App, error) {
	return s.app, nil
}

func (s stubApplicationReader) Update(context.Context, domainidentity.Principal, string, domainapp.UpsertInput) (domainapp.App, error) {
	return s.app, nil
}

type stubCatalogReader struct {
	bindings []domaincatalog.ApplicationEnvironment
	envs     []domaincatalog.Environment
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
	if len(s.bindings) == 0 {
		return domaincatalog.ApplicationEnvironment{}, nil
	}
	return s.bindings[0], nil
}

func (s stubCatalogReader) UpdateApplicationEnvironment(context.Context, domainidentity.Principal, string, domaincatalog.ApplicationEnvironmentInput) (domaincatalog.ApplicationEnvironment, error) {
	if len(s.bindings) == 0 {
		return domaincatalog.ApplicationEnvironment{}, nil
	}
	return s.bindings[0], nil
}

type stubBuildReader struct {
	record       domainbuild.Record
	triggerInput *domainbuild.TriggerInput
	triggerCount *int
}

func (stubBuildReader) List(context.Context, domainidentity.Principal, domainbuild.Filter) ([]domainbuild.Record, error) {
	return nil, nil
}

func (s stubBuildReader) Trigger(_ context.Context, _ domainidentity.Principal, input domainbuild.TriggerInput) (domainbuild.Record, error) {
	if s.triggerInput != nil {
		*s.triggerInput = input
	}
	if s.triggerCount != nil {
		*s.triggerCount = *s.triggerCount + 1
	}
	if s.record.ID != "" || s.record.Metadata != nil {
		return s.record, nil
	}
	return domainbuild.Record{}, nil
}

type stubWorkflowReader struct {
	record       domainworkflow.Run
	triggerInput *domainworkflow.Input
	triggerCount *int
}

func (stubWorkflowReader) List(context.Context, domainidentity.Principal, string, int) ([]domainworkflow.Run, error) {
	return nil, nil
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

type stubReleaseReader struct {
	record       domainrelease.Record
	triggerInput *domainrelease.TriggerInput
	triggerCount *int
	trigger      func(domainrelease.TriggerInput) domainrelease.Record
}

func (stubReleaseReader) List(context.Context, domainidentity.Principal, domainrelease.Filter) ([]domainrelease.Record, error) {
	return nil, nil
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

type stubRepository struct{}

func (stubRepository) ListReleaseBundles(context.Context, domaindelivery.ReleaseBundleFilter) ([]domaindelivery.ReleaseBundle, error) {
	return nil, nil
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

func (stubRepository) ListExecutionTasks(context.Context, domaindelivery.ExecutionTaskFilter) ([]domaindelivery.ExecutionTask, error) {
	return nil, nil
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

func (stubRepository) UpsertExecutionArtifact(context.Context, domaindelivery.ExecutionArtifact) (domaindelivery.ExecutionArtifact, error) {
	return domaindelivery.ExecutionArtifact{}, nil
}

func (stubRepository) ListApprovalPolicies(context.Context) ([]domaindelivery.ApprovalPolicy, error) {
	return nil, nil
}

func (stubRepository) GetApprovalPolicy(context.Context, string) (domaindelivery.ApprovalPolicy, error) {
	return domaindelivery.ApprovalPolicy{}, nil
}

func (stubRepository) CreateApprovalPolicy(context.Context, domaindelivery.ApprovalPolicyInput) (domaindelivery.ApprovalPolicy, error) {
	return domaindelivery.ApprovalPolicy{}, nil
}

func (stubRepository) UpdateApprovalPolicy(context.Context, string, domaindelivery.ApprovalPolicyInput) (domaindelivery.ApprovalPolicy, error) {
	return domaindelivery.ApprovalPolicy{}, nil
}

func (stubRepository) DeleteApprovalPolicy(context.Context, string) error {
	return nil
}

func (stubRepository) ListDeliveryBlueprints(context.Context) ([]domaindelivery.DeliveryBlueprint, error) {
	return nil, nil
}

func (stubRepository) GetDeliveryBlueprint(context.Context, string) (domaindelivery.DeliveryBlueprint, error) {
	return domaindelivery.DeliveryBlueprint{}, nil
}

func (stubRepository) CreateDeliveryBlueprint(context.Context, domaindelivery.DeliveryBlueprintInput) (domaindelivery.DeliveryBlueprint, error) {
	return domaindelivery.DeliveryBlueprint{}, nil
}

func (stubRepository) UpdateDeliveryBlueprint(context.Context, string, domaindelivery.DeliveryBlueprintInput) (domaindelivery.DeliveryBlueprint, error) {
	return domaindelivery.DeliveryBlueprint{}, nil
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
