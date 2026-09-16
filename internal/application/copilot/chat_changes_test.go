package copilot

import (
	"context"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"testing"
)

type fakeChatChanges struct {
	fakeWorkbenchModelInvoker
	calls int
}

func (f *fakeChatChanges) RequestToolApproval(context.Context, domainidentity.Principal, domainaigateway.ToolInvocationRequest) (domainaigateway.ToolInvocationResult, error) {
	f.calls++
	return domainaigateway.ToolInvocationResult{Result: "pending_approval", RelatedIDs: map[string]any{"approvalRequestId": "approval-1"}}, nil
}

func TestChatChangeRepeatKeepsSavedRequest(t *testing.T) {
	gateway := &fakeChatChanges{}
	service := &Service{workbenchInvoker: gateway}
	run := domaincopilot.AgentRun{Input: map[string]any{"_sohaPrincipal": map[string]any{"userId": "user", "roles": []string{"admin"}, "permissionKeys": []string{"ai.gateway.invoke"}}}}
	input := map[string]any{"toolName": "delivery.applications.create", "arguments": map[string]any{"name": "Example", "key": "example"}}
	output, err := service.requestChatChange(context.Background(), run, input)
	if err != nil {
		t.Fatal(err)
	}
	run.ToolExecutions = []domaincopilot.ToolExecution{{ToolName: "change.request", Status: "success", Input: map[string]any{"input": input}, Output: output}}
	if _, err = service.requestChatChange(context.Background(), run, input); err != nil || gateway.calls != 1 {
		t.Fatalf("repeat created another request: %v, %d", err, gateway.calls)
	}
	changed := map[string]any{"toolName": "delivery.applications.create", "arguments": map[string]any{"name": "Changed", "key": "changed"}}
	if _, err = service.requestChatChange(context.Background(), run, changed); err == nil || gateway.calls != 1 {
		t.Fatal("changed arguments reused approval or created an extra request")
	}
}

func TestChatChangeArgumentsRejectUnexpectedAuthority(t *testing.T) {
	valid := map[string]any{"name": "Example", "key": "example", "enabled": false}
	if _, err := chatChangeArguments("delivery.applications.create", valid); err != nil {
		t.Fatal(err)
	}
	for _, input := range []map[string]any{
		{"name": "Example", "key": "example", "approved": true},
		{"name": "Example", "key": "example", "enabled": "true"},
		{"name": "Example"},
		{"applicationId": "app", "applicationEnvironmentId": "env", "action": "delete"},
	} {
		if _, err := chatChangeArguments("delivery.applications.create", input); err == nil {
			t.Fatalf("unexpected arguments accepted: %#v", input)
		}
	}
}
