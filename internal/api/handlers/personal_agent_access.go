package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/api/dto"
	middleware "github.com/opensoha/soha/internal/api/middleware"
	response "github.com/opensoha/soha/internal/api/response"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type CopilotPersonalAgentService interface {
	GetPersonalAgentAccess(context.Context, identity.Principal) (domain.PersonalAgentAccess, error)
	RequestPersonalAgentAccess(context.Context, identity.Principal) (domain.PersonalAgentAccess, error)
	ListPersonalAgentAccessRequests(context.Context, identity.Principal) ([]domain.PersonalAgentAccess, error)
	DecidePersonalAgentAccess(context.Context, identity.Principal, string, string, int64) (domain.PersonalAgentAccess, error)
	ListPersonalSessions(context.Context, identity.Principal) ([]domain.Session, error)
	CreatePersonalSession(context.Context, identity.Principal, string, string, string, map[string]any, map[string]any, string, []string, string) (domain.Session, error)
}

type copilotPersonalAgentHandler struct{ service CopilotPersonalAgentService }

func (h *copilotPersonalAgentHandler) available(c *gin.Context) bool {
	if h.service == nil {
		writeError(c, apperrors.ErrServiceUnavailable)
		return false
	}
	return true
}

func (h *copilotPersonalAgentHandler) GetPersonalAgentAccess(c *gin.Context) {
	if !h.available(c) {
		return
	}
	item, err := h.service.GetPersonalAgentAccess(c.Request.Context(), middleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, http.StatusOK, item)
}
func (h *copilotPersonalAgentHandler) RequestPersonalAgentAccess(c *gin.Context) {
	if !h.available(c) {
		return
	}
	item, err := h.service.RequestPersonalAgentAccess(c.Request.Context(), middleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, http.StatusOK, item)
}
func (h *copilotPersonalAgentHandler) ListPersonalAgentAccessRequests(c *gin.Context) {
	if !h.available(c) {
		return
	}
	items, err := h.service.ListPersonalAgentAccessRequests(c.Request.Context(), middleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Items(c, http.StatusOK, items)
}
func (h *copilotPersonalAgentHandler) DecidePersonalAgentAccess(c *gin.Context) {
	if !h.available(c) {
		return
	}
	var req struct {
		Status  string `json:"status"`
		Version int64  `json:"version"`
	}
	if err := discoveryJSON(c, &req); err != nil {
		writeError(c, err)
		return
	}
	item, err := h.service.DecidePersonalAgentAccess(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("userID"), req.Status, req.Version)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, http.StatusOK, item)
}
func (h *copilotPersonalAgentHandler) ListPersonalSessions(c *gin.Context) {
	if !h.available(c) {
		return
	}
	items, err := h.service.ListPersonalSessions(c.Request.Context(), middleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Items(c, http.StatusOK, items)
}
func (h *copilotPersonalAgentHandler) CreatePersonalSession(c *gin.Context) {
	if !h.available(c) {
		return
	}
	var req dto.CreateCopilotSessionRequest
	if err := discoveryJSON(c, &req); err != nil {
		writeError(c, err)
		return
	}
	item, err := h.service.CreatePersonalSession(c.Request.Context(), middleware.PrincipalFromContext(c), req.Title, req.Mode, req.AgentProviderID, req.Scope, req.PinnedContext, req.Source, req.Tags, localeFromRequest(c.GetHeader("Accept-Language")))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, http.StatusOK, item)
}
