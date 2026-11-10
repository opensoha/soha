package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type workloadScaleAssessmentSpy struct {
	conditionalWorkloadProviderSpy
	assessments int
}

type originalScaleProviderSpy struct {
	workloadScaleAssessmentSpy
	writes int
}

func (s *originalScaleProviderSpy) ExecuteOriginalConditionalMutation(_ context.Context, _ domainidentity.Principal, call domainai.ToolInvocationRequest) (map[string]any, error) {
	s.writes++
	return map[string]any{"mutationRequestId": call.RequestID}, nil
}

func (s *originalScaleProviderSpy) FindOriginalConditionalMutation(context.Context, domainidentity.Principal, domainai.ToolInvocationRequest) (map[string]any, error) {
	return nil, nil
}

func (s *workloadScaleAssessmentSpy) AssessOriginalWorkloadScale(_ context.Context, _ domainidentity.Principal, _ domainai.ToolInvocationRequest) (domainai.CapabilityAssessment, error) {
	s.assessments++
	return domainai.CapabilityAssessment{Verdict: "inconclusive", Summary: "owned fixture"}, nil
}

func TestScaleAssessmentCapabilityIsReadOnlyBoundAndOptional(t *testing.T) {
	legacy, err := NewConditionalResourceCapabilityProvider(&conditionalWorkloadProviderSpy{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range legacy.Tools() {
		if tool.Name == workloadScaleAssessmentTool {
			t.Fatal("legacy service advertised unsupported scale assessment")
		}
	}
	spy := &workloadScaleAssessmentSpy{}
	provider, err := NewConditionalResourceCapabilityProvider(spy)
	if err != nil {
		t.Fatal(err)
	}
	invoker := mustValueAs[ToolCapabilityInvoker](t, provider)
	var selected domainai.ToolCapability
	for _, tool := range provider.Tools() {
		if tool.Name == workloadScaleAssessmentTool {
			selected = tool
		}
	}
	assertScaleAssessmentMetadata(t, selected)
	input := map[string]any{"clusterId": "cluster-a", "namespace": "team-a", "kind": "Deployment", "name": "app", "mutationRequestId": "original", "replicas": json.Number("2"), "preconditions": map[string]any{"expectedUid": "uid", "expectedResourceVersion": "rv"}}
	if _, _, err := invoker.InvokeTool(t.Context(), domainidentity.Principal{}, selected, input); !errors.Is(err, apperrors.ErrAccessDenied) || spy.assessments != 0 {
		t.Fatal("unbound provider invocation accepted", err)
	}
	call := domainai.ToolInvocationRequest{ToolName: workloadScaleAssessmentTool, CapabilityVersion: "1", Input: input}
	ctx := domainai.WithExecutionAuthorization(t.Context(), domainai.ExecutionAuthorization{Call: call})
	if _, _, err := invoker.InvokeTool(ctx, domainidentity.Principal{}, selected, input); err != nil || spy.assessments != 1 {
		t.Fatal("bound read assessment failed", err)
	}
	changed := map[string]any{"replicas": 3}
	if _, _, err := invoker.InvokeTool(ctx, domainidentity.Principal{}, selected, changed); !errors.Is(err, apperrors.ErrAccessDenied) || spy.assessments != 1 {
		t.Fatal("changed execution input reached the service", err)
	}
}

func assertScaleAssessmentMetadata(t *testing.T, selected domainai.ToolCapability) {
	t.Helper()
	if !selected.ProducesAssessment || selected.RiskLevel != domainai.RiskLevelRead || selected.RequiresApproval || selected.Execution == nil || !selected.Execution.Idempotent || len(selected.InputSemantics) != 1 || selected.InputSemantics[0].Kind != "k8s.conditional_mutation" || selected.Version != "1" {
		t.Fatal("scale assessment lost its read-only or original-receipt contract", selected)
	}
}

func TestDedicatedScaleDeclaresApprovalOriginalRecoveryAndDesiredVerification(t *testing.T) {
	spy := &originalScaleProviderSpy{}
	provider, err := NewConditionalResourceCapabilityProvider(spy)
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]domainai.ToolCapability{}
	for _, tool := range provider.Tools() {
		tools[tool.Name] = tool
	}
	legacy, dedicated := tools["k8s.workloads.conditional.action"], tools[conditionalWorkloadScaleTool]
	for _, tool := range []domainai.ToolCapability{legacy, dedicated} {
		if !tool.RequiresApproval || tool.RiskLevel != domainai.RiskLevelHigh || tool.Execution == nil || !tool.Execution.Idempotent || tool.Execution.RecoveryMode != "original_call" || len(tool.Execution.Checks) != 2 || tool.Execution.Checks[0].ToolName != "k8s.resources.identity.read" {
			t.Fatal("scale lost approval, original recovery or identity check", tool)
		}
	}
	if legacy.Execution.Checks[1].ToolName != "k8s.resources.conditional.assess" || dedicated.Execution.Checks[1].ToolName != workloadScaleAssessmentTool {
		t.Fatal("old revision verification changed or dedicated desired-state verification is missing")
	}
	input := map[string]any{"clusterId": "cluster-a", "namespace": "team-a", "kind": "Deployment", "name": "app", "replicas": 2, "preconditions": map[string]any{"expectedUid": "uid", "expectedResourceVersion": "rv"}}
	invoker := mustValueAs[ToolCapabilityInvoker](t, provider)
	if _, _, err := invoker.InvokeTool(t.Context(), domainidentity.Principal{}, dedicated, input); !errors.Is(err, apperrors.ErrAccessDenied) || spy.writes != 0 {
		t.Fatal("unbound scale reached the owning service", err)
	}
	call := domainai.ToolInvocationRequest{ToolName: conditionalWorkloadScaleTool, CapabilityVersion: "1", RequestID: "original", Input: input}
	ctx := domainai.WithExecutionAuthorization(t.Context(), domainai.ExecutionAuthorization{Call: call, ApprovalID: "approved"})
	if _, _, err := invoker.InvokeTool(ctx, domainidentity.Principal{}, dedicated, input); err != nil || spy.writes != 1 {
		t.Fatal("bound original scale did not reach its owning service", err)
	}
}
