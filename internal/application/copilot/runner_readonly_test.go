package copilot

import (
	"context"
	"errors"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
)

type readOnlyRunnerRepository struct {
	*agentRuntimeCallbackTestRepository
	creates int
}

func (r *readOnlyRunnerRepository) CreateDelegatedAgentRun(context.Context, string, domaincopilot.AgentRun) (domaincopilot.AgentRun, error) {
	r.creates++
	return domaincopilot.AgentRun{}, nil
}

func TestReadOnlyRunnerCannotExecuteDelegateOrCallbackWithValidCredentials(t *testing.T) {
	for _, reason := range []string{"native", "explicit", "reconciliation"} {
		t.Run(reason, func(t *testing.T) {
			permission := appaccess.PermPlatformNodesView
			run := domaincopilot.AgentRun{ID: "external", ProviderID: "hermes-api", ProviderKind: "hermes", CreatedBy: "reader", CapabilityID: "general", Status: "running", CallbackToken: "valid-token", ClaimedByAgentID: "valid-runner", Input: map[string]any{"_sohaPrincipal": map[string]any{"userId": "reader", "roles": []string{"reader"}, "permissionKeys": []string{permission}}}, ToolBindings: []domaincopilot.AgentToolBinding{{ID: "node", ToolName: "k8s.nodes.detail", PermissionKey: permission}, {ID: "delegate", ToolName: "agent.delegate", PermissionKey: appaccess.PermObserveAIChatUse}}, Output: map[string]any{}}
			switch reason {
			case "native":
				run.ProviderKind = "native"
			case "explicit":
				run.Output["executionReadOnly"] = true
			case "reconciliation":
				run.Output["stopReason"] = "legacy_reconciliation_required"
			}
			repo := &readOnlyRunnerRepository{agentRuntimeCallbackTestRepository: &agentRuntimeCallbackTestRepository{agentRun: run}}
			s := newTestService(repo.agentRuntimeCallbackTestRepository)
			s.agentRuns = repo
			s.permissions = appaccess.NewPermissionResolver(inspectionAuthzRoleReader{matrix: map[string][]string{"reader": {permission}}})
			s.agentPrincipals = &chatToolPrincipalResolver{principal: domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}}
			gateway := &chatGatewayToolStub{}
			s.runtime.Tools = gateway
			for _, binding := range []string{"node", "delegate"} {
				_, err := s.RecordAgentToolCall(t.Context(), domaincopilot.AgentToolCallInput{RunID: run.ID, CallbackToken: run.CallbackToken, AgentID: run.ClaimedByAgentID, ToolBindingID: binding, Input: map[string]any{"query": "evidence", "clusterId": "cluster", "nodeName": "node"}})
				if !errors.Is(err, apperrors.ErrAccessDenied) {
					t.Fatalf("readonly tool %s: %v", binding, err)
				}
			}
			if _, err := s.delegateChatSpecialist(t.Context(), run, map[string]any{"query": "evidence"}); !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("readonly delegate: %v", err)
			}
			_, err := s.RecordAgentRunCallback(t.Context(), domaincopilot.AgentRunCallbackInput{RunID: run.ID, CallbackToken: run.CallbackToken, AgentID: run.ClaimedByAgentID, Status: "completed", Payload: map[string]any{"executionReadOnly": false, "answer": "late"}})
			if !errors.Is(err, apperrors.ErrAccessDenied) || gateway.calls != 0 || repo.creates != 0 || repo.callback.RunID != "" || len(repo.agentRun.ToolExecutions) != 0 || repo.agentRun.Status != "running" {
				t.Fatalf("readonly runner mutated: %v tools=%d creates=%d callback=%+v", err, gateway.calls, repo.creates, repo.callback)
			}
		})
	}
}
