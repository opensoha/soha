package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type pluginRepositoryHandlerStub struct {
	previewErr     error
	updateErr      error
	updateIDs      []string
	previewMaxTime time.Duration
	installs       int
}

func (s *pluginRepositoryHandlerStub) PreviewRepository(ctx context.Context, _ domainidentity.Principal, _ domainplugin.PluginRepositoryPreviewRequest) (domainplugin.PluginRepositoryPreview, error) {
	if deadline, ok := ctx.Deadline(); ok {
		s.previewMaxTime = time.Until(deadline)
	}
	return domainplugin.PluginRepositoryPreview{}, s.previewErr
}

func (s *pluginRepositoryHandlerStub) InstallRepository(context.Context, domainidentity.Principal, domainplugin.PluginRepositoryInstallRequest) (domainplugin.InstalledPlugin, error) {
	s.installs++
	return domainplugin.InstalledPlugin{}, nil
}

func (s *pluginRepositoryHandlerStub) UpgradeRepository(context.Context, domainidentity.Principal, string, domainplugin.PluginRepositoryUpgradeRequest) (domainplugin.InstalledPlugin, error) {
	return domainplugin.InstalledPlugin{}, nil
}

func (s *pluginRepositoryHandlerStub) ListRepositoryRefs(context.Context, domainidentity.Principal, domainplugin.PluginRepositoryRefsRequest) (domainplugin.PluginRepositoryRefList, error) {
	return domainplugin.PluginRepositoryRefList{}, s.previewErr
}

func (s *pluginRepositoryHandlerStub) ListUpdates(_ context.Context, _ domainidentity.Principal, ids ...string) ([]domainplugin.PluginUpdate, error) {
	s.updateIDs = ids
	return []domainplugin.PluginUpdate{}, s.updateErr
}

func TestRepositoryPluginHTTPBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &pluginRepositoryHandlerStub{}
	handler := &PluginHandler{repository: stub}
	router := gin.New()
	router.POST("/preview", handler.PreviewRepository)
	router.POST("/refs", handler.ListRepositoryRefs)
	router.GET("/updates", handler.ListUpdates)
	router.POST("/install", handler.InstallRepository)
	router.POST("/legacy-install", handler.Install)
	router.POST("/legacy-upgrade", handler.Upgrade)

	send := func(path, body string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
		return response
	}
	for _, tc := range []struct {
		err    error
		status int
	}{
		{nil, http.StatusOK},
		{domainplugin.ErrRepositoryRateLimited, http.StatusTooManyRequests},
		{context.DeadlineExceeded, http.StatusGatewayTimeout},
	} {
		stub.previewErr = tc.err
		response := send("/preview", `{"repositoryUrl":"https://github.com/example/demo"}`)
		if response.Code != tc.status || response.Header().Get("Cache-Control") != "no-store" ||
			stub.previewMaxTime <= 0 || stub.previewMaxTime > pluginRepositoryRequestTimeout {
			t.Fatalf("preview got status %d, headers %v; want status %d with no-store", response.Code, response.Header(), tc.status)
		}
	}
	for _, request := range []string{
		`{"previewToken":"example","pluginId":"legacy-id"}`,
		`{"previewToken":"example"} {}`,
		`{"previewToken":"` + strings.Repeat("a", 16<<10) + `"}`,
	} {
		if response := send("/install", request); response.Code != http.StatusBadRequest || stub.installs != 0 {
			t.Fatalf("ambiguous request reached install: status=%d calls=%d", response.Code, stub.installs)
		}
	}
	if response := send("/install", `{"previewToken":"example"}`); response.Code != http.StatusCreated || stub.installs != 1 {
		t.Fatalf("confirmed request was not delegated: status=%d calls=%d", response.Code, stub.installs)
	}
	for _, path := range []string{"/legacy-install", "/legacy-upgrade"} {
		if response := send(path, `{"previewToken":"example","pluginId":"fixture"}`); response.Code != http.StatusBadRequest {
			t.Fatalf("legacy route accepted a preview token: %s status=%d", path, response.Code)
		}
	}
	stub.previewErr = nil
	refs := send("/refs", `{"repositoryUrl":"https://github.com/example/demo","kind":"release"}`)
	if refs.Code != http.StatusOK || !strings.Contains(refs.Body.String(), `"data":`) || refs.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("invalid refs response: %d %s", refs.Code, refs.Body)
	}
	updates := httptest.NewRecorder()
	router.ServeHTTP(updates, httptest.NewRequest(http.MethodGet, "/updates?pluginId=example.first&pluginId=example.second", nil))
	if updates.Code != http.StatusOK || !strings.Contains(updates.Body.String(), `"items":[]`) || updates.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("invalid updates response: %d %s", updates.Code, updates.Body)
	}
	if !slices.Equal(stub.updateIDs, []string{"example.first", "example.second"}) {
		t.Fatalf("update selection was not passed to the service: %v", stub.updateIDs)
	}
	stub.updateIDs = nil
	updates = httptest.NewRecorder()
	router.ServeHTTP(updates, httptest.NewRequest(http.MethodGet, "/updates?pluginId=%zz&pluginId=example.first", nil))
	if updates.Code != http.StatusBadRequest || stub.updateIDs != nil {
		t.Fatalf("malformed update selection reached the service: %d %v", updates.Code, stub.updateIDs)
	}
	stub.updateErr = apperrors.ErrServiceUnavailable
	updates = httptest.NewRecorder()
	router.ServeHTTP(updates, httptest.NewRequest(http.MethodGet, "/updates", nil))
	if updates.Code != http.StatusServiceUnavailable {
		t.Fatalf("upstream outage should be visible on updates: %d %s", updates.Code, updates.Body)
	}
}
