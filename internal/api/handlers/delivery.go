package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/soha/soha/internal/api/dto"
	apiMiddleware "github.com/soha/soha/internal/api/middleware"
	apiresponse "github.com/soha/soha/internal/api/response"
	domainapp "github.com/soha/soha/internal/domain/application"
	domaindelivery "github.com/soha/soha/internal/domain/delivery"
	domainidentity "github.com/soha/soha/internal/domain/identity"
)

type DeliveryService interface {
	GetApplicationDetail(context.Context, domainidentity.Principal, string) (domaindelivery.ApplicationDetail, error)
	GetApplicationRuntimeDetail(context.Context, domainidentity.Principal, string) (domaindelivery.ApplicationRuntimeDetail, error)
	GetApplicationWorkloadRuntimeDetail(context.Context, domainidentity.Principal, string, string, string) (domaindelivery.ApplicationWorkloadRuntimeDetail, error)
	GetApplicationEnvironmentDetail(context.Context, domainidentity.Principal, string) (domaindelivery.ApplicationEnvironmentDetail, error)
	TriggerApplicationDeliveryAction(context.Context, domainidentity.Principal, string, domaindelivery.ApplicationDeliveryActionInput) (domaindelivery.ApplicationDeliveryActionResult, error)
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
	ListDeliveryBlueprints(context.Context, domainidentity.Principal) ([]domaindelivery.DeliveryBlueprint, error)
	CreateDeliveryBlueprint(context.Context, domainidentity.Principal, domaindelivery.DeliveryBlueprintInput) (domaindelivery.DeliveryBlueprint, error)
	UpdateDeliveryBlueprint(context.Context, domainidentity.Principal, string, domaindelivery.DeliveryBlueprintInput) (domaindelivery.DeliveryBlueprint, error)
	RenderDeliveryBlueprintSpec(context.Context, domainidentity.Principal, string) (domaindelivery.RenderedDeliverySpec, error)
	BootstrapApplicationFromBlueprint(context.Context, domainidentity.Principal, string) (domaindelivery.BlueprintBootstrapResult, error)
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

func (h *DeliveryHandler) GetApplicationRuntimeDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetApplicationRuntimeDetail(c.Request.Context(), principal, c.Param("applicationID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) GetApplicationWorkloadRuntimeDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetApplicationWorkloadRuntimeDetail(
		c.Request.Context(),
		principal,
		c.Param("applicationID"),
		c.Param("applicationEnvironmentID"),
		c.Param("workloadName"),
	)
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

func (h *DeliveryHandler) TriggerApplicationDeliveryAction(c *gin.Context) {
	var req dto.ApplicationDeliveryActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid application delivery action payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.TriggerApplicationDeliveryAction(c.Request.Context(), principal, c.Param("applicationID"), domaindelivery.ApplicationDeliveryActionInput{
		Action:                   domaindelivery.ApplicationDeliveryActionKind(req.Action),
		ApplicationEnvironmentID: req.ApplicationEnvironmentID,
		TargetID:                 req.TargetID,
		BuildSourceID:            req.BuildSourceID,
		RefType:                  req.RefType,
		RefName:                  req.RefName,
		ImageTag:                 req.ImageTag,
		ReleaseName:              req.ReleaseName,
		ContainerName:            req.ContainerName,
		Variables:                req.Variables,
		BuildArgs:                req.BuildArgs,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
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

func (h *DeliveryHandler) ListDeliveryBlueprints(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListDeliveryBlueprints(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *DeliveryHandler) CreateDeliveryBlueprint(c *gin.Context) {
	input, err := decodeDeliveryBlueprintRequest(c)
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateDeliveryBlueprint(c.Request.Context(), principal, input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *DeliveryHandler) UpdateDeliveryBlueprint(c *gin.Context) {
	input, err := decodeDeliveryBlueprintRequest(c)
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateDeliveryBlueprint(c.Request.Context(), principal, c.Param("blueprintID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) RenderDeliveryBlueprintSpec(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.RenderDeliveryBlueprintSpec(c.Request.Context(), principal, c.Param("blueprintID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) BootstrapApplicationFromBlueprint(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.BootstrapApplicationFromBlueprint(c.Request.Context(), principal, c.Param("blueprintID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
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
	if !authorizeDeliveryRunner(c, h.runnerToken) {
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
	if !authorizeDeliveryRunner(c, h.runnerToken) {
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

func decodeDeliveryBlueprintRequest(c *gin.Context) (domaindelivery.DeliveryBlueprintInput, error) {
	var req dto.DeliveryBlueprintRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		return domaindelivery.DeliveryBlueprintInput{}, errors.New("invalid delivery blueprint payload")
	}
	draft := domaindelivery.BlueprintApplicationDraft{}
	if err := remarshal(req.ApplicationDraft, &draft); err != nil {
		return domaindelivery.DeliveryBlueprintInput{}, errors.New("invalid applicationDraft payload")
	}
	buildSources := []domainapp.BuildSourceInput{}
	if err := remarshal(req.BuildSources, &buildSources); err != nil {
		return domaindelivery.DeliveryBlueprintInput{}, errors.New("invalid buildSources payload")
	}
	environmentBindings := []domaindelivery.BlueprintEnvironmentBindingTemplate{}
	if err := remarshal(req.EnvironmentBindings, &environmentBindings); err != nil {
		return domaindelivery.DeliveryBlueprintInput{}, errors.New("invalid environmentBindings payload")
	}
	files := make([]domaindelivery.BlueprintFileTemplate, 0, len(req.Files))
	for _, item := range req.Files {
		files = append(files, domaindelivery.BlueprintFileTemplate{
			Path:     item.Path,
			Kind:     item.Kind,
			Content:  item.Content,
			Required: item.Required,
			Purpose:  item.Purpose,
		})
	}
	return domaindelivery.DeliveryBlueprintInput{
		ID:                  req.ID,
		Key:                 req.Key,
		Name:                req.Name,
		Description:         req.Description,
		ApplicationDraft:    draft,
		BuildSources:        buildSources,
		EnvironmentBindings: environmentBindings,
		Files:               files,
		ExecutionHints:      req.ExecutionHints,
		PostCreateActions:   req.PostCreateActions,
		Enabled:             req.Enabled,
	}, nil
}

func remarshal(source any, target any) error {
	payload, err := json.Marshal(source)
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, target)
}
