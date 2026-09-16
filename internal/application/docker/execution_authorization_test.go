package docker

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domain "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
)

func TestDockerQueueRechecksIdentityAndGatewayBeforeDispatchAndCommandStart(t *testing.T) {
	for _, scenario := range []string{"allowed", "token-revoked", "role-revoked", "gateway-revoked", "missing-resolver", "corrupt-proof", "revoked-after-claim"} {
		t.Run(scenario, func(t *testing.T) {
			repo := newMemoryDockerRepo()
			key, _ := keyring.NewKey("test", "docker-execution-test-key", time.Now().Add(-time.Hour), nil)
			keys, _ := keyring.New(key, nil)
			service := New(repo, dockerTestPermissions(), nil, WithCredentialEncryptionKeys(keys))
			principal := dockerTestPrincipal()
			principal.AccessTokenID = "original-token"
			revoked := false
			resolve := func(_ context.Context, actor, token string) (domainidentity.Principal, error) {
				if actor != principal.UserID || token != "original-token" {
					t.Error("original execution identity lost")
				}
				if scenario == "token-revoked" {
					return domainidentity.Principal{}, apperrors.ErrUnauthorized
				}
				current := principal
				if scenario == "role-revoked" {
					current.Roles = nil
				}
				return current, nil
			}
			check := func(_ context.Context, current domainidentity.Principal, authorization domainai.ExecutionAuthorization) error {
				if current.UserID != principal.UserID || authorization.Call.AIClientID != "original-client" || authorization.Call.CapabilityVersion != "1" || authorization.ApprovalID != "original-approval" {
					t.Error("gateway evidence lost")
				}
				if revoked || scenario == "gateway-revoked" {
					return apperrors.ErrAccessDenied
				}
				return nil
			}
			service.SetGatewayExecutionAuthorizer(resolve, check)
			ctx := domainai.WithExecutionAuthorization(context.Background(), domainai.ExecutionAuthorization{ActorID: principal.UserID, ApprovalID: "original-approval", Call: domainai.ToolInvocationRequest{ToolName: "docker.projects.deploy.trigger", CapabilityVersion: "1", AIClientID: "original-client", Input: map[string]any{"sensitiveFixture": "not-for-runner"}}})
			queued, err := service.enqueueOperation(ctx, principal, OperationKindProjectDeploy, "host", "project", "", map[string]any{"action": "deploy"})
			if err != nil || queued.ExecutionAuthorization == "" {
				t.Fatalf("enqueue: %+v %v", queued, err)
			}
			verifyDockerExecutionProofIsPrivate(t, queued)
			// A reconstructed service reads only the durable receipt, not caller context.
			service = New(repo, dockerTestPermissions(), nil, WithCredentialEncryptionKeys(keys))
			if scenario != "missing-resolver" {
				service.SetGatewayExecutionAuthorizer(resolve, check)
			}
			if scenario == "corrupt-proof" {
				queued.ExecutionAuthorization = "invalid"
				repo.operations[queued.ID] = queued
			}
			claimed, err := service.ClaimOperation(context.Background(), domain.OperationClaimInput{WorkerID: "runner", CallbackTokenSupported: true})
			if scenario != "allowed" && scenario != "revoked-after-claim" {
				verifyDeniedDockerDispatch(t, service, repo, principal, queued, claimed, err)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			revoked = scenario == "revoked-after-claim"
			verifyDockerCommandStart(t, service, claimed, revoked)
		})
	}
}

func verifyDockerExecutionProofIsPrivate(t *testing.T, queued domain.Operation) {
	t.Helper()
	raw, _ := json.Marshal(queued)
	if strings.Contains(string(raw), "not-for-runner") || strings.Contains(string(raw), "original-token") || strings.Contains(string(raw), queued.ExecutionAuthorization) {
		t.Fatal("execution proof leaked in public operation")
	}
}

func verifyDockerCommandStart(t *testing.T, service *Service, claimed domain.Operation, revoked bool) {
	t.Helper()
	callback := domain.OperationCallbackInput{OperationID: claimed.ID, WorkerID: "runner", CallbackToken: claimed.CallbackToken, Status: OperationStatusRunning}
	current, err := service.RecordOperationCallback(context.Background(), callback)
	if err != nil {
		t.Fatal(err)
	}
	if !revoked {
		if current.Status != OperationStatusRunning {
			t.Fatal(current.Status)
		}
		return
	}
	if current.Status != OperationStatusCanceling || current.Result["cancellationAcknowledged"] == true {
		t.Fatalf("revocation must await stopped-command acknowledgment: %+v", current)
	}
	callback.Status, callback.CancellationAcknowledged = OperationStatusCanceled, true
	current, err = service.RecordOperationCallback(context.Background(), callback)
	if err != nil || current.Status != OperationStatusCanceled || current.Result["cancellationAcknowledged"] != true {
		t.Fatalf("stop acknowledgment: %+v %v", current, err)
	}
}

func verifyDeniedDockerDispatch(t *testing.T, service *Service, repo *memoryDockerRepo, principal domainidentity.Principal, queued, claimed domain.Operation, err error) {
	t.Helper()
	if !errors.Is(err, apperrors.ErrAccessDenied) || claimed.ID != "" {
		t.Fatalf("denied dispatch returned payload: %+v %v", claimed, err)
	}
	saved := repo.operations[queued.ID]
	if saved.Status != OperationStatusFailed || saved.Result["executionDeniedBeforeDispatch"] != true {
		t.Fatalf("denial not durable: %+v", saved)
	}
	if _, err := service.RetryOperation(context.Background(), principal, queued.ID); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("retry bypassed original authorization: %v", err)
	}
}

func TestDockerGatewayQueueRequiresEncryption(t *testing.T) {
	repo := newMemoryDockerRepo()
	service := New(repo, dockerTestPermissions(), nil)
	principal := dockerTestPrincipal()
	ctx := domainai.WithExecutionAuthorization(context.Background(), domainai.ExecutionAuthorization{ActorID: principal.UserID})
	if _, err := service.enqueueOperation(ctx, principal, OperationKindProjectDeploy, "host", "project", "", nil); !errors.Is(err, apperrors.ErrInvalidArgument) || len(repo.operations) != 0 {
		t.Fatalf("unsealed gateway call queued: %v", err)
	}
}
