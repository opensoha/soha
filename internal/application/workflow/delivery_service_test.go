package workflow

import (
	"context"
	"errors"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"strings"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type deliveryServiceRepository struct {
	*stubWorkflowRepository
	DeliveryRepository
	batch   domainworkflow.DeliveryBatch
	run     domainworkflow.Run
	creates int
}

func (r *deliveryServiceRepository) FindDeliveryBatch(context.Context, string, string, string) (domainworkflow.DeliveryBatch, domainworkflow.Run, error) {
	return domainworkflow.DeliveryBatch{}, domainworkflow.Run{}, apperrors.ErrNotFound
}

func (r *deliveryServiceRepository) CreateDeliveryBatch(_ context.Context, batch domainworkflow.DeliveryBatch, run domainworkflow.Run, _, _ string) (domainworkflow.DeliveryBatch, domainworkflow.Run, error) {
	r.creates++
	r.batch, r.run = batch, run
	return batch, run, nil
}

type deliveryServiceRuntime struct {
	DeliveryRuntime
	freezes     int
	validated   int
	invalidRef  error
	assessments int
}

func (r *deliveryServiceRuntime) AssessDeliveryTarget(_ context.Context, _ domainidentity.Principal, _ domainworkflow.Run, target domainworkflow.DeliveryTargetSnapshot, input sohaapi.DeliveryBatchAssessmentInput) (sohaapi.DeliveryBatchAssessment, error) {
	r.assessments++
	return sohaapi.DeliveryBatchAssessment{BatchID: input.BatchID, TargetID: target.Target.ID, Verdict: "inconclusive", Summary: "runtime evidence is required"}, nil
}

func TestDeliveryBatchAssessmentRequiresAllScopesBeforeRuntimeOrProbe(t *testing.T) {
	target := domainworkflow.DeliveryTargetInput{ID: "web", ApplicationID: "app", ServiceID: "svc", Action: "build_deploy", ApplicationEnvironmentID: "dev"}
	repo := &deliveryServiceRepository{stubWorkflowRepository: &stubWorkflowRepository{}, batch: domainworkflow.DeliveryBatch{ID: "batch", Definition: domainworkflow.DeliveryWorkflowDefinition{Targets: []domainworkflow.DeliveryTargetInput{target}}, Targets: []domainworkflow.DeliveryTargetSnapshot{{Target: target, FrozenScope: map[string]string{"applicationId": "app", "serviceId": "svc", "hostId": "host"}}}}}
	runtime := &deliveryServiceRuntime{}
	permissions := appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"delivery-test": {appaccess.PermDeliveryWorkflowsView}}})
	catalog := &stubWorkflowCatalog{items: []domaincatalog.ApplicationEnvironment{{ID: "dev", ApplicationID: "app", EnvironmentKey: "dev"}, {ID: "prod", ApplicationID: "app", EnvironmentKey: "prod"}}}
	service := New(repo, &stubWorkflowApps{}, environmentWorkflowAuthorizer{}, permissions, catalog, nil, nil, nil)
	service.SetDeliveryRuntime(runtime, runtime)
	principal := domainidentity.Principal{UserID: "actor", Roles: []string{"delivery-test"}}
	input := sohaapi.DeliveryBatchAssessmentInput{BatchID: "batch", TargetID: "web"}
	ctx := domainworkflow.WithDeliveryScopeCheck(context.Background(), func(scopes []map[string]string) error { return apperrors.ErrConflict })
	if _, err := service.AssessDeliveryBatch(ctx, principal, input); !errors.Is(err, apperrors.ErrConflict) || runtime.assessments != 0 {
		t.Fatalf("scope guard bypassed: %v", err)
	}
	result, err := service.AssessDeliveryBatch(context.Background(), principal, input)
	if err != nil || result.TargetID != "web" || runtime.assessments != 1 {
		t.Fatalf("authorized target failed: %+v %v", result, err)
	}
	private := target
	private.ID, private.ApplicationEnvironmentID = "private", "prod"
	repo.batch.Definition.Targets = append(repo.batch.Definition.Targets, private)
	repo.batch.Targets = append(repo.batch.Targets, domainworkflow.DeliveryTargetSnapshot{Target: private})
	if _, err := service.AssessDeliveryBatch(context.Background(), principal, input); !errors.Is(err, apperrors.ErrAccessDenied) || runtime.assessments != 1 {
		t.Fatalf("partial batch assessed: %v", err)
	}
}

