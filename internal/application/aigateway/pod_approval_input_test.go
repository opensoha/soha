package aigateway

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestPodApprovalProtectsExactArgvStdinAndCredential(t *testing.T) {
	// #nosec G101 -- synthetic encryption key used only by this in-memory approval fixture.
	service := NewWithDeps(ServiceDeps{RelayConfig: LLMRelayConfig{CredentialEncryptionKey: "task115-pod-approval-key"}})
	stdin := base64.StdEncoding.EncodeToString([]byte("PRIVATE_STDIN_MARKER"))
	input := map[string]any{"clusterId": "cluster-a", "namespace": "team-a", "podName": "api", "idempotencyKey": "original", "execution": map[string]any{"expectedUid": "original-uid", "container": "api", "argv": []string{"printf", "%s", "PRIVATE_ARGV_MARKER"}, "stdinBase64": stdin, "timeoutSeconds": 10, "maxOutputBytes": 65536}}
	request := domainai.ApprovalRequest{ID: "approval", ToolName: protectedPodExecTool, ActorType: "user", ActorID: "actor", ActorTokenID: "credential", ActorPermissionCeiling: []string{"ai.gateway.invoke", "platform.pods.exec"}, AIClientID: "client", SkillID: "k8s-pod-executor"}
	if err := service.protectApprovalInput(&request, input); err != nil {
		t.Fatal(err)
	}
	public, err := json.Marshal(request)
	if err != nil || strings.Contains(string(public), "PRIVATE_") || strings.Contains(string(public), stdin) || strings.Contains(string(public), request.ProtectedToolInput) || strings.Contains(request.ProtectedToolInput, "PRIVATE_") {
		t.Fatal("approval exposed argv/stdin or ciphertext")
	}
	display := mapValue(request.ToolInput["execution"])
	if display["argvCount"] != 3 || display["stdinBytes"] != len("PRIVATE_STDIN_MARKER") || !strings.Contains(string(public), "sha256:") {
		t.Fatal("approval lost bounded intent summary")
	}
	exact, err := service.approvalExecutionInput(request)
	if err != nil || !executionJSONEqual(exact, input) || !approvalInputMatchesCall(request, input) {
		t.Fatal("approval rewrote exact argv/stdin", err)
	}
	for _, scenario := range []string{"id", "actor", "credential", "ceiling", "client", "skill", "tool", "target", "digest", "cipher", "missing", "wrong-key"} {
		t.Run(scenario, func(t *testing.T) {
			podApprovalProtectsExactArgvStdinAndCredentialPhase1(t, &service, &request, &scenario)
		})
	}
	for _, field := range []string{"argv", "stdinBase64", "expectedUid", "container", "timeoutSeconds", "maxOutputBytes", "unknown"} {
		copy := mergeAnyMaps(input, nil)
		copy["execution"] = mergeAnyMaps(mapValue(input["execution"]), map[string]any{field: "different"})
		if approvalInputMatchesCall(request, copy) {
			t.Fatalf("changed %s matched approval", field)
		}
	}
}

func TestPodApprovalFailsClosedWithoutPersistentKeyOrCredential(t *testing.T) {
	request := domainai.ApprovalRequest{ID: "approval", ToolName: protectedPodExecTool, ActorTokenID: "credential"}
	if err := NewWithDeps(ServiceDeps{}).protectApprovalInput(&request, map[string]any{}); !errors.Is(err, apperrors.ErrServiceUnavailable) {
		t.Fatal("Pod input stored without persistent encryption", err)
	}
	request.ActorTokenID = ""
	// #nosec G101 -- synthetic encryption key used only by this in-memory approval fixture.
	service := NewWithDeps(ServiceDeps{RelayConfig: LLMRelayConfig{CredentialEncryptionKey: "task115-pod-approval-key"}})
	if err := service.protectApprovalInput(&request, map[string]any{}); !errors.Is(err, apperrors.ErrUnsupportedOperation) {
		t.Fatal("unbound Pod approval accepted", err)
	}
}

func podApprovalProtectsExactArgvStdinAndCredentialPhase1(t *testing.T, sharedService **Service, sharedRequest *domainai.ApprovalRequest, sharedScenario *string) {

	copy := (*sharedRequest)
	copy.ToolInput = mergeAnyMaps((*sharedRequest).ToolInput, nil)
	current := (*sharedService)
	switch *sharedScenario {
	case "id":
		copy.ID = "different"
	case "actor":
		copy.ActorID = "different"
	case "credential":
		copy.ActorTokenID = "different"
	case "ceiling":
		copy.ActorPermissionCeiling = []string{"*"}
	case "client":
		copy.AIClientID = "different"
	case "skill":
		copy.SkillID = "k8s-sre"
	case "tool":
		copy.ToolName = protectedResourceUpdateTool
	case "target":
		copy.ToolInput["podName"] = "different"
	case "digest":
		copy.ToolInput["inputDigest"] = "different"
	case "cipher":
		copy.ProtectedToolInput += "tampered"
	case "missing":
		copy.ProtectedToolInput = ""
	case "wrong-key":
		current = NewWithDeps(ServiceDeps{RelayConfig: LLMRelayConfig{CredentialEncryptionKey: "different-key"}})
	}
	if _, err := current.approvalExecutionInput(copy); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("borrowed or changed protected execution accepted", err)
	}
}
