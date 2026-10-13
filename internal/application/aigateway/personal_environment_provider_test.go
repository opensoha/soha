package aigateway

import (
	"context"
	"errors"
	"testing"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type personalEnvironmentInspectorStub struct {
	principal                   domainidentity.Principal
	calls                       int
	commandCalls, recoveryCalls int
	commandInput                domaincopilot.PersonalCommandInput
	command                     domaincopilot.PersonalCommand
	commandError                error
}

func (s *personalEnvironmentInspectorStub) ExecutePersonalCommand(_ context.Context, principal domainidentity.Principal, input domaincopilot.PersonalCommandInput) (domaincopilot.PersonalCommand, error) {
	s.principal, s.commandInput, s.commandCalls = principal, input, s.commandCalls+1
	return s.command, s.commandError
}
func (s *personalEnvironmentInspectorStub) FindPersonalCommand(_ context.Context, principal domainidentity.Principal, input domaincopilot.PersonalCommandInput) (domaincopilot.PersonalCommand, error) {
	s.principal, s.commandInput, s.recoveryCalls = principal, input, s.recoveryCalls+1
	return s.command, s.commandError
}
func (s *personalEnvironmentInspectorStub) GetPersonalCommand(_ context.Context, principal domainidentity.Principal, _ string) (domaincopilot.PersonalCommand, error) {
	s.principal = principal
	return s.command, s.commandError
}
func (s *personalEnvironmentInspectorStub) CancelPersonalCommand(_ context.Context, principal domainidentity.Principal, _ string) (domaincopilot.PersonalCommand, error) {
	s.principal = principal
	return s.command, s.commandError
}

func (s *personalEnvironmentInspectorStub) InspectPersonalEnvironment(_ context.Context, principal domainidentity.Principal) (domaincopilot.PersonalEnvironment, error) {
	s.principal, s.calls = principal, s.calls+1
	return domaincopilot.PersonalEnvironment{AssistantID: "owned-assistant", State: "unknown", Version: 7}, nil
}

func (s *personalEnvironmentInspectorStub) ReadPersonalEnvironmentFile(_ context.Context, principal domainidentity.Principal, path string) (domaincopilot.PersonalEnvironmentFile, error) {
	s.principal, s.calls = principal, s.calls+1
	return domaincopilot.PersonalEnvironmentFile{AssistantID: "owned-assistant", Path: path, EnvironmentVersion: 8, Content: "fixture"}, nil
}

func TestPersonalEnvironmentToolUsesAuthenticatedOwnerAndPreservesUnknownReceipt(t *testing.T) {
	t.Parallel()
	inspector := &personalEnvironmentInspectorStub{}
	provider := NewPersonalEnvironmentCapabilityProvider(inspector)
	tools := provider.Tools()
	if len(tools) != 7 || tools[0].Version != "1" || tools[0].InputSchema["additionalProperties"] != false {
		t.Fatalf("unbounded environment catalog: %+v", tools)
	}
	invoker, ok := provider.(ToolCapabilityInvoker)
	if !ok {
		t.Fatal("environment provider does not support invocation")
	}
	principal := domainidentity.Principal{UserID: "authenticated-owner"}
	output, related, err := invoker.InvokeTool(t.Context(), principal, tools[0], map[string]any{})
	view, ok := output.(domaincopilot.PersonalEnvironment)
	if err != nil || !ok || view.State != "unknown" || inspector.principal.UserID != principal.UserID || related["personalAssistantId"] != view.AssistantID || related["environmentVersion"] != view.Version {
		t.Fatalf("environment receipt lost owner/state: %+v %+v %v", output, related, err)
	}
	for _, input := range []map[string]any{{"ownerId": "other"}, {"nodeId": "node"}, {"command": []string{"/bin/sh"}}} {
		if _, _, err := invoker.InvokeTool(t.Context(), principal, tools[0], input); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("caller-selected environment accepted: %+v %v", input, err)
		}
	}
	if inspector.calls != 1 {
		t.Fatal("invalid input reached the environment service")
	}
}

func TestPersonalFileToolHasNoCallerSelectedOwnerNodeOrCommand(t *testing.T) {
	t.Parallel()
	inspector := &personalEnvironmentInspectorStub{}
	provider := NewPersonalEnvironmentCapabilityProvider(inspector)
	invoker, ok := provider.(ToolCapabilityInvoker)
	if !ok {
		t.Fatal("missing file invoker")
	}
	tool := provider.Tools()[1]
	principal := domainidentity.Principal{UserID: "authenticated-owner"}
	for _, input := range []map[string]any{{}, {"path": nil}, {"path": "notes.txt", "ownerId": "foreign"}, {"path": "notes.txt", "argv": []string{"/bin/sh"}}} {
		if _, _, err := invoker.InvokeTool(t.Context(), principal, tool, input); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("unbounded file input accepted: %v", err)
		}
	}
	if inspector.calls != 0 {
		t.Fatal("invalid file input was dispatched")
	}
	output, related, err := invoker.InvokeTool(t.Context(), principal, tool, map[string]any{"path": "notes.txt"})
	file, valid := output.(domaincopilot.PersonalEnvironmentFile)
	if err != nil || !valid || file.Path != "notes.txt" || inspector.principal.UserID != principal.UserID || related["environmentVersion"] != file.EnvironmentVersion {
		t.Fatalf("file observation lost its authenticated owner: %+v %v", output, err)
	}
}
