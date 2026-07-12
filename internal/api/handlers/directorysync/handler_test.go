package directorysync

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	appdirectorysync "github.com/opensoha/soha/internal/application/directorysync"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domain "github.com/opensoha/soha/internal/domain/directorysync"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type auditRecorderStub struct{ entries []domainaudit.Entry }

func (s *auditRecorderStub) Record(_ context.Context, entry domainaudit.Entry) error {
	s.entries = append(s.entries, entry)
	return nil
}

type operationRecorderStub struct{ entries []domainoperation.Entry }

func (s *operationRecorderStub) Record(_ context.Context, entry domainoperation.Entry) error {
	s.entries = append(s.entries, entry)
	return nil
}

type repositoryStub struct {
	connection        domain.Connection
	policy            domain.Policy
	run               domain.Run
	webhookCredential domain.WebhookCredential
	event             domain.EventEnvelope
	scimSnapshot      domain.Snapshot
	scimPerson        domain.Person
	scimScope         string
}

func (s *repositoryStub) CreateConnection(_ context.Context, c domain.Connection, p domain.Policy) (domain.Connection, error) {
	s.connection, s.policy = c, p
	return c, nil
}
func (s *repositoryStub) UpdateConnection(_ context.Context, c domain.Connection, p domain.Policy) (domain.Connection, error) {
	s.connection, s.policy = c, p
	return c, nil
}
func (s *repositoryStub) GetConnection(context.Context, string) (domain.Connection, domain.Policy, error) {
	return s.connection, s.policy, nil
}
func (s *repositoryStub) ListConnections(context.Context) ([]domain.Connection, error) {
	return []domain.Connection{s.connection}, nil
}
func (s *repositoryStub) ListRuns(context.Context, string, int) ([]domain.Run, error) {
	return nil, nil
}
func (s *repositoryStub) GetActiveRun(context.Context, string) (domain.Run, error) { return s.run, nil }
func (s *repositoryStub) ListConflicts(context.Context, string, int) ([]domain.Conflict, error) {
	return nil, nil
}
func (s *repositoryStub) ResolveConflict(context.Context, string, string, string, time.Time) error {
	return nil
}
func (s *repositoryStub) UnlinkIdentity(context.Context, string, string, time.Time) (domain.IdentityLinkSuppression, error) {
	return domain.IdentityLinkSuppression{}, nil
}
func (s *repositoryStub) ClearSuppression(context.Context, string, string, time.Time) error {
	return nil
}
func (s *repositoryStub) SetWebhookCredential(context.Context, domain.WebhookCredential) error {
	return nil
}
func (s *repositoryStub) GetWebhookCredential(context.Context, string) (domain.WebhookCredential, error) {
	return s.webhookCredential, nil
}
func (s *repositoryStub) EnqueueEvent(_ context.Context, event domain.EventEnvelope) (bool, error) {
	s.event = event
	return true, nil
}
func (s *repositoryStub) SetSCIMToken(context.Context, string, string, time.Time) error { return nil }
func (s *repositoryStub) ResolveSCIMConnectionForScope(_ context.Context, _ string, scope string) (string, error) {
	s.scimScope = scope
	return "c-1", nil
}
func (s *repositoryStub) UpsertSCIMOrganization(context.Context, string, domain.Organization) error {
	return nil
}
func (s *repositoryStub) DeleteSCIMOrganization(context.Context, string, string) error { return nil }
func (s *repositoryStub) UpsertSCIMPerson(_ context.Context, _ string, item domain.Person) error {
	s.scimPerson = item
	return nil
}
func (s *repositoryStub) DeleteSCIMPerson(context.Context, string, string) error { return nil }
func (s *repositoryStub) ReplaceSCIMMemberships(context.Context, string, string, []string) error {
	return nil
}
func (s *repositoryStub) SCIMSnapshot(context.Context, string) (domain.Snapshot, error) {
	return s.scimSnapshot, nil
}
func (s *repositoryStub) SetConnectionCredential(context.Context, domain.ConnectionCredential) error {
	return nil
}
func (s *repositoryStub) GetConnectionCredential(context.Context, string) (domain.ConnectionCredential, error) {
	return domain.ConnectionCredential{}, nil
}
func (s *repositoryStub) GetRun(context.Context, string) (domain.Run, error) { return s.run, nil }
func (s *repositoryStub) TransitionRun(_ context.Context, _ string, status string, _ domain.RunStats, _, _ string) error {
	s.run.Status = status
	return nil
}