func (r *deliveryServiceRuntime) ResolveDeliveryTargetScopes(_ context.Context, _ domainidentity.Principal, snapshot domainworkflow.DeliveryTargetSnapshot) ([]map[string]string, error) {
	if len(snapshot.FrozenScope) > 0 {
		return []map[string]string{snapshot.FrozenScope}, nil
	}
	return []map[string]string{{"applicationId": snapshot.Target.ApplicationID, "serviceId": snapshot.Target.ServiceID}}, nil
}

func (r *deliveryServiceRuntime) ValidateDeliveryTarget(context.Context, domainidentity.Principal, domainworkflow.DeliveryTargetInput) error {
	r.validated++
	return r.invalidRef
}

func TestPrepareDeliveryWorkflowValidatesReferencesWithoutFreezingOrSaving(t *testing.T) {
	repo := &deliveryServiceRepository{stubWorkflowRepository: &stubWorkflowRepository{}}
	runtime := &deliveryServiceRuntime{}
	permissions := appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"delivery-test": {appaccess.PermDeliveryWorkflowsTrigger, appaccess.PermDeliveryBuildsTrigger}}})
	service := New(repo, &stubWorkflowApps{}, nil, permissions, nil, nil, nil, nil)
	service.SetDeliveryRuntime(runtime, runtime)
	input := domainworkflow.DeliveryWorkflowInput{Definition: domainworkflow.DeliveryWorkflowDefinition{Name: "Build", Targets: []domainworkflow.DeliveryTargetInput{{ID: "build", ApplicationID: "app", ServiceID: "service", Action: "build"}}}}
	principal := domainidentity.Principal{UserID: "actor", Roles: []string{"delivery-test"}}
	prepared, err := service.PrepareDeliveryWorkflow(context.Background(), principal, "", input)
	if err != nil || runtime.validated != 1 || runtime.freezes != 0 || repo.creates != 0 || prepared.Definition.MaxConcurrency != 4 {
		t.Fatalf("preparation created execution or omitted validation: %+v %v", prepared, err)
	}
	runtime.invalidRef = apperrors.ErrNotFound
	if _, err := service.PrepareDeliveryWorkflow(context.Background(), principal, "", input); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("missing reference accepted: %v", err)
	}
}

func (r *deliveryServiceRuntime) FreezeDeliveryTarget(_ context.Context, _ domainidentity.Principal, target domainworkflow.DeliveryTargetInput) (domainworkflow.DeliveryTargetSnapshot, error) {
	r.freezes++
	return domainworkflow.DeliveryTargetSnapshot{Target: target, ApplicationName: target.ApplicationID, ServiceName: target.ServiceID, ServiceVersion: 1, ConfigurationDigest: "sha256:" + strings.Repeat("a", 64), BuildSourceID: "shared", BuildFingerprint: "identical-input"}, nil
}

func (r *deliveryServiceRuntime) CurrentExecutionPrincipal(_ context.Context, userID, _ string) (domainidentity.Principal, error) {
	return domainidentity.Principal{UserID: userID, Roles: []string{"delivery-test"}}, nil
}

