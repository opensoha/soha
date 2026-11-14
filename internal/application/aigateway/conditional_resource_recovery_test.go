package aigateway

import (
	"context"
	"testing"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

func TestConditionalApprovalRetainsOriginalHumanSession(t *testing.T) {
	repo := &memoryGatewayRepository{}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{
		"admin":     {appaccess.PermAIGatewayManage},
		"developer": {appaccess.PermAIGatewayInvoke},
	}}), nil, repo)
	service.relayConfig.CredentialEncryptionKey = "task115-human-approval-fixture-key"
	service.relayCredentials = newRelayCredentialCodec(service.relayConfig)
	spy := &originalConditionalProviderSpy{}
	provider, err := NewConditionalResourceCapabilityProvider(spy)
	if err != nil {
		t.Fatal(err)
	}
	service.SetCapabilityProviders(provider, BuiltinCapabilityProvider{})
	call := domainai.ToolInvocationRequest{ToolName: "k8s.resources.conditional.update", CapabilityVersion: "1", RequestID: "original-human-request", SessionID: "original-auth-session", Input: map[string]any{
		"clusterId": "cluster-a", "namespace": "team-a", "kind": "ConfigMap", "name": "app", "content": "kind: ConfigMap",
		"preconditions": map[string]any{"expectedUid": "uid", "expectedResourceVersion": "rv"},
	}}
	held, err := service.InvokeTool(t.Context(), testPrincipal("developer"), call)
	if err != nil || held.Result != "pending_approval" || spy.executions != 0 {
		t.Fatal("human call did not wait for independent approval", err)
	}
	id := mustMapFieldAs[string](t, held.RelatedIDs, "approvalRequestId")
	approved, err := service.ApproveApprovalRequest(t.Context(), testPrincipal("admin"), id, domainai.ApprovalDecisionInput{})
	if err != nil || approved.Request.Status != "executed" || spy.executions != 1 {
		t.Fatal("approved original human call did not execute once", err)
	}
	if spy.call.SessionID != call.SessionID || spy.call.RequestID != call.RequestID || spy.call.CapabilityVersion != call.CapabilityVersion {
		t.Fatal("trusted original human credential or invocation identity was lost")
	}
	if _, err := service.ApproveApprovalRequest(t.Context(), testPrincipal("admin"), id, domainai.ApprovalDecisionInput{}); err == nil || spy.executions != 1 {
		t.Fatal("duplicate approval dispatched another mutation")
	}
}

type originalConditionalProviderSpy struct {
	conditionalWorkloadProviderSpy
	executions, reads int
	call              domainai.ToolInvocationRequest
}

func (s *originalConditionalProviderSpy) ExecuteOriginalConditionalMutation(_ context.Context, _ domainidentity.Principal, call domainai.ToolInvocationRequest) (map[string]any, error) {
	s.executions++
	s.call = call
	return map[string]any{"applied": true}, nil
}
func (s *originalConditionalProviderSpy) FindOriginalConditionalMutation(_ context.Context, _ domainidentity.Principal, call domainai.ToolInvocationRequest) (map[string]any, error) {
	s.reads++
	s.call = call
	return map[string]any{"applied": true}, nil
}

