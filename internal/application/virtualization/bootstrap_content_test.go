package virtualization

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestVMBootstrapSurvivesRestartWithoutPlaintextPersistence(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo()
	connection := repo.addConnection(domain.Connection{Provider: ProviderPVE, Enabled: true})
	adapter := &recoveryAdapter{}
	service := newTestService(repo, &captureOperations{}, adapter)
	content := "#cloud-config\nwrite_files:\n - content: bootstrap-sensitive-test-value\n"
	input := CreateVMInput{ConnectionID: connection.ID, Name: "worker", CloudInit: content, IdempotencyKey: "worker-with-bootstrap"}
	first, err := service.CreateVM(ctx, testPrincipal(), input)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(first.Payload)
	if strings.Contains(string(raw), "bootstrap-sensitive-test-value") || first.Payload["cloudInit"] != nil || first.Payload["cloudInitCredential"] == nil {
		t.Fatalf("bootstrap persisted in plaintext: %s", raw)
	}
	replay, err := service.CreateVM(ctx, testPrincipal(), input)
	if err != nil || replay.ID != first.ID {
		t.Fatalf("encryption nonce changed idempotent receipt: %v", err)
	}
	rebuilt := newTestService(repo, &captureOperations{}, adapter)
	calls := 0
	adapter.create = func(input domain.AdapterCreateVMInput) (domain.AdapterVM, error) {
		calls++
		if input.CloudInit != content {
			t.Fatal("bootstrap changed across restart")
		}
		return domain.AdapterVM{}, errors.New("provider echoed bootstrap-sensitive-test-value")
	}
	claimed, err := repo.ClaimTask(ctx, "worker", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rebuilt.executeTask(ctx, claimed)
	current, _ := repo.GetTask(ctx, first.ID)
	raw, _ = json.Marshal(current)
	if calls != 1 || strings.Contains(string(raw), "bootstrap-sensitive-test-value") || current.Result["providerEffect"] != "unknown" {
		t.Fatalf("protected bootstrap outcome: calls=%d task=%s", calls, raw)
	}
	logs, err := repo.ListTaskLogs(ctx, first.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(logs)
	if strings.Contains(string(raw), "bootstrap-sensitive-test-value") {
		t.Fatalf("bootstrap leaked to task logs: %s", raw)
	}
}

func TestVMBootstrapFailsClosedWithoutKeyAndReadsLegacyContent(t *testing.T) {
	repo := newMemoryRepo()
	connection := repo.addConnection(domain.Connection{Provider: ProviderPVE, Enabled: true})
	service := newTestService(repo, &captureOperations{}, fakeAdapter{})
	service.credentialKey = ""
	if _, err := service.CreateVM(context.Background(), testPrincipal(), CreateVMInput{ConnectionID: connection.ID, Name: "worker", CloudInit: "secret"}); !errors.Is(err, apperrors.ErrInvalidArgument) || len(repo.tasks) != 0 {
		t.Fatalf("bootstrap accepted without key: %v", err)
	}
	if content, err := service.vmBootstrapContent(map[string]any{"cloudInit": "legacy"}); err != nil || content != "legacy" {
		t.Fatalf("legacy queued content: %q %v", content, err)
	}
	service.credentialKey = "original"
	payload := map[string]any{}
	if err := service.sealVMBootstrap(payload, "protected"); err != nil {
		t.Fatal(err)
	}
	service.credentialKey = "wrong-key"
	if _, err := service.vmBootstrapContent(payload); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("wrong key did not block: %v", err)
	}
}
