package aigateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	contractresource "github.com/opensoha/soha/contracts/resource"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type podCapabilityFixture struct {
	executions, reads, scopes int
	check                     func(context.Context, domainidentity.Principal, domainai.ExecutionAuthorization) error
	input                     contractresource.PodArgvToolInput
	receipt                   contractresource.PodExecutionReceipt
}

func (f *podCapabilityFixture) ExecutePodArgv(ctx context.Context, principal domainidentity.Principal, input contractresource.PodArgvToolInput) (contractresource.PodExecutionReceipt, error) {
	permit, ok := domainai.ExecutionAuthorizationFrom(ctx)
	if !ok || permit.ActorTokenID != principal.AccessTokenID || permit.ApprovalID == "" {
		return f.receipt, apperrors.ErrAccessDenied
	}
	if err := f.check(ctx, principal, permit); err != nil {
		return f.receipt, err
	}
	f.executions++
	f.input = input
	return f.receipt, nil
}
func (f *podCapabilityFixture) GetPodExecution(_ context.Context, _ domainidentity.Principal, _ contractresource.PodExecutionLookupInput) (contractresource.PodExecutionReceipt, error) {
	f.reads++
	return f.receipt, nil
}

func (f *podCapabilityFixture) FindOriginalPodExecution(ctx context.Context, principal domainidentity.Principal, input contractresource.PodArgvToolInput) (contractresource.PodExecutionReceipt, error) {
	permit, ok := domainai.ExecutionAuthorizationFrom(ctx)
	if !ok || permit.Call.ToolName != "k8s.pods.exec.receipt" || permit.ActorTokenID != principal.AccessTokenID {
		return contractresource.PodExecutionReceipt{}, apperrors.ErrAccessDenied
	}
	if err := f.check(ctx, principal, permit); err != nil {
		return contractresource.PodExecutionReceipt{}, err
	}
	if f.executions == 0 {
		return contractresource.PodExecutionReceipt{}, apperrors.ErrNotFound
	}
	original, err := f.input.Normalized()
	if err != nil {
		return contractresource.PodExecutionReceipt{}, err
	}
	if !reflect.DeepEqual(original, input) {
		return contractresource.PodExecutionReceipt{}, apperrors.ErrConflict
	}
	f.reads++
	return f.receipt, nil
}
func (f *podCapabilityFixture) ResolvePodExecutionScope(_ context.Context, principal domainidentity.Principal, input contractresource.PodExecutionLookupInput) (map[string]string, error) {
	if principal.AccessTokenID == "" {
		return nil, apperrors.ErrUnsupportedOperation
	}
	if input.Validate() != nil {
		return nil, apperrors.ErrInvalidArgument
	}
	f.scopes++
	return map[string]string{"clusterId": input.ClusterID, "namespace": input.Namespace, "podName": input.PodName}, nil
}

func podCapabilityInput() map[string]any {
	return map[string]any{"clusterId": "cluster-a", "namespace": "team-a", "podName": "api", "idempotencyKey": "original", "execution": map[string]any{"expectedUid": "reviewed-uid", "container": "main", "argv": []string{"printf", "%s", "PRIVATE_ARGV_MARKER"}, "stdinBase64": base64.StdEncoding.EncodeToString([]byte("PRIVATE_STDIN_MARKER"))}}
}

func podCapabilityReceipt() contractresource.PodExecutionReceipt {
	code := int64(0)
	now := time.Now().UTC()
	return contractresource.PodExecutionReceipt{ID: "72dd7404-d216-49de-a15c-2087dbdbe5b1", Status: "succeeded", ClusterID: "cluster-a", Namespace: "team-a", PodName: "api", ExpectedUID: "reviewed-uid", Container: "main", CreatedAt: now, DeadlineAt: now.Add(time.Minute), ExpiresAt: now.Add(time.Hour), FinishedAt: &now, OutputAvailable: true,
		Output: &contractresource.PodExecutionOutput{Status: "succeeded", ExitCode: &code, Stdout: "ok", Stderr: "", StdoutEncoding: "utf-8", StderrEncoding: "utf-8", StdoutBytes: 2, MaxOutputBytes: 65536, UIDVerifiedBeforeStart: true}}
}

