package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	appidentity "github.com/opensoha/soha/internal/application/identity"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type cliHandlerStub struct {
	IdentityCLIDeviceService
	IdentityCLISessionService
	calls  int
	create domainidentity.CLIDeviceCreate
	err    error
}

func (s *cliHandlerStub) CLIAuthOptions(context.Context) domainidentity.CLIAuthOptions {
	s.calls++
	return domainidentity.CLIAuthOptions{ProtocolVersion: 2, SupportedMethods: []string{"key"}}
}
func (s *cliHandlerStub) CreateCLIDeviceAuthorization(_ context.Context, in domainidentity.CLIDeviceCreate, _ string) (domainidentity.CLIDeviceAuthorization, error) {
	s.calls++
	s.create = in
	return domainidentity.CLIDeviceAuthorization{UserCode: "001234"}, s.err
}
func (s *cliHandlerStub) ExchangeCLIDeviceToken(context.Context, domainidentity.CLIDeviceTokenRequest) (domainidentity.AuthResult, error) {
	s.calls++
	return domainidentity.AuthResult{}, s.err
}
func (s *cliHandlerStub) LogoutCLISession(context.Context, string) error { s.calls++; return s.err }
func TestCLIAuthHandlerRejectsMissingServices(t *testing.T) {
	stub := &cliHandlerStub{}
	var typedNil *cliHandlerStub
	for _, services := range []struct {
		device  IdentityCLIDeviceService
		session IdentityCLISessionService
	}{{nil, stub}, {stub, nil}, {typedNil, stub}, {stub, typedNil}} {
		if _, err := NewCLIAuthHandler(services.device, services.session); err == nil {
			t.Fatal("missing or typed nil dependency accepted")
		}
	}
}
func TestCLIAuthHandlerStrictBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, body, contentType string
		status                  int
	}{
		{"valid", `{"clientId":"soha-cli","codeChallenge":"challenge","codeChallengeMethod":"S256"}`, "application/json", 200},
		{"unknown-field", `{"deviceCode":"test-secret","unexpected":true}`, "application/json", 400},
		{"trailing-json", `{} {}`, "application/json", 400},
		{"wrong-media", `{}`, "text/plain", 400},
		{"invalid-json", `{"password":"test-secret"`, "application/json", 400},
		{"bounded-body", `{"deviceName":"` + strings.Repeat("s", maxCLIAuthRequestBytes) + `"}`, "application/json", 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &cliHandlerStub{}
			h, err := NewCLIAuthHandler(stub, stub)
			if err != nil {
				t.Fatal(err)
			}
			router := gin.New()
			router.POST("/", h.Create)
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.contentType)
			out := httptest.NewRecorder()
			router.ServeHTTP(out, req)
			if out.Code != tc.status {
				t.Fatalf("status %d, want %d", out.Code, tc.status)
			}
			if out.Header().Get("Cache-Control") != "no-store" || out.Header().Get("Referrer-Policy") != "no-referrer" {
				t.Fatal("credential responses must not be cached or referred")
			}
			if len(out.Result().Cookies()) > 0 || strings.Contains(out.Body.String(), "test-secret") {
				t.Fatal("CLI auth set a cookie or echoed submitted credentials")
			}
			if tc.status != 200 && stub.calls != 0 {
				t.Fatal("malformed request reached application service")
			}
			if tc.status == 200 && !strings.Contains(out.Body.String(), `"001234"`) {
				t.Fatal("leading zero in the verification code was lost")
			}
		})
	}
}
func TestCLIAuthPollingAndLogoutResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name    string
		pending bool
		status  int
	}{{"authorization_pending", true, 409}, {"slow_down", false, 429}} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &cliHandlerStub{err: &appidentity.CLIPollError{Code: tc.name, Seconds: 10, Pending: tc.pending}}
			h, _ := NewCLIAuthHandler(stub, stub)
			router := gin.New()
			router.POST("/", h.Exchange)
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			out := httptest.NewRecorder()
			router.ServeHTTP(out, req)
			if out.Code != tc.status || out.Header().Get("Retry-After") != "10" || !strings.Contains(out.Body.String(), tc.name) {
				t.Fatal("polling status/code/backoff were not preserved")
			}
		})
	}
	stub := &cliHandlerStub{}
	h, _ := NewCLIAuthHandler(stub, stub)
	router := gin.New()
	router.POST("/", h.Logout)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"refreshToken":"test-secret"}`))
	req.Header.Set("Content-Type", "application/json")
	out := httptest.NewRecorder()
	router.ServeHTTP(out, req)
	if out.Code != 200 || out.Body.Len() != 0 || out.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("logout must have an empty, non-cacheable 200 response")
	}
}

func TestMFAEnrollmentStepUpErrorIsActionable(t *testing.T) {
	out := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(out)
	writeMFAError(c, apperrors.ErrMFARequired)
	if out.Code != 403 || !strings.Contains(out.Body.String(), `"mfa_required"`) {
		t.Fatal("MFA policy rejection must remain a specific 403")
	}
}
