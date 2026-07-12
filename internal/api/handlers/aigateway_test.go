package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type relayHandlerTestCase struct {
	name         string
	path         string
	providerKind string
	endpoint     string
	body         string
	contentType  string
	handler      func(*AIGatewayHandler, *gin.Context)
}

func runRelayHandlerTestCase(t *testing.T, testCase relayHandlerTestCase) {
	t.Helper()
	service := &stubAIGatewayService{}
	handler := NewAIGatewayHandler(service)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, testCase.path, strings.NewReader(testCase.body))
	ctx.Request.Header.Set("Content-Type", testCase.contentType)
	ctx.Set("principal", domainidentity.Principal{UserID: "u-1"})

	testCase.handler(handler, ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	request := service.relayReq
	if request.ProviderKind != testCase.providerKind || request.Endpoint != testCase.endpoint || request.Method != http.MethodPost {
		t.Fatalf("relay request = %#v", request)
	}
	if string(request.Body) != testCase.body {
		t.Fatalf("relay body = %s, want %s", request.Body, testCase.body)
	}
	if request.Headers.Get("Content-Type") != testCase.contentType {
		t.Fatalf("relay content type = %q, want %q", request.Headers.Get("Content-Type"), testCase.contentType)
	}
}

func runRelayHandlerTestCases(t *testing.T, testCases []relayHandlerTestCase) {
	t.Helper()
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			runRelayHandlerTestCase(t, testCase)
		})
	}
}

func compatibleRelayPath(providerKind, endpoint string) string {
	return fmt.Sprintf("/api/v1/ai-gateway/llm/%s/v1/%s", providerKind, endpoint)
}

type stubAIGatewayService struct {
	governanceCalled bool
	governanceReq    domainaigateway.GovernanceStatusRequest
	listPATReq       domainaigateway.PersonalAccessTokenListRequest
	rotatePATID      string
	rotatePATReq     domainaigateway.TokenRotationInput
	cacheStatsReq    domainaigateway.LLMRelayCacheStatsRequest
	cachePurgeReq    domainaigateway.LLMRelayCachePurgeRequest
	maxRelayBodySize int64
	relayErr         error
	relayReq         appaigateway.LLMRelayHTTPRequest
}

func (s *stubAIGatewayService) Capabilities(context.Context, domainidentity.Principal, domainaigateway.ManifestRequest) (domainaigateway.Manifest, error) {
	return domainaigateway.Manifest{}, nil
}

func (s *stubAIGatewayService) InvokeTool(context.Context, domainidentity.Principal, domainaigateway.ToolInvocationRequest) (domainaigateway.ToolInvocationResult, error) {
	return domainaigateway.ToolInvocationResult{}, nil
}

func (s *stubAIGatewayService) ReadResource(context.Context, domainidentity.Principal, domainaigateway.ResourceReadRequest) (domainaigateway.ResourceReadResult, error) {
	return domainaigateway.ResourceReadResult{}, nil
}

func (s *stubAIGatewayService) GetPrompt(context.Context, domainidentity.Principal, domainaigateway.PromptGetRequest) (domainaigateway.PromptGetResult, error) {
	return domainaigateway.PromptGetResult{}, nil
}

func (s *stubAIGatewayService) ListPersonalAccessTokens(_ context.Context, _ domainidentity.Principal, req domainaigateway.PersonalAccessTokenListRequest) ([]domainaigateway.PersonalAccessToken, error) {
	s.listPATReq = req
	return nil, nil
}

func (s *stubAIGatewayService) CreatePersonalAccessToken(context.Context, domainidentity.Principal, domainaigateway.PersonalAccessTokenInput) (domainaigateway.CreatedPersonalAccessToken, error) {
	return domainaigateway.CreatedPersonalAccessToken{}, nil
}

func (s *stubAIGatewayService) RevokePersonalAccessToken(context.Context, domainidentity.Principal, string) error {
	return nil
}

func (s *stubAIGatewayService) RotatePersonalAccessToken(_ context.Context, _ domainidentity.Principal, tokenID string, req domainaigateway.TokenRotationInput) (domainaigateway.CreatedPersonalAccessToken, error) {
	s.rotatePATID = tokenID
	s.rotatePATReq = req
	return domainaigateway.CreatedPersonalAccessToken{Token: domainaigateway.PersonalAccessToken{ID: "pat-new", TokenPrefix: "soha_pat_new"}, Value: "soha_pat_secret"}, nil
}

func (s *stubAIGatewayService) ListServiceAccounts(context.Context, domainidentity.Principal) ([]domainaigateway.ServiceAccount, error) {
	return nil, nil
}

func (s *stubAIGatewayService) CreateServiceAccount(context.Context, domainidentity.Principal, domainaigateway.ServiceAccountInput) (domainaigateway.ServiceAccount, error) {
	return domainaigateway.ServiceAccount{}, nil
}

func (s *stubAIGatewayService) ListServiceAccountTokens(context.Context, domainidentity.Principal) ([]domainaigateway.ServiceAccountToken, error) {
	return nil, nil
}

