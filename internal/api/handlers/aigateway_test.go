package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type stubAIGatewayService struct {
	governanceCalled bool
	governanceReq    domainaigateway.GovernanceStatusRequest
	listPATReq       domainaigateway.PersonalAccessTokenListRequest
	rotatePATID      string
	rotatePATReq     domainaigateway.TokenRotationInput
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

func TestAIGatewayGovernanceStatusBoundsWindowHours(t *testing.T) {
	gin.SetMode(gin.TestMode)

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

func TestListPersonalAccessTokensBindsScopeFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)

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
	gin.SetMode(gin.TestMode)

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