func TestDeliveryBatchChecksFrozenScopesBeforePersisting(t *testing.T) {
	repo := &deliveryServiceRepository{stubWorkflowRepository: &stubWorkflowRepository{}}
	runtime := &deliveryServiceRuntime{}
	permissions := appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"delivery-test": {appaccess.PermDeliveryWorkflowsTrigger, appaccess.PermDeliveryBuildsTrigger}}})
	service := New(repo, &stubWorkflowApps{}, nil, permissions, nil, nil, nil, nil)
	service.SetDeliveryRuntime(runtime, runtime)
	checked := false
	ctx := domainworkflow.WithDeliveryScopeCheck(context.Background(), func(scopes []map[string]string) error {
		checked = len(scopes) == 1 && scopes[0]["applicationId"] == "app"
		return apperrors.ErrConflict
	})
	input := domainworkflow.DeliveryBatchInput{IdempotencyKey: "scope-guard-creation", Definition: &domainworkflow.DeliveryWorkflowDefinition{Name: "build", Targets: []domainworkflow.DeliveryTargetInput{{ID: "api", ApplicationID: "app", ServiceID: "service", Action: "build"}}}}
	_, err := service.CreateDeliveryBatch(ctx, domainidentity.Principal{UserID: "actor", Roles: []string{"delivery-test"}}, input)
	if !errors.Is(err, apperrors.ErrConflict) || !checked || repo.creates != 0 {
		t.Fatalf("scope check escaped enqueue boundary: %v checked=%v creates=%d", err, checked, repo.creates)
	}
}

func TestDeliveryBatchAuthorizesEveryTargetBeforeFreezeOrCreation(t *testing.T) {
	repo := &deliveryServiceRepository{stubWorkflowRepository: &stubWorkflowRepository{}}
	runtime := &deliveryServiceRuntime{}
	permissions := appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"delivery-test": {appaccess.PermDeliveryWorkflowsTrigger, appaccess.PermDeliveryBuildsTrigger, appaccess.PermDeliveryReleasesTrigger}}})
	catalog := &stubWorkflowCatalog{items: []domaincatalog.ApplicationEnvironment{{ID: "dev", ApplicationID: "app", EnvironmentKey: "dev"}, {ID: "prod", ApplicationID: "app", EnvironmentKey: "prod"}}}
	service := New(repo, &stubWorkflowApps{}, environmentWorkflowAuthorizer{}, permissions, catalog, nil, nil, nil)
	service.SetDeliveryRuntime(runtime, runtime)
	input := domainworkflow.DeliveryBatchInput{IdempotencyKey: "request-12345", Definition: &domainworkflow.DeliveryWorkflowDefinition{Name: "release", Targets: []domainworkflow.DeliveryTargetInput{
		{ID: "web-dev", ApplicationID: "app", ServiceID: "web", ApplicationEnvironmentID: "dev", Action: "build_deploy"},
		{ID: "web-prod", ApplicationID: "app", ServiceID: "web", ApplicationEnvironmentID: "prod", Action: "build_deploy"},
	}}}
	if _, err := service.CreateDeliveryBatch(context.Background(), domainidentity.Principal{UserID: "actor", Roles: []string{"delivery-test"}}, input); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("mixed permission targets accepted: %v", err)
	}
	if runtime.freezes != 0 || repo.creates != 0 {
		t.Fatalf("partial unauthorized batch created work: freezes=%d creates=%d", runtime.freezes, repo.creates)
	}
}

