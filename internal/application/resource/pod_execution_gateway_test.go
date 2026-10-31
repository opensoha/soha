package resource

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	contractresource "github.com/opensoha/soha-contracts/resource"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type gatewayNativeIdentity struct{ calls atomic.Int64 }

func (s *gatewayNativeIdentity) CurrentStreamingPrincipal(_ context.Context, principal domainidentity.Principal, _ domainidentity.AccessContext) (domainidentity.Principal, error) {
	s.calls.Add(1)
	if principal.GovernedAI != nil {
		return domainidentity.Principal{}, apperrors.ErrUnauthorized
	}
	return principal, nil
}

type gatewayPodAuthorizationStub struct {
	checks  atomic.Int64
	denied  atomic.Bool
	capture sync.Once
	context context.Context
}

func (s *gatewayPodAuthorizationStub) resolve(_ context.Context, principal domainidentity.Principal) (domainidentity.Principal, error) {
	if principal.GovernedAI == nil {
		return domainidentity.Principal{}, apperrors.ErrUnauthorized
	}
	return principal, nil
}

func (s *gatewayPodAuthorizationStub) check(ctx context.Context, _ domainidentity.Principal, _ domainai.ExecutionAuthorization) error {
	s.capture.Do(func() { s.context = ctx })
	s.checks.Add(1)
	if s.denied.Load() {
		return apperrors.ErrAccessDenied
	}
	return nil
}

func gatewayPodTestFixture(t *testing.T) (*GatewayPodExecutions, *executionMemoryStore, *podExecutionDirectStub, *gatewayPodAuthorizationStub, domainidentity.Principal, contractresource.PodArgvToolInput) {
	t.Helper()
	w, store, direct, _ := podExecutionTestService()
	w.executionIdentity = &gatewayNativeIdentity{}
	authorizer := &gatewayPodAuthorizationStub{}
	gateway, err := NewGatewayPodExecutions(w, authorizer.resolve, authorizer.check)
	if err != nil {
		t.Fatal(err)
	}
	principal := domainidentity.Principal{UserID: "actor", AccessTokenID: "original-token", GovernedAI: &domainidentity.GovernedAIBinding{Mode: "gateway_only", AIClientID: "codex", SkillID: "k8s-pod-executor"}}
	input := contractresource.PodArgvToolInput{ClusterID: "cluster-a", Namespace: "team-a", PodName: "app", IdempotencyKey: "original", Execution: podExecutionTestInput()}
	return gateway, store, direct, authorizer, principal, input
}

func gatewayPodTestAuthorization(t *testing.T, principal domainidentity.Principal, tool string, input any) domainai.ExecutionAuthorization {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	if json.Unmarshal(raw, &values) != nil {
		t.Fatal("input cannot be represented")
	}
	return domainai.ExecutionAuthorization{ActorID: principal.UserID, ActorTokenID: principal.AccessTokenID, ApprovalID: "reviewed-approval", Call: domainai.ToolInvocationRequest{ToolName: tool, CapabilityVersion: "1", Input: values}}
}

