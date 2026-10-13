package aigateway

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestPersonalCommandProviderRequiresServerTaskIdentityAndRecoversWithoutDispatch(t *testing.T) {
	t.Parallel()
	owner := domainidentity.Principal{UserID: "owner"}
	parent := domaincopilot.NativeTaskParent{RunID: "agent:" + uuid.NewString(), OwnerID: owner.UserID, Deadline: time.Now().Add(time.Minute)}
	ctx := context.WithValue(t.Context(), nativeCapabilityParentKey{}, nativeCapabilityParentScope{parent: parent})
	ctx = context.WithValue(ctx, capabilityExecutionKey{}, domainworkflow.NodeExecution{Scope: domainworkflow.ScopeCapabilityTask})
	stub := &personalEnvironmentInspectorStub{command: domaincopilot.PersonalCommand{ID: uuid.NewString(), State: "unknown"}}
	provider := &personalEnvironmentCapabilityProvider{inspector: stub}
	tool := personalCommandCapabilities()[1]
	input := map[string]any{"argv": []string{"/bin/echo", "fixture"}, "idempotencyKey": "fixed"}
	assertPersonalCommandRejectsCallerAuthority(t, provider, stub, ctx, owner, input)
	output, _, err := provider.InvokeTool(ctx, owner, tool, input)
	if err != nil || stub.commandCalls != 1 || stub.commandInput.TaskID != domaincopilot.PersonalTaskID(parent.RunID) || !stub.commandInput.TaskDeadline.Equal(parent.Deadline) || stub.principal.UserID != owner.UserID {
		t.Fatalf("server identity was lost: %+v %v", stub.commandInput, err)
	}
	ref := provider.TaskReference(tool, output, output)
	if ref == nil || ref.Terminal || ref.Outcome != "unknown" || ref.CancelCall.Input["commandId"] != stub.command.ID {
		t.Fatalf("unknown result marked complete: %+v", ref)
	}
	recovered, found, err := provider.RecoverTool(ctx, owner, tool, input)
	if err != nil || !found || recovered == nil || stub.recoveryCalls != 1 || stub.commandCalls != 1 {
		t.Fatal("recovery dispatched or changed the original record")
	}
	stub.commandError = apperrors.ErrNotFound
	if _, found, err := provider.RecoverTool(ctx, owner, tool, input); err != nil || found || stub.commandCalls != 1 {
		t.Fatal("missing recovery record was recreated")
	}
}

func assertPersonalCommandRejectsCallerAuthority(t *testing.T, provider *personalEnvironmentCapabilityProvider, stub *personalEnvironmentInspectorStub, ctx context.Context, owner domainidentity.Principal, input map[string]any) {
	t.Helper()
	tool := personalCommandCapabilities()[1]
	if _, _, err := provider.InvokeTool(t.Context(), owner, tool, input); !errors.Is(err, apperrors.ErrAccessDenied) || stub.commandCalls != 0 {
		t.Fatal("direct tool invocation supplied task authority")
	}
	for _, field := range []string{"taskId", "deadline", "nodeId", "ownerId", "quota"} {
		invalid := map[string]any{"argv": input["argv"], "idempotencyKey": "fixed", field: "forged"}
		if _, _, err := provider.InvokeTool(ctx, owner, tool, invalid); !errors.Is(err, apperrors.ErrInvalidArgument) || stub.commandCalls != 0 {
			t.Fatalf("caller supplied %s: %v", field, err)
		}
	}
}

func TestPersonalCommandAssessmentRequiresActualRecentReceipt(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	for _, state := range []string{"dispatching", "unknown", "cancelling", "cancellation_unknown", "not_dispatched", "canceled", "succeeded"} {
		item := domaincopilot.PersonalCommand{ID: "command", State: state, ObservedAt: &now, OutputDigest: "sha256:" + strings.Repeat("1", 64)}
		assessment := personalCommandAssessment(item)
		if (assessment.Verdict == "satisfied") != (state == "succeeded") {
			t.Fatalf("%s: %+v", state, assessment)
		}
	}
	old, future := now.Add(-6*time.Minute), now.Add(2*time.Minute)
	for _, observed := range []*time.Time{nil, &old, &future} {
		if personalCommandAssessment(domaincopilot.PersonalCommand{State: "succeeded", ObservedAt: observed, OutputDigest: "sha256:" + strings.Repeat("1", 64)}).Verdict == "satisfied" {
			t.Fatal("missing/stale/future success receipt satisfied assessment")
		}
	}
	if personalCommandReadiness(domaincopilot.PersonalEnvironment{State: "ready"}).Verdict == "satisfied" {
		t.Fatal("ready state without Agent receipt passed preflight")
	}
	provider := &personalEnvironmentCapabilityProvider{}
	tool := personalCommandCapabilities()[1]
	item := domaincopilot.PersonalCommand{ID: "command", State: "succeeded"}
	if provider.TaskReference(tool, item, map[string]any{"status": "succeeded"}) != nil {
		t.Fatal("redacted command identity retained an actionable task reference")
	}
}
