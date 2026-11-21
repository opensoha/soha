package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	apierrors "github.com/opensoha/soha/internal/api/errors"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const pluginRepositoryRequestTimeout = 12 * time.Second

type PluginRepositoryService interface {
	PreviewRepository(context.Context, domainidentity.Principal, domainplugin.PluginRepositoryPreviewRequest) (domainplugin.PluginRepositoryPreview, error)
	InstallRepository(context.Context, domainidentity.Principal, domainplugin.PluginRepositoryInstallRequest) (domainplugin.InstalledPlugin, error)
	UpgradeRepository(context.Context, domainidentity.Principal, string, domainplugin.PluginRepositoryUpgradeRequest) (domainplugin.InstalledPlugin, error)
	ListRepositoryRefs(context.Context, domainidentity.Principal, domainplugin.PluginRepositoryRefsRequest) (domainplugin.PluginRepositoryRefList, error)
	ListUpdates(context.Context, domainidentity.Principal, ...string) ([]domainplugin.PluginUpdate, error)
}

func (h *PluginHandler) ListRepositoryRefs(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req domainplugin.PluginRepositoryRefsRequest
	if !h.readRepositoryRequest(c, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), pluginRepositoryRequestTimeout)
	defer cancel()
	result, err := h.repository.ListRepositoryRefs(ctx, apiMiddleware.PrincipalFromContext(c), req)
	if err != nil {
		writePluginRepositoryError(c, errors.Join(err, ctx.Err()))
		return
	}
	apiresponse.Item(c, http.StatusOK, result)
}

func (h *PluginHandler) ListUpdates(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h.repository == nil {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), pluginRepositoryRequestTimeout)
	defer cancel()
	items, err := h.repository.ListUpdates(ctx, apiMiddleware.PrincipalFromContext(c), query["pluginId"]...)
	if err != nil {
		if errors.Is(err, apperrors.ErrServiceUnavailable) && ctx.Err() == nil {
			writeError(c, err)
		} else {
			writePluginRepositoryError(c, errors.Join(err, ctx.Err()))
		}
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PluginHandler) PreviewRepository(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req domainplugin.PluginRepositoryPreviewRequest
	if !h.readRepositoryRequest(c, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), pluginRepositoryRequestTimeout)
	defer cancel()
	item, err := h.repository.PreviewRepository(ctx, apiMiddleware.PrincipalFromContext(c), req)
	if err != nil {
		writePluginRepositoryError(c, errors.Join(err, ctx.Err()))
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PluginHandler) InstallRepository(c *gin.Context) {
	var req domainplugin.PluginRepositoryInstallRequest
	if !h.readRepositoryRequest(c, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), pluginRepositoryRequestTimeout)
	defer cancel()
	item, err := h.repository.InstallRepository(ctx, apiMiddleware.PrincipalFromContext(c), req)
	if err != nil {
		writePluginRepositoryError(c, errors.Join(err, ctx.Err()))
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *PluginHandler) UpgradeRepository(c *gin.Context) {
	var req domainplugin.PluginRepositoryUpgradeRequest
	if !h.readRepositoryRequest(c, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), pluginRepositoryRequestTimeout)
	defer cancel()
	item, err := h.repository.UpgradeRepository(ctx, apiMiddleware.PrincipalFromContext(c), c.Param("pluginID"), req)
	if err != nil {
		writePluginRepositoryError(c, errors.Join(err, ctx.Err()))
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PluginHandler) readRepositoryRequest(c *gin.Context, dst any) bool {
	if h.repository == nil {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(dst) != nil || decoder.Decode(new(any)) != io.EOF {
		writeError(c, apperrors.ErrInvalidArgument)
		return false
	}
	return true
}

func writePluginRepositoryError(c *gin.Context, err error) {
	code := apierrors.Code(err)
	status := apierrors.StatusCode(err)
	switch {
	case errors.Is(err, domainplugin.ErrRepositoryRateLimited):
		status, code = http.StatusTooManyRequests, "plugin_repository_rate_limited"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		status, code = http.StatusGatewayTimeout, "plugin_repository_timeout"
		err = apperrors.NewBusiness(apperrors.ErrServiceUnavailable, code, "The plugin repository request timed out.", "插件仓库请求超时。")
	case errors.Is(err, apperrors.ErrServiceUnavailable):
		status, code = http.StatusBadGateway, "plugin_repository_unavailable"
	}
	if status == http.StatusInternalServerError {
		writeError(c, err)
		return
	}
	apiresponse.Error(c, status, code, apierrors.Message(err, apierrors.RequestLanguage(c.Request)))
}