func TestGatewayPodExecutionUsesOriginalExecutorAndReadOnlyReceipt(t *testing.T) {
	gateway, _, direct, authorizer, principal, input := gatewayPodTestFixture(t)
	permit := gatewayPodTestAuthorization(t, principal, gatewayPodExecTool, input)
	ctx := domainai.WithExecutionAuthorization(t.Context(), permit)
	first, err := gateway.ExecutePodArgv(ctx, principal, input)
	if err != nil || first.Status != "succeeded" || first.Output == nil || direct.calls.Load() != 1 || authorizer.checks.Load() < 3 {
		t.Fatalf("delegated original execution failed: %v", err)
	}
	second, err := gateway.ExecutePodArgv(ctx, principal, input)
	if err != nil || first.ID != second.ID || direct.calls.Load() != 1 {
		t.Fatal("original key repeated provider execution")
	}
	for _, byID := range []bool{false, true} {
		lookup := contractresource.PodExecutionLookupInput{ClusterID: input.ClusterID, Namespace: input.Namespace, PodName: input.PodName, IdempotencyKey: input.IdempotencyKey}
		if byID {
			lookup.ExecutionID, lookup.IdempotencyKey = first.ID, ""
		}
		read := domainai.WithExecutionAuthorization(t.Context(), gatewayPodTestAuthorization(t, principal, gatewayPodExecReceiptTool, lookup))
		item, err := gateway.GetPodExecution(read, principal, lookup)
		if err != nil || item.ID != first.ID || direct.calls.Load() != 1 {
			t.Fatal("receipt lookup submitted a provider or changed the original receipt")
		}
	}
	identity, ok := gateway.workloads.executionIdentity.(*gatewayNativeIdentity)
	if !ok || identity.calls.Load() != 0 {
		t.Fatal("Gateway cleared governed provenance and used native streaming identity")
	}
}

func TestGatewayPodExecutionRejectsBorrowedPermitBeforeClaim(t *testing.T) {
	for _, scenario := range []string{"cluster", "namespace", "pod", "key", "uid", "container", "argv", "stdin", "timeout", "output", "actor", "credential", "missing-credential", "tool", "version", "unbound-permit", "no-approval", "no-permit", "denied"} {
		t.Run(scenario, func(t *testing.T) { gatewayPodExecutionRejectsBorrowedPermitBeforeClaimPhase1(t, &scenario) })
	}
}

func TestGatewayPodPrivateContextCannotBeBorrowedByNativeExecution(t *testing.T) {
	gateway, store, direct, authorizer, principal, input := gatewayPodTestFixture(t)
	ctx := domainai.WithExecutionAuthorization(t.Context(), gatewayPodTestAuthorization(t, principal, gatewayPodExecTool, input))
	if _, err := gateway.workloads.ExecutePodArgv(ctx, principal, domainidentity.AccessContext{}, input.ClusterID, input.Namespace, input.PodName, input.IdempotencyKey, input.Execution); !errors.Is(err, apperrors.ErrUnauthorized) || len(store.records) != 0 {
		t.Fatal("a generic Gateway permit opened native streaming")
	}
	if _, err := gateway.ExecutePodArgv(ctx, principal, input); err != nil {
		t.Fatal(err)
	}
	if _, err := gateway.workloads.ExecutePodArgv(authorizer.context, principal, domainidentity.AccessContext{}, input.ClusterID, input.Namespace, input.PodName, "borrowed-key", input.Execution); !errors.Is(err, apperrors.ErrAccessDenied) || direct.calls.Load() != 1 || len(store.records) != 1 {
		t.Fatal("private context was borrowed for another attempt")
	}
}

func TestGatewayPodRevocationCancelsAndHidesOutputWithoutReplay(t *testing.T) {
	gateway, store, direct, authorizer, principal, input := gatewayPodTestFixture(t)
	direct.execute = func(ctx context.Context, _ contractresource.PodArgvInput) (contractresource.PodArgvResult, error) {
		authorizer.denied.Store(true)
		select {
		case <-ctx.Done():
			return contractresource.PodArgvResult{}, ctx.Err()
		case <-time.After(5 * time.Second):
			return contractresource.PodArgvResult{}, errors.New("authorization guard did not cancel")
		}
	}
	ctx := domainai.WithExecutionAuthorization(t.Context(), gatewayPodTestAuthorization(t, principal, gatewayPodExecTool, input))
	if _, err := gateway.ExecutePodArgv(ctx, principal, input); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("revoked execution did not fail closed: %v", err)
	}
	for _, record := range store.records {
		if record.Receipt.Status != "unknown" || record.Receipt.Output != nil || record.Receipt.OutputAvailable {
			t.Fatal("revoked execution retained an authorized output")
		}
	}
	authorizer.denied.Store(false)
	item, err := gateway.ExecutePodArgv(ctx, principal, input)
	if err != nil || item.Status != "unknown" || direct.calls.Load() != 1 {
		t.Fatal("revoked unknown attempt was replayed")
	}
}

