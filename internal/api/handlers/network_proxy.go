package handlers

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type NetworkProxyManagement interface {
	Create(context.Context, domainidentity.Principal, domain.InstanceInput) (domain.Instance, error)
	List(context.Context, domainidentity.Principal, string, string, int) ([]domain.Instance, error)
	Get(context.Context, domainidentity.Principal, string) (domain.Instance, error)
	UpdateConfiguration(context.Context, domainidentity.Principal, string, domain.ConfigurationInput) (domain.Instance, error)
	Traffic(context.Context, domainidentity.Principal, string, time.Time, time.Time) (domain.Traffic, error)
	Connections(context.Context, domainidentity.Principal, string) (domain.ConnectionsSnapshot, error)
	Close(context.Context, domainidentity.Principal, string, string) (domain.CloseCommand, error)
}

type NetworkProxyHandler struct{ service NetworkProxyManagement }

func NewNetworkProxyHandler(service NetworkProxyManagement) *NetworkProxyHandler {
	return &NetworkProxyHandler{service: service}
}

func (h *NetworkProxyHandler) Create(c *gin.Context) {
	var input sohaapi.NetworkProxyInstanceInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.service.Create(c.Request.Context(), principal(c), domain.InstanceInput{
		ID: input.ID, Name: input.Name, Engine: string(input.Engine), Host: input.Host,
	})
	respondItem(c, http.StatusCreated, item, err)
}

func (h *NetworkProxyHandler) List(c *gin.Context) {
	limit := 100
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(c, apperrors.ErrInvalidArgument)
			return
		}
		limit = parsed
	}
	items, err := h.service.List(c.Request.Context(), principal(c), c.Query("search"), c.Query("engine"), limit)
	respondItems(c, items, err)
}

func (h *NetworkProxyHandler) Get(c *gin.Context) {
	item, err := h.service.Get(c.Request.Context(), principal(c), c.Param("instanceID"))
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkProxyHandler) UpdateConfiguration(c *gin.Context) {
	var input sohaapi.NetworkProxyConfigurationInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.service.UpdateConfiguration(c.Request.Context(), principal(c), c.Param("instanceID"), domain.ConfigurationInput{
		ExpectedRevision: int64(input.ExpectedRevision), Enabled: input.Enabled, Content: input.Content,
	})
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkProxyHandler) Traffic(c *gin.Context) {
	var from, to time.Time
	var err error
	if raw := c.Query("from"); raw != "" {
		from, err = time.Parse(time.RFC3339, raw)
	}
	if err == nil {
		if raw := c.Query("to"); raw != "" {
			to, err = time.Parse(time.RFC3339, raw)
		}
	}
	if err != nil {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	result, err := h.service.Traffic(c.Request.Context(), principal(c), c.Param("instanceID"), from, to)
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	apiresponse.JSON(c, http.StatusOK, result)
}

func (h *NetworkProxyHandler) Connections(c *gin.Context) {
	result, err := h.service.Connections(c.Request.Context(), principal(c), c.Param("instanceID"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	apiresponse.JSON(c, http.StatusOK, result)
}

func (h *NetworkProxyHandler) Close(c *gin.Context) {
	result, err := h.service.Close(c.Request.Context(), principal(c), c.Param("instanceID"), c.Param("connectionID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusAccepted, result)
}
