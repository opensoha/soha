package aigateway

import (
	"context"
	"slices"
	"testing"

	contractresource "github.com/opensoha/soha-contracts/resource"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

type conditionalWorkloadProviderSpy struct {
	calls int
	input contractresource.ConditionalWorkloadActionInput
}

func (*conditionalWorkloadProviderSpy) GetResourceIdentity(context.Context, domainidentity.Principal, string, string, string, string) (contractresource.ResourceIdentity, error) {
	panic("unexpected resource read")
}
func (*conditionalWorkloadProviderSpy) PlanResourceYAMLConditional(context.Context, domainidentity.Principal, string, string, string, string, contractresource.ConditionalResourceYAMLInput) (domainoperation.Plan, error) {
	panic("unexpected YAML plan")
}
func (*conditionalWorkloadProviderSpy) ApplyResourceYAMLConditional(context.Context, domainidentity.Principal, string, string, string, string, contractresource.ConditionalResourceYAMLInput) (domainresource.ResourceYAMLView, error) {
	panic("unexpected YAML write")
}
func (*conditionalWorkloadProviderSpy) DeleteResourceConditional(context.Context, domainidentity.Principal, string, string, string, string, contractresource.ResourceMutationPreconditions) (contractresource.ResourceDeletionReceipt, error) {
	panic("unexpected resource delete")
}
func (s *conditionalWorkloadProviderSpy) ExecuteWorkloadActionConditional(_ context.Context, _ domainidentity.Principal, cluster, namespace, kind, name, action string, input contractresource.ConditionalWorkloadActionInput) (contractresource.ConditionalWorkloadActionReceipt, error) {
	s.calls++
	s.input = input
	if cluster != "cluster-a" || namespace != "team-a" || kind != "Deployment" || name != "app" || action != "scale" {
		panic("wrong typed target")
	}
	return contractresource.ConditionalWorkloadActionReceipt{Applied: !input.DryRun, DryRun: input.DryRun, Kind: kind, Name: name, Namespace: namespace, Action: action, UID: input.Preconditions.ExpectedUID, ResourceVersion: "next", Preconditions: input.Preconditions}, nil
}

func TestConditionalWorkloadCapabilitiesSeparateAnalysisFromApprovedExecution(t *testing.T) {
	spy := &conditionalWorkloadProviderSpy{}
	provider, err := NewConditionalResourceCapabilityProvider(spy)
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"clusterId": "cluster-a", "namespace": "team-a", "kind": "Deployment", "name": "app", "action": "scale", "replicas": float64(0), "preconditions": map[string]any{"expectedUid": "reviewed", "expectedResourceVersion": "rv"}}
	for _, tool := range provider.Tools() {
		if tool.Name != "k8s.workloads.conditional.preflight" && tool.Name != "k8s.workloads.conditional.action" {
			continue
		}
		preview := tool.Name == "k8s.workloads.conditional.preflight"
		if tool.RequiresApproval == preview || (tool.RiskLevel == domainai.RiskLevelHigh) == preview || tool.Execution.Idempotent != preview {
			t.Fatalf("invalid execution contract: %+v", tool)
		}
		if _, _, err := mustValueAs[*conditionalResourceCapabilityProvider](t, provider).InvokeTool(t.Context(), domainidentity.Principal{}, tool, input); err != nil {
			t.Fatal(err)
		}
		if spy.input.DryRun != preview || spy.input.Preconditions.ExpectedUID != "reviewed" || spy.input.Replicas == nil || *spy.input.Replicas != 0 {
			t.Fatalf("altered typed input: %+v", spy.input)
		}
	}
	if spy.calls != 2 {
		t.Fatalf("calls=%d", spy.calls)
	}
}

func TestConditionalMutationSkillIsSeparateFromReadOnlyDiagnosis(t *testing.T) {
	provider, err := NewConditionalResourceCapabilityProvider(&originalScaleProviderSpy{})
	if err != nil {
		t.Fatal(err)
	}
	service := newCreationGateway(t, &memoryGatewayRepository{}, provider)
	tools := map[string]domainai.ToolCapability{}
	for _, tool := range service.gatewayRegistry().Tools() {
		tools[tool.Name] = tool
	}
	operatorCount, readerCount := 0, 0
	for _, skill := range service.gatewayRegistry().Skills() {
		switch skill.ID {
		case "k8s-operator":
			operatorCount++
			if !slices.Equal(skill.CapabilityRefs, []string{"k8s.resources.identity.read", "k8s.resources.conditional.preflight", "k8s.resources.conditional.update", "k8s.resources.conditional.delete", "k8s.resources.conditional.assess", "k8s.workloads.conditional.preflight", "k8s.workloads.conditional.action", "k8s.workloads.scale", "k8s.workloads.scale.assess", "k8s.deployments.rollout_status"}) {
				t.Fatalf("unexpected operator refs: %v", skill.CapabilityRefs)
			}
			for _, name := range skill.CapabilityRefs {
				if _, ok := tools[name]; !ok {
					t.Fatalf("operator references missing tool %s", name)
				}
			}
			for _, name := range []string{"k8s.resources.conditional.update", "k8s.resources.conditional.delete", "k8s.workloads.conditional.action", "k8s.workloads.scale"} {
				if !slices.Contains(skill.CapabilityRefs, name) || !tools[name].RequiresApproval || tools[name].RiskLevel != domainai.RiskLevelHigh {
					t.Fatalf("mutation lacks separate approval: %s", name)
				}
			}
		case "k8s-sre":
			readerCount++
			for _, name := range skill.CapabilityRefs {
				tool := tools[name]
				if tool.RequiresApproval || tool.RiskLevel != domainai.RiskLevelRead && tool.RiskLevel != domainai.RiskLevelAnalyze {
					t.Fatalf("read-only skill gained mutation %s", name)
				}
			}
		}
	}
	if operatorCount != 1 || readerCount != 1 {
		t.Fatalf("operator=%d reader=%d", operatorCount, readerCount)
	}
}
