package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appnetworkaccess "github.com/opensoha/soha/internal/application/networkaccess"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	domainnetworkingest "github.com/opensoha/soha/internal/domain/networkingest"
	domainnetworkruntime "github.com/opensoha/soha/internal/domain/networkruntime"
)

type networkEnrollmentStub struct {
	secret domainnetworkruntime.EnrollmentSecret
}

type networkAccessGrantStub struct {
	secret    domainnetworkruntime.AccessGrantSecret
	sessionID string
	input     appnetworkaccess.AccessGrantInput
}

type networkTelemetryStub struct {
	filter  domainnetworkingest.SummaryFilter
	summary domainnetworkingest.Summary
}

func (s *networkTelemetryStub) Summary(_ context.Context, filter domainnetworkingest.SummaryFilter) (domainnetworkingest.Summary, error) {
	s.filter = filter
	return s.summary, nil
}

func (s *networkAccessGrantStub) Create(_ context.Context, _ domainidentity.Principal, sessionID string, input appnetworkaccess.AccessGrantInput) (domainnetworkruntime.AccessGrantSecret, error) {
	s.sessionID, s.input = sessionID, input
	return s.secret, nil
}

func (*networkAccessGrantStub) List(context.Context, domainidentity.Principal, domainnetworkruntime.AccessGrantFilter) ([]domainnetworkruntime.AccessGrant, error) {
	return nil, nil
}

func (s *networkAccessGrantStub) Get(context.Context, domainidentity.Principal, string) (domainnetworkruntime.AccessGrant, error) {
	return s.secret.Grant, nil
}

func (s *networkAccessGrantStub) Revoke(context.Context, domainidentity.Principal, string) (domainnetworkruntime.AccessGrant, error) {
	return s.secret.Grant, nil
}

type networkAccessSessionStub struct {
	NetworkAccessService
	command   domainnetworkaccess.SessionCommand
	sessionID string
	input     domainnetworkaccess.SessionActionInput
}

func (s *networkAccessSessionStub) ExecuteSessionAction(_ context.Context, _ domainidentity.Principal, sessionID string, input domainnetworkaccess.SessionActionInput) (domainnetworkaccess.SessionCommand, error) {
	s.sessionID, s.input = sessionID, input
	return s.command, nil
}

func (s networkEnrollmentStub) Create(context.Context, domainidentity.Principal, appnetworkaccess.EnrollmentInput) (domainnetworkruntime.EnrollmentSecret, error) {
	return s.secret, nil
}

func (networkEnrollmentStub) List(context.Context, domainidentity.Principal, int) ([]domainnetworkruntime.EnrollmentChallenge, error) {
	return nil, nil
}

func (s networkEnrollmentStub) Get(context.Context, domainidentity.Principal, string) (domainnetworkruntime.EnrollmentChallenge, error) {
	return s.secret.EnrollmentChallenge, nil
}

func (s networkEnrollmentStub) Revoke(context.Context, domainidentity.Principal, string) (domainnetworkruntime.EnrollmentChallenge, error) {
	return s.secret.EnrollmentChallenge, nil
}

func TestBindNetworkQueryRejectsOutOfRangeExplicitLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, raw := range []string{"0", "201"} {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest("GET", "/?limit="+raw, nil)
		if bindNetworkQuery(context, &sohaapi.ListNetworkSitesParams{}) {
			t.Errorf("limit %s was accepted", raw)
		}
	}
}

func TestCreateEnrollmentReturnsNestedOneTimeTokenWithoutDigest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	handler := &NetworkAccessHandler{enrollments: networkEnrollmentStub{secret: domainnetworkruntime.EnrollmentSecret{
		EnrollmentChallenge: domainnetworkruntime.EnrollmentChallenge{
			ID: "enrollment-1", ChallengeID: "challenge-1", ChallengeHash: "sha256:do-not-return",
			RuntimeID: "endpoint-1", RuntimeKind: "endpoint", DeviceID: "device-1", SubjectID: "user-1",
			Status: domainnetworkruntime.EnrollmentPending, ExpiresAt: now.Add(10 * time.Minute), CreatedBy: "operator-1", CreatedAt: now,
		},
		Token: "0123456789abcdef0123456789abcdef",
	}}}
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/v1/network-access/enrollments", strings.NewReader(`{"runtimeId":"endpoint-1","runtimeKind":"endpoint","deviceId":"device-1","subjectId":"user-1"}`))
	context.Request.Header.Set("Content-Type", "application/json")

	handler.CreateEnrollment(context)

	if recorder.Code != http.StatusCreated || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response status=%d cache-control=%q body=%s", recorder.Code, recorder.Header().Get("Cache-Control"), recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, want := range []string{`"enrollment"`, `"token":"0123456789abcdef0123456789abcdef"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("response body %s missing %s", body, want)
		}
	}
	if strings.Contains(body, "challengeHash") || strings.Contains(body, "do-not-return") {
		t.Fatalf("response leaked enrollment digest: %s", body)
	}
}