func TestDeliveryBatchUsesOneScopedRunAndNeverSharesBuildAcrossApplications(t *testing.T) {
	repo := &deliveryServiceRepository{stubWorkflowRepository: &stubWorkflowRepository{}}
	runtime := &deliveryServiceRuntime{}
	permissions := appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"delivery-test": {appaccess.PermDeliveryWorkflowsTrigger, appaccess.PermDeliveryBuildsTrigger, appaccess.PermDeliveryReleasesTrigger}}})
	service := New(repo, &stubWorkflowApps{}, nil, permissions, nil, nil, nil, nil)
	service.SetDeliveryRuntime(runtime, runtime)
	input := domainworkflow.DeliveryBatchInput{IdempotencyKey: "request-54321", Definition: &domainworkflow.DeliveryWorkflowDefinition{Name: "release", Mode: domainworkflow.DeliveryModeBuildAll, Targets: []domainworkflow.DeliveryTargetInput{
		{ID: "web-dev", ApplicationID: "a", ServiceID: "web", ApplicationEnvironmentID: "dev", Action: "build_deploy"},
		{ID: "web-prod", ApplicationID: "a", ServiceID: "web", ApplicationEnvironmentID: "prod", Action: "build_deploy"},
		{ID: "pay-dev", ApplicationID: "b", ServiceID: "pay", ApplicationEnvironmentID: "dev", Action: "build_deploy"},
	}}}
	batch, err := service.CreateDeliveryBatch(context.Background(), domainidentity.Principal{UserID: "actor", Roles: []string{"delivery-test"}, AccessTokenID: "scoped-token"}, input)
	if err != nil {
		t.Fatal(err)
	}
	if repo.creates != 1 || repo.run.Scope != domainworkflow.ScopeDeliveryBatch || repo.run.ApplicationID != "" || batch.RootRunID != repo.run.ID {
		t.Fatalf("incorrect root scope: %+v", repo.run)
	}
	if repo.run.Metadata["executionTokenId"] != "scoped-token" {
		t.Fatal("execution lost its revocable token identity")
	}
	if batch.ServiceCount != 2 || batch.TargetCount != 3 || batch.BuildCount != 0 {
		t.Fatalf("incorrect counts: %+v", batch)
	}
	if batch.Targets[0].BuildNodeID != batch.Targets[1].BuildNodeID || batch.Targets[0].BuildNodeID == batch.Targets[2].BuildNodeID {
		t.Fatal("build deduplication crossed application authority")
	}
	dag, ok := definitionFromRunMetadata(repo.run)
	if !ok || dag.Mode != domainworkflow.ScopeDeliveryBatch || dag.Nodes[0].TargetID != "web-dev" {
		t.Fatalf("frozen DAG lost target context: %+v", dag)
	}
}

type deliveryRecipeCatalog struct {
	stubWorkflowCatalog
	template domaincatalog.WorkflowTemplate
	reads    int
}

