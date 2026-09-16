package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
)

func TestDeliveryRestoresPrivateGatewayAuthorizationAcrossWorkers(t *testing.T) {
	definition := domainworkflow.DeliveryWorkflowDefinition{Name: "gateway delivery", Targets: deliveryCheckTargets(1)}
	service, repo, runtime := newDeliveryExecutorCheck(t, definition)
	principal := domainidentity.Principal{UserID: "actor", Roles: []string{"delivery-test"}}
	authorization := domainai.ExecutionAuthorization{ActorID: "actor", ApprovalID: "original-approval", Call: domainai.ToolInvocationRequest{ToolName: "delivery.batches.create", CapabilityVersion: "1", AIClientID: "original-client", Input: map[string]any{"sensitiveFixture": "not-for-client"}}}
	ctx := domainai.WithExecutionAuthorization(context.Background(), authorization)
	input := domainworkflow.DeliveryBatchInput{IdempotencyKey: "gateway-delivery", Definition: &definition}
	if _, err := service.CreateDeliveryBatch(ctx, principal, input); !errors.Is(err, apperrors.ErrInvalidArgument) || repo.creates != 1 {
		t.Fatalf("gateway batch queued without encryption: %v", err)
	}
	key, _ := keyring.NewKey("test", "delivery-gateway-test-key", time.Now().Add(-time.Hour), nil)
	keys, _ := keyring.New(key, nil)
	revoked := false
	checks := 0
	check := func(_ context.Context, current domainidentity.Principal, got domainai.ExecutionAuthorization) error {
		checks++
		if current.UserID != principal.UserID || got.ApprovalID != authorization.ApprovalID || got.Call.AIClientID != authorization.Call.AIClientID || got.Call.CapabilityVersion != "1" {
			t.Error("original gateway authorization changed")
		}
		if revoked {
			return apperrors.ErrAccessDenied
		}
		return nil
	}
	service.SetGatewayExecutionAuthorizer(keys, check)
	if _, err := service.CreateDeliveryBatch(ctx, principal, input); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(repo.run)
	if repo.run.GatewayAuthorization == "" || strings.Contains(string(raw), "not-for-client") || strings.Contains(string(raw), repo.run.GatewayAuthorization) {
		t.Fatalf("private authorization lost or exposed: %s", raw)
	}
	restarted := New(repo, &stubWorkflowApps{}, nil, service.permissions, nil, nil, nil, nil)
	restarted.SetDeliveryRuntime(runtime, runtime)
	restarted.SetGatewayExecutionAuthorizer(keys, check)
	tickDeliveryCheck(t, restarted, repo)
	if checks == 0 || len(runtime.gatewayCalls) != 1 || runtime.gatewayCalls[0].Call.AIClientID != "original-client" {
		t.Fatal("node did not restore original Gateway context")
	}
	revoked = true
	tickDeliveryCheck(t, restarted, repo)
	if len(runtime.calls) != 1 || runtime.cancelCall == 0 {
		t.Fatal("revoked gateway grant dispatched another stage")
	}
	runtime.stopAck = true
	tickDeliveryCheck(t, restarted, repo)
	if len(runtime.calls) != 1 {
		t.Fatal("cancellation confirmation restarted execution")
	}
}
