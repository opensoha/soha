package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/soha/soha/internal/api/middleware"
	apiresponse "github.com/soha/soha/internal/api/response"
	domainaudit "github.com/soha/soha/internal/domain/audit"
	domainoperation "github.com/soha/soha/internal/domain/operation"
	"github.com/soha/soha/internal/platform/runtimeobs"
)

func (h *PlatformHandler) ListClusterEvents(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	limit := parseLimit(c.Query("limit"), 20)
	items, err := h.resources.ListClusterEvents(c.Request.Context(), principal, c.Param("clusterID"), namespace, limit)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) ListAuditLogs(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	limit := parseLimit(c.Query("limit"), 50)
	items, err := h.audit.ListAuthorized(c.Request.Context(), principal, domainaudit.Filter{
		Action: c.Query("action"),
		Result: c.Query("result"),
		Limit:  limit,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) ListEvents(c *gin.Context) {
	limit := parseLimit(c.Query("limit"), 50)
	items, err := h.events.List(c.Request.Context(), limit)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) GetEvent(c *gin.Context) {
	item, err := h.events.Get(c.Request.Context(), c.Param("eventID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) ListOperationLogs(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	limit := parseLimit(c.Query("limit"), 50)
	items, err := h.operations.ListAuthorized(c.Request.Context(), principal, domainoperation.Filter{
		OperationType: c.Query("operationType"),
		Result:        c.Query("result"),
		Limit:         limit,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) ListMCPCapabilities(c *gin.Context) {
	items, err := h.integration.ListCapabilities(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

type ReadinessProbe interface {
	Ping(context.Context) error
}

type RuntimeMetricsProvider interface {
	Snapshot() runtimeobs.Snapshot
}

type SystemHandler struct {
	postgres ReadinessProbe
	metrics  RuntimeMetricsProvider
}
