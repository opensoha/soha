package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	appagentharness "github.com/opensoha/soha/internal/application/agentharness"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/keyring"
)

type AgentProviderControlPlane interface {
	Catalog(context.Context, domainidentity.Principal) (appagentharness.ProviderCatalog, error)
	RegistrySnapshot(string) (appagentharness.ProviderRegistrySnapshot, error)
	Acknowledge(appagentharness.RegistryAcknowledgement) (appagentharness.RegistryAcknowledgement, error)
	RuntimeStatus(context.Context, domainidentity.Principal) (appagentharness.ProviderRuntimeStatus, error)
}

type AgentProviderHandler struct {
	service    AgentProviderControlPlane
	runnerKeys keyring.Ring
}

func NewAgentProviderHandler(service AgentProviderControlPlane, runnerKeys keyring.Ring) *AgentProviderHandler {
	return &AgentProviderHandler{service: service, runnerKeys: runnerKeys}
}

func RegisterProtectedAgentProviderRoutes(group gin.IRoutes, handler *AgentProviderHandler) {
	group.GET("/ai/agent-providers/catalog", handler.catalog)
	group.GET("/ai/agent-providers/runtime-status", handler.runtimeStatus)
}

func RegisterRunnerAgentProviderRoutes(group gin.IRoutes, handler *AgentProviderHandler) {
	group.GET("/ai/agent-providers/registry-snapshot", handler.registrySnapshot)
	group.POST("/ai/agent-providers/registry-acks", handler.registryAck)
}

func (h *AgentProviderHandler) catalog(c *gin.Context) {
	item, err := h.service.Catalog(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AgentProviderHandler) registrySnapshot(c *gin.Context) {
	if !authorizeAIAgentRunnerKeys(c, h.runnerKeys) {
		apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "invalid ai agent runner token")
		return
	}
	item, err := h.service.RegistrySnapshot(strings.TrimSpace(c.Query("runnerId")))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AgentProviderHandler) registryAck(c *gin.Context) {
	if !authorizeAIAgentRunnerKeys(c, h.runnerKeys) {
		apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "invalid ai agent runner token")
		return
	}
	var input appagentharness.RegistryAcknowledgement
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid agent provider registry acknowledgement")
		return
	}
	item, err := h.service.Acknowledge(input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}

func (h *AgentProviderHandler) runtimeStatus(c *gin.Context) {
	item, err := h.service.RuntimeStatus(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