func (s *stubAIGatewayService) CreateServiceAccountToken(context.Context, domainidentity.Principal, string, domainaigateway.ServiceAccountTokenInput) (domainaigateway.CreatedServiceAccountToken, error) {
	return domainaigateway.CreatedServiceAccountToken{}, nil
}

func (s *stubAIGatewayService) RevokeServiceAccountToken(context.Context, domainidentity.Principal, string) error {
	return nil
}

func (s *stubAIGatewayService) RotateServiceAccountToken(context.Context, domainidentity.Principal, string, domainaigateway.TokenRotationInput) (domainaigateway.CreatedServiceAccountToken, error) {
	return domainaigateway.CreatedServiceAccountToken{}, nil
}

func (s *stubAIGatewayService) ListAIClients(context.Context, domainidentity.Principal) ([]domainaigateway.AIClient, error) {
	return nil, nil
}

func (s *stubAIGatewayService) CreateAIClient(context.Context, domainidentity.Principal, domainaigateway.AIClientInput) (domainaigateway.AIClient, error) {
	return domainaigateway.AIClient{}, nil
}

func (s *stubAIGatewayService) UpdateAIClient(context.Context, domainidentity.Principal, string, domainaigateway.AIClientInput) (domainaigateway.AIClient, error) {
	return domainaigateway.AIClient{}, nil
}

func (s *stubAIGatewayService) ListToolGrants(context.Context, domainidentity.Principal, domainaigateway.ToolGrantFilter) ([]domainaigateway.ToolGrant, error) {
	return nil, nil
}

func (s *stubAIGatewayService) CreateToolGrant(context.Context, domainidentity.Principal, domainaigateway.ToolGrantInput) (domainaigateway.ToolGrant, error) {
	return domainaigateway.ToolGrant{}, nil
}

func (s *stubAIGatewayService) DeleteToolGrant(context.Context, domainidentity.Principal, string) error {
	return nil
}

func (s *stubAIGatewayService) ListAccessPolicies(context.Context, domainidentity.Principal, domainaigateway.AccessPolicyFilter) ([]domainaigateway.AccessPolicy, error) {
	return nil, nil
}

func (s *stubAIGatewayService) CreateAccessPolicy(context.Context, domainidentity.Principal, domainaigateway.AccessPolicyInput) (domainaigateway.AccessPolicy, error) {
	return domainaigateway.AccessPolicy{}, nil
}

func (s *stubAIGatewayService) UpdateAccessPolicy(context.Context, domainidentity.Principal, string, domainaigateway.AccessPolicyInput) (domainaigateway.AccessPolicy, error) {
	return domainaigateway.AccessPolicy{}, nil
}

func (s *stubAIGatewayService) DeleteAccessPolicy(context.Context, domainidentity.Principal, string) error {
	return nil
}

func (s *stubAIGatewayService) GovernanceStatus(_ context.Context, _ domainidentity.Principal, req domainaigateway.GovernanceStatusRequest) (domainaigateway.GovernanceStatus, error) {
	s.governanceCalled = true
	s.governanceReq = req
	return domainaigateway.GovernanceStatus{WindowHours: req.WindowHours}, nil
}

func (s *stubAIGatewayService) ListSkillBindings(context.Context, domainidentity.Principal, domainaigateway.SkillBindingFilter) ([]domainaigateway.SkillBinding, error) {
	return nil, nil
}

func (s *stubAIGatewayService) CreateSkillBinding(context.Context, domainidentity.Principal, domainaigateway.SkillBindingInput) (domainaigateway.SkillBinding, error) {
	return domainaigateway.SkillBinding{}, nil
}

func (s *stubAIGatewayService) UpdateSkillBinding(context.Context, domainidentity.Principal, string, domainaigateway.SkillBindingInput) (domainaigateway.SkillBinding, error) {
	return domainaigateway.SkillBinding{}, nil
}

func (s *stubAIGatewayService) DeleteSkillBinding(context.Context, domainidentity.Principal, string) error {
	return nil
}

func (s *stubAIGatewayService) ListAuditLogs(context.Context, domainidentity.Principal, domainaigateway.AuditLogFilter) ([]domainaigateway.AuditLog, error) {
	return nil, nil
}

func (s *stubAIGatewayService) ListApprovalRequests(context.Context, domainidentity.Principal, domainaigateway.ApprovalRequestFilter) ([]domainaigateway.ApprovalRequest, error) {
	return nil, nil
}

func (s *stubAIGatewayService) GetApprovalTimeline(context.Context, domainidentity.Principal, string) (domainaigateway.ApprovalTimeline, error) {
	return domainaigateway.ApprovalTimeline{}, nil
}

func (s *stubAIGatewayService) ApproveApprovalRequest(context.Context, domainidentity.Principal, string, domainaigateway.ApprovalDecisionInput) (domainaigateway.ApprovalDecisionResult, error) {
	return domainaigateway.ApprovalDecisionResult{}, nil
}