type serviceStub struct {
	repository *repositoryStub
	snapshot   domain.Snapshot
	trigger    string
	pullErr    error
}

func (s *serviceStub) CreateConnection(ctx context.Context, c domain.Connection, p domain.Policy) (domain.Connection, error) {
	return s.repository.CreateConnection(ctx, c, p)
}
func (s *serviceStub) PullSnapshot(context.Context, string, appdirectorysync.Connector) (domain.Snapshot, string, error) {
	return s.snapshot, "cursor-1", s.pullErr
}
func (s *serviceStub) Preview(context.Context, string, domain.Snapshot) (domain.Plan, error) {
	return domain.Plan{Changes: []domain.Change{{Kind: "organization", Action: "create", ExternalID: "d-1"}}}, nil
}
func (s *serviceStub) Apply(context.Context, string, domain.Snapshot, string) (domain.Run, domain.Plan, error) {
	return domain.Run{ID: "run-1", Status: domain.RunSucceeded}, domain.Plan{}, nil
}
func (s *serviceStub) ApplyTriggered(_ context.Context, _ string, _ domain.Snapshot, _ string, trigger string) (domain.Run, domain.Plan, error) {
	s.trigger = trigger
	return domain.Run{ID: "run-scim", Status: domain.RunSucceeded}, domain.Plan{}, nil
}

type connectorStub struct{}

func (connectorStub) Validate(context.Context, domain.Connection) (domain.Capabilities, error) {
	return domain.Capabilities{Organizations: true}, nil
}
func (connectorStub) ListOrganizations(context.Context, domain.Connection) ([]domain.Organization, string, error) {
	return nil, "", nil
}
func (connectorStub) ListPeople(context.Context, domain.Connection) ([]domain.Person, error) {
	return nil, nil
}
func (connectorStub) ListMemberships(context.Context, domain.Connection) ([]domain.Membership, error) {
	return nil, nil
}

type factoryStub struct{}

func (factoryStub) Connector(string) (appdirectorysync.Connector, error) { return connectorStub{}, nil }

func TestCreateConnectionUsesNestedPolicyAndSafeDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &repositoryStub{}
	handler := New(repo, &serviceStub{repository: repo}, factoryStub{})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("principal", domainidentity.Principal{UserID: "admin-1"})
	ctx.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"Feishu directory","providerType":"FEISHU","loginProviderId":"login-1","policy":{"schedule":"*/15 * * * *"}}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	handler.CreateConnection(ctx)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if repo.connection.ID == "" || repo.connection.ProviderType != domain.ProviderFeishu {
		t.Fatalf("connection = %#v", repo.connection)
	}
	if !repo.policy.SyncOrganizations || repo.policy.SyncPeople {
		t.Fatalf("policy defaults = %#v", repo.policy)
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	data := payload["data"].(map[string]any)
	if _, exposed := data["credentialRef"]; exposed {
		t.Fatal("credentialRef must not be exposed")
	}
	if data["providerType"] != "feishu" {
		t.Fatalf("providerType = %#v", data["providerType"])
	}
	policy := data["policy"].(map[string]any)
	if policy["syncOrganizations"] != true || policy["syncPeople"] != false {
		t.Fatalf("response policy = %#v", policy)
	}
}

func TestPreviewPullsRemoteSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &repositoryStub{connection: domain.Connection{ID: "c-1", ProviderType: domain.ProviderFeishu}, policy: domain.DefaultPolicy("c-1")}
	handler := New(repo, &serviceStub{repository: repo, snapshot: domain.Snapshot{Organizations: []domain.Organization{{ExternalID: "d-1"}}}}, factoryStub{})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "connectionID", Value: "c-1"}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	handler.Preview(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "cursor-1") || !strings.Contains(recorder.Body.String(), `"create":1`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestSyncReportsUnavailableLoginProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &repositoryStub{connection: domain.Connection{ID: "c-1", ProviderType: domain.ProviderFeishu}, policy: domain.DefaultPolicy("c-1")}
	handler := New(repo, &serviceStub{repository: repo, pullErr: apperrors.ErrNotFound}, factoryStub{})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "connectionID", Value: "c-1"}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	handler.Sync(ctx)

	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "directory_login_provider_unavailable") || !strings.Contains(recorder.Body.String(), "select it again") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestConflictListIsEmptyUntilConflictRepositoryIsAvailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := New(&repositoryStub{}, &serviceStub{}, factoryStub{})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	handler.ListConflicts(ctx)

	if recorder.Code != http.StatusOK || recorder.Body.String() != "{\"items\":[]}" {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestFeishuEventIsVerifiedAndQueued(t *testing.T) {
	gin.SetMode(gin.TestMode)
	policy := domain.DefaultPolicy("c-1")
	policy.Mode = domain.PolicyScheduledAndRealtime
	repo := &repositoryStub{connection: domain.Connection{ID: "c-1", ProviderType: domain.ProviderFeishu, Enabled: true}, policy: policy, webhookCredential: domain.WebhookCredential{VerificationToken: "verify-token"}}
	handler := New(repo, &serviceStub{}, factoryStub{})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "connectionID", Value: "c-1"}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"header":{"event_id":"event-1","event_type":"contact.user.updated_v3","create_time":"1783846400","token":"verify-token"},"event":{"object_id":"ou_1"}}`))

	handler.IngestEvent(ctx)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if repo.event.ProviderEventID != "event-1" || repo.event.ConnectionID != "c-1" {
		t.Fatalf("event=%#v", repo.event)
	}
}

func TestSCIMCreateUserHonorsPeopleGateAndUsesReconciler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name       string
		syncPeople bool
		wantStatus int
	}{{"disabled", false, http.StatusForbidden}, {"enabled", true, http.StatusCreated}} {
		t.Run(test.name, func(t *testing.T) {
			policy := domain.DefaultPolicy("c-1")
			policy.SyncPeople = test.syncPeople
			repo := &repositoryStub{connection: domain.Connection{ID: "c-1", ProviderType: domain.ProviderSCIM, Enabled: true}, policy: policy}
			service := &serviceStub{repository: repo}
			handler := New(repo, service, factoryStub{})
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"id":"u-1","userName":"ada","displayName":"Ada","active":true,"emails":[{"value":"ada@example.com","primary":true}]}`))
			ctx.Request.Header.Set("Authorization", "Bearer scim-token")
			ctx.Request.Header.Set("Content-Type", "application/scim+json")
			handler.SCIMCreateUser(ctx)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if test.syncPeople && (repo.scimPerson.ExternalID != "u-1" || service.trigger != "scim") {
				t.Fatalf("person=%#v trigger=%q", repo.scimPerson, service.trigger)
			}
			if repo.scimScope != domain.SCIMScopeWrite {
				t.Fatalf("scope=%q, want %q", repo.scimScope, domain.SCIMScopeWrite)
			}
		})
	}
}

func TestSCIMListUsersRequiresReadScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &repositoryStub{scimSnapshot: domain.Snapshot{}}
	handler := New(repo, &serviceStub{repository: repo}, factoryStub{})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/scim/v2/Users", nil)
	ctx.Request.Header.Set("Authorization", "Bearer scim-token")

	handler.SCIMListUsers(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if repo.scimScope != domain.SCIMScopeRead {
		t.Fatalf("scope=%q, want %q", repo.scimScope, domain.SCIMScopeRead)
	}
}

func TestSCIMRejectsMissingBearerToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := New(&repositoryStub{}, &serviceStub{}, factoryStub{})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	handler.SCIMListUsers(ctx)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateConnectionRecordsAuditAndOperation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &repositoryStub{}
	handler := New(repo, &serviceStub{repository: repo}, factoryStub{})
	audit, operations := &auditRecorderStub{}, &operationRecorderStub{}
	handler.SetRecorders(audit, operations)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("principal", domainidentity.Principal{UserID: "admin-1", UserName: "Admin"})
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/access/directory-connections", strings.NewReader(`{"name":"Feishu","providerType":"feishu","policy":{"mode":"manual"}}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	handler.CreateConnection(ctx)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(audit.entries) != 1 || audit.entries[0].ActorID != "admin-1" || audit.entries[0].ResourceKind != "DirectoryConnection" {
		t.Fatalf("audit=%#v", audit.entries)
	}
	if len(operations.entries) != 1 || operations.entries[0].OperationType != "access.directory.connection.create" || operations.entries[0].ActorID != "admin-1" {
		t.Fatalf("operations=%#v", operations.entries)
	}
}