func newPodCapabilityGateway(t *testing.T, repo *memoryGatewayRepository, fixture *podCapabilityFixture) (*Service, CapabilityProvider, domainidentity.Principal) {
	t.Helper()
	provider, err := NewPodExecutionCapabilityProvider(fixture)
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{appaccess.PermAIGatewayView, appaccess.PermAIGatewayInvoke, appaccess.PermPlatformPodsExec, appaccess.PermObserveAIChatUse}
	service := newTestService(appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{"operator": keys, "admin": {appaccess.PermAIGatewayManage}}}), nil, repo)
	service.relayConfig.CredentialEncryptionKey = "task115-pod-capability-key"
	service.relayCredentials = newRelayCredentialCodec(service.relayConfig)
	service.SetCapabilityProviders(provider, BuiltinCapabilityProvider{})
	actor := testPrincipal("operator")
	actor.AccessTokenID = "original-credential"
	actor.PermissionKeys = keys
	actor.GovernedAI = &domainidentity.GovernedAIBinding{Mode: "gateway_only", AIClientID: "codex", SkillID: "k8s-pod-executor"}
	service.identity = &governedApprovalIdentity{principal: actor}
	fixture.check = service.CheckExecutionAuthorization
	return service, provider, actor
}

func TestPodCapabilityRequiresProtectedApprovalEvenWithAllowPolicy(t *testing.T) {
	repo := &memoryGatewayRepository{aiClients: map[string]domainai.AIClient{"codex": {ID: "codex", Status: "active"}}, accessPolicies: []domainai.AccessPolicy{{ID: "explicit-allow", Enabled: true, SubjectType: "role", SubjectID: "operator", Effect: "allow", ToolPatterns: []string{"k8s.pods.exec", "k8s.pods.exec.receipt"}, RiskLevels: []domainai.RiskLevel{domainai.RiskLevelHigh, domainai.RiskLevelRead}, ResourceScopes: map[string]any{"podName": "api"}, ApprovalPolicy: map[string]any{"strategy": "allow"}}}}
	fixture := &podCapabilityFixture{receipt: podCapabilityReceipt()}
	service, provider, actor := newPodCapabilityGateway(t, repo, fixture)
	call := domainai.ToolInvocationRequest{ToolName: protectedPodExecTool, CapabilityVersion: "1", AIClientID: "codex", SkillID: "k8s-pod-executor", Input: podCapabilityInput()}
	manifest, err := service.Capabilities(t.Context(), actor, domainai.ManifestRequest{AIClientID: call.AIClientID, SkillID: call.SkillID})
	if err != nil {
		t.Fatal(err)
	}
	assertPodExecRequiresApprovalInManifest(t, manifest)
	held, err := service.InvokeTool(t.Context(), actor, call)
	if err != nil || !held.RequiresApproval || held.Result == "success" || fixture.executions != 0 || len(repo.approvalRequests) != 1 {
		t.Fatalf("mandatory approval escaped: %v %+v", err, held)
	}
	request := repo.approvalRequests[0]
	assertProtectedPodApprovalRequest(t, request, actor)
	result, err := service.ApproveApprovalRequest(t.Context(), testPrincipal("admin"), request.ID, domainai.ApprovalDecisionInput{})
	if err != nil || result.Invocation == nil || result.Invocation.Task == nil || result.Invocation.Task.Outcome != "succeeded" || fixture.executions != 1 {
		t.Fatalf("approved execution failed: %v %+v", err, result)
	}
	if fixture.input.Execution.Argv[2] != "PRIVATE_ARGV_MARKER" || fixture.input.Execution.StdinBase64 != mapValue(call.Input["execution"])["stdinBase64"] {
		t.Fatal("approval changed exact argv/stdin")
	}
	verifyCreationReceiptSchema(t, provider.Tools()[0], result.Invocation.Output)
	lookup := domainai.ToolInvocationRequest{ToolName: "k8s.pods.exec.receipt", CapabilityVersion: "1", AIClientID: call.AIClientID, SkillID: call.SkillID, Input: map[string]any{"clusterId": "cluster-a", "namespace": "team-a", "podName": "api", "executionId": fixture.receipt.ID}}
	read, err := service.InvokeTool(t.Context(), actor, lookup)
	if err != nil || read.Task == nil || read.Task.ID != fixture.receipt.ID || fixture.executions != 1 || fixture.reads != 1 {
		t.Fatal("receipt resubmitted or lost original reference", err)
	}
}