func (s *stubAIGatewayService) RejectApprovalRequest(context.Context, domainidentity.Principal, string, domainaigateway.ApprovalDecisionInput) (domainaigateway.ApprovalDecisionResult, error) {
	return domainaigateway.ApprovalDecisionResult{}, nil
}

func (s *stubAIGatewayService) CancelApprovalRequest(context.Context, domainidentity.Principal, string, domainaigateway.ApprovalDecisionInput) (domainaigateway.ApprovalDecisionResult, error) {
	return domainaigateway.ApprovalDecisionResult{}, nil
}

func (s *stubAIGatewayService) ListLLMUpstreams(context.Context, domainidentity.Principal, domainaigateway.LLMUpstreamFilter) ([]domainaigateway.LLMUpstream, error) {
	return nil, nil
}

func (s *stubAIGatewayService) CreateLLMUpstream(context.Context, domainidentity.Principal, domainaigateway.LLMUpstreamInput) (domainaigateway.LLMUpstream, error) {
	return domainaigateway.LLMUpstream{}, nil
}

func (s *stubAIGatewayService) UpdateLLMUpstream(context.Context, domainidentity.Principal, string, domainaigateway.LLMUpstreamInput) (domainaigateway.LLMUpstream, error) {
	return domainaigateway.LLMUpstream{}, nil
}

func (s *stubAIGatewayService) TestLLMUpstream(context.Context, domainidentity.Principal, string) (domainaigateway.LLMUpstreamTestResult, error) {
	return domainaigateway.LLMUpstreamTestResult{}, nil
}

func (s *stubAIGatewayService) RunLLMRelayHealthChecks(context.Context, domainidentity.Principal) (domainaigateway.LLMRelayHealthCheckRun, error) {
	return domainaigateway.LLMRelayHealthCheckRun{}, nil
}

func (s *stubAIGatewayService) ListLLMModelRoutes(context.Context, domainidentity.Principal, domainaigateway.LLMModelRouteFilter) ([]domainaigateway.LLMModelRoute, error) {
	return nil, nil
}

func (s *stubAIGatewayService) CreateLLMModelRoute(context.Context, domainidentity.Principal, domainaigateway.LLMModelRouteInput) (domainaigateway.LLMModelRoute, error) {
	return domainaigateway.LLMModelRoute{}, nil
}

func (s *stubAIGatewayService) UpdateLLMModelRoute(context.Context, domainidentity.Principal, string, domainaigateway.LLMModelRouteInput) (domainaigateway.LLMModelRoute, error) {
	return domainaigateway.LLMModelRoute{}, nil
}

func (s *stubAIGatewayService) DeleteLLMModelRoute(context.Context, domainidentity.Principal, string) error {
	return nil
}

func (s *stubAIGatewayService) ListLLMCallLogs(context.Context, domainidentity.Principal, domainaigateway.LLMCallLogFilter) ([]domainaigateway.LLMCallLog, error) {
	return nil, nil
}

func (s *stubAIGatewayService) LLMRelayMetrics(context.Context, domainidentity.Principal) (domainaigateway.LLMRelayMetrics, error) {
	return domainaigateway.LLMRelayMetrics{}, nil
}

func (s *stubAIGatewayService) LLMRelayCacheStats(_ context.Context, _ domainidentity.Principal, req domainaigateway.LLMRelayCacheStatsRequest) (domainaigateway.LLMRelayCacheStats, error) {
	s.cacheStatsReq = req
	return domainaigateway.LLMRelayCacheStats{GeneratedAt: time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC), WindowHours: req.WindowHours, ResponseCacheEnabled: true, ResponseCacheHits: 2}, nil
}

func (s *stubAIGatewayService) PurgeLLMRelayCache(_ context.Context, _ domainidentity.Principal, req domainaigateway.LLMRelayCachePurgeRequest) (domainaigateway.LLMRelayCachePurgeResult, error) {
	s.cachePurgeReq = req
	return domainaigateway.LLMRelayCachePurgeResult{Status: "dry_run", PurgedCount: 3, DryRun: req.DryRun}, nil
}

func (s *stubAIGatewayService) LLMRelayMaxRequestBodyBytes() int64 {
	if s.maxRelayBodySize > 0 {
		return s.maxRelayBodySize
	}
	return 32 << 20
}

func (s *stubAIGatewayService) RelayLLMHTTP(_ context.Context, _ domainidentity.Principal, _ domainidentity.AccessContext, req appaigateway.LLMRelayHTTPRequest, _ http.ResponseWriter) error {
	s.relayReq = req
	return s.relayErr
}

func (s *stubAIGatewayService) RelayLLMWebSocket(_ context.Context, _ domainidentity.Principal, _ domainidentity.AccessContext, req appaigateway.LLMRelayHTTPRequest, _ http.ResponseWriter, _ *http.Request) error {
	s.relayReq = req
	return s.relayErr
}

