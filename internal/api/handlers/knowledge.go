package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

type KnowledgeService interface {
	ListBases(context.Context, domainidentity.Principal) ([]domainknowledge.KnowledgeBase, error)
	GetBase(context.Context, domainidentity.Principal, string) (domainknowledge.KnowledgeBase, error)
	CreateBase(context.Context, domainidentity.Principal, domainknowledge.BaseInput) (domainknowledge.KnowledgeBase, error)
	UpdateBase(context.Context, domainidentity.Principal, string, domainknowledge.BaseInput) (domainknowledge.KnowledgeBase, error)
	DeleteBase(context.Context, domainidentity.Principal, string) error
	ListSources(context.Context, domainidentity.Principal, string) ([]domainknowledge.Source, error)
	CreateSource(context.Context, domainidentity.Principal, string, domainknowledge.SourceInput) (domainknowledge.Source, error)
	SyncSource(context.Context, domainidentity.Principal, string, string) (domainknowledge.SyncRun, error)
	ListDocuments(context.Context, domainidentity.Principal, string, int) ([]domainknowledge.Document, error)
	ListSyncRuns(context.Context, domainidentity.Principal, string, int) ([]domainknowledge.SyncRun, error)
	ListIndexRevisions(context.Context, domainidentity.Principal, string, int) ([]domainknowledge.IndexRevision, error)
	Search(context.Context, domainidentity.Principal, domainknowledge.SearchRequest) (domainknowledge.SearchResult, error)
}

type ContextInspector interface {
	Inspect(context.Context, domainidentity.Principal, domaincopilot.ContextBuildInput) (domaincopilot.ContextInspection, error)
}

type KnowledgeProductionService interface {
	ListConnectors(context.Context, domainidentity.Principal, int) ([]domainknowledge.ConnectorDefinition, error)
	CreateConnector(context.Context, domainidentity.Principal, domainknowledge.ConnectorInput) (domainknowledge.ConnectorDefinition, error)
	ValidateConnector(context.Context, domainidentity.Principal, string) (domainknowledge.ConnectorValidationResult, error)
	CreateIngestionJob(context.Context, domainidentity.Principal, string, string) (domainknowledge.IngestionJob, error)
	GetIngestionJob(context.Context, domainidentity.Principal, string) (domainknowledge.IngestionJob, error)
	CancelIngestionJob(context.Context, domainidentity.Principal, string) (domainknowledge.IngestionJob, error)
	RetryIngestionJob(context.Context, domainidentity.Principal, string) (domainknowledge.IngestionJob, error)
}

type KnowledgeHandler struct {
	knowledge  KnowledgeService
	production KnowledgeProductionService
	context    ContextInspector
}

func NewKnowledgeHandler(knowledge KnowledgeService, contextInspector ContextInspector) *KnowledgeHandler {
	production, _ := knowledge.(KnowledgeProductionService)
	return &KnowledgeHandler{knowledge: knowledge, production: production, context: contextInspector}
}

