package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainevent "github.com/opensoha/soha/internal/domain/event"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/runtimeobs"
)

const connectorEventSinkMaxBodyBytes = 1 << 20

func (h *clusterEventResourceHandler) ListClusterEvents(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	limit := parseLimit(c.Query("limit"), 20)
	items, err := h.service.ListClusterEvents(c.Request.Context(), principal, c.Param("clusterID"), namespace, limit)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) ListAuditLogs(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	filter, err := parsePlatformAuditFilter(c)
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	items, err := h.audit.ListAuthorized(c.Request.Context(), principal, filter)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PlatformHandler) AuditSummary(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	filter, err := parsePlatformAuditFilter(c)
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	item, err := h.audit.SummaryAuthorized(c.Request.Context(), principal, filter)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) ExportAuditLogs(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	filter, err := parsePlatformAuditFilter(c)
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	export, err := h.audit.ExportCSVAuthorized(c.Request.Context(), principal, filter)
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Content-Type", export.ContentType)
	c.Header("Content-Disposition", `attachment; filename="`+export.Filename+`"`)
	c.Header("X-Soha-Audit-Export-Count", fmt.Sprint(export.Count))
	c.Data(http.StatusOK, export.ContentType, export.Content)
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

type connectorEventSinkRequest struct {
	ConnectorID string                       `json:"connectorId"`
	Events      []domainevent.ConnectorEvent `json:"events"`
}

func (h *PlatformHandler) IngestConnectorEvents(c *gin.Context) {
	if h.events == nil {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	auth, err := h.authorizeConnectorEventSink(c)
	if err != nil {
		writeError(c, err)
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, connectorEventSinkMaxBodyBytes)
	var req connectorEventSinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid connector event sink payload")
		return
	}
	count, err := h.events.IngestConnectorEvents(c.Request.Context(), domainevent.ConnectorEventIngestInput{
		ConnectorID:   req.ConnectorID,
		Events:        req.Events,
		RequestPath:   c.FullPath(),
		RequestMethod: c.Request.Method,
		RequestID:     c.GetString("request_id"),
		SourceIP:      c.ClientIP(),
		ActorID:       auth.actorID,
		ActorName:     auth.actorName,
		ActorRoles:    auth.roles,
		ActorTeams:    auth.teams,
		AuthKind:      auth.kind,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusAccepted, gin.H{"accepted": count})
}

type connectorEventSinkAuth struct {
	kind      string
	actorID   string
	actorName string
	roles     []string
	teams     []string
}

func (h *PlatformHandler) authorizeConnectorEventSink(c *gin.Context) (connectorEventSinkAuth, error) {
	principal := apiMiddleware.PrincipalFromContext(c)
	accessCtx := apiMiddleware.AccessContextFromContext(c)
	if accessCtx.TokenKind == "service_account_token" && accessCtx.SubjectType == "service_account" && hasAnyRunnerPermission(principal.PermissionKeys, appaccess.PermAIGatewayInvoke) {
		return connectorEventSinkAuth{
			kind:      "service_account_token",
			actorID:   principal.UserID,
			actorName: principal.UserName,
			roles:     append([]string(nil), principal.Roles...),
			teams:     append([]string(nil), principal.Teams...),
		}, nil
	}
	if err := h.events.ValidateConnectorEventSinkToken(connectorEventSinkToken(c)); err != nil {
		return connectorEventSinkAuth{}, err
	}
	return connectorEventSinkAuth{kind: "static_token"}, nil
}

func connectorEventSinkToken(c *gin.Context) string {
	token := strings.TrimSpace(c.GetHeader("X-Soha-Connector-Event-Sink-Token"))
	if token == "" {
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		if len(header) >= len("Bearer ") && strings.EqualFold(header[:len("Bearer ")], "Bearer ") {
			token = strings.TrimSpace(header[len("Bearer "):])
		}
	}
	return token
}

func (h *PlatformHandler) ListOperationLogs(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	filter, err := parsePlatformOperationFilter(c)
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	items, err := h.operations.ListAuthorized(c.Request.Context(), principal, filter)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PlatformHandler) OperationSummary(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	filter, err := parsePlatformOperationFilter(c)
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	item, err := h.operations.SummaryAuthorized(c.Request.Context(), principal, filter)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) ExportOperationLogs(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	filter, err := parsePlatformOperationFilter(c)
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	export, err := h.operations.ExportCSVAuthorized(c.Request.Context(), principal, filter)
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Content-Type", export.ContentType)
	c.Header("Content-Disposition", `attachment; filename="`+export.Filename+`"`)
	c.Header("X-Soha-Operation-Export-Count", fmt.Sprint(export.Count))
	c.Data(http.StatusOK, export.ContentType, export.Content)
}

func parsePlatformAuditFilter(c *gin.Context) (domainaudit.Filter, error) {
	from, err := parseAIGatewayAuditTime(firstNonEmpty(c.Query("from"), c.Query("startTime"), c.Query("createdAtFrom")))
	if err != nil {
		return domainaudit.Filter{}, err
	}
	to, err := parseAIGatewayAuditTime(firstNonEmpty(c.Query("to"), c.Query("endTime"), c.Query("createdAtTo")))
	if err != nil {
		return domainaudit.Filter{}, err
	}
	return domainaudit.Filter{
		ActorID:           firstNonEmpty(c.Query("actorId"), c.Query("actor")),
		ActorName:         c.Query("actorName"),
		ClusterID:         c.Query("clusterId"),
		Namespace:         c.Query("namespace"),
		ResourceKind:      c.Query("resourceKind"),
		ResourceName:      c.Query("resourceName"),
		Action:            c.Query("action"),
		Result:            c.Query("result"),
		RequestID:         firstNonEmpty(c.Query("requestId"), c.Query("requestID")),
		RequestPath:       c.Query("requestPath"),
		RequestMethod:     firstNonEmpty(c.Query("requestMethod"), c.Query("method")),
		SourceIP:          firstNonEmpty(c.Query("sourceIp"), c.Query("sourceIP"), c.Query("ip")),
		ApprovalRequestID: firstNonEmpty(c.Query("approvalRequestId"), c.Query("approvalId"), c.Query("approvalID")),
		AgentRunID:        firstNonEmpty(c.Query("agentRunId"), c.Query("agentRunID"), c.Query("runId")),
		RootCauseRunID:    firstNonEmpty(c.Query("rootCauseRunId"), c.Query("rootCauseRunID")),
		MetadataKey:       c.Query("metadataKey"),
		MetadataValue:     c.Query("metadataValue"),
		From:              from,
		To:                to,
		Limit:             parseLimit(c.Query("limit"), 50),
	}, nil
}

func parsePlatformOperationFilter(c *gin.Context) (domainoperation.Filter, error) {
	from, err := parseAIGatewayAuditTime(firstNonEmpty(c.Query("from"), c.Query("startTime"), c.Query("createdAtFrom")))
	if err != nil {
		return domainoperation.Filter{}, err
	}
	to, err := parseAIGatewayAuditTime(firstNonEmpty(c.Query("to"), c.Query("endTime"), c.Query("createdAtTo")))
	if err != nil {
		return domainoperation.Filter{}, err
	}
	return domainoperation.Filter{
		OperationType:     firstNonEmpty(c.Query("operationType"), c.Query("type")),
		ActorID:           firstNonEmpty(c.Query("actorId"), c.Query("actor")),
		ClusterID:         c.Query("clusterId"),
		Namespace:         c.Query("namespace"),
		ResourceKind:      c.Query("resourceKind"),
		ResourceName:      c.Query("resourceName"),
		Result:            c.Query("result"),
		RequestID:         firstNonEmpty(c.Query("requestId"), c.Query("requestID")),
		RequestPath:       c.Query("requestPath"),
		RequestMethod:     firstNonEmpty(c.Query("requestMethod"), c.Query("method")),
		SourceIP:          firstNonEmpty(c.Query("sourceIp"), c.Query("sourceIP"), c.Query("ip")),
		ApprovalRequestID: firstNonEmpty(c.Query("approvalRequestId"), c.Query("approvalId"), c.Query("approvalID")),
		AgentRunID:        firstNonEmpty(c.Query("agentRunId"), c.Query("agentRunID"), c.Query("runId")),
		RootCauseRunID:    firstNonEmpty(c.Query("rootCauseRunId"), c.Query("rootCauseRunID")),
		MetadataKey:       c.Query("metadataKey"),
		MetadataValue:     c.Query("metadataValue"),
		From:              from,
		To:                to,
		Limit:             parseLimit(c.Query("limit"), 50),
	}, nil
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