func TestGatewayPodScopeAdmissionIsReadOnlyAndUsesCurrentCredential(t *testing.T) {
	for _, scenario := range []string{"unchanged", "missing-token", "changed-token", "changed-actor", "changed-governance", "revoked", "permission"} {
		t.Run(scenario, func(t *testing.T) {
			gateway, store, direct, _, principal, input := gatewayPodTestFixture(t)
			current := principal
			switch scenario {
			case "missing-token":
				principal.AccessTokenID = ""
			case "changed-token":
				current.AccessTokenID = "other"
			case "changed-actor":
				current.UserID = "other"
			case "changed-governance":
				current.GovernedAI = nil
			case "permission":
				gateway.workloads.authorizer = exactPermissionAuthorizer{}
			}
			gateway.resolve = func(context.Context, domainidentity.Principal) (domainidentity.Principal, error) {
				if scenario == "revoked" {
					return current, apperrors.ErrUnauthorized
				}
				return current, nil
			}
			lookup := contractresource.PodExecutionLookupInput{ClusterID: input.ClusterID, Namespace: input.Namespace, PodName: input.PodName, IdempotencyKey: input.IdempotencyKey}
			scope, err := gateway.ResolvePodExecutionScope(t.Context(), principal, lookup)
			if scenario == "unchanged" {
				if err != nil || scope["clusterId"] != input.ClusterID || scope["namespace"] != input.Namespace || scope["podName"] != input.PodName {
					t.Fatal("wrong authorized scope", err)
				}
			} else if err == nil {
				t.Fatal("invalid current identity/scope accepted")
			}
			if direct.calls.Load() != 0 || len(store.records) != 0 {
				t.Fatal("scope admission executed a process")
			}
		})
	}
}

func gatewayPodExecutionRejectsBorrowedPermitBeforeClaimPhase1(t *testing.T, sharedScenario *string) {

	gateway, store, direct, authorizer, principal, input := gatewayPodTestFixture(t)
	permit := gatewayPodTestAuthorization(t, principal, gatewayPodExecTool, input)
	tamperBorrowedPodInput(*sharedScenario, &input)
	switch *sharedScenario {
	case "actor":
		principal.UserID = "other"
	case "credential":
		principal.AccessTokenID = "other"
	case "missing-credential":
		principal.AccessTokenID = ""
	case "tool":
		permit.Call.ToolName = gatewayPodExecReceiptTool
	case "version":
		permit.Call.CapabilityVersion = "2"
	case "unbound-permit":
		permit.ActorTokenID = ""
	case "no-approval":
		permit.ApprovalID = ""
	case "denied":
		authorizer.denied.Store(true)
	}
	ctx := t.Context()
	if (*sharedScenario) != "no-permit" {
		ctx = domainai.WithExecutionAuthorization(ctx, permit)
	}
	if _, err := gateway.ExecutePodArgv(ctx, principal, input); err == nil || direct.calls.Load() != 0 || len(store.records) != 0 {
		t.Fatal("borrowed, changed or denied intent reached claim/provider")
	}
}

func tamperBorrowedPodInput(scenario string, input *contractresource.PodArgvToolInput) {
	switch scenario {
	case "cluster":
		input.ClusterID = "other"
	case "namespace":
		input.Namespace = "other"
	case "pod":
		input.PodName = "other"
	case "key":
		input.IdempotencyKey = "other"
	case "uid":
		input.Execution.ExpectedUID = "other"
	case "container":
		input.Execution.Container = "other"
	case "argv":
		input.Execution.Argv = []string{"different"}
	case "stdin":
		input.Execution.StdinBase64 = "aGVsbG8="
	case "timeout":
		input.Execution.TimeoutSeconds = 1
	case "output":
		input.Execution.MaxOutputBytes = 1
	}
}