// RegisterKnowledgeRoutes is the single integration hook for the protected /api/v1 group.
func RegisterKnowledgeRoutes(group gin.IRoutes, handler *KnowledgeHandler) {
	group.GET("/ai/knowledge-bases", handler.listBases)
	group.POST("/ai/knowledge-bases", handler.createBase)
	group.GET("/ai/knowledge-bases/:baseID", handler.getBase)
	group.PATCH("/ai/knowledge-bases/:baseID", handler.updateBase)
	group.DELETE("/ai/knowledge-bases/:baseID", handler.deleteBase)
	group.GET("/ai/knowledge-bases/:baseID/sources", handler.listSources)
	group.POST("/ai/knowledge-bases/:baseID/sources", handler.createSource)
	group.POST("/ai/knowledge-bases/:baseID/sources/:sourceID/sync", handler.syncSource)
	group.GET("/ai/knowledge-bases/:baseID/documents", handler.listDocuments)
	group.GET("/ai/knowledge-bases/:baseID/sync-runs", handler.listSyncRuns)
	group.GET("/ai/knowledge-bases/:baseID/index-revisions", handler.listIndexRevisions)
	group.POST("/ai/knowledge/search", handler.search)
	group.GET("/ai/knowledge/connectors", handler.listConnectors)
	group.POST("/ai/knowledge/connectors", handler.createConnector)
	group.POST("/ai/knowledge/connectors/:connectorID/validate", handler.validateConnector)
	group.POST("/ai/knowledge-bases/:baseID/sync-jobs", handler.createIngestionJob)
	group.GET("/ai/knowledge/sync-jobs/:jobID", handler.getIngestionJob)
	group.POST("/ai/knowledge/sync-jobs/:jobID/cancel", handler.cancelIngestionJob)
	group.POST("/ai/knowledge/sync-jobs/:jobID/retry", handler.retryIngestionJob)
	group.POST("/ai/context/inspect", handler.inspectContext)
}

