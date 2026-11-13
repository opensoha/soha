package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	runnerpkg "github.com/opensoha/soha-agent/internal/agent/runner"
	"go.uber.org/zap"
)

type fakeOutpostRuntime struct {
	request runnerpkg.ForwardAuthRequest
	ready   bool
	result  runnerpkg.ForwardAuthResult
}

func (f *fakeOutpostRuntime) ListActiveTasks() []runnerpkg.ActiveTask { return nil }
func (f *fakeOutpostRuntime) GetActiveTask(string) (runnerpkg.ActiveTask, bool) {
	return runnerpkg.ActiveTask{}, false
}
func (f *fakeOutpostRuntime) CancelActiveTask(string, string) bool { return false }
func (f *fakeOutpostRuntime) OutpostStatus() runnerpkg.OutpostStatus {
	return runnerpkg.OutpostStatus{Enabled: true, Ready: f.ready}
}
func (f *fakeOutpostRuntime) CheckOutpostAccess(_ context.Context, request runnerpkg.ForwardAuthRequest) (runnerpkg.ForwardAuthResult, error) {
	f.request = request
	if f.result.Decision != "" {
		return f.result, nil
	}
	return runnerpkg.ForwardAuthResult{Decision: "allow", StatusCode: http.StatusNoContent, Headers: map[string]string{"X-Soha-User": "alice"}}, nil
}

func TestOutpostForwardAuthRoute(t *testing.T) {
	runtime := &fakeOutpostRuntime{ready: true}
	server := New(cfgpkg.Config{
		HTTP:         cfgpkg.HTTPConfig{Addr: "127.0.0.1:0", BasePath: "/api/v1"},
		ControlPlane: cfgpkg.ControlPlaneConfig{Outpost: cfgpkg.OutpostConfig{Enabled: true}},
	}, zap.NewNop(), nil, runtime)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/outpost/forward-auth", nil)
	req.Header.Set("X-Forwarded-Uri", "/private?from=test")
	req.Header.Set("X-Forwarded-Host", "app.example.com")
	req.Header.Set("X-Forwarded-Method", "POST")
	req.AddCookie(&http.Cookie{Name: proxySessionCookieName, Value: "proxy-session-1"})
	recorder := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNoContent || recorder.Header().Get("X-Soha-User") != "alice" {
		t.Fatalf("response = %d %#v", recorder.Code, recorder.Header())
	}
	if runtime.request.OriginalURL != "https://app.example.com/private?from=test" || runtime.request.RequestPath != "/private" || runtime.request.Method != "POST" || runtime.request.RequestHost != "app.example.com" || runtime.request.SessionToken != "proxy-session-1" {
		t.Fatalf("request = %#v", runtime.request)
	}
}

func TestOutpostForwardAuthHeaderOverridesCookie(t *testing.T) {
	runtime := &fakeOutpostRuntime{ready: true}
	server := New(cfgpkg.Config{
		HTTP:         cfgpkg.HTTPConfig{Addr: "127.0.0.1:0", BasePath: "/api/v1"},
		ControlPlane: cfgpkg.ControlPlaneConfig{Outpost: cfgpkg.OutpostConfig{Enabled: true}},
	}, zap.NewNop(), nil, runtime)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/outpost/forward-auth", nil)
	req.Header.Set("X-Forwarded-Uri", "/private")
	req.Header.Set("X-Soha-Session-Token", "header-session")
	req.AddCookie(&http.Cookie{Name: proxySessionCookieName, Value: "cookie-session"})
	server.httpServer.Handler.ServeHTTP(httptest.NewRecorder(), req)
	if runtime.request.SessionToken != "header-session" {
		t.Fatalf("session token = %q, want header-session", runtime.request.SessionToken)
	}
}

func TestOutpostForwardAuthNginxModeConvertsRedirectToUnauthorized(t *testing.T) {
	runtime := &fakeOutpostRuntime{ready: true, result: runnerpkg.ForwardAuthResult{
		Decision: "redirect", StatusCode: http.StatusFound, RedirectURL: "https://soha.example.com/api/v1/provider/proxy/start",
	}}
	server := New(cfgpkg.Config{
		HTTP:         cfgpkg.HTTPConfig{Addr: "127.0.0.1:0", BasePath: "/api/v1"},
		ControlPlane: cfgpkg.ControlPlaneConfig{Outpost: cfgpkg.OutpostConfig{Enabled: true}},
	}, zap.NewNop(), nil, runtime)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/outpost/forward-auth?mode=nginx", nil)
	req.Header.Set("X-Forwarded-Uri", "/private")
	recorder := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized || recorder.Header().Get("X-Soha-Login-URL") == "" {
		t.Fatalf("response = %d %#v", recorder.Code, recorder.Header())
	}
}

func TestOutpostForwardAuthAcceptsDedicatedProxyTokenHeader(t *testing.T) {
	runtime := &fakeOutpostRuntime{ready: true}
	server := New(cfgpkg.Config{
		HTTP:         cfgpkg.HTTPConfig{Addr: "127.0.0.1:0", BasePath: "/api/v1"},
		Auth:         cfgpkg.AuthConfig{BearerToken: "outpost-token"},
		ControlPlane: cfgpkg.ControlPlaneConfig{Outpost: cfgpkg.OutpostConfig{Enabled: true}},
	}, zap.NewNop(), nil, runtime)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/outpost/forward-auth", nil)
	req.Header.Set("X-Forwarded-Uri", "/private")
	req.Header.Set(outpostAuthHeaderName, "outpost-token")
	recorder := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("response status = %d", recorder.Code)
	}
	if runtime.request.OriginalURL == "" {
		t.Fatal("forward-auth request was not evaluated")
	}
}

func TestOutpostReadinessFailsWithoutSignedConfiguration(t *testing.T) {
	server := New(cfgpkg.Config{
		HTTP:         cfgpkg.HTTPConfig{Addr: "127.0.0.1:0", BasePath: "/api/v1"},
		ControlPlane: cfgpkg.ControlPlaneConfig{Outpost: cfgpkg.OutpostConfig{Enabled: true}},
	}, zap.NewNop(), nil, &fakeOutpostRuntime{})
	recorder := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}
