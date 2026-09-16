package copilot

import (
	"context"
	"errors"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestChatArtifactPersistsOnlyAnOwnedStaticDraft(t *testing.T) {
	permission := appaccess.PermObserveAIChatUse
	repo := &agentRuntimeCallbackTestRepository{agentRun: domaincopilot.AgentRun{
		ID: "run", CreatedBy: "reader", ClaimedByAgentID: "runner", CallbackToken: "callback", CapabilityID: "general", Status: "running",
		ToolBindings: []domaincopilot.AgentToolBinding{{ID: "draft", ToolKind: "internal_api", ToolName: "artifact.preview", PermissionKey: permission}},
		Input: map[string]any{
			"_sohaPrincipal": map[string]any{"userId": "reader", "roles": []string{"reader"}, "permissionKeys": []string{permission}},
			"_sohaEvidence":  []domaincopilot.ContextEvidence{{CitationID: "baseline", Content: "replicas: 2\n"}},
		},
	}}
	service := newTestService(repo)
	service.permissions = appaccess.NewPermissionResolver(inspectionAuthzRoleReader{matrix: map[string][]string{"reader": {permission}}})
	service.agentPrincipals = &chatToolPrincipalResolver{principal: domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}}
	input := map[string]any{"title": "deployment.yaml", "artifactKind": "configuration_preview", "format": "yaml", "content": "replicas: 3\n", "baselineCitationId": "baseline"}
	result, err := service.RecordAgentToolCall(context.Background(), domaincopilot.AgentToolCallInput{RunID: "run", AgentID: "runner", CallbackToken: "callback", ToolBindingID: "draft", Input: input})
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.agentRun.AnalysisArtifacts) != 1 {
		t.Fatal("draft not persisted")
	}
	artifact := repo.agentRun.AnalysisArtifacts[0]
	if artifact.RunID != result.Output["artifactId"] || artifact.DataSourceSnapshot["baseline"] != "replicas: 2\n" || artifact.DataSourceSnapshot["content"] != "replicas: 3\n" || artifact.DataSourceSnapshot["state"] != "draft" {
		t.Fatalf("wrong artifact: %+v", artifact)
	}
	if _, exists := result.Output["_artifact"]; exists {
		t.Fatal("internal artifact payload leaked into model context")
	}
	input["baselineCitationId"] = "another-turn"
	if _, err := chatArtifactPreview(repo.agentRun, input); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("unbound baseline accepted: %v", err)
	}
}
