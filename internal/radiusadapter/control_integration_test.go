package radiusadapter

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	controlapi "github.com/opensoha/soha/internal/api/networkcontrol"
	controlapp "github.com/opensoha/soha/internal/application/networkcontrol"
	domainaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	domainruntime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	accessrepo "github.com/opensoha/soha/internal/repository/networkaccess"
	runtimerepo "github.com/opensoha/soha/internal/repository/networkruntime"
	testpostgres "github.com/opensoha/soha/internal/testsupport/postgres"
	"go.uber.org/zap"
)

func networkCommandStore(t *testing.T) *dbstore.Store {
	t.Helper()
	text := os.Getenv("SOHA_CATALOG_TEST_POSTGRES_PORT")
	if text == "" {
		t.Skip("requires this run's disposable PostgreSQL")
	}
	port, err := strconv.Atoi(text)
	if err != nil {
		t.Fatal(err)
	}
	cfg := testpostgres.Configure(t, config.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "postgres", User: "pgsql", Password: "test-only", SSLMode: "disable", MaxOpenConns: 6, MaxIdleConns: 2})
	admin, err := dbstore.New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	cfg.Name = "network_command_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec(t.Context(), "CREATE DATABASE "+cfg.Name); err != nil {
		t.Fatal(err)
	}
	store, err := dbstore.New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
		if err := admin.Exec(context.Background(), "DROP DATABASE "+cfg.Name); err != nil {
			t.Error(err)
		}
	})
	if err := store.MigrateFromFile(t.Context(), "../../migrations/postgres/0001_init.sql"); err != nil {
		t.Fatal(err)
	}
	return store
}

type callbackLossTransport struct {
	base  http.RoundTripper
	lost  bool
	posts [][]byte
}

