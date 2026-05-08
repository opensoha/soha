package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/kubecrux/kubecrux/internal/api/dto"
	apiMiddleware "github.com/kubecrux/kubecrux/internal/api/middleware"
	apiresponse "github.com/kubecrux/kubecrux/internal/api/response"
	domaindelivery "github.com/kubecrux/kubecrux/internal/domain/delivery"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
)

type DeliveryService interface {
	GetApplicationDetail(context.Context, domainidentity.Principal, string) (domaindelivery.ApplicationDetail, error)
	GetApplicationEnvironmentDetail(context.Context, domainidentity.Principal, string) (domaindelivery.ApplicationEnvironmentDetail, error)
	ListReleaseBoard(context.Context, domainidentity.Principal) ([]domaindelivery.ReleaseBoardEntry, error)
	ListTargetCandidates(context.Context, domainidentity.Principal, string, string, string) ([]domaindelivery.TargetCandidate, error)
	ListReleaseBundles(context.Context, domainidentity.Principal, domaindelivery.ReleaseBundleFilter) ([]domaindelivery.ReleaseBundle, error)
	GetReleaseBundle(context.Context, domainidentity.Principal, string) (domaindelivery.ReleaseBundle, error)
	ListExecutionTasks(context.Context, domainidentity.Principal, domaindelivery.ExecutionTaskFilter) ([]domaindelivery.ExecutionTask, error)
	GetExecutionTask(context.Context, domainidentity.Principal, string) (domaindelivery.ExecutionTask, error)
	ListExecutionLogs(context.Context, domainidentity.Principal, string, int) ([]domaindelivery.ExecutionLog, error)
	ListExecutionArtifacts(context.Context, domainidentity.Principal, string) ([]domaindelivery.ExecutionArtifact, error)
	ListReleaseBundleArtifacts(context.Context, domainidentity.Principal, string) ([]domaindelivery.ExecutionArtifact, error)
	ListApprovalPolicies(context.Context, domainidentity.Principal) ([]domaindelivery.ApprovalPolicy, error)
	CreateApprovalPolicy(context.Context, domainidentity.Principal, domaindelivery.ApprovalPolicyInput) (domaindelivery.ApprovalPolicy, error)
	UpdateApprovalPolicy(context.Context, domainidentity.Principal, string, domaindelivery.ApprovalPolicyInput) (domaindelivery.ApprovalPolicy, error)
	DeleteApprovalPolicy(context.Context, domainidentity.Principal, string) error
	CancelExecutionTask(context.Context, domainidentity.Principal, string, domaindelivery.ExecutionTaskActionInput) (domaindelivery.ExecutionTask, error)
	RetryExecutionTask(context.Context, domainidentity.Principal, string, domaindelivery.ExecutionTaskActionInput) (domaindelivery.ExecutionTask, error)
	GetExecutionTaskForRunner(context.Context, string) (domaindelivery.ExecutionTask, error)
	RecordCallback(context.Context, domaindelivery.ExecutionCallbackInput) (domaindelivery.ExecutionTask, error)
	ClaimExecutionTask(context.Context, []string, string, string) (domaindelivery.ExecutionTask, error)
}

type DeliveryHandler struct {
	service     DeliveryService
	runnerToken string
}

func NewDeliveryHandler(service DeliveryService, runnerToken string) *DeliveryHandler {
	return &DeliveryHandler{service: service, runnerToken: runnerToken}
}

