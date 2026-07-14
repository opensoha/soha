package handlers

import (
	"errors"
	"maps"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appaiproduction "github.com/opensoha/soha/internal/application/aiproduction"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type AIProductionHandler struct {
	service        *appaiproduction.Service
	auth           EvaluationAuthorizer
	features       map[string]bool
	strictFeatures bool
}

func NewAIProductionHandler(service *appaiproduction.Service, auth EvaluationAuthorizer, featureSets ...map[string]bool) *AIProductionHandler {
	h := &AIProductionHandler{service: service, auth: auth}
	if len(featureSets) > 0 {
		h.features = maps.Clone(featureSets[0])
		h.strictFeatures = true
	}
	return h
}

func (h *AIProductionHandler) featureEnabled(key string) bool {
	return h != nil && (!h.strictFeatures || h.features[key])
}

func RegisterAIProductionRoutes(group gin.IRoutes, h *AIProductionHandler) {
	if h.featureEnabled("agent.fleet_rollout") {
		group.GET("/ai/agent-providers/rollouts", h.listRollouts)
		group.POST("/ai/agent-providers/rollouts", h.createRollout)
		group.POST("/ai/agent-providers/rollouts/:rolloutID/:action", h.transitionRollout)
	}
	if h.featureEnabled("agent.conformance_suite") {
		group.GET("/ai/agent-providers/conformance-runs", h.listConformance)
		group.POST("/ai/agent-providers/conformance-runs", h.createConformance)
	}
	if h.featureEnabled("agent.environment_management") {
		group.GET("/ai/environments/templates", h.listEnvironmentTemplates)
		group.POST("/ai/environments/templates", h.putEnvironmentTemplate)
		group.GET("/ai/environments/leases", h.listEnvironmentLeases)
		group.POST("/ai/environments/leases/:leaseID/release", h.releaseEnvironmentLease)
		group.POST("/ai/environments/gc", h.gcEnvironments)
	}
	if h.featureEnabled("ai.production_operations") {
		group.GET("/ai/operations", h.listOperations)
		group.POST("/ai/operations", h.startOperation)
		group.GET("/ai/operations/runbook-evidence", h.listRunbookEvidence)
		group.POST("/ai/knowledge-bases/:baseID/rebuild", h.rebuildKnowledgeBase)
	}
}
func (h *AIProductionHandler) allowed(c *gin.Context, permissions ...string) bool {
	if h == nil || h.service == nil || h.auth == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "service_unavailable", "AI production control plane unavailable")
		return false
	}
	err := h.auth.AuthorizeAny(c, apiMiddleware.PrincipalFromContext(c), permissions...)
	if err == nil {
		return true
	}
	if errors.Is(err, apperrors.ErrAccessDenied) {
		apiresponse.Error(c, http.StatusForbidden, "forbidden", "AI production access denied")
	} else {
		apiresponse.Error(c, http.StatusServiceUnavailable, "service_unavailable", "AI production authorization unavailable")
	}
	return false
}
func (h *AIProductionHandler) listRollouts(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIAgentFleetView, appaccess.PermAIAgentFleetManage) {
		return
	}
	items, err := h.service.ListRollouts(c)
	writeProductionItems(c, items, err)
}
func (h *AIProductionHandler) createRollout(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIAgentFleetManage) {
		return
	}
	var input struct {
		ID               string            `json:"id"`
		Name             string            `json:"name"`
		DesiredRevision  uint64            `json:"desiredRevision"`
		PreviousRevision uint64            `json:"previousRevision"`
		Environments     []string          `json:"environments"`
		Platforms        []string          `json:"platforms"`
		Architectures    []string          `json:"architectures"`
		Labels           map[string]string `json:"labels"`
		CanaryPercent    int               `json:"canaryPercent"`
	}
	if c.ShouldBindJSON(&input) != nil {
		writeProductionError(c, errors.New("invalid provider rollout"))
		return
	}
	item, err := h.service.CreateRollout(c, appaiproduction.ProviderRollout{ID: input.ID, Name: input.Name, DesiredRevision: input.DesiredRevision, PreviousRevision: input.PreviousRevision, CanaryPercent: input.CanaryPercent, Target: appaiproduction.FleetTarget{Environments: input.Environments, Platforms: input.Platforms, Architectures: input.Architectures, Labels: input.Labels}})
	writeProductionItem(c, http.StatusAccepted, item, err)
}
func (h *AIProductionHandler) transitionRollout(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIAgentFleetManage) {
		return
	}
	action := c.Param("action")
	if action != "pause" && action != "resume" && action != "rollback" {
		writeProductionError(c, errors.New("invalid rollout action"))
		return
	}
	item, err := h.service.TransitionRollout(c, c.Param("rolloutID"), action)
	writeProductionItem(c, http.StatusOK, item, err)
}
func (h *AIProductionHandler) listConformance(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIAgentFleetView, appaccess.PermAIAgentFleetManage) {
		return
	}
	items, err := h.service.ListConformanceRuns(c)
	writeProductionItems(c, items, err)
}
func (h *AIProductionHandler) createConformance(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIAgentFleetManage) {
		return
	}
	var input appaiproduction.ConformanceRun
	if c.ShouldBindJSON(&input) != nil {
		writeProductionError(c, errors.New("invalid conformance run"))
		return
	}
	item, err := h.service.CreateConformanceRun(c, input)
	writeProductionItem(c, http.StatusAccepted, item, err)
}
func (h *AIProductionHandler) listEnvironmentTemplates(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIEnvironmentsView, appaccess.PermAIEnvironmentsManage) {
		return
	}
	items, err := h.service.ListEnvironmentTemplates(c)
	writeProductionItems(c, items, err)
}
func (h *AIProductionHandler) putEnvironmentTemplate(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIEnvironmentsManage) {
		return
	}
	var input appaiproduction.EnvironmentTemplate
	if c.ShouldBindJSON(&input) != nil {
		writeProductionError(c, errors.New("invalid environment template"))
		return
	}
	item, err := h.service.PutEnvironmentTemplate(c, input)
	writeProductionItem(c, http.StatusCreated, item, err)
}
func (h *AIProductionHandler) listEnvironmentLeases(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIEnvironmentsView, appaccess.PermAIEnvironmentsManage) {
		return
	}
	items, err := h.service.ListEnvironmentLeases(c)
	writeProductionItems(c, items, err)
}
func (h *AIProductionHandler) releaseEnvironmentLease(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIEnvironmentsManage) {
		return
	}
	item, err := h.service.ReleaseEnvironmentLease(c, c.Param("leaseID"))
	writeProductionItem(c, http.StatusOK, item, err)
}
func (h *AIProductionHandler) gcEnvironments(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIEnvironmentsManage) {
		return
	}
	item, err := h.service.GCEnvironmentLeases(c)
	writeProductionItem(c, http.StatusAccepted, item, err)
}
func (h *AIProductionHandler) listOperations(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIOperationsView, appaccess.PermAIOperationsManage) {
		return
	}
	items, err := h.service.ListOperations(c)
	writeProductionItems(c, items, err)
}
func (h *AIProductionHandler) startOperation(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIOperationsManage) {
		return
	}
	var input appaiproduction.Operation
	if c.ShouldBindJSON(&input) != nil {
		writeProductionError(c, errors.New("invalid AI production operation"))
		return
	}
	item, err := h.service.StartOperation(c, input)
	writeProductionItem(c, http.StatusAccepted, item, err)
}
func (h *AIProductionHandler) listRunbookEvidence(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIOperationsView, appaccess.PermAIOperationsManage) {
		return
	}
	items, err := h.service.ListRunbookEvidence(c)
	writeProductionItems(c, items, err)
}