func (h *KnowledgeHandler) listConnectors(c *gin.Context) {
	if !h.requireProduction(c) {
		return
	}
	items, err := h.production.ListConnectors(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), knowledgeQueryInt(c, "limit", 50))
	if err != nil {
		writeKnowledgeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *KnowledgeHandler) createConnector(c *gin.Context) {
	if !h.requireProduction(c) {
		return
	}
	var input domainknowledge.ConnectorInput
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid connector payload")
		return
	}
	item, err := h.production.CreateConnector(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input)
	if err != nil {
		writeKnowledgeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *KnowledgeHandler) validateConnector(c *gin.Context) {
	if !h.requireProduction(c) {
		return
	}
	item, err := h.production.ValidateConnector(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("connectorID"))
	if err != nil {
		writeKnowledgeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *KnowledgeHandler) createIngestionJob(c *gin.Context) {
	if !h.requireProduction(c) {
		return
	}
	var input struct {
		SourceID string `json:"sourceId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "sourceId is required")
		return
	}
	item, err := h.production.CreateIngestionJob(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("baseID"), input.SourceID)
	if err != nil {
		writeKnowledgeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}

func (h *KnowledgeHandler) getIngestionJob(c *gin.Context) {
	h.handleIngestionAction(c, func(ctx context.Context, principal domainidentity.Principal, jobID string) (domainknowledge.IngestionJob, error) {
		return h.production.GetIngestionJob(ctx, principal, jobID)
	}, http.StatusOK)
}

func (h *KnowledgeHandler) cancelIngestionJob(c *gin.Context) {
	h.handleIngestionAction(c, func(ctx context.Context, principal domainidentity.Principal, jobID string) (domainknowledge.IngestionJob, error) {
		return h.production.CancelIngestionJob(ctx, principal, jobID)
	}, http.StatusAccepted)
}

func (h *KnowledgeHandler) retryIngestionJob(c *gin.Context) {
	h.handleIngestionAction(c, func(ctx context.Context, principal domainidentity.Principal, jobID string) (domainknowledge.IngestionJob, error) {
		return h.production.RetryIngestionJob(ctx, principal, jobID)
	}, http.StatusAccepted)
}

func (h *KnowledgeHandler) handleIngestionAction(
	c *gin.Context,
	action func(context.Context, domainidentity.Principal, string) (domainknowledge.IngestionJob, error),
	status int,
) {
	if !h.requireProduction(c) {
		return
	}
	item, err := action(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("jobID"))
	if err != nil {
		writeKnowledgeError(c, err)
		return
	}
	apiresponse.Item(c, status, item)
}

func (h *KnowledgeHandler) requireProduction(c *gin.Context) bool {
	if h.production != nil {
		return true
	}
	apiresponse.Error(c, http.StatusServiceUnavailable, "service_unavailable", "production knowledge service unavailable")
	return false
}

func (h *KnowledgeHandler) listBases(c *gin.Context) {
	items, err := h.knowledge.ListBases(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
	if err != nil {
		writeKnowledgeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *KnowledgeHandler) getBase(c *gin.Context) {
	item, err := h.knowledge.GetBase(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("baseID"))
	if err != nil {
		writeKnowledgeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *KnowledgeHandler) createBase(c *gin.Context) {
	var input domainknowledge.BaseInput
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid knowledge base payload")
		return
	}
	item, err := h.knowledge.CreateBase(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input)
	if err != nil {
		writeKnowledgeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}
func (h *KnowledgeHandler) updateBase(c *gin.Context) {
	var input domainknowledge.BaseInput
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid knowledge base payload")
		return
	}
	item, err := h.knowledge.UpdateBase(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("baseID"), input)
	if err != nil {
		writeKnowledgeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *KnowledgeHandler) deleteBase(c *gin.Context) {
	if err := h.knowledge.DeleteBase(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("baseID")); err != nil {
		writeKnowledgeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}
func (h *KnowledgeHandler) listSources(c *gin.Context) {
	items, err := h.knowledge.ListSources(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("baseID"))
	if err != nil {
		writeKnowledgeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *KnowledgeHandler) createSource(c *gin.Context) {
	var input domainknowledge.SourceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid source payload")
		return
	}
	item, err := h.knowledge.CreateSource(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("baseID"), input)
	if err != nil {
		writeKnowledgeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}
func (h *KnowledgeHandler) syncSource(c *gin.Context) {
	item, err := h.knowledge.SyncSource(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("baseID"), c.Param("sourceID"))
	if err != nil {
		writeKnowledgeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}
func (h *KnowledgeHandler) listDocuments(c *gin.Context) {
	items, err := h.knowledge.ListDocuments(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("baseID"), knowledgeQueryInt(c, "limit", 50))
	if err != nil {
		writeKnowledgeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *KnowledgeHandler) listSyncRuns(c *gin.Context) {
	items, err := h.knowledge.ListSyncRuns(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("baseID"), knowledgeQueryInt(c, "limit", 50))
	if err != nil {
		writeKnowledgeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *KnowledgeHandler) listIndexRevisions(c *gin.Context) {
	items, err := h.knowledge.ListIndexRevisions(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("baseID"), knowledgeQueryInt(c, "limit", 50))
	if err != nil {
		writeKnowledgeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *KnowledgeHandler) search(c *gin.Context) {
	var input domainknowledge.SearchRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid knowledge search payload")
		return
	}
	item, err := h.knowledge.Search(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input)
	if err != nil {
		writeKnowledgeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *KnowledgeHandler) inspectContext(c *gin.Context) {
	if h.context == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "service_unavailable", "context inspector unavailable")
		return
	}
	var input domaincopilot.ContextBuildInput
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid context inspection payload")
		return
	}
	item, err := h.context.Inspect(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input)
	if err != nil {
		writeKnowledgeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func knowledgeQueryInt(c *gin.Context, key string, fallback int) int {
	value, err := strconv.Atoi(c.Query(key))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func writeKnowledgeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domainknowledge.ErrAccessDenied):
		apiresponse.Error(c, http.StatusForbidden, "forbidden", "knowledge access denied")
	case errors.Is(err, domainknowledge.ErrBaseNotFound), errors.Is(err, domainknowledge.ErrSourceNotFound):
		apiresponse.Error(c, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, domainknowledge.ErrIngestionNotFound):
		apiresponse.Error(c, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, domainknowledge.ErrIngestionConflict):
		apiresponse.Error(c, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, domainknowledge.ErrInvalidInput), errors.Is(err, domainknowledge.ErrRetrievalExhausted):
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", err.Error())
	case errors.Is(err, domainknowledge.ErrSourceUnavailable):
		apiresponse.Error(c, http.StatusServiceUnavailable, "service_unavailable", err.Error())
	default:
		writeError(c, err)
	}
}