func (h *DeliveryHandler) GetApplicationDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetApplicationDetail(c.Request.Context(), principal, c.Param("applicationID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) GetApplicationEnvironmentDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetApplicationEnvironmentDetail(c.Request.Context(), principal, c.Param("applicationEnvironmentID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) ListReleaseBoard(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListReleaseBoard(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *DeliveryHandler) ListTargetCandidates(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListTargetCandidates(c.Request.Context(), principal, c.Query("clusterId"), c.Query("namespace"), c.Query("search"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *DeliveryHandler) ListReleaseBundles(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListReleaseBundles(c.Request.Context(), principal, domaindelivery.ReleaseBundleFilter{
		ApplicationID:            c.Query("applicationId"),
		ApplicationEnvironmentID: c.Query("applicationEnvironmentId"),
		Limit:                    parseLimit(c.Query("limit"), 50),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *DeliveryHandler) GetReleaseBundle(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetReleaseBundle(c.Request.Context(), principal, c.Param("bundleID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) ListExecutionTasks(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListExecutionTasks(c.Request.Context(), principal, domaindelivery.ExecutionTaskFilter{
		ApplicationID:            c.Query("applicationId"),
		ApplicationEnvironmentID: c.Query("applicationEnvironmentId"),
		ReleaseBundleID:          c.Query("releaseBundleId"),
		Status:                   c.Query("status"),
		ProviderKind:             c.Query("providerKind"),
		Limit:                    parseLimit(c.Query("limit"), 50),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *DeliveryHandler) GetExecutionTask(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetExecutionTask(c.Request.Context(), principal, c.Param("taskID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) ListExecutionLogs(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListExecutionLogs(c.Request.Context(), principal, c.Param("taskID"), parseLimit(c.Query("limit"), 200))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *DeliveryHandler) ListExecutionArtifacts(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListExecutionArtifacts(c.Request.Context(), principal, c.Param("taskID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *DeliveryHandler) ListReleaseBundleArtifacts(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListReleaseBundleArtifacts(c.Request.Context(), principal, c.Param("bundleID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *DeliveryHandler) ListApprovalPolicies(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListApprovalPolicies(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *DeliveryHandler) CreateApprovalPolicy(c *gin.Context) {
	var req dto.ApprovalPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid approval policy payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateApprovalPolicy(c.Request.Context(), principal, domaindelivery.ApprovalPolicyInput{
		ID:                req.ID,
		Key:               req.Key,
		Name:              req.Name,
		Description:       req.Description,
		Mode:              req.Mode,
		RequiredApprovals: req.RequiredApprovals,
		SLAMinutes:        req.SLAMinutes,
		ApproverRoles:     req.ApproverRoles,
		ChangeWindow:      req.ChangeWindow,
		Enabled:           req.Enabled,
		Metadata:          req.Metadata,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *DeliveryHandler) UpdateApprovalPolicy(c *gin.Context) {
	var req dto.ApprovalPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid approval policy payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateApprovalPolicy(c.Request.Context(), principal, c.Param("approvalPolicyID"), domaindelivery.ApprovalPolicyInput{
		ID:                req.ID,
		Key:               req.Key,
		Name:              req.Name,
		Description:       req.Description,
		Mode:              req.Mode,
		RequiredApprovals: req.RequiredApprovals,
		SLAMinutes:        req.SLAMinutes,
		ApproverRoles:     req.ApproverRoles,
		ChangeWindow:      req.ChangeWindow,
		Enabled:           req.Enabled,
		Metadata:          req.Metadata,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) DeleteApprovalPolicy(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteApprovalPolicy(c.Request.Context(), principal, c.Param("approvalPolicyID")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *DeliveryHandler) CancelExecutionTask(c *gin.Context) {
	var req dto.ExecutionTaskActionRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid execution task action payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CancelExecutionTask(c.Request.Context(), principal, c.Param("taskID"), domaindelivery.ExecutionTaskActionInput{
		Reason: req.Reason,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}

func (h *DeliveryHandler) RetryExecutionTask(c *gin.Context) {
	var req dto.ExecutionTaskActionRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid execution task action payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.RetryExecutionTask(c.Request.Context(), principal, c.Param("taskID"), domaindelivery.ExecutionTaskActionInput{
		Reason: req.Reason,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}

func (h *DeliveryHandler) GetExecutionTaskRunnerStatus(c *gin.Context) {
	if !h.authorizeRunner(c.GetHeader("Authorization")) {
		apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "invalid runner token")
		return
	}
	item, err := h.service.GetExecutionTaskForRunner(c.Request.Context(), c.Param("taskID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) RecordExecutionCallback(c *gin.Context) {
	var req dto.ExecutionCallbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid execution callback payload")
		return
	}
	item, err := h.service.RecordCallback(c.Request.Context(), domaindelivery.ExecutionCallbackInput{
		CallbackToken: req.CallbackToken,
		Status:        req.Status,
		Payload:       req.Payload,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}

func (h *DeliveryHandler) ClaimExecutionTask(c *gin.Context) {
	if !h.authorizeRunner(c.GetHeader("Authorization")) {
		apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "invalid runner token")
		return
	}
	var req dto.ClaimExecutionTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid execution claim payload")
		return
	}
	item, err := h.service.ClaimExecutionTask(c.Request.Context(), req.ProviderKinds, req.AgentID, req.RuntimeEndpoint)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}

func (h *DeliveryHandler) authorizeRunner(header string) bool {
	if strings.TrimSpace(h.runnerToken) == "" {
		return false
	}
	token := strings.TrimSpace(header)
	token = strings.TrimPrefix(token, "Bearer ")
	token = strings.TrimPrefix(token, "bearer ")
	return strings.TrimSpace(token) == strings.TrimSpace(h.runnerToken)
}
