package delivery

import (
	"context"
	"errors"
	"reflect"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type manifestDeliveryStub struct {
	deployment      domainmanifest.Deployment
	snapshot        domainmanifest.DeliverySnapshot
	applied         []domainmanifest.DeliverySnapshot
	validationError error
}

func (m *manifestDeliveryStub) DeliveryDeployment(context.Context, domainidentity.Principal, string, string, domaincatalog.ReleaseTarget) (domainmanifest.Deployment, error) {
	return m.deployment, nil
}

func (m *manifestDeliveryStub) CreateDeliverySnapshot(_ context.Context, _ domainidentity.Principal, _, environmentID string, target domaincatalog.ReleaseTarget, revision int, planID string, _ domainmanifest.DeliveryArtifacts) (domainmanifest.DeliverySnapshot, error) {
	m.snapshot = domainmanifest.DeliverySnapshot{DeliveryPlanID: planID, TargetID: target.ID, BindingID: target.ConfigRef, ApplicationEnvironmentID: environmentID, ClusterID: target.ClusterID, Namespace: target.Namespace, Revision: revision, RenderedDigest: "fixed-digest", PreflightTaskID: "preflight"}
	return m.snapshot, nil
}
func (m *manifestDeliveryStub) ValidateDeliverySnapshot(context.Context, domainidentity.Principal, domainmanifest.DeliverySnapshot) error {
	return m.validationError
}
func (m *manifestDeliveryStub) ApplyDeliverySnapshot(_ context.Context, _ domainidentity.Principal, snapshot domainmanifest.DeliverySnapshot) (domainmanifest.Deployment, domaindelivery.ExecutionTask, error) {
	m.applied = append(m.applied, snapshot)
	return domainmanifest.Deployment{ID: "deployment"}, domaindelivery.ExecutionTask{ID: "apply"}, nil
}

func TestManifestRuntimeUsesInventoryInsteadOfPackageNameOrApplicationSelector(t *testing.T) {
	ctx := context.Background()
	manifest := &manifestDeliveryStub{deployment: domainmanifest.Deployment{ID: "deployment", Status: domainmanifest.DeploymentStatus{Phase: "converged", Inventory: []domainmanifest.ResourceInventory{
		{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "payments", Name: "api", UID: "uid-api"},
		{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "elsewhere", Name: "foreign", UID: "uid-foreign"},
		{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "payments", Name: "not-applied"},
	}}}}
	s := &Service{manifestDelivery: manifest, targets: stubTargetReader{deployments: map[string][]domainresource.DeploymentView{
		"cluster/payments": {{Name: "api", DesiredReplicas: 1, ReadyReplicas: 1, Labels: map[string]string{"app": "wrong-service"}}, {Name: "other", Labels: map[string]string{"app": "same-app"}}},
	}}}
	binding := domaincatalog.ApplicationEnvironment{ID: "env", ApplicationID: "app", ResourceSelector: domaincatalog.ResourceSelector{MatchLabels: map[string]string{"app": "same-app"}}, Targets: []domaincatalog.ReleaseTarget{{ID: "target", ExecutorKind: "manifest_ssa", WorkloadKind: "ManifestPackage", WorkloadName: "package", ClusterID: "cluster", Namespace: "payments", Enabled: true, Metadata: map[string]any{"serviceId": "svc-api"}}}}
	resolved, deployments, err := s.manifestRuntimeBinding(ctx, domainidentity.Principal{}, binding)
	if err != nil || len(deployments) != 1 || len(resolved.Targets) != 1 || binding.Targets[0].WorkloadName != "package" {
		t.Fatalf("runtime binding = %#v, %v", resolved, err)
	}
	workloads, err := s.listRuntimeWorkloadsForBinding(ctx, domainidentity.Principal{}, domainapp.App{}, resolved, nil, nil, nil, nil, nil)
	if err != nil || len(workloads) != 1 || workloads[0].WorkloadName != "api" || workloads[0].ServiceID != "svc-api" {
		t.Fatalf("runtime workloads = %#v, %v", workloads, err)
	}
	service := runtimeServiceForWorkload(workloads[0], []domainapp.Service{{ID: "wrong", Key: "wrong-service"}, {ID: "svc-api"}})
	if service == nil || service.ID != "svc-api" {
		t.Fatalf("explicit service association lost: %#v", service)
	}
	manifest.deployment.Status.Inventory = nil
	resolved, _, err = s.manifestRuntimeBinding(ctx, domainidentity.Principal{}, binding)
	if err != nil || len(resolved.Targets) != 0 {
		t.Fatalf("unapplied package became a workload: %#v, %v", resolved, err)
	}
}

func TestManifestDeliveryPlanRetainsApprovalAndRejectsStaleSnapshot(t *testing.T) {
	for _, change := range []string{"none", "target", "inputs"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			repo := &planRepository{}
			targets := []domaincatalog.ReleaseTarget{{ID: "target", ExecutorKind: "manifest_ssa", ConfigRef: "manifest-binding", ClusterID: "cluster", Namespace: "payments", Enabled: true}}
			service := New(stubApplicationReader{app: domainapp.App{ID: "app-1", Name: "Payments"}}, stubCatalogReader{bindings: []domaincatalog.ApplicationEnvironment{{ID: "binding-1", ApplicationID: "app-1", Targets: targets, ReleasePolicy: domaincatalog.ReleasePolicy{RequiresApproval: true}}}}, stubBuildReader{}, stubWorkflowReader{}, stubReleaseReader{}, repo, nil, nil,
				deliveryActionPermissions(appaccess.PermDeliveryApplicationsView, appaccess.PermDeliveryReleasesTrigger, appaccess.PermDeliveryApplicationEnvApprove))
			runtime := &manifestDeliveryStub{}
			service.SetManifestDelivery(runtime)
			plan, err := service.CreateDeliveryPlan(ctx, deliveryActionPrincipal(), domaindelivery.DeliveryPlanInput{ApplicationID: "app-1", ApplicationEnvironmentID: "binding-1", Action: domaindelivery.ApplicationDeliveryActionDeploy, TargetID: "target", ManifestRevision: 2})
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.ManifestSnapshots) != 1 || plan.ManifestSnapshots[0].Revision != 2 || len(runtime.applied) != 0 {
				t.Fatalf("snapshot not saved: %#v", plan)
			}
			result, err := service.ConfirmDeliveryPlan(ctx, deliveryActionPrincipal(), plan.ID)
			if err != nil || result.Plan.Status != domaindelivery.DeliveryPlanStatusWaitingApproval || len(runtime.applied) != 0 {
				t.Fatalf("approval bypassed: %v", err)
			}
			if _, err := service.DecideDeliveryPlanApproval(ctx, deliveryActionPrincipal(), plan.ID, domaindelivery.DeliveryPlanApprovalInput{Action: "approve"}); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "target":
				targets[0].Namespace = "outside"
			case "inputs":
				runtime.validationError = apperrors.ErrConflict
			}
			result, err = service.ConfirmDeliveryPlan(ctx, deliveryActionPrincipal(), plan.ID)
			if change != "none" {
				if !errors.Is(err, apperrors.ErrConflict) || len(runtime.applied) != 0 || repo.plan.Status != domaindelivery.DeliveryPlanStatusDraft {
					t.Fatalf("stale snapshot applied: %v", err)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(runtime.applied, plan.ManifestSnapshots) || len(result.Result.ManifestDeployments) != 1 || result.Result.RelatedIDs.ExecutionTaskID != "apply" {
				t.Fatalf("snapshot execution failed: %#v %v", result, err)
			}
		})
	}
}

func TestManifestDirectActionRequiresPlan(t *testing.T) {
	service := New(stubApplicationReader{app: domainapp.App{ID: "app-1"}}, stubCatalogReader{bindings: []domaincatalog.ApplicationEnvironment{{ID: "binding-1", ApplicationID: "app-1", Targets: []domaincatalog.ReleaseTarget{{ID: "target", ExecutorKind: "manifest_ssa", Enabled: true}}}}}, stubBuildReader{}, stubWorkflowReader{}, stubReleaseReader{}, &planRepository{}, nil, nil, deliveryActionPermissions(appaccess.PermDeliveryReleasesTrigger))
	runtime := &manifestDeliveryStub{}
	service.SetManifestDelivery(runtime)
	_, err := service.TriggerApplicationDeliveryAction(context.Background(), deliveryActionPrincipal(), "app-1", domaindelivery.ApplicationDeliveryActionInput{Action: domaindelivery.ApplicationDeliveryActionDeploy, ApplicationEnvironmentID: "binding-1"})
	if !errors.Is(err, apperrors.ErrInvalidArgument) || len(runtime.applied) != 0 {
		t.Fatalf("direct action bypassed plan: %v", err)
	}
}