func assertPodExecRequiresApprovalInManifest(t *testing.T, manifest domainai.Manifest) {
	t.Helper()
	found := false
	for _, tool := range manifest.Tools {
		if tool.Name == protectedPodExecTool {
			found = true
			if !tool.RequiresApproval {
				t.Fatal("allow policy hid mandatory approval in discovery")
			}
		}
	}
	if !found {
		t.Fatal("approved-scope exec tool missing from manifest")
	}
}

func assertProtectedPodApprovalRequest(t *testing.T, request domainai.ApprovalRequest, actor domainidentity.Principal) {
	t.Helper()
	raw, _ := json.Marshal(request)
	if strings.Contains(string(raw), "PRIVATE_") || request.ProtectedToolInput == "" || request.ActorTokenID != actor.AccessTokenID {
		t.Fatal("approval lost protection or original credential")
	}
}

func TestPodCapabilityRejectsReadOnlySkillMissingPermissionAndScopeBeforeApproval(t *testing.T) {
	for _, scenario := range []string{"readonly-skill", "permission", "pod-scope", "invalid-shell", "unbound"} {
		t.Run(scenario, func(t *testing.T) {
			repo := &memoryGatewayRepository{aiClients: map[string]domainai.AIClient{"codex": {ID: "codex", Status: "active"}}}
			fixture := &podCapabilityFixture{receipt: podCapabilityReceipt()}
			service, _, actor := newPodCapabilityGateway(t, repo, fixture)
			call := domainai.ToolInvocationRequest{ToolName: protectedPodExecTool, CapabilityVersion: "1", AIClientID: "codex", SkillID: "k8s-pod-executor", Input: podCapabilityInput()}
			switch scenario {
			case "readonly-skill":
				actor.GovernedAI.SkillID = "k8s-sre"
				call.SkillID = "k8s-sre"
			case "permission":
				actor.PermissionKeys = []string{appaccess.PermAIGatewayInvoke}
			case "pod-scope":
				repo.accessPolicies = []domainai.AccessPolicy{{ID: "other-pod", Enabled: true, SubjectType: "role", SubjectID: "operator", Effect: "allow", ToolPatterns: []string{"k8s.pods.exec"}, RiskLevels: []domainai.RiskLevel{domainai.RiskLevelHigh}, ResourceScopes: map[string]any{"podName": "other"}}}
			case "invalid-shell":
				call.Input["shell"] = "sh -c anything"
			case "unbound":
				actor.AccessTokenID = ""
			}
			if _, err := service.InvokeTool(t.Context(), actor, call); err == nil || fixture.executions != 0 || len(repo.approvalRequests) != 0 {
				t.Fatal("denied execution reached approval/provider", err)
			}
		})
	}
}

func TestPodGatewayOutputRedactsTextWithholdsBinaryAndPreservesOriginal(t *testing.T) {
	for _, scenario := range []string{"text", "redacted", "binary_withheld", "unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			receipt := podCapabilityReceipt()
			switch scenario {
			case "redacted":
				receipt.Output.Stdout = "authorization: Bearer private-bearer"
				receipt.Output.Stderr = "password=private-password"
			case "binary_withheld":
				receipt.Output.StdoutEncoding = "base64"
				receipt.Output.Stdout = base64.StdEncoding.EncodeToString([]byte("private-binary-secret"))
			case "unavailable":
				receipt.Output = nil
				receipt.OutputAvailable = false
			}
			before, _ := json.Marshal(receipt)
			result := podGatewayOutput(receipt)
			after, _ := json.Marshal(receipt)
			if string(before) != string(after) || result.Receipt.OutputAvailable || result.OutputState != scenario {
				t.Fatal("wrong output projection", result.OutputState)
			}
			raw, _ := json.Marshal(result)
			if strings.Contains(string(raw), "private-bearer") || strings.Contains(string(raw), "private-password") || strings.Contains(string(raw), "private-binary-secret") || strings.Contains(string(raw), base64.StdEncoding.EncodeToString([]byte("private-binary-secret"))) {
				t.Fatal("Gateway output leaked synthetic secret")
			}
			if (scenario == "text" || scenario == "redacted") != (result.Output != nil) {
				t.Fatal("wrong output availability")
			}
		})
	}
}