func TestConditionalMutationProviderUsesOriginalRequestForExecutionAndReadOnlyRecovery(t *testing.T) {
	spy := &originalConditionalProviderSpy{}
	provider, err := NewConditionalResourceCapabilityProvider(spy)
	if err != nil {
		t.Fatal(err)
	}
	call := domainai.ToolInvocationRequest{ToolName: "k8s.resources.conditional.update", CapabilityVersion: "1", RequestID: "server-original", Input: map[string]any{"clusterId": "cluster-a", "namespace": "team-a", "kind": "ConfigMap", "name": "app", "content": "private-yaml-value", "preconditions": map[string]any{"expectedUid": "uid", "expectedResourceVersion": "rv"}}}
	var tool domainai.ToolCapability
	for _, candidate := range provider.Tools() {
		if candidate.Name == call.ToolName {
			tool = candidate
		}
	}
	if tool.Execution == nil || !tool.Execution.Idempotent || tool.Execution.RecoveryMode != "original_call" || !tool.RequiresApproval {
		t.Fatal("recoverable tool lacks approval/original-call contract")
	}
	ctx := domainai.WithExecutionAuthorization(t.Context(), domainai.ExecutionAuthorization{Call: call, ApprovalID: "approved"})
	if _, _, err := mustValueAs[*conditionalResourceCapabilityProvider](t, provider).InvokeTool(ctx, domainidentity.Principal{}, tool, call.Input); err != nil || spy.executions != 1 || spy.call.RequestID != "server-original" {
		t.Fatal("original execution identity lost", err)
	}
	output, found, err := mustValueAs[OriginalToolRecoveryProvider](t, provider).RecoverOriginalTool(t.Context(), domainidentity.Principal{}, tool, call)
	if err != nil || !found || output == nil || spy.executions != 1 || spy.reads != 1 || spy.call.RequestID != "server-original" {
		t.Fatal("recovery replayed or lost the original identity", err)
	}
	if _, _, err := mustValueAs[*conditionalResourceCapabilityProvider](t, provider).InvokeTool(t.Context(), domainidentity.Principal{}, tool, call.Input); err == nil || spy.executions != 1 {
		t.Fatal("unapproved execution reached domain")
	}
}

func TestConditionalMutationPlanKeepsProtectedStorageAndReceiptBinding(t *testing.T) {
	provider, err := NewConditionalResourceCapabilityProvider(&originalConditionalProviderSpy{})
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]domainai.ToolCapability{}
	for _, tool := range provider.Tools() {
		tools[tool.Name] = tool
	}
	write := domainai.CapabilityPlanStep{ID: "update", Call: sohaapi.CapabilityCall{ToolName: "k8s.resources.conditional.update", CapabilityVersion: "1", Input: map[string]any{"clusterId": "c", "namespace": "n", "kind": "ConfigMap", "name": "app", "content": "kind: ConfigMap", "preconditions": map[string]any{"expectedUid": "uid", "expectedResourceVersion": "rv"}}}}
	if issues := validatePlannedCapability(write, tools, false); len(issues) != 1 || issues[0].Code != "protected_input_not_supported" {
		t.Fatal("plain durable YAML became admissible", issues)
	}
	if issues := validatePlannedCapability(write, tools, true); len(issues) != 0 {
		t.Fatal("protected original request cannot enter a durable plan", issues)
	}
	assess := domainai.CapabilityPlanStep{ID: "assess", DependsOn: []string{"update"}, Call: sohaapi.CapabilityCall{ToolName: "k8s.resources.conditional.assess", CapabilityVersion: "1", Input: map[string]any{"clusterId": "c", "namespace": "n", "kind": "ConfigMap", "name": "app"}}, Bindings: []domainai.CapabilityInputBinding{{StepID: "update", OutputPath: "/mutationRequestId", InputPath: "/mutationRequestId"}}}
	if issues := validatePlannedBindings(assess, map[string]domainai.CapabilityPlanStep{"update": write, "assess": assess}, tools); len(issues) != 0 {
		t.Fatal("original receipt binding lost its scope semantics", issues)
	}
	checks := tools[write.Call.ToolName].Execution.Checks
	if len(checks) != 2 || checks[1].ToolName != "k8s.resources.conditional.assess" || checks[1].Purpose != "verification" {
		t.Fatal("Native write lacks a receipt-bound verification")
	}
	if checks := tools["k8s.resources.conditional.delete"].Execution.Checks; len(checks) != 2 || checks[1].ToolName != "k8s.resources.conditional.assess" {
		t.Fatal("original-UID deletion lost versioned receipt-bound observation")
	}
}