func (r *callbackLossTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method != http.MethodPost || !strings.HasSuffix(request.URL.Path, "/nas-session-commands/result") {
		return r.base.RoundTrip(request)
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	_ = request.Body.Close()
	request.Body = io.NopCloser(strings.NewReader(string(body)))
	r.posts = append(r.posts, append([]byte(nil), body...))
	response, err := r.base.RoundTrip(request)
	if err == nil && response.StatusCode == http.StatusNoContent && !r.lost {
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		r.lost = true
		return nil, io.ErrUnexpectedEOF // Real callback has committed; only its ACK is lost.
	}
	return response, err
}

func TestNetworkCommandRuntimeWithPostgres(t *testing.T) {
	store := networkCommandStore(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Now().UTC()
	runtimeID, nasID, sessionID := "task115-owned-runtime", "task115-owned-nas", "task115-owned-session"
	credentials := newNetworkRuntimeTLS(t, runtimeID)
	repository := runtimerepo.New(store.DB())
	reads := accessrepo.New(store.DB())
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatal(err)
	}
	service, err := controlapp.New(repository, schemas, controlapp.Options{MaxClockSkew: time.Minute, ConfigurationTTL: time.Minute, LeaseTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reads.PublishPolicySnapshot(ctx, domainaccess.PolicySnapshot{ContentHash: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("owned-policy"))), Policies: []domainaccess.Policy{}, ProtectedResourceIDs: []string{}, PublishedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := service.RefreshSnapshot(ctx); err != nil {
		t.Fatal(err)
	}
	router, err := controlapi.NewRouter(service, store, controlapi.Options{MaxBodyBytes: 1 << 20, RequestsPerMinute: 100})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(router)
	server.TLS = credentials.server
	server.StartTLS()
	defer server.Close()
	transport := &http.Transport{TLSClientConfig: credentials.client, Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	base := server.URL + "/api/network-control/v1/runtimes/" + runtimeID
	request := func(client *http.Client, method, path string, body []byte, token string) (int, []byte) {
		return networkCommandRuntimeWithPostgresPhase1(client, method, path, body, token, &t, &ctx)
	}
	t.Run("mtls_enrollment_and_runtime_identity", func(t *testing.T) {
		networkCommandRuntimeWithPostgresPhase2(t, &ctx, &now, &runtimeID, &credentials, &repository, &server, &client, &base, &request)
	})
	if t.Failed() {
		return
	}
	if err := store.Exec(ctx, `INSERT INTO network_runtime_sessions(id,runtime_id,subject_id,device_id,site_id,nas_id,mode,access_profile,status,policy_version,valid_until) VALUES ($1,$2, 'fixture-subject','fixture-device','fixture-site',$3,'internal_direct','full','active',1,$4)`, sessionID, runtimeID, nasID, now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	hash := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("owned-original-plan")))
	command := domainaccess.SessionCommand{ID: uuid.NewString(), SessionID: sessionID, RuntimeID: runtimeID, NASID: nasID, Action: "disconnect", TargetAccessProfile: "deny", PolicyVersion: 1, Status: "pending", ReasonCode: "manual_revoke", PlanHash: hash, EffectiveAt: now, ExpiresAt: now.Add(4 * time.Minute), CreatedAt: now}
	if _, err := reads.CreateSessionCommand(ctx, command); err != nil {
		t.Fatal(err)
	}
	t.Run("original_lookup_parent_and_plan_binding", func(t *testing.T) {
		networkCommandRuntimeWithPostgresPhase3(t, &ctx, &sessionID, &reads, &hash, &command)
	})
	loss := &callbackLossTransport{base: transport}
	lossClient := &http.Client{Transport: loss, Timeout: 5 * time.Second}
	journalPath := filepath.Join(t.TempDir(), "results.json")
	journal, err := OpenJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	executions := 0
	executor := func(ignoredContext context.Context, input networkprotocol.NASSessionCommand) ExecutionResult {
		return networkCommandRuntimeWithPostgresPhase4(ignoredContext, input, &t, &nasID, &sessionID, &command, &executions)
	}
	newAdapter := func(j *Journal) *Adapter {
		return networkCommandRuntimeWithPostgresPhase5(j, &t, &runtimeID, &nasID, &schemas, &server, &lossClient, &executor)
	}
	t.Run("real_callback_commit_then_ack_loss", func(t *testing.T) {
		networkCommandRuntimeWithPostgresPhase6(t, &ctx, &sessionID, &reads, &hash, &command, &loss, &journal, &executions, &newAdapter)
	})
	if t.Failed() {
		return
	}
	t.Run("reopen_journal_replays_result_without_execution", func(t *testing.T) {
		networkCommandRuntimeWithPostgresPhase7(t, &ctx, &sessionID, &reads, &client, &base, &request, &command, &loss, &journalPath, &executions, &newAdapter)
	})
	t.Run("revoked_runtime_cannot_poll_or_callback", func(t *testing.T) {
		networkCommandRuntimeWithPostgresPhase8(t, &store, &ctx, &runtimeID, &client, &base, &request, &loss, &executions)
	})
}

func networkCommandRuntimeWithPostgresPhase1(client *http.Client, method, path string, body []byte, token string, sharedT **testing.T, sharedCtx *context.Context) (int, []byte) {

	(*sharedT).Helper()
	req, err := http.NewRequestWithContext((*sharedCtx), method, path, strings.NewReader(string(body)))
	if err != nil {
		(*sharedT).Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(req)
	if err != nil {
		(*sharedT).Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		(*sharedT).Fatal(err)
	}
	return response.StatusCode, raw
}

func networkCommandRuntimeWithPostgresPhase2(t *testing.T, sharedCtx *context.Context, sharedNow *time.Time, sharedRuntimeID *string, sharedCredentials *networkRuntimeTLS, sharedRepository **runtimerepo.Repository, sharedServer **httptest.Server, sharedClient **http.Client, sharedBase *string, sharedRequest *func(client *http.Client, method string, path string, body []byte, token string) (int, []byte)) {

	token := uuid.NewString() + uuid.NewString()
	hash := sha256.Sum256([]byte(token))
	enrollment := domainruntime.EnrollmentChallenge{ID: uuid.NewString(), ChallengeID: uuid.NewString(), ChallengeHash: fmt.Sprintf("sha256:%x", hash), RuntimeID: (*sharedRuntimeID), RuntimeKind: "nas", DeviceID: "fixture-device", SubjectID: "fixture-subject", Status: "pending", ExpiresAt: (*sharedNow).Add(5 * time.Minute), CreatedBy: "fixture-owner", CreatedAt: (*sharedNow)}
	if err := (*sharedRepository).CreateEnrollment((*sharedCtx), enrollment); err != nil {
		t.Fatal(err)
	}
	public, err := x509.MarshalPKIXPublicKey((*sharedCredentials).leaf.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	// Use an existing protocol capability for this synthetic NAS identity;
	// NAS command authorization is its certificate kind, not a new capability.
	payload, _ := json.Marshal(networkprotocol.EnrollmentRequest{EnrollmentID: enrollment.ID, ChallengeID: enrollment.ChallengeID, DeviceID: enrollment.DeviceID, DevicePublicKey: string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})), Platform: "linux", ClientVersion: "task115-fixture", Capabilities: []string{"route-coordinator"}})
	message, _ := json.Marshal(networkprotocol.RuntimeMessage{SchemaVersion: networkprotocol.RuntimeSchemaVersion, MessageID: uuid.NewString(), MessageType: networkprotocol.MessageEnrollmentRequest, ProducerID: (*sharedRuntimeID), RuntimeID: (*sharedRuntimeID), RuntimeKind: "nas", OccurredAt: (*sharedNow), ExpiresAt: (*sharedNow).Add(time.Minute), Payload: payload})
	status, body := (*sharedRequest)((*sharedClient), http.MethodPost, (*sharedBase)+"/enroll", message, token)
	if status != 201 {
		t.Fatalf("enroll status=%d body=%s", status, body)
	}
	identity, err := networkidentity.ParseCertificate((*sharedCredentials).leaf, networkidentity.ScopeNetworkControl)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := (*sharedRepository).ActiveCredential((*sharedCtx), identity.CertificateFingerprint, time.Now())
	if err != nil || credential.RuntimeID != (*sharedRuntimeID) {
		t.Fatalf("credential bound=%t err=%v", credential.RuntimeID == (*sharedRuntimeID), err)
	}
	status, _ = (*sharedRequest)((*sharedClient), http.MethodGet, (*sharedServer).URL+"/api/network-control/v1/runtimes/unrelated-runtime/nas-session-commands/next", nil, "")
	if status != 403 && status != 401 {
		t.Fatalf("wrong runtime path status=%d", status)
	}
}

func networkCommandRuntimeWithPostgresPhase3(t *testing.T, sharedCtx *context.Context, sharedSessionID *string, sharedReads **accessrepo.Repository, sharedHash *string, sharedCommand *domainaccess.SessionCommand) {

	byID, err := (*sharedReads).GetSessionCommandForSession((*sharedCtx), (*sharedSessionID), (*sharedCommand).ID)
	if err != nil || byID.ID != (*sharedCommand).ID {
		t.Fatal("original ID lookup failed", err)
	}
	byPlan, err := (*sharedReads).GetSessionCommandForPlan((*sharedCtx), (*sharedSessionID), (*sharedHash))
	if err != nil || byPlan.ID != (*sharedCommand).ID {
		t.Fatal("original plan lookup failed", err)
	}
	if _, err := (*sharedReads).GetSessionCommandForSession((*sharedCtx), "unrelated", (*sharedCommand).ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("cross-session ID err=%v", err)
	}
	if _, err := (*sharedReads).GetSessionCommandForPlan((*sharedCtx), "unrelated", (*sharedHash)); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("cross-session hash err=%v", err)
	}
}

func networkCommandRuntimeWithPostgresPhase4(_ context.Context, input networkprotocol.NASSessionCommand, sharedT **testing.T, sharedNasID *string, sharedSessionID *string, sharedCommand *domainaccess.SessionCommand, sharedExecutions *int) ExecutionResult {

	(*sharedExecutions)++
	if input.CommandID != (*sharedCommand).ID || input.SessionID != (*sharedSessionID) || input.NASID != (*sharedNasID) || input.Action != "disconnect" || input.SubjectID != "fixture-subject" || input.DeviceID != "fixture-device" {
		(*sharedT).Error("executor target binding changed")
		return ExecutionResult{Status: "rejected", ReasonCode: "fixture_mismatch"}
	}
	return ExecutionResult{Status: "applied", ReasonCode: "disconnect_applied"}
}

func networkCommandRuntimeWithPostgresPhase5(j *Journal, sharedT **testing.T, sharedRuntimeID *string, sharedNasID *string, sharedSchemas **networkprotocol.Schemas, sharedServer **httptest.Server, sharedLossClient **http.Client, sharedExecutor *func(_ context.Context, input networkprotocol.NASSessionCommand) ExecutionResult) *Adapter {

	(*sharedT).Helper()
	adapter, err := New(Config{RuntimeID: (*sharedRuntimeID), NASID: (*sharedNasID), ControlURL: (*sharedServer).URL, CommandTimeout: 5 * time.Second, MaxClockSkew: time.Minute}, (*sharedLossClient), (*sharedSchemas), j, (*sharedExecutor), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		(*sharedT).Fatal(err)
	}
	return adapter
}

func networkCommandRuntimeWithPostgresPhase6(t *testing.T, sharedCtx *context.Context, sharedSessionID *string, sharedReads **accessrepo.Repository, sharedHash *string, sharedCommand *domainaccess.SessionCommand, sharedLoss **callbackLossTransport, sharedJournal **Journal, sharedExecutions *int, sharedNewAdapter *func(j *Journal) *Adapter) {

	if err := (*sharedNewAdapter)((*sharedJournal)).processOnce((*sharedCtx)); err == nil {
		t.Fatal("expected callback ACK loss")
	}
	original, err := (*sharedReads).GetSessionCommandForPlan((*sharedCtx), (*sharedSessionID), (*sharedHash))
	if err != nil || original.ID != (*sharedCommand).ID || original.Status != "applied" || original.CompletedAt == nil || (*sharedExecutions) != 1 || (*sharedJournal).Len() != 1 || !(*sharedLoss).lost {
		t.Fatalf("committed status=%s executions=%d journal=%d lost=%t err=%v", original.Status, (*sharedExecutions), (*sharedJournal).Len(), (*sharedLoss).lost, err)
	}
	session, err := (*sharedReads).GetSession((*sharedCtx), (*sharedSessionID))
	if err != nil || session.Status != "revoked" {
		t.Fatalf("session status=%s err=%v", session.Status, err)
	}
}

func networkCommandRuntimeWithPostgresPhase7(t *testing.T, sharedCtx *context.Context, sharedSessionID *string, sharedReads **accessrepo.Repository, sharedClient **http.Client, sharedBase *string, sharedRequest *func(client *http.Client, method string, path string, body []byte, token string) (int, []byte), sharedCommand *domainaccess.SessionCommand, sharedLoss **callbackLossTransport, sharedJournalPath *string, sharedExecutions *int, sharedNewAdapter *func(j *Journal) *Adapter) {

	reopened, err := OpenJournal((*sharedJournalPath))
	if err != nil || reopened.Len() != 1 {
		t.Fatal("original result journal missing", err)
	}
	if err := (*sharedNewAdapter)(reopened).processOnce((*sharedCtx)); err != nil {
		t.Fatal(err)
	}
	if (*sharedExecutions) != 1 || reopened.Len() != 0 || len((*sharedLoss).posts) != 2 || string((*sharedLoss).posts[0]) != string((*sharedLoss).posts[1]) {
		t.Fatalf("replay executions=%d journal=%d posts=%d", (*sharedExecutions), reopened.Len(), len((*sharedLoss).posts))
	}
	status, _ := (*sharedRequest)((*sharedClient), http.MethodGet, (*sharedBase)+"/nas-session-commands/next", nil, "")
	if status != 204 {
		t.Fatalf("terminal command redelivered: %d", status)
	}
	original, err := (*sharedReads).GetSessionCommandForSession((*sharedCtx), (*sharedSessionID), (*sharedCommand).ID)
	if err != nil || original.Status != "applied" {
		t.Fatal("original terminal receipt changed", err)
	}
}

func networkCommandRuntimeWithPostgresPhase8(t *testing.T, sharedStore **dbstore.Store, sharedCtx *context.Context, sharedRuntimeID *string, sharedClient **http.Client, sharedBase *string, sharedRequest *func(client *http.Client, method string, path string, body []byte, token string) (int, []byte), sharedLoss **callbackLossTransport, sharedExecutions *int) {

	if err := (*sharedStore).Exec((*sharedCtx), `UPDATE network_runtime_credentials SET status='revoked',revoked_at=now() WHERE runtime_id=$1`, (*sharedRuntimeID)); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		path, body := (*sharedBase)+"/nas-session-commands/next", []byte(nil)
		if method == http.MethodPost {
			path = (*sharedBase) + "/nas-session-commands/result"
			body = (*sharedLoss).posts[0]
		}
		status, _ := (*sharedRequest)((*sharedClient), method, path, body, "")
		if status != 401 && status != 403 {
			t.Fatalf("revoked runtime %s status=%d", method, status)
		}
	}
	var count int64
	if err := (*sharedStore).DB().Raw(`SELECT count(*) FROM network_nas_session_commands`).Scan(&count).Error; err != nil || count != 1 || (*sharedExecutions) != 1 {
		t.Fatalf("revocation changed original command count=%d executions=%d err=%v", count, (*sharedExecutions), err)
	}
}
