package resource

import (
	"context"
	"testing"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func TestPlanResourceYAMLUpdateDryRunsAndRedactsSecrets(t *testing.T) {
	direct := &resourceUpdatePlanDirectStub{analysis: domainresource.ResourceUpdateAnalysis{
		FieldManager: "opensoha-resource-edit/v1", ChangedFields: []string{"/data/token"},
		Owners:    []domainresource.ManagedFieldOwner{{Manager: "helm", Operation: "Apply", APIVersion: "v1", Fields: []string{"/data"}}},
		Conflicts: []domainresource.FieldConflict{},
	}}
	service := &GenericResources{
		resourceAccess: &resourceAccess{
			resolver: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a"}}},
			authorizer: exactPermissionAuthorizer{
				resourcePermissionKey("configuration", "Secret", domainaccess.ActionUpdate): true,
			},
		},
		direct: direct,
	}
	request := domainresource.ResourceUpdatePlanRequest{
		Namespace: "team-a",
		Kind:      "Secret",
		Name:      "registry",
		Content:   "apiVersion: v1\nkind: Secret\nmetadata:\n  name: registry\n  namespace: team-a\ndata:\n  token: c2VjcmV0\n",
	}

	plan, err := service.PlanResourceYAMLUpdate(context.Background(), domainidentity.Principal{}, "cluster-a", request)
	if err != nil {
		t.Fatalf("PlanResourceYAMLUpdate() error = %v", err)
	}
	if !plan.Ready || plan.RiskLevel != "high" || !plan.RequiresApproval || len(plan.InputHash) != 64 {
		t.Fatalf("plan = %#v", plan)
	}
	if direct.dryRunCalls != 1 || direct.namespace != "team-a" || direct.kind != "Secret" || direct.name != "registry" {
		t.Fatalf("dry-run = %#v", direct)
	}
	if len(plan.Changes) != 1 || !plan.Changes[0].SensitiveValuesRedacted {
		t.Fatalf("changes = %#v", plan.Changes)
	}
	if plan.KubernetesResourceUpdate == nil || plan.KubernetesResourceUpdate.FieldManager != "opensoha-resource-edit/v1" || len(plan.KubernetesResourceUpdate.ChangedFields) != 1 {
		t.Fatalf("analysis = %#v", plan.KubernetesResourceUpdate)
	}
}

func TestPlanResourceYAMLUpdateBlocksFieldConflicts(t *testing.T) {
	direct := &resourceUpdatePlanDirectStub{analysis: domainresource.ResourceUpdateAnalysis{
		FieldManager: "opensoha-resource-edit/v1", ChangedFields: []string{"/spec/replicas"},
		Owners:    []domainresource.ManagedFieldOwner{},
		Conflicts: []domainresource.FieldConflict{{Field: "/spec/replicas", Manager: "helm", Message: "field is owned by helm"}},
	}}
	service := &GenericResources{resourceAccess: &resourceAccess{
		resolver:   stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a"}}},
		authorizer: exactPermissionAuthorizer{resourcePermissionKey("workloads", "Deployment", domainaccess.ActionUpdate): true},
	}, direct: direct}
	plan, err := service.PlanResourceYAMLUpdate(context.Background(), domainidentity.Principal{}, "cluster-a", domainresource.ResourceUpdatePlanRequest{
		Namespace: "team-a", Kind: "Deployment", Name: "api", Content: "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\n  namespace: team-a\nspec:\n  replicas: 2\n",
	})
	if err != nil {
		t.Fatalf("PlanResourceYAMLUpdate() error = %v", err)
	}
	if plan.Ready || plan.KubernetesResourceUpdate == nil || len(plan.KubernetesResourceUpdate.Conflicts) != 1 {
		t.Fatalf("plan = %#v", plan)
	}
}

type resourceUpdatePlanDirectStub struct {
	dryRunCalls int
	namespace   string
	kind        string
	name        string
	analysis    domainresource.ResourceUpdateAnalysis
}

func (*resourceUpdatePlanDirectStub) CreateResourceYAML(context.Context, string, string, string, string) (domainresource.ResourceYAMLView, error) {
	return domainresource.ResourceYAMLView{}, nil
}
func (*resourceUpdatePlanDirectStub) GetResourceYAML(context.Context, string, string, string, string) (domainresource.ResourceYAMLView, error) {
	return domainresource.ResourceYAMLView{}, nil
}
func (*resourceUpdatePlanDirectStub) ApplyResourceYAML(context.Context, string, string, string, string, string) (domainresource.ResourceYAMLView, error) {
	return domainresource.ResourceYAMLView{}, nil
}
func (s *resourceUpdatePlanDirectStub) DryRunResourceYAML(_ context.Context, _, namespace, kind, name, _ string) (domainresource.ResourceUpdateAnalysis, error) {
	s.dryRunCalls++
	s.namespace, s.kind, s.name = namespace, kind, name
	return s.analysis, nil
}
func (*resourceUpdatePlanDirectStub) DeleteResource(context.Context, string, string, string, string) error {
	return nil
}
