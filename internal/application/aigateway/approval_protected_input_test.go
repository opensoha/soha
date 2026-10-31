package aigateway

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestProtectedApprovalInputRoundTripDoesNotExposeOrRewriteYAML(t *testing.T) {
	service := NewWithDeps(ServiceDeps{RelayConfig: LLMRelayConfig{CredentialEncryptionKey: "task115-protected-input-test-key"}})
	content := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: tls\nstringData:\n  tls.key: PRIVATE_MARKER\n  password: PASSWORD_MARKER\n"
	input := map[string]any{"clusterId": "cluster", "namespace": "test", "kind": "Secret", "name": "tls", "content": content, "preconditions": map[string]any{"expectedUid": "uid", "expectedResourceVersion": "rv"}}
	request := domainai.ApprovalRequest{ID: "approval", ToolName: protectedResourceUpdateTool, ActorType: "user", ActorID: "actor", AIClientID: "client", SkillID: "skill"}
	if err := service.protectApprovalInput(&request, input); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(request)
	if strings.Contains(string(raw), "PRIVATE_MARKER") || strings.Contains(string(raw), "PASSWORD_MARKER") || strings.Contains(string(raw), request.ProtectedToolInput) || strings.Contains(request.ProtectedToolInput, "PRIVATE_MARKER") {
		t.Fatal("approval API/storage leaked protected YAML")
	}
	if !strings.Contains(string(raw), "sha256:") {
		t.Fatal("review digest missing")
	}
	actual, err := service.approvalExecutionInput(request)
	if err != nil || actual["content"] != content {
		t.Fatal("exact approved YAML was rewritten", err)
	}
	if !approvalInputMatchesCall(request, input) {
		t.Fatal("frozen capability call did not match protected approval")
	}
	for _, scenario := range []string{"id", "actor", "tool", "target", "content", "cipher", "missing", "wrong-key"} {
		t.Run(scenario, func(t *testing.T) {
			copy := request
			copy.ToolInput = mergeAnyMaps(request.ToolInput, nil)
			current := service
			switch scenario {
			case "id":
				copy.ID = "other"
			case "actor":
				copy.ActorID = "other"
			case "tool":
				copy.ToolName = "other"
			case "target":
				copy.ToolInput["name"] = "other"
			case "content":
				copy.ToolInput["content"] = "other"
			case "cipher":
				copy.ProtectedToolInput += "tampered"
			case "missing":
				copy.ProtectedToolInput = ""
			case "wrong-key":
				current = NewWithDeps(ServiceDeps{RelayConfig: LLMRelayConfig{CredentialEncryptionKey: "different-key"}})
			}
			if _, err := current.approvalExecutionInput(copy); !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("tampered input accepted: %v", err)
			}
		})
	}
}

func TestProtectedApprovalInputFailsClosedWithoutKeyAndPreservesLegacy(t *testing.T) {
	service := NewWithDeps(ServiceDeps{})
	request := domainai.ApprovalRequest{ID: "approval", ToolName: protectedResourceUpdateTool}
	if err := service.protectApprovalInput(&request, map[string]any{"content": "sensitive"}); !errors.Is(err, apperrors.ErrServiceUnavailable) {
		t.Fatalf("unprotected storage accepted: %v", err)
	}
	legacy := domainai.ApprovalRequest{ToolName: "delivery.actions.trigger", ToolInput: map[string]any{"applicationId": "app"}}
	actual, err := service.approvalExecutionInput(legacy)
	if err != nil || actual["applicationId"] != "app" {
		t.Fatal("legacy approval changed", err)
	}
}
