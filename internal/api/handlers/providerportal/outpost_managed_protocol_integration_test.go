package providerportal

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appruntime "github.com/opensoha/soha/internal/application/outpostruntime"
	"github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	domainruntime "github.com/opensoha/soha/internal/domain/outpostruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
	runtimerepo "github.com/opensoha/soha/internal/repository/outpostruntime"
	"gorm.io/gorm"
)

// This verifies real SQL and signed Core/Agent HTTP control. It deliberately
// does not execute a Kubernetes task or claim that workload scaling was tested.
//
//nolint:funlen // One process exercises trusted enrollment followed by a signed drain.
func TestOutpostManagedControlHTTPWithPostgres(t *testing.T) {
	binary := os.Getenv("SOHA_SSO_TEST_AGENT_BINARY")
	if binary == "" {
		t.Skip("set SOHA_SSO_TEST_AGENT_BINARY to the built Soha Agent")
	}
	f := newSSOProtocolFixture(t)
	ctx := context.Background()
	publicKey, signingKey, err := ed25519.GenerateKey(rand.Reader)
	ssoNoError(t, err)
	f.providers.SetOutpostSigningKey("managed-test", signingKey)
	port := reserveSSOAgentPort(t)
	agentURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	outpost, err := f.providers.CreateOutpost(ctx, f.principal, domainprovider.OutpostInput{Name: "Managed protocol edge", Mode: "external", ForwardAuthURL: agentURL + "/api/v1/outpost/forward-auth"})
	ssoNoError(t, err)
	db := ssoTestDatabase(t)
	slot := domainruntime.Slot{ID: "fixture-slot-" + outpost.ID, OutpostID: outpost.ID, ExecutorID: "fixture-executor", Namespace: "fixture-only", Name: "not-a-live-workload", UID: "fixture-uid-" + outpost.ID, TemplateDigest: "fixture-digest", MaxReplicas: 1, AllowAdoption: true, PreviousOwnerStopped: true}
	t.Cleanup(func() {
		// Only this disposable fixture owns the synthetic workload UID.
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("DELETE FROM identity_outposts WHERE id = ?", outpost.ID).Error; err != nil {
				return err
			}
			return tx.Exec("DELETE FROM runtime_slot_owners WHERE workload_uid = ? AND owner_kind = 'outpost' AND owner_id = ?", slot.UID, outpost.ID).Error
		})
		if err != nil {
			t.Errorf("remove managed Outpost fixture: %v", err)
		}
	})
	runtimeStore := runtimerepo.New(db)
	runtime, err := appruntime.New(runtimeStore, appaccess.NewPermissionResolver(managedProtocolPermissions{}), []domainruntime.Slot{slot}, nil)
	ssoNoError(t, err)
	_, err = appruntime.NewExecutorService(runtime, protocolNoExecution{}, []appruntime.ExecutorCredential{{ExecutorID: slot.ExecutorID, Token: strings.Repeat("x", 32)}})
	ssoNoError(t, err)
	state, err := runtime.Bind(ctx, f.principal, outpost.ID, domainruntime.BindingInput{SlotID: slot.ID, ExpectedGeneration: 0, RequestID: "bind", ExplicitAdoption: true})
	ssoNoError(t, err)
	if err := f.providers.DeleteOutpost(ctx, f.principal, outpost.ID); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("bound Outpost deletion must conflict: %v", err)
	}
	f.providers.SetOutpostRuntimeControl(runtime)
	input := onboardingTestInput("proxy")
	input.Provider.Config = map[string]any{"mode": "forward_auth", "outpostId": outpost.ID, "externalHosts": []string{"app.example.test"}}
	f.onboard(t, input)
	sessionID := f.login(t, "managed-outpost")
	session, err := f.providers.IssueProxySession(ctx, f.principal, domainidentity.AccessContext{TokenKind: "session_access", SessionID: sessionID})
	ssoNoError(t, err)
	directory := t.TempDir()
	ssoNoError(t, os.Chmod(directory, 0700)) // #nosec G302 -- Fixture directory needs owner traversal; 0700 grants no group or world access.
	config := fmt.Sprintf("app:\n  env: development\nhttp:\n  addr: :%d\n  base_path: /api/v1\nauth:\n  bearer_token_file: %s\nlogger:\n  level: error\nkubernetes:\n  enabled: false\ncontrol_plane:\n  enabled: true\n  base_url: %s\n  bearer_token_file: %s\n  outpost:\n    enabled: true\n    agent_id: %s\n    instance_id: fixture-instance\n    runtime_control_enabled: true\n    state_dir: %s\n    protocol_version: v1\n    trust_key_id: managed-test\n    trust_public_key: %s\n    poll_interval: 200ms\n    heartbeat_interval: 200ms\n", port, filepath.Join(directory, "agent-token"), f.server.URL, filepath.Join(directory, "control-token"), outpost.ID, filepath.Join(directory, "state"), base64.StdEncoding.EncodeToString(publicKey))
	serviceToken := "managed-protocol-test-service-token"
	stop := startSSOTestAgent(t, binary, directory, config, serviceToken, outpost.Token)
	defer stop()
	waitSSOCondition(t, "signed serve and authenticated instance ACK", func() bool {
		view, err := runtime.View(ctx, f.principal, outpost.ID)
		return err == nil && view.SupportsDrain
	})
	checkSSOOutpost(t, f.client, agentURL, serviceToken, session.Token, "", "app.example.test", http.StatusOK)
	state, err = runtime.Control(ctx, f.principal, outpost.ID, domainruntime.ControlInput{Action: "stop", ExpectedGeneration: state.Generation, RequestID: "stop", DrainTimeoutSeconds: 30})
	ssoNoError(t, err)
	t.Cleanup(func() { _ = db.Exec("DELETE FROM execution_tasks WHERE id = ?", state.LatestTaskID).Error })
	// A private task must bind its provider to the same approved executor as
	// its payload; adding an application-less owner cannot exempt arbitrary tasks.
	err = db.Exec(`INSERT INTO execution_tasks(id, task_kind, provider_kind, target_kind, status, payload)
SELECT ?, task_kind, 'outpost_runtime.different-executor', target_kind, status, payload
FROM execution_tasks WHERE id = ?`, uuid.NewString(), state.LatestTaskID).Error
	var constraintError *pgconn.PgError
	if !errors.As(err, &constraintError) || constraintError.Code != "23514" || constraintError.ConstraintName != "execution_tasks_vm_command_owner" {
		t.Fatalf("private owner mismatch did not fail its owner constraint: %v", err)
	}
	waitSSOCondition(t, "signed drain ACK with no active checks", func() bool {
		drain, err := runtimeStore.DrainStatus(ctx, outpost.ID, state.Generation, time.Now().UTC())
		return err == nil && drain.Instances == 1 && !drain.AcceptingRequests && drain.ActiveChecks == 0
	})
	checkSSOOutpost(t, f.client, agentURL, serviceToken, session.Token, "", "app.example.test", http.StatusServiceUnavailable)
}

type managedProtocolPermissions struct{}

func (managedProtocolPermissions) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"protocol-admin": {appaccess.PermIdentityOutpostsView, appaccess.PermIdentityOutpostsManage}}, nil
}

type protocolNoExecution struct{}

func (protocolNoExecution) ClaimOutpostRuntimeExecutionTask(context.Context, string) (delivery.ExecutionTask, error) {
	return delivery.ExecutionTask{}, apperrors.ErrNotFound
}
func (protocolNoExecution) GetOutpostRuntimeExecutionTask(context.Context, string, string) (delivery.ExecutionTask, error) {
	return delivery.ExecutionTask{}, apperrors.ErrNotFound
}
func (protocolNoExecution) RecordOutpostRuntimeCallback(context.Context, string, delivery.ExecutionCallbackInput) (delivery.ExecutionTask, error) {
	return delivery.ExecutionTask{}, apperrors.ErrNotFound
}