func TestPodCapabilityTaskOutcomeNeverTreatsUnknownAsSuccess(t *testing.T) {
	provider, err := NewPodExecutionCapabilityProvider(&podCapabilityFixture{})
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"running", "succeeded", "failed", "timed_out", "canceled", "transport_lost", "unknown"} {
		item := podCapabilityReceipt()
		item.Status = status
		output := podGatewayOutput(item)
		task := mustValueAs[ToolTaskReferenceProvider](t, provider).TaskReference(provider.Tools()[0], output, output)
		if task == nil || task.Terminal != (status != "running") || (task.Outcome == "succeeded") != (status == "succeeded") || task.CancelCall != nil {
			t.Fatalf("invalid %s task outcome: %+v", status, task)
		}
		if task.StatusCall.ToolName != "k8s.pods.exec.receipt" {
			t.Fatal("status can execute")
		}
		changed := output
		changed.Receipt.PodName = "different"
		if mustValueAs[ToolTaskReferenceProvider](t, provider).TaskReference(provider.Tools()[0], output, changed) != nil {
			t.Fatal("task restored hidden or changed target")
		}
	}
	for _, skill := range (BuiltinCapabilityProvider{}).Skills() {
		if (skill.ID == "k8s-sre" || skill.ID == "k8s-operator") && slices.Contains(skill.CapabilityRefs, protectedPodExecTool) {
			t.Fatal("existing skill gained exec")
		}
	}
	var missing *podCapabilityFixture
	if _, err := NewPodExecutionCapabilityProvider(missing); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatal("typed nil provider accepted")
	}
}

func TestProtectedPodInputCannotEnterDurablePlanOrNativeCheckpoint(t *testing.T) {
	repo := &memoryGatewayRepository{aiClients: map[string]domainai.AIClient{"codex": {ID: "codex", Status: "active"}}}
	fixture := &podCapabilityFixture{receipt: podCapabilityReceipt()}
	service, provider, actor := newPodCapabilityGateway(t, repo, fixture)
	tool := provider.Tools()[0]
	if !tool.Execution.Idempotent || tool.Execution.RecoveryMode != "original_call" {
		t.Fatal("original-key domain fence or receipt-only recovery was misrepresented")
	}
	step := domainai.CapabilityPlanStep{ID: "exec", Call: sohaapi.CapabilityCall{ToolName: tool.Name, CapabilityVersion: "1", Input: podCapabilityInput()}}
	issues := validatePlannedCapability(step, map[string]domainai.ToolCapability{tool.Name: tool}, false)
	if len(issues) != 1 || issues[0].Code != "protected_input_not_supported" {
		t.Fatal("protected argv/stdin can enter plain durable plan storage", issues)
	}
	manifest, err := service.WorkbenchCapabilities(t.Context(), actor, domainai.ManifestRequest{AIClientID: "codex", SkillID: "k8s-pod-executor"})
	if err != nil {
		t.Fatal(err)
	}
	lookupVisible := false
	for _, entry := range manifest.Tools {
		if entry.Name == tool.Name {
			t.Fatal("Native Chat advertises input it cannot protect at rest")
		}
		if entry.Name == "k8s.pods.exec.receipt" {
			lookupVisible = true
		}
	}
	if !lookupVisible {
		t.Fatal("safe receipt observation was removed")
	}
	ctx := WithNativeWorkbenchInvocation(t.Context())
	_, err = service.InvokeTool(ctx, actor, domainai.ToolInvocationRequest{ToolName: tool.Name, CapabilityVersion: "1", AIClientID: "codex", SkillID: "k8s-pod-executor", Input: podCapabilityInput()})
	if !errors.Is(err, apperrors.ErrUnsupportedOperation) || fixture.executions != 0 || len(repo.approvalRequests) != 0 {
		t.Fatal("Native protected-input rejection reached approval/provider", err)
	}
}
