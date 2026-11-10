package aigateway

import (
	"errors"
	"testing"

	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestPodCapabilityRecoveryUsesExactFrozenInputAndReadOnlyPermit(t *testing.T) {
	repo := &memoryGatewayRepository{aiClients: map[string]domainai.AIClient{"codex": {ID: "codex", Status: "active"}}}
	fixture := &podCapabilityFixture{receipt: podCapabilityReceipt()}
	s, _, actor := newPodCapabilityGateway(t, repo, fixture)
	call := domainai.ToolInvocationRequest{RequestID: "original-recovery", ToolName: protectedPodExecTool, CapabilityVersion: "1", AIClientID: "codex", SkillID: "k8s-pod-executor", Input: podCapabilityInput()}
	missing, err := s.recoverCapabilityCall(t.Context(), actor, call)
	if err != nil || missing != nil || fixture.executions != 0 {
		t.Fatal("missing receipt dispatched or fabricated a process", err)
	}
	_, err = s.InvokeTool(t.Context(), actor, call)
	if err != nil || len(repo.approvalRequests) != 1 {
		t.Fatal("approval missing", err)
	}
	_, err = s.ApproveApprovalRequest(t.Context(), testPrincipal("admin"), repo.approvalRequests[0].ID, domainai.ApprovalDecisionInput{})
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := s.recoverCapabilityCall(t.Context(), actor, call)
	if err != nil || recovered == nil || recovered.Task == nil || recovered.Task.ID != fixture.receipt.ID || fixture.executions != 1 || fixture.reads != 1 || len(repo.approvalRequests) != 1 {
		t.Fatal("recovery did not use original read-only receipt", err, recovered)
	}
	call.Input = podCapabilityInput()
	mustMapFieldAs[map[string]any](t, call.Input, "execution")["argv"] = []string{"different-original"}
	if _, err := s.recoverCapabilityCall(t.Context(), actor, call); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("another argv satisfied original recovery", err)
	}
	if fixture.executions != 1 {
		t.Fatal("recovery re-executed a process")
	}
	limited := actor
	limited.PermissionKeys = []string{"ai.gateway.invoke"}
	if _, err := s.recoverCapabilityCall(t.Context(), limited, call); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("recovery ignored credential ceiling", err)
	}
}