func (c *deliveryRecipeCatalog) GetWorkflowTemplate(context.Context, string) (domaincatalog.WorkflowTemplate, error) {
	return c.template, nil
}
func (c *deliveryRecipeCatalog) GetWorkflowTemplateVersion(_ context.Context, _ string, version int64) (domaincatalog.WorkflowTemplate, error) {
	c.reads++
	if version != c.template.PublishedVersion {
		return domaincatalog.WorkflowTemplate{}, apperrors.ErrNotFound
	}
	return c.template, nil
}
func TestDeliveryBatchFreezesPublishedRecipeDefaults(t *testing.T) {
	repo := &deliveryServiceRepository{stubWorkflowRepository: &stubWorkflowRepository{}}
	runtime := &deliveryServiceRuntime{}
	permissions := appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"delivery-test": {appaccess.PermDeliveryWorkflowsTrigger, appaccess.PermDeliveryBuildsTrigger, appaccess.PermDeliveryReleasesTrigger, appaccess.PermDeliveryWorkflowTemplatesView}}})
	preset := domaincatalog.BuiltinDeliveryRecipes()[1]
	catalog := &deliveryRecipeCatalog{template: domaincatalog.WorkflowTemplate{ID: preset.ID, Enabled: true, PublishedVersion: 2, PublicationState: "published", ContentDigest: "sha256:" + strings.Repeat("a", 64), Definition: preset.Definition}}
	service := New(repo, &stubWorkflowApps{}, nil, permissions, catalog, nil, nil, nil)
	service.SetDeliveryRuntime(runtime, runtime)
	principal := domainidentity.Principal{UserID: "actor", Roles: []string{"delivery-test"}}
	definition := domainworkflow.DeliveryWorkflowDefinition{Name: "release", WorkflowTemplateID: preset.ID, WorkflowTemplateVersion: 2, Targets: []domainworkflow.DeliveryTargetInput{
		{ID: "web-dev", ApplicationID: "a", ServiceID: "web", ApplicationEnvironmentID: "dev", Action: "build_deploy"},
		{ID: "pay-dev", ApplicationID: "b", ServiceID: "pay", ApplicationEnvironmentID: "dev", Action: "build_deploy"},
	}}
	batch, err := service.CreateDeliveryBatch(context.Background(), principal, domainworkflow.DeliveryBatchInput{IdempotencyKey: "recipe-request", Definition: &definition})
	if err != nil {
		t.Fatal(err)
	}
	if batch.Definition.Mode != domainworkflow.DeliveryModeBuildAll || batch.Definition.MaxConcurrency != 4 || !batch.Definition.StopsOnFailure() || batch.WorkflowTemplateDigest != catalog.template.ContentDigest || catalog.reads != 1 {
		t.Fatalf("recipe not frozen: %+v", batch)
	}
	dag, _ := definitionFromRunMetadata(repo.run)
	foundBarrier := false
	for _, node := range dag.Nodes {
		if node.Stage == "barrier" {
			foundBarrier = true
		}
	}
	if !foundBarrier {
		t.Fatal("recipe did not affect compiled build barrier")
	}
	catalog.template.Definition["executionMode"] = domainworkflow.DeliveryModeSerial
	if batch.Definition.Mode != domainworkflow.DeliveryModeBuildAll {
		t.Fatal("template update changed batch")
	}
	definition.Mode, definition.MaxConcurrency = domainworkflow.DeliveryModeBuildAll, 7
	stop := false
	definition.StopOnFailure = &stop
	resolved, _, err := service.resolveDeliveryRecipe(context.Background(), principal, definition)
	if err != nil || resolved.Mode != definition.Mode || resolved.MaxConcurrency != 7 || resolved.StopsOnFailure() {
		t.Fatalf("explicit settings lost: %+v %v", resolved, err)
	}
	definition.WorkflowTemplateVersion++
	if _, _, err := service.resolveDeliveryRecipe(context.Background(), principal, definition); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("missing version accepted: %v", err)
	}
	definition.WorkflowTemplateVersion--
	catalog.template.Definition["nodes"] = []any{map[string]any{"type": "unknown"}}
	if _, _, err := service.resolveDeliveryRecipe(context.Background(), principal, definition); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("unhandled stages silently ignored: %v", err)
	}
}

func (r *deliveryServiceRepository) GetDeliveryBatch(context.Context, string) (domainworkflow.DeliveryBatch, domainworkflow.Run, error) {
	return r.batch, r.run, nil
}
func TestDeliveryBatchDetailFiltersTargetsAndRefusesInvisibleBatch(t *testing.T) {
	repo := &deliveryServiceRepository{stubWorkflowRepository: &stubWorkflowRepository{}}
	targets := []domainworkflow.DeliveryTargetSnapshot{
		{Target: domainworkflow.DeliveryTargetInput{ID: "dev-service", ApplicationID: "app", ServiceID: "api", ApplicationEnvironmentID: "dev"}},
		{Target: domainworkflow.DeliveryTargetInput{ID: "secret-service", ApplicationID: "app", ServiceID: "private", ApplicationEnvironmentID: "prod"}},
	}
	repo.batch = domainworkflow.DeliveryBatch{ID: "batch", Definition: domainworkflow.DeliveryWorkflowDefinition{Name: "secret release"}, Targets: targets}
	repo.run = domainworkflow.Run{Status: "running", NodeRuns: []domainworkflow.NodeRun{{NodeID: "dev-service:health", TargetID: "dev-service", Stage: "health", Status: "completed"}, {NodeID: "secret-service:build", TargetID: "secret-service", Stage: "build", Status: "running"}}}
	permissions := appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"delivery-test": {appaccess.PermDeliveryWorkflowsView}}})
	catalog := &stubWorkflowCatalog{items: []domaincatalog.ApplicationEnvironment{{ID: "dev", ApplicationID: "app", EnvironmentKey: "dev"}, {ID: "prod", ApplicationID: "app", EnvironmentKey: "prod"}}}
	service := New(repo, &stubWorkflowApps{}, environmentWorkflowAuthorizer{}, permissions, catalog, nil, nil, nil)
	principal := domainidentity.Principal{UserID: "actor", Roles: []string{"delivery-test"}}
	visible, err := service.GetDeliveryBatch(context.Background(), principal, "batch")
	if err != nil || len(visible.Targets) != 1 || len(visible.Nodes) != 1 || visible.Status != "completed" || visible.Definition.Name == "secret release" {
		t.Fatalf("detail leaked hidden target: %+v %v", visible, err)
	}
	repo.batch.Targets = targets[1:]
	if _, err := service.GetDeliveryBatch(context.Background(), principal, "batch"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("invisible batch disclosed: %v", err)
	}
}

