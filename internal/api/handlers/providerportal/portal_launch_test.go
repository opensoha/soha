package providerportal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainportal "github.com/opensoha/soha/internal/domain/providerportal"
)

type portalLaunchInteractorStub struct {
	decision domainportal.LaunchDecision
	calls    int
}

func (s *portalLaunchInteractorStub) Launch(context.Context, domainidentity.Principal, string) (domainportal.LaunchDecision, error) {
	s.calls++
	return s.decision, nil
}
func (*portalLaunchInteractorStub) SetFavorite(context.Context, domainidentity.Principal, string) (domainportal.Application, error) {
	return domainportal.Application{}, nil
}
func (*portalLaunchInteractorStub) DeleteFavorite(context.Context, domainidentity.Principal, string) error {
	return nil
}
func (*portalLaunchInteractorStub) ListRecent(context.Context, domainidentity.Principal, int) ([]domainportal.ApplicationLaunch, error) {
	return nil, nil
}

type browserHandoffIssuerStub struct {
	called       bool
	calls        int
	access       domainidentity.AccessContext
	audience     string
	result       domainportal.BrowserHandoff
	auditReasons []string
}

func (s *browserHandoffIssuerStub) CreateBrowserHandoff(_ context.Context, _ domainidentity.Principal, access domainidentity.AccessContext, _ domainportal.Application, audience string) (domainportal.BrowserHandoff, error) {
	s.called = true
	s.calls++
	s.access = access
	s.audience = audience
	return s.result, nil
}

func (s *browserHandoffIssuerStub) AuditBrowserHandoffFailure(_ context.Context, _ domainidentity.Principal, _ string, reason string) {
	s.auditReasons = append(s.auditReasons, reason)
}

func TestDesktopPortalLaunchReturnsBrowserHandoffURL(t *testing.T) {
	expiresAt := time.Now().UTC().Add(time.Minute)
	application := domainportal.Application{ID: "app-1", Name: "Soha Console", ProviderType: domainportal.ProviderTypeOIDC, LaunchURL: "/oauth2/authorize"}
	handoff := &browserHandoffIssuerStub{result: domainportal.BrowserHandoff{ID: "handoff-1", ExpiresAt: expiresAt}}
	interactor := &portalLaunchInteractorStub{decision: domainportal.LaunchDecision{
		Application: application, LaunchURL: application.LaunchURL, ProviderType: application.ProviderType, Decision: "allow",
	}}
	handler := New(Services{
		PortalInteractor: interactor,
		BrowserHandoffs:  handoff,
		AccessURL:        staticAccessURL("https://public.soha.example/"),
	})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "http://internal.example/api/v1/portal/applications/app-1/launch", strings.NewReader(`{"surface":"desktop"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Request.Header.Set("X-Forwarded-Host", "attacker.example")
	ctx.Request.Header.Set("X-Forwarded-Proto", "https")
	ctx.Params = gin.Params{{Key: "applicationID", Value: "app-1"}}
	ctx.Set("principal", domainidentity.Principal{UserID: "user-1"})
	ctx.Set("access_context", domainidentity.AccessContext{TokenKind: "session_access", SessionID: "desktop-session"})
	handler.LaunchPortalApplication(ctx)

	var envelope struct {
		Data domainportal.LaunchDecision `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || interactor.calls != 1 || !handoff.called || handoff.access.SessionID != "desktop-session" || handoff.audience != "https://public.soha.example" || envelope.Data.LaunchURL != "/auth/browser-handoff/handoff-1" || envelope.Data.Application.LaunchURL != envelope.Data.LaunchURL || envelope.Data.HandoffExpiresAt == nil {
		t.Fatalf("desktop launch status=%d handoff=%#v response=%#v", recorder.Code, handoff, envelope.Data)
	}
}

func TestDesktopPortalLaunchIsRateLimitedPerAppSession(t *testing.T) {
	application := domainportal.Application{ID: "app-1", Name: "Soha Console", ProviderType: domainportal.ProviderTypeOIDC, LaunchURL: "/oauth2/authorize"}
	handoff := &browserHandoffIssuerStub{result: domainportal.BrowserHandoff{ID: "handoff-1", ExpiresAt: time.Now().UTC().Add(time.Minute)}}
	interactor := &portalLaunchInteractorStub{decision: domainportal.LaunchDecision{
		Application: application, LaunchURL: application.LaunchURL, ProviderType: application.ProviderType, Decision: "allow",
	}}
	handler := New(Services{
		PortalInteractor: interactor,
		BrowserHandoffs:  handoff,
	})
	var lastStatus int
	for range 21 {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "https://soha.example/api/v1/portal/applications/app-1/launch", strings.NewReader(`{"surface":"desktop"}`))
		ctx.Request.Header.Set("Content-Type", "application/json")
		ctx.Params = gin.Params{{Key: "applicationID", Value: "app-1"}}
		ctx.Set("principal", domainidentity.Principal{UserID: "user-1"})
		ctx.Set("access_context", domainidentity.AccessContext{TokenKind: "session_access", SessionID: "desktop-session"})
		handler.LaunchPortalApplication(ctx)
		lastStatus = recorder.Code
	}
	if lastStatus != http.StatusTooManyRequests || interactor.calls != 20 || handoff.calls != 20 || len(handoff.auditReasons) != 1 || handoff.auditReasons[0] != "rate_limited" {
		t.Fatalf("status=%d launches=%d handoffs=%d audit=%v", lastStatus, interactor.calls, handoff.calls, handoff.auditReasons)
	}
}

func TestWebPortalLaunchKeepsExistingLaunchBehavior(t *testing.T) {
	application := domainportal.Application{ID: "app-1", Name: "Soha Console", ProviderType: domainportal.ProviderTypeLink, LaunchURL: "https://console.example"}
	handoff := &browserHandoffIssuerStub{}
	handler := New(Services{
		PortalInteractor: &portalLaunchInteractorStub{decision: domainportal.LaunchDecision{
			Application: application, LaunchURL: application.LaunchURL, ProviderType: application.ProviderType, Decision: "allow",
		}},
		BrowserHandoffs: handoff,
	})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/portal/applications/app-1/launch", nil)
	ctx.Params = gin.Params{{Key: "applicationID", Value: "app-1"}}
	ctx.Set("principal", domainidentity.Principal{UserID: "user-1"})
	handler.LaunchPortalApplication(ctx)
	if recorder.Code != http.StatusOK || handoff.called || !strings.Contains(recorder.Body.String(), "https://console.example") {
		t.Fatalf("web launch status=%d handoff=%v body=%s", recorder.Code, handoff.called, recorder.Body.String())
	}
}

func TestPortalLaunchSurfaceDefaultsEmptyObjectToWeb(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/portal/applications/app-1/launch", strings.NewReader(`{}`))

	surface, ok := portalLaunchSurface(ctx)
	if !ok || surface != "web" {
		t.Fatalf("surface=%q ok=%v status=%d", surface, ok, recorder.Code)
	}
}
