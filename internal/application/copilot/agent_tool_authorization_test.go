package copilot

import (
	"context"
	"errors"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	apperrors "github.com/opensoha/soha/internal/platform/apperrors"
)

type chatToolPrincipalResolver struct{ principal domainidentity.Principal }

type chatGatewayToolStub struct {
	*fakeWorkbenchModelInvoker
	calls int
	input domainaigateway.ToolInvocationRequest
}

func (g *chatGatewayToolStub) InvokeTool(_ context.Context, _ domainidentity.Principal, input domainaigateway.ToolInvocationRequest) (domainaigateway.ToolInvocationResult, error) {
	g.calls++
	g.input = input
	return domainaigateway.ToolInvocationResult{Result: "success", Output: map[string]any{"ready": true}}, nil
}

func (r *chatToolPrincipalResolver) CurrentPrincipal(context.Context, string) (domainidentity.Principal, error) {
	return r.principal, nil
}

func TestChatAgentToolRechecksAuthorization(t *testing.T) {
	permission := appaccess.PermPlatformNodesView
	roles := map[string][]string{"reader": {permission}}
	repo := &agentRuntimeCallbackTestRepository{agentRun: domaincopilot.AgentRun{
		ID: "agent:chat-tool", CreatedBy: "reader", CapabilityID: "general",
		Status: domaincopilot.AgentRunStatusRunning, CallbackToken: "callback", ClaimedByAgentID: "runner",
		Input: map[string]any{"_sohaPrincipal": map[string]any{
			"userId": "reader", "roles": []string{"reader"}, "permissionKeys": []string{permission},
		}},
		ToolBindings: []domaincopilot.AgentToolBinding{{ID: "node", ToolKind: "mcp", ToolName: "k8s.nodes.detail", PermissionKey: permission}},
	}}
	service := newTestService(repo)
	service.permissions = appaccess.NewPermissionResolver(inspectionAuthzRoleReader{matrix: roles})
	service.agentPrincipals = &chatToolPrincipalResolver{principal: domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}}
	gateway := &chatGatewayToolStub{}
	service.workbenchInvoker = gateway
	call := domaincopilot.AgentToolCallInput{RunID: repo.agentRun.ID, CallbackToken: "callback", AgentID: "runner", ToolBindingID: "node", Input: map[string]any{"clusterId": "cluster", "nodeName": "node"}}
	if result, err := service.RecordAgentToolCall(context.Background(), call); err != nil || result.ToolExecution.Status != "success" {
		t.Fatalf("authorized tool: result=%+v err=%v", result, err)
	}
	roles["reader"] = nil
	if _, err := service.RecordAgentToolCall(context.Background(), call); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("revoked permission must deny next call: %v", err)
	}
	if len(repo.agentRun.ToolExecutions) != 1 || gateway.calls != 1 {
		t.Fatal("revoked call executed")
	}
	roles["reader"] = []string{permission}
	mapValue(repo.agentRun.Input["_sohaPrincipal"])["permissionKeys"] = []string{appaccess.PermObserveAIChatUse}
	if _, err := service.RecordAgentToolCall(context.Background(), call); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("current roles must not exceed request ceiling: %v", err)
	}
	service.agentPrincipals = nil
	if _, err := service.RecordAgentToolCall(context.Background(), call); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("missing identity service must fail closed: %v", err)
	}
}

func TestChatKnowledgeToolPinsBasesAndSharesEvidenceBudget(t *testing.T) {
	gateway := &chatGatewayToolStub{}
	service := &Service{workbenchInvoker: gateway}
	run := domaincopilot.AgentRun{Input: map[string]any{
		"_sohaPrincipal":        map[string]any{"userId": "reader", "roles": []string{"reader"}, "permissionKeys": []string{appaccess.PermAIKnowledgeView}},
		"_sohaKnowledgeBaseIds": []string{"selected"},
	}, ToolExecutions: []domaincopilot.ToolExecution{{Output: map[string]any{"estimatedTokens": 5999}}}}
	binding := domaincopilot.AgentToolBinding{ToolName: "knowledge.search"}
	output, err := service.executeChatAgentTool(context.Background(), run, binding, map[string]any{"query": "maintenance", "knowledgeBaseIds": []string{"unselected"}})
	if err != nil {
		t.Fatal(err)
	}
	ids := agentPrincipalStringList(gateway.input.Input["knowledgeBaseIds"])
	if len(ids) != 1 || ids[0] != "selected" || output["truncated"] != true || output["estimatedTokens"] != 1 {
		t.Fatalf("selection or budget bypass: ids=%v output=%v", ids, output)
	}
	run.ToolExecutions[0].Output["estimatedTokens"] = 6000
	if _, err := service.executeChatAgentTool(context.Background(), run, binding, map[string]any{"query": "maintenance"}); err == nil || gateway.calls != 1 {
		t.Fatal("exhausted budget reached Gateway")
	}
}

func TestChatCancellationCannotTargetAnotherUsersRun(t *testing.T) {
	service, repo := newInspectionAuthzTestService(map[string][]string{"chat": {appaccess.PermObserveAIChatUse}})
	repo.agentRuns = []domaincopilot.AgentRun{{ID: "private-run", CreatedBy: "owner", Status: "running"}}
	_, err := service.CancelAgentRun(context.Background(), domainidentity.Principal{UserID: "other", Roles: []string{"chat"}}, "private-run")
	if !errors.Is(err, apperrors.ErrNotFound) || repo.agentRuns[0].Status != "running" {
		t.Fatalf("cross-user cancel: %v", err)
	}
}