type workflowCreationRepository struct {
	*deliveryServiceRepository
	workflow           domainworkflow.DeliveryWorkflow
	digest, actor, key string
}

func (r *workflowCreationRepository) FindDeliveryWorkflowCreation(_ context.Context, actor, key, digest string) (domainworkflow.DeliveryWorkflow, error) {
	if r.key != key || r.actor != actor {
		return domainworkflow.DeliveryWorkflow{}, apperrors.ErrNotFound
	}
	if r.digest != digest {
		return domainworkflow.DeliveryWorkflow{}, apperrors.ErrConflict
	}
	return r.workflow, nil
}
func (r *workflowCreationRepository) CreateDeliveryWorkflowIdempotent(_ context.Context, item domainworkflow.DeliveryWorkflow, key, digest string) (domainworkflow.DeliveryWorkflow, error) {
	r.creates++
	item.Version = 1
	r.workflow, r.digest, r.actor, r.key = item, digest, item.CreatedBy, key
	return item, nil
}
func TestDeliveryWorkflowCreationReplaysReceiptUnderCurrentAuthorization(t *testing.T) {
	ctx := context.Background()
	repo := &workflowCreationRepository{deliveryServiceRepository: &deliveryServiceRepository{stubWorkflowRepository: &stubWorkflowRepository{}}}
	runtime := &deliveryServiceRuntime{}
	permissions := appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"delivery-test": {appaccess.PermDeliveryWorkflowsTrigger, appaccess.PermDeliveryBuildsTrigger}}})
	service := New(repo, &stubWorkflowApps{}, nil, permissions, nil, nil, nil, nil)
	service.SetDeliveryRuntime(runtime, runtime)
	principal := domainidentity.Principal{UserID: "actor", Roles: []string{"delivery-test"}}
	input := domainworkflow.DeliveryWorkflowInput{IdempotencyKey: "workflow-create-intent", Definition: domainworkflow.DeliveryWorkflowDefinition{Name: "Build", Targets: []domainworkflow.DeliveryTargetInput{{ID: "build", ApplicationID: "app", ServiceID: "service", Action: "build"}}}}
	created, err := service.SaveDeliveryWorkflow(ctx, principal, "", input)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := service.SaveDeliveryWorkflow(ctx, principal, "", input)
	if err != nil || replayed.ID != created.ID || repo.creates != 1 || runtime.validated != 1 || runtime.freezes != 0 {
		t.Fatalf("duplicate creation or execution: creates=%d validated=%d err=%v", repo.creates, runtime.validated, err)
	}
	changed := input
	changed.Definition.Name = "different intent"
	if _, err := service.SaveDeliveryWorkflow(ctx, principal, "", changed); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("changed intent replayed: %v", err)
	}
	principal.Roles = nil
	if _, err := service.SaveDeliveryWorkflow(ctx, principal, "", input); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("revoked actor read receipt: %v", err)
	}
}