func (h *AIProductionHandler) rebuildKnowledgeBase(c *gin.Context) {
	if !h.allowed(c, appaccess.PermAIKnowledgeRebuild) {
		return
	}
	baseID := strings.TrimSpace(c.Param("baseID"))
	if baseID == "" {
		writeProductionError(c, errors.New("knowledge base id is required"))
		return
	}
	item, err := h.service.StartOperation(c, appaiproduction.Operation{
		Kind: "index_rebuild", TargetRef: "knowledge-base:" + baseID, RunbookID: "rag-index-rebuild",
	})
	writeProductionItem(c, http.StatusAccepted, item, err)
}
func writeProductionItem(c *gin.Context, status int, item any, err error) {
	if err != nil {
		writeProductionError(c, err)
		return
	}
	apiresponse.Item(c, status, item)
}
func writeProductionItems(c *gin.Context, items any, err error) {
	if err != nil {
		writeProductionError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func writeProductionError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, appaiproduction.ErrNotFound):
		apiresponse.Error(c, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, appaiproduction.ErrConflict):
		apiresponse.Error(c, http.StatusConflict, "conflict", err.Error())
	case strings.Contains(err.Error(), "invalid"), strings.Contains(err.Error(), "required"), strings.Contains(err.Error(), "exceeds"):
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", err.Error())
	default:
		apiresponse.Error(c, http.StatusInternalServerError, "internal_error", "AI production operation failed")
	}
}