func TestCreateAccessGrantBindsAuthSessionAndReturnsOnlyOneTimeToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	grants := &networkAccessGrantStub{secret: domainnetworkruntime.AccessGrantSecret{Grant: domainnetworkruntime.AccessGrant{
		ID: "grant-1", SubjectID: "user-1", AuthSessionID: "do-not-return", DeviceID: "device-1", SiteID: "site-1",
		NetworkSpaceID: "space-1", Mode: domainnetworkaccess.ModeInternalZTNA, ResourceIDs: []string{"resource-1"},
		PolicyVersion: 7, Status: domainnetworkruntime.AccessGrantIssued, TokenHash: "sha256:do-not-return",
		ExpiresAt: now.Add(5 * time.Minute), CreatedBy: "user-1", CreatedAt: now,
	}, Token: "0123456789abcdef0123456789abcdef"}}
	handler := &NetworkAccessHandler{grants: grants}
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Set("access_context", domainidentity.AccessContext{TokenKind: "session_access", SessionID: "auth-session-1"})
	context.Request = httptest.NewRequest(http.MethodPost, "/api/v1/network-access/access-grants", strings.NewReader(`{"deviceId":"device-1","siteId":"site-1","networkSpaceId":"space-1","mode":"internal_ztna","resourceIds":["resource-1"],"ttlSeconds":300}`))
	context.Request.Header.Set("Content-Type", "application/json")

	handler.CreateAccessGrant(context)

	if recorder.Code != http.StatusCreated || recorder.Header().Get("Cache-Control") != "no-store" || grants.sessionID != "auth-session-1" || grants.input.Mode != domainnetworkaccess.ModeInternalZTNA || grants.input.TTL != 5*time.Minute {
		t.Fatalf("response=%d session=%q input=%#v cache-control=%q body=%s", recorder.Code, grants.sessionID, grants.input, recorder.Header().Get("Cache-Control"), recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"token":"0123456789abcdef0123456789abcdef"`) || strings.Contains(body, "tokenHash") || strings.Contains(body, "authSession") || strings.Contains(body, "do-not-return") {
		t.Fatalf("unsafe access grant response: %s", body)
	}
}

func TestExecuteSessionActionReturnsAcceptedPublicCommand(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	stub := &networkAccessSessionStub{command: domainnetworkaccess.SessionCommand{
		ID: "command-1", SessionID: "session-1", RuntimeID: "runtime-1", NASID: "nas-1",
		Action: domainnetworkaccess.SessionActionDisconnect, TargetAccessProfile: domainnetworkaccess.ProfileDeny,
		PolicyVersion: 7, Status: domainnetworkaccess.SessionCommandPending, ReasonCode: "manual_revoke",
		PlanHash: "sha256:secret-internal-plan-hash", EffectiveAt: now, ExpiresAt: now.Add(5 * time.Minute), CreatedAt: now,
	}}
	handler := &NetworkAccessHandler{service: stub}
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Params = gin.Params{{Key: "sessionID", Value: "session-1"}}
	context.Request = httptest.NewRequest(http.MethodPost, "/api/v1/network-access/sessions/session-1/actions/execute", strings.NewReader(`{"action":"disconnect","targetAccessProfile":"deny","reasonCode":"manual_revoke","planHash":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`))
	context.Request.Header.Set("Content-Type", "application/json")

	handler.ExecuteSessionAction(context)

	if recorder.Code != http.StatusAccepted || stub.sessionID != "session-1" || stub.input.PlanHash == "" {
		t.Fatalf("response status=%d session/input=%q/%#v body=%s", recorder.Code, stub.sessionID, stub.input, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"id":"command-1"`) || strings.Contains(body, "planHash") || strings.Contains(body, "radiusAttributes") {
		t.Fatalf("public command response = %s", body)
	}
}

func TestGetTelemetrySummaryForwardsBoundedFilterAndReturnsAggregateOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	from := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	telemetry := &networkTelemetryStub{summary: domainnetworkingest.Summary{
		From: from, To: to, EventCount: 1, ProxyFlowCount: 1, UploadBytes: 40, DownloadBytes: 60,
		Producers: []domainnetworkingest.ProducerSummary{}, ProxyFlows: []domainnetworkingest.ProxyFlowSummary{},
	}}
	handler := &NetworkAccessHandler{telemetry: telemetry}
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/v1/network-access/telemetry/summary?from="+from.Format(time.RFC3339)+"&to="+to.Format(time.RFC3339)+"&producerId=endpoint-1&limit=10", nil)

	handler.GetTelemetrySummary(context)

	if recorder.Code != http.StatusOK || recorder.Header().Get("Cache-Control") != "no-store" || !telemetry.filter.From.Equal(from) || !telemetry.filter.To.Equal(to) || telemetry.filter.ProducerID != "endpoint-1" || telemetry.filter.Limit != 10 {
		t.Fatalf("response=%d filter=%#v cache-control=%q body=%s", recorder.Code, telemetry.filter, recorder.Header().Get("Cache-Control"), recorder.Body.String())
	}
	if body := recorder.Body.String(); !strings.Contains(body, `"proxyFlowCount":1`) || strings.Contains(body, "destinationHost") || strings.Contains(body, "payload") {
		t.Fatalf("unsafe telemetry response: %s", body)
	}
}

func TestGetTelemetrySummaryReturnsUnavailableWithoutQueryClient(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewNetworkAccessHandler(nil, nil, nil, nil)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/v1/network-access/telemetry/summary", nil)

	handler.GetTelemetrySummary(context)

	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), `"code":"network_ingest_unavailable"`) {
		t.Fatalf("response=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