func TestSCIMPatchUserAppliesSupportedPatchAndRejectsUnknownPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	active := true
	for _, test := range []struct {
		name, payload string
		want          int
	}{
		{"supported", `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","path":"displayName","value":"Grace"},{"op":"replace","path":"active","value":false}]}`, http.StatusOK},
		{"unknown path", `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","path":"roles","value":[]}]}`, http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy := domain.DefaultPolicy("c-1")
			policy.SyncPeople = true
			repo := &repositoryStub{connection: domain.Connection{ID: "c-1", ProviderType: domain.ProviderSCIM, Enabled: true}, policy: policy, scimSnapshot: domain.Snapshot{People: []domain.Person{{ExternalID: "u-1", Username: "ada", DisplayName: "Ada", Status: domain.ProjectionActive}}}}
			handler := New(repo, &serviceStub{repository: repo}, factoryStub{})
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Params = gin.Params{{Key: "resourceID", Value: "u-1"}}
			ctx.Request = httptest.NewRequest(http.MethodPatch, "/scim/v2/Users/u-1", strings.NewReader(test.payload))
			ctx.Request.Header.Set("Authorization", "Bearer scim-token")
			ctx.Request.Header.Set("Content-Type", "application/scim+json")
			handler.SCIMPatchUser(ctx)
			if recorder.Code != test.want {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if test.want == http.StatusOK && (repo.scimPerson.DisplayName != "Grace" || repo.scimPerson.Status != domain.ProjectionSuspended) {
				t.Fatalf("person=%#v active=%v", repo.scimPerson, active)
			}
		})
	}
}

func TestFeishuChallengeAndDingTalkSignedEventFixtures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Run("Feishu challenge", func(t *testing.T) {
		policy := domain.DefaultPolicy("c-1")
		policy.Mode = domain.PolicyScheduledAndRealtime
		repo := &repositoryStub{connection: domain.Connection{ID: "c-1", ProviderType: domain.ProviderFeishu, Enabled: true}, policy: policy, webhookCredential: domain.WebhookCredential{VerificationToken: "verify-token"}}
		handler := New(repo, &serviceStub{}, factoryStub{})
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Params = gin.Params{{Key: "connectionID", Value: "c-1"}}
		ctx.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"challenge":"challenge-1","token":"verify-token"}`))
		handler.IngestEvent(ctx)
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "challenge-1") {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	})
	t.Run("DingTalk signature", func(t *testing.T) {
		now := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
		timestamp := "1783843200000"
		secret := "event-secret"
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(timestamp))
		signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
		policy := domain.DefaultPolicy("c-1")
		policy.Mode = domain.PolicyScheduledAndRealtime
		repo := &repositoryStub{connection: domain.Connection{ID: "c-1", ProviderType: domain.ProviderDingTalk, Enabled: true}, policy: policy, webhookCredential: domain.WebhookCredential{VerificationToken: secret}}
		handler := New(repo, &serviceStub{}, factoryStub{})
		handler.now = func() time.Time { return now }
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Params = gin.Params{{Key: "connectionID", Value: "c-1"}}
		ctx.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"eventId":"evt-1","eventType":"org_dept_modify","timestamp":1783843200000}`))
		ctx.Request.Header.Set("X-DingTalk-Timestamp", timestamp)
		ctx.Request.Header.Set("X-DingTalk-Signature", signature)
		handler.IngestEvent(ctx)
		if recorder.Code != http.StatusAccepted || repo.event.ProviderEventID != "evt-1" {
			t.Fatalf("status=%d body=%s event=%#v", recorder.Code, recorder.Body.String(), repo.event)
		}
	})
}
