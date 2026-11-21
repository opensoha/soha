package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/stretchr/testify/require"
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
		require.Equal(t, tc.status, response.Code)
		require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		require.Positive(t, stub.previewMaxTime)
		require.LessOrEqual(t, stub.previewMaxTime, pluginRepositoryRequestTimeout)
	}
	for _, request := range []string{
		`{"previewToken":"example","pluginId":"legacy-id"}`,
		`{"previewToken":"example"} {}`,
		`{"previewToken":"` + strings.Repeat("a", 16<<10) + `"}`,
	} {
		response := send("/install", request)
		require.Equal(t, http.StatusBadRequest, response.Code, "ambiguous install request")
		require.Zero(t, stub.installs, "ambiguous request reached install")
	}
	response := send("/install", `{"previewToken":"example"}`)
	require.Equal(t, http.StatusCreated, response.Code)
	require.Equal(t, 1, stub.installs, "confirmed request was not delegated")
	for _, path := range []string{"/legacy-install", "/legacy-upgrade"} {
		response := send(path, `{"previewToken":"example","pluginId":"fixture"}`)
		require.Equal(t, http.StatusBadRequest, response.Code, "legacy route %s accepted a preview token", path)
	}
	stub.previewErr = nil
	refs := send("/refs", `{"repositoryUrl":"https://github.com/example/demo","kind":"release"}`)
	require.Equal(t, http.StatusOK, refs.Code)
	require.Contains(t, refs.Body.String(), `"data":`)
	require.Equal(t, "no-store", refs.Header().Get("Cache-Control"))
	updates := httptest.NewRecorder()
	router.ServeHTTP(updates, httptest.NewRequest(http.MethodGet, "/updates?pluginId=example.first&pluginId=example.second", nil))
	require.Equal(t, http.StatusOK, updates.Code)
	require.Contains(t, updates.Body.String(), `"items":[]`)
	require.Equal(t, "no-store", updates.Header().Get("Cache-Control"))
	require.Equal(t, []string{"example.first", "example.second"}, stub.updateIDs)
	stub.updateIDs = nil
	updates = httptest.NewRecorder()
	router.ServeHTTP(updates, httptest.NewRequest(http.MethodGet, "/updates?pluginId=%zz&pluginId=example.first", nil))
	require.Equal(t, http.StatusBadRequest, updates.Code)
	require.Nil(t, stub.updateIDs, "malformed update selection reached the service")
	stub.updateErr = apperrors.ErrServiceUnavailable
	updates = httptest.NewRecorder()
	router.ServeHTTP(updates, httptest.NewRequest(http.MethodGet, "/updates", nil))
	require.Equal(t, http.StatusServiceUnavailable, updates.Code, "upstream outage should be visible on updates")
}
