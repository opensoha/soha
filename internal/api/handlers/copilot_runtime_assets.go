package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type CopilotRuntimeAssetService interface {
	ListRuntimeAssets(context.Context, identity.Principal) ([]domain.RuntimeAsset, error)
	SetRuntimeAssetEnabled(context.Context, identity.Principal, domain.RuntimeAssetRef, bool) (domain.RuntimeAsset, error)
	ProbeRuntimeMCP(context.Context, identity.Principal, string, string, string) (domain.RuntimeMCPProbe, error)
}

type copilotRuntimeAssetHandler struct{ service CopilotRuntimeAssetService }

func (h *copilotRuntimeAssetHandler) ListRuntimeAssets(c *gin.Context) {
	if h.service == nil {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	items, err := h.service.ListRuntimeAssets(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *copilotRuntimeAssetHandler) SetRuntimeAssetEnabled(c *gin.Context) {
	if h.service == nil {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if !bindRuntimeProfileJSON(c, &input) {
		return
	}
	if input.Enabled == nil {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	ref := domain.RuntimeAssetRef{Kind: c.Param("kind"), ID: c.Param("assetId"), Version: c.Param("version")}
	item, err := h.service.SetRuntimeAssetEnabled(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), ref, *input.Enabled)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *copilotRuntimeAssetHandler) ProbeRuntimeMCP(c *gin.Context) {
	if h.service == nil {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	var input struct {
		Scope string `json:"scope"`
	}
	if !bindRuntimeProfileJSON(c, &input) {
		return
	}
	item, err := h.service.ProbeRuntimeMCP(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("assetId"), c.Param("version"), input.Scope)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