func TestAIGatewayGovernanceStatusBoundsWindowHours(t *testing.T) {
	for _, tc := range []struct {
		name        string
		query       string
		wantStatus  int
		wantWindow  int
		wantService bool
	}{
		{name: "default window", query: "", wantStatus: http.StatusOK, wantWindow: 0, wantService: true},
		{name: "explicit max window", query: "?windowHours=168", wantStatus: http.StatusOK, wantWindow: 168, wantService: true},
		{name: "negative window", query: "?windowHours=-1", wantStatus: http.StatusBadRequest},
		{name: "too large window", query: "?windowHours=169", wantStatus: http.StatusBadRequest},
		{name: "non integer window", query: "?windowHours=abc", wantStatus: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &stubAIGatewayService{}
			handler := NewAIGatewayHandler(service)
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/ai-gateway/governance/status"+tc.query, nil)
			ctx.Set("principal", domainidentity.Principal{UserID: "u-1"})

			handler.GovernanceStatus(ctx)

			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d, body=%s", recorder.Code, tc.wantStatus, recorder.Body.String())
			}
			if service.governanceCalled != tc.wantService {
				t.Fatalf("service called = %v, want %v", service.governanceCalled, tc.wantService)
			}
			if tc.wantService && service.governanceReq.WindowHours != tc.wantWindow {
				t.Fatalf("windowHours = %d, want %d", service.governanceReq.WindowHours, tc.wantWindow)
			}
			if !tc.wantService {
				var payload map[string]any
				if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
					t.Fatalf("decode error payload: %v", err)
				}
				if payload["error"] == nil {
					t.Fatalf("expected error response, got %s", recorder.Body.String())
				}
			}
		})
	}
}

