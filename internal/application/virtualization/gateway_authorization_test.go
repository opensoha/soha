package virtualization

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestGatewayVMQueueSealsAndRechecksAuthorizationAfterRestart(t *testing.T) {
	for _, scenario := range []string{"allowed", "revoked", "missing-checker", "wrong-key"} {
		t.Run(scenario, func(t *testing.T) {
			repo := newMemoryRepo()
			connection := repo.addConnection(domain.Connection{Provider: ProviderPVE, Enabled: true})
			adapter := &recoveryAdapter{}
			service := newTestService(repo, &captureOperations{}, adapter)
			actor := testPrincipal()
			authorization := domainaigateway.ExecutionAuthorization{ActorID: actor.UserID, Call: domainaigateway.ToolInvocationRequest{ToolName: "virtualization.vms.create.trigger", CapabilityVersion: "1", AIClientID: "codex", SkillID: "deployment", Input: map[string]any{"cloudInit": "sensitive-bootstrap-material"}}}
			ctx := domainaigateway.WithExecutionAuthorization(context.Background(), authorization)
			task, err := service.CreateVM(ctx, actor, CreateVMInput{ConnectionID: connection.ID, Name: "worker", IdempotencyKey: "gateway-worker-1"})
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(task)
			if strings.Contains(string(encoded), "sensitive-bootstrap-material") || task.Payload["gatewayAuthorizationCredential"] == nil {
				t.Fatal("gateway provenance was not sealed")
			}
			rebuilt := newTestService(repo, &captureOperations{}, adapter)
			checks, writes := 0, 0
			rebuilt.SetGatewayExecutionAuthorizer(func(_ context.Context, current domainidentity.Principal, original domainaigateway.ExecutionAuthorization) error {
				checks++
				if current.UserID != actor.UserID || original.Call.AIClientID != "codex" || original.Call.SkillID != "deployment" || original.Call.Input["cloudInit"] != "sensitive-bootstrap-material" {
					t.Fatal("queued call changed after restart")
				}
				if scenario == "revoked" {
					return errors.New("revoked with sensitive-bootstrap-material")
				}
				return nil
			})
			switch scenario {
			case "missing-checker":
				rebuilt.SetGatewayExecutionAuthorizer(nil)
			case "wrong-key":
				rebuilt.credentialKey = "wrong"
			}
			adapter.create = func(domain.AdapterCreateVMInput) (domain.AdapterVM, error) {
				writes++
				return domain.AdapterVM{ID: "702", Name: "worker"}, nil
			}
			claimed, err := repo.ClaimTask(context.Background(), "worker", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			rebuilt.executeTask(context.Background(), claimed)
			current, _ := repo.GetTask(context.Background(), task.ID)
			encoded, _ = json.Marshal(current)
			if strings.Contains(string(encoded), "sensitive-bootstrap-material") {
				t.Fatal("authorization failure leaked original input")
			}
			if scenario == "allowed" {
				if writes != 1 || checks < 2 {
					t.Fatalf("execution was not rechecked at dispatch: writes=%d checks=%d", writes, checks)
				}
			} else if writes != 0 {
				t.Fatal("unauthorized queued task mutated provider")
			}
		})
	}
}

func TestGatewayVMQueueRequiresEncryption(t *testing.T) {
	service := newTestService(newMemoryRepo(), &captureOperations{}, fakeAdapter{})
	service.credentialKey = ""
	ctx := domainaigateway.WithExecutionAuthorization(context.Background(), domainaigateway.ExecutionAuthorization{})
	if err := service.sealGatewayExecution(ctx, map[string]any{}); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("unsealed queue provenance accepted: %v", err)
	}
}
