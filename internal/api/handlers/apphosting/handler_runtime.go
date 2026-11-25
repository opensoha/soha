package apphosting

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/api/middleware"
	"github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type RuntimeControlsService interface {
	GetRuntimeConfig(context.Context, identity.Principal, string) (sohaapi.HostedAppRuntimeConfig, error)
	UpdateRuntimeConfig(context.Context, identity.Principal, string, sohaapi.UpdateHostedAppRuntimeConfigRequest) (sohaapi.HostedAppRuntimeConfig, error)
	GetRuntimeUsage(context.Context, identity.Principal, string) (sohaapi.HostedAppRuntimeUsage, error)
	GetRevisionLogs(context.Context, identity.Principal, string, string) (sohaapi.HostedAppRevisionLogs, error)
	GetVisitorAttributes(context.Context, identity.Principal, string) (sohaapi.HostedAppVisitorAttributes, error)
	RequestVisitorAttributes(context.Context, identity.Principal, string, sohaapi.RequestHostedAppVisitorAttributesRequest) (sohaapi.HostedAppVisitorAttributes, error)
	DecideVisitorAttributes(context.Context, identity.Principal, string, sohaapi.DecideHostedAppVisitorAttributesRequest) (sohaapi.HostedAppVisitorAttributes, error)
}

func (h *Handler) runtimeReady(c *gin.Context) bool {
	c.Header("Cache-Control", "private, no-store")
	if h.runtimeControls == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return false
	}
	return true
}

func (h *Handler) GetRuntimeConfig(c *gin.Context) {
	if !h.runtimeReady(c) {
		return
	}
	out, err := h.runtimeControls.GetRuntimeConfig(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("appId"))
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) UpdateRuntimeConfig(c *gin.Context) {
	var req sohaapi.UpdateHostedAppRuntimeConfigRequest
	if !h.runtimeReady(c) || !decodeStrictJSON(c, &req, false) {
		return
	}
	out, err := h.runtimeControls.UpdateRuntimeConfig(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("appId"), req)
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) GetRuntimeUsage(c *gin.Context) {
	if !h.runtimeReady(c) {
		return
	}
	out, err := h.runtimeControls.GetRuntimeUsage(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("appId"))
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) GetRevisionLogs(c *gin.Context) {
	if !h.runtimeReady(c) {
		return
	}
	out, err := h.runtimeControls.GetRevisionLogs(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("appId"), c.Param("revisionId"))
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) GetVisitorAttributes(c *gin.Context) {
	if !h.runtimeReady(c) {
		return
	}
	out, err := h.runtimeControls.GetVisitorAttributes(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("appId"))
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) RequestVisitorAttributes(c *gin.Context) {
	var req sohaapi.RequestHostedAppVisitorAttributesRequest
	if !h.runtimeReady(c) || !decodeStrictJSON(c, &req, false) {
		return
	}
	out, err := h.runtimeControls.RequestVisitorAttributes(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("appId"), req)
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) DecideVisitorAttributes(c *gin.Context) {
	var req sohaapi.DecideHostedAppVisitorAttributesRequest
	if !h.runtimeReady(c) || !decodeStrictJSON(c, &req, false) {
		return
	}
	out, err := h.runtimeControls.DecideVisitorAttributes(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("appId"), req)
	respondItem(c, http.StatusOK, out, err)
}