func TestLLMRelayCacheStatsBindsQuery(t *testing.T) {
	service := &stubAIGatewayService{}
	handler := NewAIGatewayHandler(service)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/ai-gateway/relay/cache/stats?windowHours=12&publicModel=gpt-public&upstreamId=upstream-1", nil)
	ctx.Set("principal", domainidentity.Principal{UserID: "u-1"})

	handler.LLMRelayCacheStats(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if service.cacheStatsReq.WindowHours != 12 || service.cacheStatsReq.PublicModel != "gpt-public" || service.cacheStatsReq.UpstreamID != "upstream-1" {
		t.Fatalf("cache stats request = %#v", service.cacheStatsReq)
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok || data["responseCacheHits"] != float64(2) {
		t.Fatalf("unexpected response payload: %s", recorder.Body.String())
	}
}

func TestLLMRelayCacheStatsRejectsInvalidWindow(t *testing.T) {
	service := &stubAIGatewayService{}
	handler := NewAIGatewayHandler(service)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/ai-gateway/relay/cache/stats?windowHours=169", nil)
	ctx.Set("principal", domainidentity.Principal{UserID: "u-1"})

	handler.LLMRelayCacheStats(ctx)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if service.cacheStatsReq.WindowHours != 0 {
		t.Fatalf("service should not be called, got request %#v", service.cacheStatsReq)
	}
}

func TestPurgeLLMRelayCacheBindsBody(t *testing.T) {
	olderThan := "2026-06-25T00:00:00Z"
	service := &stubAIGatewayService{}
	handler := NewAIGatewayHandler(service)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/ai-gateway/relay/cache/purge", strings.NewReader(`{"publicModel":"gpt-public","upstreamId":"upstream-1","routeGroup":"prod","olderThan":"`+olderThan+`","dryRun":true}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("principal", domainidentity.Principal{UserID: "u-1"})

	handler.PurgeLLMRelayCache(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if service.cachePurgeReq.PublicModel != "gpt-public" || service.cachePurgeReq.UpstreamID != "upstream-1" || service.cachePurgeReq.RouteGroup != "prod" || !service.cachePurgeReq.DryRun {
		t.Fatalf("purge request = %#v", service.cachePurgeReq)
	}
	if service.cachePurgeReq.OlderThan == nil || service.cachePurgeReq.OlderThan.Format(time.RFC3339) != olderThan {
		t.Fatalf("olderThan = %#v, want %s", service.cachePurgeReq.OlderThan, olderThan)
	}
}

func TestRelayOpenAIChatCompletionsReturnsNativeError(t *testing.T) {
	service := &stubAIGatewayService{
		relayErr: apperrors.ErrAccessDenied,
	}
	handler := NewAIGatewayHandler(service)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/ai-gateway/llm/openai/v1/chat/completions", strings.NewReader(`{"model":"gpt-4.1"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("principal", domainidentity.Principal{UserID: "u-1"})

	handler.RelayOpenAIChatCompletions(ctx)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
	if service.relayReq.ProviderKind != "openai" || service.relayReq.Endpoint != "chat/completions" {
		t.Fatalf("relay request = %#v", service.relayReq)
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode relay error: %v", err)
	}
	if _, hasData := payload["data"]; hasData {
		t.Fatalf("relay error should not use OpenSoha envelope: %s", recorder.Body.String())
	}
	errorPayload, ok := payload["error"].(map[string]any)
	if !ok || errorPayload["code"] != "access_denied" {
		t.Fatalf("relay error payload = %#v", payload)
	}
}

func TestRelayOpenAIChatCompletionsForwardsDebugHeaders(t *testing.T) {
	service := &stubAIGatewayService{}
	handler := NewAIGatewayHandler(service)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/ai-gateway/llm/openai/v1/chat/completions", strings.NewReader(`{"model":"gpt-4.1"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Request.Header.Set("X-Soha-Upstream-ID", "upstream-debug")
	ctx.Request.Header.Set("X-Soha-Route-Trace", "true")
	ctx.Set("principal", domainidentity.Principal{UserID: "u-1"})

	handler.RelayOpenAIChatCompletions(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if service.relayReq.Headers.Get("X-Soha-Upstream-ID") != "upstream-debug" || service.relayReq.Headers.Get("X-Soha-Route-Trace") != "true" {
		t.Fatalf("debug headers not forwarded: %#v", service.relayReq.Headers)
	}
}

func TestRelayOpenAIEmbeddingsForwardsNativeRelayRequest(t *testing.T) {
	runRelayHandlerTestCase(t, relayHandlerTestCase{
		path: "/api/v1/ai-gateway/llm/openai/v1/embeddings", providerKind: "openai", endpoint: "embeddings",
		body: `{"model":"text-embedding-3-small","input":"hello"}`, contentType: "application/json",
		handler: (*AIGatewayHandler).RelayOpenAIEmbeddings,
	})
}

func TestRelayOpenAIImageGenerationsForwardsNativeRelayRequest(t *testing.T) {
	runRelayHandlerTestCase(t, relayHandlerTestCase{
		path: "/api/v1/ai-gateway/llm/openai/v1/images/generations", providerKind: "openai", endpoint: "images/generations",
		body: `{"model":"gpt-image-1","prompt":"draw a badge"}`, contentType: "application/json",
		handler: (*AIGatewayHandler).RelayOpenAIImageGenerations,
	})
}

func TestRelayOpenAIImageMultipartForwardsNativeRelayRequest(t *testing.T) {
	for _, tt := range []struct {
		name     string
		path     string
		endpoint string
		handler  func(*AIGatewayHandler, *gin.Context)
	}{
		{
			name:     "edits",
			path:     "/api/v1/ai-gateway/llm/openai/v1/images/edits",
			endpoint: "images/edits",
			handler:  (*AIGatewayHandler).RelayOpenAIImageEdits,
		},
		{
			name:     "variations",
			path:     "/api/v1/ai-gateway/llm/openai/v1/images/variations",
			endpoint: "images/variations",
			handler:  (*AIGatewayHandler).RelayOpenAIImageVariations,
		},
	} {
		runRelayHandlerTestCase(t, relayHandlerTestCase{
			name: tt.name, path: tt.path, providerKind: "openai", endpoint: tt.endpoint,
			body: "multipart-body", contentType: "multipart/form-data; boundary=test", handler: tt.handler,
		})
	}
}

func TestRelayOpenAIAudioSpeechForwardsNativeRelayRequest(t *testing.T) {
	runRelayHandlerTestCase(t, relayHandlerTestCase{
		path: "/api/v1/ai-gateway/llm/openai/v1/audio/speech", providerKind: "openai", endpoint: "audio/speech",
		body: `{"model":"tts-1","input":"hello","voice":"alloy"}`, contentType: "application/json",
		handler: (*AIGatewayHandler).RelayOpenAIAudioSpeech,
	})
}

func TestRelayOpenAIRealtimeForwardsWebSocketRelayRequest(t *testing.T) {
	service := &stubAIGatewayService{}
	handler := NewAIGatewayHandler(service)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/ai-gateway/llm/openai/v1/realtime?model=gpt-realtime", nil)
	ctx.Request.Header.Set("OpenAI-Organization", "org-1")
	ctx.Set("principal", domainidentity.Principal{UserID: "u-1"})

	handler.RelayOpenAIRealtime(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if service.relayReq.ProviderKind != "openai" || service.relayReq.Endpoint != "realtime" || service.relayReq.Method != http.MethodGet {
		t.Fatalf("relay request = %#v", service.relayReq)
	}
	if service.relayReq.QueryModel != "gpt-realtime" {
		t.Fatalf("query model = %q", service.relayReq.QueryModel)
	}
	if service.relayReq.Headers.Get("OpenAI-Organization") != "org-1" {
		t.Fatalf("headers = %#v", service.relayReq.Headers)
	}
}

func TestRelayOpenAIAudioMultipartForwardsNativeRelayRequest(t *testing.T) {
	for _, tt := range []struct {
		name     string
		path     string
		endpoint string
		handler  func(*AIGatewayHandler, *gin.Context)
	}{
		{
			name:     "transcriptions",
			path:     "/api/v1/ai-gateway/llm/openai/v1/audio/transcriptions",
			endpoint: "audio/transcriptions",
			handler:  (*AIGatewayHandler).RelayOpenAIAudioTranscriptions,
		},
		{
			name:     "translations",
			path:     "/api/v1/ai-gateway/llm/openai/v1/audio/translations",
			endpoint: "audio/translations",
			handler:  (*AIGatewayHandler).RelayOpenAIAudioTranslations,
		},
	} {
		runRelayHandlerTestCase(t, relayHandlerTestCase{
			name: tt.name, path: tt.path, providerKind: "openai", endpoint: tt.endpoint,
			body: "multipart-body", contentType: "multipart/form-data; boundary=test", handler: tt.handler,
		})
	}
}

func TestRelayFirstClassOpenAICompatibleChatCompletionsForwardNativeRelayRequest(t *testing.T) {
	for _, tt := range []struct {
		name         string
		path         string
		providerKind string
		handler      func(*AIGatewayHandler, *gin.Context)
	}{
		{
			name:         "deepseek",
			path:         "/api/v1/ai-gateway/llm/deepseek/v1/chat/completions",
			providerKind: "deepseek",
			handler:      (*AIGatewayHandler).RelayDeepSeekChatCompletions,
		},
		{
			name:         "qwen",
			path:         "/api/v1/ai-gateway/llm/qwen/v1/chat/completions",
			providerKind: "qwen",
			handler:      (*AIGatewayHandler).RelayQwenChatCompletions,
		},
		{
			name:         "openrouter",
			path:         "/api/v1/ai-gateway/llm/openrouter/v1/chat/completions",
			providerKind: "openrouter",
			handler:      (*AIGatewayHandler).RelayOpenRouterChatCompletions,
		},
		{
			name:         "azure-openai",
			path:         "/api/v1/ai-gateway/llm/azure-openai/v1/chat/completions",
			providerKind: "azure-openai",
			handler:      (*AIGatewayHandler).RelayAzureOpenAIChatCompletions,
		},
	} {
		runRelayHandlerTestCase(t, relayHandlerTestCase{
			name: tt.name, path: tt.path, providerKind: tt.providerKind, endpoint: "chat/completions",
			body: `{"model":"provider-model"}`, contentType: "application/json", handler: tt.handler,
		})
	}
}

func TestRelayFirstClassOpenAICompatibleImageGenerationsForwardNativeRelayRequest(t *testing.T) {
	for _, tt := range []struct {
		name         string
		path         string
		providerKind string
		handler      func(*AIGatewayHandler, *gin.Context)
	}{
		{
			name:         "deepseek",
			path:         "/api/v1/ai-gateway/llm/deepseek/v1/images/generations",
			providerKind: "deepseek",
			handler:      (*AIGatewayHandler).RelayDeepSeekImageGenerations,
		},
		{
			name:         "qwen",
			path:         "/api/v1/ai-gateway/llm/qwen/v1/images/generations",
			providerKind: "qwen",
			handler:      (*AIGatewayHandler).RelayQwenImageGenerations,
		},
		{
			name:         "openrouter",
			path:         "/api/v1/ai-gateway/llm/openrouter/v1/images/generations",
			providerKind: "openrouter",
			handler:      (*AIGatewayHandler).RelayOpenRouterImageGenerations,
		},
		{
			name:         "azure-openai",
			path:         "/api/v1/ai-gateway/llm/azure-openai/v1/images/generations",
			providerKind: "azure-openai",
			handler:      (*AIGatewayHandler).RelayAzureOpenAIImageGenerations,
		},
	} {
		runRelayHandlerTestCase(t, relayHandlerTestCase{
			name: tt.name, path: tt.path, providerKind: tt.providerKind, endpoint: "images/generations",
			body: `{"model":"provider-image","prompt":"draw"}`, contentType: "application/json", handler: tt.handler,
		})
	}
}

func TestRelayFirstClassOpenAICompatibleImageMultipartForwardNativeRelayRequest(t *testing.T) {
	for _, tt := range []struct {
		name         string
		path         string
		providerKind string
		endpoint     string
		handler      func(*AIGatewayHandler, *gin.Context)
	}{
		{name: "deepseek edits", path: compatibleRelayPath("deepseek", "images/edits"), providerKind: "deepseek", endpoint: "images/edits", handler: (*AIGatewayHandler).RelayDeepSeekImageEdits},
		{name: "deepseek variations", path: compatibleRelayPath("deepseek", "images/variations"), providerKind: "deepseek", endpoint: "images/variations", handler: (*AIGatewayHandler).RelayDeepSeekImageVariations},
		{name: "qwen edits", path: compatibleRelayPath("qwen", "images/edits"), providerKind: "qwen", endpoint: "images/edits", handler: (*AIGatewayHandler).RelayQwenImageEdits},
		{name: "qwen variations", path: compatibleRelayPath("qwen", "images/variations"), providerKind: "qwen", endpoint: "images/variations", handler: (*AIGatewayHandler).RelayQwenImageVariations},
		{name: "openrouter edits", path: compatibleRelayPath("openrouter", "images/edits"), providerKind: "openrouter", endpoint: "images/edits", handler: (*AIGatewayHandler).RelayOpenRouterImageEdits},
		{name: "openrouter variations", path: compatibleRelayPath("openrouter", "images/variations"), providerKind: "openrouter", endpoint: "images/variations", handler: (*AIGatewayHandler).RelayOpenRouterImageVariations},
		{name: "azure-openai edits", path: compatibleRelayPath("azure-openai", "images/edits"), providerKind: "azure-openai", endpoint: "images/edits", handler: (*AIGatewayHandler).RelayAzureOpenAIImageEdits},
		{name: "azure-openai variations", path: compatibleRelayPath("azure-openai", "images/variations"), providerKind: "azure-openai", endpoint: "images/variations", handler: (*AIGatewayHandler).RelayAzureOpenAIImageVariations},
	} {
		runRelayHandlerTestCase(t, relayHandlerTestCase{
			name: tt.name, path: tt.path, providerKind: tt.providerKind, endpoint: tt.endpoint,
			body: "multipart-body", contentType: "multipart/form-data; boundary=test", handler: tt.handler,
		})
	}
}

func TestRelayFirstClassOpenAICompatibleAudioSpeechForwardNativeRelayRequest(t *testing.T) {
	for _, tt := range []struct {
		name         string
		path         string
		providerKind string
		handler      func(*AIGatewayHandler, *gin.Context)
	}{
		{
			name:         "deepseek",
			path:         "/api/v1/ai-gateway/llm/deepseek/v1/audio/speech",
			providerKind: "deepseek",
			handler:      (*AIGatewayHandler).RelayDeepSeekAudioSpeech,
		},
		{
			name:         "qwen",
			path:         "/api/v1/ai-gateway/llm/qwen/v1/audio/speech",
			providerKind: "qwen",
			handler:      (*AIGatewayHandler).RelayQwenAudioSpeech,
		},
		{
			name:         "openrouter",
			path:         "/api/v1/ai-gateway/llm/openrouter/v1/audio/speech",
			providerKind: "openrouter",
			handler:      (*AIGatewayHandler).RelayOpenRouterAudioSpeech,
		},
		{
			name:         "azure-openai",
			path:         "/api/v1/ai-gateway/llm/azure-openai/v1/audio/speech",
			providerKind: "azure-openai",
			handler:      (*AIGatewayHandler).RelayAzureOpenAIAudioSpeech,
		},
	} {
		runRelayHandlerTestCase(t, relayHandlerTestCase{
			name: tt.name, path: tt.path, providerKind: tt.providerKind, endpoint: "audio/speech",
			body: `{"model":"provider-tts","input":"hello","voice":"alloy"}`, contentType: "application/json", handler: tt.handler,
		})
	}
}

func TestRelayFirstClassOpenAICompatibleAudioMultipartForwardNativeRelayRequest(t *testing.T) {
	const contentType = "multipart/form-data; boundary=test"
	runRelayHandlerTestCases(t, []relayHandlerTestCase{
		{name: "deepseek transcriptions", path: compatibleRelayPath("deepseek", "audio/transcriptions"), providerKind: "deepseek", endpoint: "audio/transcriptions", body: "multipart-body", contentType: contentType, handler: (*AIGatewayHandler).RelayDeepSeekAudioTranscriptions},
		{name: "deepseek translations", path: compatibleRelayPath("deepseek", "audio/translations"), providerKind: "deepseek", endpoint: "audio/translations", body: "multipart-body", contentType: contentType, handler: (*AIGatewayHandler).RelayDeepSeekAudioTranslations},
		{name: "qwen transcriptions", path: compatibleRelayPath("qwen", "audio/transcriptions"), providerKind: "qwen", endpoint: "audio/transcriptions", body: "multipart-body", contentType: contentType, handler: (*AIGatewayHandler).RelayQwenAudioTranscriptions},
		{name: "qwen translations", path: compatibleRelayPath("qwen", "audio/translations"), providerKind: "qwen", endpoint: "audio/translations", body: "multipart-body", contentType: contentType, handler: (*AIGatewayHandler).RelayQwenAudioTranslations},
		{name: "openrouter transcriptions", path: compatibleRelayPath("openrouter", "audio/transcriptions"), providerKind: "openrouter", endpoint: "audio/transcriptions", body: "multipart-body", contentType: contentType, handler: (*AIGatewayHandler).RelayOpenRouterAudioTranscriptions},
		{name: "openrouter translations", path: compatibleRelayPath("openrouter", "audio/translations"), providerKind: "openrouter", endpoint: "audio/translations", body: "multipart-body", contentType: contentType, handler: (*AIGatewayHandler).RelayOpenRouterAudioTranslations},
		{name: "azure-openai transcriptions", path: compatibleRelayPath("azure-openai", "audio/transcriptions"), providerKind: "azure-openai", endpoint: "audio/transcriptions", body: "multipart-body", contentType: contentType, handler: (*AIGatewayHandler).RelayAzureOpenAIAudioTranscriptions},
		{name: "azure-openai translations", path: compatibleRelayPath("azure-openai", "audio/translations"), providerKind: "azure-openai", endpoint: "audio/translations", body: "multipart-body", contentType: contentType, handler: (*AIGatewayHandler).RelayAzureOpenAIAudioTranslations},
	})
}

func TestRelayGeminiGenerateContentForwardsNativeRelayRequest(t *testing.T) {
	for _, tt := range []struct {
		name     string
		action   string
		endpoint string
	}{
		{name: "generate content", action: "generateContent", endpoint: "generateContent"},
		{name: "stream generate content", action: "streamGenerateContent", endpoint: "streamGenerateContent"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			service := &stubAIGatewayService{}
			handler := NewAIGatewayHandler(service)
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/ai-gateway/llm/gemini/v1beta/models/gemini-public:"+tt.action, strings.NewReader(`{"contents":[{"parts":[{"text":"hi"}]}]}`))
			ctx.Request.Header.Set("Content-Type", "application/json")
			ctx.Params = gin.Params{{Key: "modelAction", Value: "/gemini-public:" + tt.action}}
			ctx.Set("principal", domainidentity.Principal{UserID: "u-1"})

			handler.RelayGeminiModelAction(ctx)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
			}
			if service.relayReq.ProviderKind != "gemini" || service.relayReq.Endpoint != tt.endpoint || service.relayReq.PathModel != "gemini-public" || service.relayReq.Method != http.MethodPost {
				t.Fatalf("relay request = %#v", service.relayReq)
			}
			if string(service.relayReq.Body) != `{"contents":[{"parts":[{"text":"hi"}]}]}` {
				t.Fatalf("relay body = %s", service.relayReq.Body)
			}
		})
	}
}

func TestRelayGeminiInteractionsForwardsNativeRelayRequest(t *testing.T) {
	runRelayHandlerTestCase(t, relayHandlerTestCase{
		path: "/api/v1/ai-gateway/llm/gemini/v1beta/interactions", providerKind: "gemini", endpoint: "interactions",
		body: `{"model":"gemini-image-public","input":"draw"}`, contentType: "application/json",
		handler: (*AIGatewayHandler).RelayGeminiInteractions,
	})
}

func TestRelayCohereRerankForwardsNativeRelayRequest(t *testing.T) {
	runRelayHandlerTestCase(t, relayHandlerTestCase{
		path: "/api/v1/ai-gateway/llm/cohere/v2/rerank", providerKind: "cohere", endpoint: "rerank",
		body: `{"model":"rerank-public","query":"q","documents":["a"]}`, contentType: "application/json",
		handler: (*AIGatewayHandler).RelayCohereRerank,
	})
}

func TestRelayLLMUsesConfiguredRequestBodyLimit(t *testing.T) {
	service := &stubAIGatewayService{maxRelayBodySize: 1}
	handler := NewAIGatewayHandler(service)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/ai-gateway/llm/openai/v1/chat/completions", strings.NewReader(`{"model":"gpt-public"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("principal", domainidentity.Principal{UserID: "u-1"})

	handler.RelayOpenAIChatCompletions(ctx)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusRequestEntityTooLarge, recorder.Body.String())
	}
	if len(service.relayReq.Body) != 0 {
		t.Fatalf("relay service should not receive oversized body, got %q", service.relayReq.Body)
	}
}

func TestListPersonalAccessTokensBindsScopeFilters(t *testing.T) {
	service := &stubAIGatewayService{}
	handler := NewAIGatewayHandler(service)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/ai-gateway/personal-access-tokens?scope=all&userId=user-2", nil)
	ctx.Set("principal", domainidentity.Principal{UserID: "u-1"})

	handler.ListPersonalAccessTokens(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if service.listPATReq.Scope != "all" || service.listPATReq.UserID != "user-2" {
		t.Fatalf("request = %#v, want scope=all userId=user-2", service.listPATReq)
	}
}

func TestRotatePersonalAccessTokenBindsOptionalExpiration(t *testing.T) {
	expiresAt := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	service := &stubAIGatewayService{}
	handler := NewAIGatewayHandler(service)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/ai-gateway/personal-access-tokens/pat-1/rotate", strings.NewReader(`{"expiresAt":"2026-07-01T00:00:00Z"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = gin.Params{{Key: "tokenID", Value: "pat-1"}}
	ctx.Set("principal", domainidentity.Principal{UserID: "u-1"})

	handler.RotatePersonalAccessToken(ctx)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if service.rotatePATID != "pat-1" {
		t.Fatalf("tokenID = %q, want pat-1", service.rotatePATID)
	}
	if service.rotatePATReq.ExpiresAt == nil || !service.rotatePATReq.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("expiresAt = %#v, want %v", service.rotatePATReq.ExpiresAt, expiresAt)
	}
}
