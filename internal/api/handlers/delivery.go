package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/api/dto"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
)

type DeliveryApplicationService interface {
	GetApplicationDetail(context.Context, domainidentity.Principal, string) (domaindelivery.ApplicationDetail, error)
	GetApplicationRuntimeDetail(context.Context, domainidentity.Principal, string) (domaindelivery.ApplicationRuntimeDetail, error)
	GetApplicationWorkloadRuntimeDetail(context.Context, domainidentity.Principal, string, string, string) (domaindelivery.ApplicationWorkloadRuntimeDetail, error)
	GetApplicationEnvironmentDetail(context.Context, domainidentity.Principal, string) (domaindelivery.ApplicationEnvironmentDetail, error)
	TriggerApplicationDeliveryAction(context.Context, domainidentity.Principal, string, domaindelivery.ApplicationDeliveryActionInput) (domaindelivery.ApplicationDeliveryActionResult, error)
}

type DeliveryReleaseService interface {
	ListReleaseBoard(context.Context, domainidentity.Principal) ([]domaindelivery.ReleaseBoardEntry, error)
	ListTargetCandidates(context.Context, domainidentity.Principal, string, string, string) ([]domaindelivery.TargetCandidate, error)
	ListReleaseBundles(context.Context, domainidentity.Principal, domaindelivery.ReleaseBundleFilter) ([]domaindelivery.ReleaseBundle, error)
	GetReleaseBundle(context.Context, domainidentity.Principal, string) (domaindelivery.ReleaseBundle, error)
}

type DeliveryExecutionQueryService interface {
	ListExecutionTasks(context.Context, domainidentity.Principal, domaindelivery.ExecutionTaskFilter) ([]domaindelivery.ExecutionTask, error)
	GetExecutionTask(context.Context, domainidentity.Principal, string) (domaindelivery.ExecutionTask, error)
	ListExecutionLogs(context.Context, domainidentity.Principal, string, int) ([]domaindelivery.ExecutionLog, error)
	ListArtifacts(context.Context, domainidentity.Principal, domaindelivery.ArtifactFilter) ([]domaindelivery.ExecutionArtifact, error)
	ListExecutionArtifacts(context.Context, domainidentity.Principal, string) ([]domaindelivery.ExecutionArtifact, error)
	ListReleaseBundleArtifacts(context.Context, domainidentity.Principal, string) ([]domaindelivery.ExecutionArtifact, error)
}

type DeliveryRuntimeService interface {
	GetBuildRuntimeDetail(context.Context, domainidentity.Principal, string) (domaindelivery.RuntimeObjectDetail, error)
	GetWorkflowRuntimeDetail(context.Context, domainidentity.Principal, string) (domaindelivery.RuntimeObjectDetail, error)
	GetReleaseRuntimeDetail(context.Context, domainidentity.Principal, string) (domaindelivery.RuntimeObjectDetail, error)
	GetReleaseBundleRuntimeDetail(context.Context, domainidentity.Principal, string) (domaindelivery.RuntimeObjectDetail, error)
	GetExecutionTaskRuntimeDetail(context.Context, domainidentity.Principal, string) (domaindelivery.RuntimeObjectDetail, error)
}

type DeliveryBlueprintService interface {
	ListDeliveryBlueprints(context.Context, domainidentity.Principal) ([]domaindelivery.DeliveryBlueprint, error)
	CreateDeliveryBlueprint(context.Context, domainidentity.Principal, domaindelivery.DeliveryBlueprintInput) (domaindelivery.DeliveryBlueprint, error)
	UpdateDeliveryBlueprint(context.Context, domainidentity.Principal, string, domaindelivery.DeliveryBlueprintInput) (domaindelivery.DeliveryBlueprint, error)
	GetDeliveryBlueprintUsage(context.Context, domainidentity.Principal, string) (domaincatalog.TemplateUsageSummary, error)
	RenderDeliveryBlueprintSpec(context.Context, domainidentity.Principal, string) (domaindelivery.RenderedDeliverySpec, error)
	BootstrapApplicationFromBlueprint(context.Context, domainidentity.Principal, string) (domaindelivery.BlueprintBootstrapResult, error)
}

type DeliveryDraftPlanService interface {
	CreateDeliveryDraft(context.Context, domainidentity.Principal, domaindelivery.DeliveryDraftInput) (domaindelivery.DeliveryDraft, error)
	GetDeliveryDraft(context.Context, domainidentity.Principal, string) (domaindelivery.DeliveryDraft, error)
	ConfirmDeliveryDraft(context.Context, domainidentity.Principal, string) (domaindelivery.DeliveryDraftConfirmResult, error)
	CreateDeliveryPlan(context.Context, domainidentity.Principal, domaindelivery.DeliveryPlanInput) (domaindelivery.DeliveryPlan, error)
	GetDeliveryPlan(context.Context, domainidentity.Principal, string) (domaindelivery.DeliveryPlan, error)
	ConfirmDeliveryPlan(context.Context, domainidentity.Principal, string) (domaindelivery.DeliveryPlanConfirmResult, error)
	DecideDeliveryPlanApproval(context.Context, domainidentity.Principal, string, domaindelivery.DeliveryPlanApprovalInput) (domaindelivery.DeliveryPlan, error)
}

type DeliveryExecutionActionService interface {
	CancelExecutionTask(context.Context, domainidentity.Principal, string, domaindelivery.ExecutionTaskActionInput) (domaindelivery.ExecutionTask, error)
	RetryExecutionTask(context.Context, domainidentity.Principal, string, domaindelivery.ExecutionTaskActionInput) (domaindelivery.ExecutionTask, error)
}

type DeliveryRunnerService interface {
	GetExecutionTaskForRunner(context.Context, string) (domaindelivery.ExecutionTask, error)
	RecordCallback(context.Context, domaindelivery.ExecutionCallbackInput) (domaindelivery.ExecutionTask, error)
	ClaimExecutionTask(context.Context, []string, string, string) (domaindelivery.ExecutionTask, error)
}

type DeliveryService interface {
	DeliveryApplicationService
	DeliveryReleaseService
	DeliveryExecutionQueryService
	DeliveryRuntimeService
	DeliveryBlueprintService
	DeliveryDraftPlanService
	DeliveryExecutionActionService
	DeliveryRunnerService
}

type DeliveryServices struct {
	Applications DeliveryApplicationService
	Releases     DeliveryReleaseService
	Executions   DeliveryExecutionQueryService
	Runtime      DeliveryRuntimeService
	Blueprints   DeliveryBlueprintService
	Drafts       DeliveryDraftPlanService
	Actions      DeliveryExecutionActionService
	Runner       DeliveryRunnerService
}

type DeliveryHandler struct {
	applications DeliveryApplicationService
	releases     DeliveryReleaseService
	executions   DeliveryExecutionQueryService
	runtime      DeliveryRuntimeService
	blueprints   DeliveryBlueprintService
	drafts       DeliveryDraftPlanService
	actions      DeliveryExecutionActionService
	runner       DeliveryRunnerService
	runnerKeys   keyring.Ring
}

func NewDeliveryHandler(service DeliveryService, runnerToken string) *DeliveryHandler {
	return NewDeliveryHandlerWithRunnerKeys(service, legacyRunnerKeyring(runnerToken))
}

func NewDeliveryHandlerWithRunnerKeys(service DeliveryService, keys keyring.Ring) *DeliveryHandler {
	return NewDeliveryHandlerWithServices(DeliveryServices{
		Applications: service, Releases: service, Executions: service, Runtime: service,
		Blueprints: service, Drafts: service, Actions: service, Runner: service,
	}, keys)
}

func NewDeliveryHandlerWithServices(services DeliveryServices, keys keyring.Ring) *DeliveryHandler {
	return &DeliveryHandler{
		applications: services.Applications, releases: services.Releases, executions: services.Executions,
		runtime: services.Runtime, blueprints: services.Blueprints, drafts: services.Drafts,
		actions: services.Actions, runner: services.Runner, runnerKeys: keys,
	}
}

func (h *DeliveryHandler) GetApplicationDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.applications.GetApplicationDetail(c.Request.Context(), principal, c.Param("applicationID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) GetApplicationRuntimeDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.applications.GetApplicationRuntimeDetail(c.Request.Context(), principal, c.Param("applicationID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) GetApplicationWorkloadRuntimeDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.applications.GetApplicationWorkloadRuntimeDetail(
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
	item, err := h.applications.GetApplicationEnvironmentDetail(c.Request.Context(), principal, c.Param("applicationEnvironmentID"))
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
	item, err := h.applications.TriggerApplicationDeliveryAction(c.Request.Context(), principal, c.Param("applicationID"), domaindelivery.ApplicationDeliveryActionInput{
		Action:                   domaindelivery.ApplicationDeliveryActionKind(req.Action),
		ApplicationEnvironmentID: req.ApplicationEnvironmentID,
		TargetID:                 req.TargetID,
		TargetIDs:                req.TargetIDs,
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
	items, err := h.releases.ListReleaseBoard(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *DeliveryHandler) ListTargetCandidates(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.releases.ListTargetCandidates(c.Request.Context(), principal, c.Query("clusterId"), c.Query("namespace"), c.Query("search"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *DeliveryHandler) ListReleaseBundles(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.releases.ListReleaseBundles(c.Request.Context(), principal, domaindelivery.ReleaseBundleFilter{
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
	item, err := h.releases.GetReleaseBundle(c.Request.Context(), principal, c.Param("bundleID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) ListExecutionTasks(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.executions.ListExecutionTasks(c.Request.Context(), principal, domaindelivery.ExecutionTaskFilter{
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
	item, err := h.executions.GetExecutionTask(c.Request.Context(), principal, c.Param("taskID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) ListExecutionLogs(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.executions.ListExecutionLogs(c.Request.Context(), principal, c.Param("taskID"), parseLimit(c.Query("limit"), 200))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *DeliveryHandler) ListArtifacts(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.executions.ListArtifacts(c.Request.Context(), principal, domaindelivery.ArtifactFilter{
		ApplicationID:            c.Query("applicationId"),
		ApplicationEnvironmentID: c.Query("applicationEnvironmentId"),
		WorkflowRunID:            c.Query("workflowRunId"),
		WorkflowNodeID:           c.Query("workflowNodeId"),
		ReleaseBundleID:          c.Query("releaseBundleId"),
		ExecutionTaskID:          c.Query("executionTaskId"),
		Kind:                     c.Query("kind"),
		Status:                   c.Query("status"),
		Limit:                    parseLimit(c.Query("limit"), 100),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *DeliveryHandler) ListExecutionArtifacts(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.executions.ListExecutionArtifacts(c.Request.Context(), principal, c.Param("taskID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *DeliveryHandler) GetBuildRuntimeDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.runtime.GetBuildRuntimeDetail(c.Request.Context(), principal, c.Param("buildID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) GetWorkflowRuntimeDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.runtime.GetWorkflowRuntimeDetail(c.Request.Context(), principal, c.Param("workflowRunID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) GetReleaseRuntimeDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.runtime.GetReleaseRuntimeDetail(c.Request.Context(), principal, c.Param("releaseID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) GetReleaseBundleRuntimeDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.runtime.GetReleaseBundleRuntimeDetail(c.Request.Context(), principal, c.Param("bundleID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) GetExecutionTaskRuntimeDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.runtime.GetExecutionTaskRuntimeDetail(c.Request.Context(), principal, c.Param("taskID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) ListReleaseBundleArtifacts(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.executions.ListReleaseBundleArtifacts(c.Request.Context(), principal, c.Param("bundleID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *DeliveryHandler) ListDeliveryBlueprints(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.blueprints.ListDeliveryBlueprints(c.Request.Context(), principal)
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
	item, err := h.blueprints.CreateDeliveryBlueprint(c.Request.Context(), principal, input)
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
	item, err := h.blueprints.UpdateDeliveryBlueprint(c.Request.Context(), principal, c.Param("blueprintID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) GetDeliveryBlueprintUsage(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.blueprints.GetDeliveryBlueprintUsage(c.Request.Context(), principal, c.Param("blueprintID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) RenderDeliveryBlueprintSpec(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.blueprints.RenderDeliveryBlueprintSpec(c.Request.Context(), principal, c.Param("blueprintID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) BootstrapApplicationFromBlueprint(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.blueprints.BootstrapApplicationFromBlueprint(c.Request.Context(), principal, c.Param("blueprintID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) CreateDeliveryDraft(c *gin.Context) {
	input, err := decodeDeliveryDraftRequest(c)
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.drafts.CreateDeliveryDraft(c.Request.Context(), principal, input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *DeliveryHandler) GetDeliveryDraft(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.drafts.GetDeliveryDraft(c.Request.Context(), principal, c.Param("draftID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) ConfirmDeliveryDraft(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.drafts.ConfirmDeliveryDraft(c.Request.Context(), principal, c.Param("draftID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) CreateDeliveryPlan(c *gin.Context) {
	var req dto.DeliveryPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid delivery plan payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.drafts.CreateDeliveryPlan(c.Request.Context(), principal, deliveryPlanInputFromRequest(req))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *DeliveryHandler) GetDeliveryPlan(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.drafts.GetDeliveryPlan(c.Request.Context(), principal, c.Param("planID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeliveryHandler) ConfirmDeliveryPlan(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.drafts.ConfirmDeliveryPlan(c.Request.Context(), principal, c.Param("planID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}

func (h *DeliveryHandler) DecideDeliveryPlanApproval(c *gin.Context) {
	var req dto.DeliveryPlanApprovalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid delivery plan approval payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.drafts.DecideDeliveryPlanApproval(c.Request.Context(), principal, c.Param("planID"), domaindelivery.DeliveryPlanApprovalInput{Action: req.Action, Comment: req.Comment})
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
	item, err := h.actions.CancelExecutionTask(c.Request.Context(), principal, c.Param("taskID"), domaindelivery.ExecutionTaskActionInput{
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
	item, err := h.actions.RetryExecutionTask(c.Request.Context(), principal, c.Param("taskID"), domaindelivery.ExecutionTaskActionInput{
		Reason: req.Reason,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}

func (h *DeliveryHandler) GetExecutionTaskRunnerStatus(c *gin.Context) {
	if !authorizeDeliveryRunnerKeys(c, h.runnerKeys) {
		apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "invalid runner token")
		return
	}
	item, err := h.runner.GetExecutionTaskForRunner(c.Request.Context(), c.Param("taskID"))
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
	item, err := h.runner.RecordCallback(c.Request.Context(), domaindelivery.ExecutionCallbackInput{
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
	if !authorizeDeliveryRunnerKeys(c, h.runnerKeys) {
		apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "invalid runner token")
		return
	}
	var req dto.ClaimExecutionTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid execution claim payload")
		return
	}
	item, err := h.runner.ClaimExecutionTask(c.Request.Context(), req.ProviderKinds, req.AgentID, req.RuntimeEndpoint)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			c.Status(http.StatusNoContent)
			return
		}
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

func decodeDeliveryDraftRequest(c *gin.Context) (domaindelivery.DeliveryDraftInput, error) {
	var req dto.DeliveryDraftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		return domaindelivery.DeliveryDraftInput{}, errors.New("invalid delivery draft payload")
	}
	draft := domaindelivery.BlueprintApplicationDraft{}
	if err := remarshal(req.ApplicationDraft, &draft); err != nil {
		return domaindelivery.DeliveryDraftInput{}, errors.New("invalid applicationDraft payload")
	}
	services := []domaindelivery.DeliveryDraftService{}
	if err := remarshal(req.Services, &services); err != nil {
		return domaindelivery.DeliveryDraftInput{}, errors.New("invalid services payload")
	}
	buildSources := []domainapp.BuildSourceInput{}
	if err := remarshal(req.BuildSources, &buildSources); err != nil {
		return domaindelivery.DeliveryDraftInput{}, errors.New("invalid buildSources payload")
	}
	environmentBindings := []domaindelivery.BlueprintEnvironmentBindingTemplate{}
	if err := remarshal(req.EnvironmentBindings, &environmentBindings); err != nil {
		return domaindelivery.DeliveryDraftInput{}, errors.New("invalid environmentBindings payload")
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
	return domaindelivery.DeliveryDraftInput{
		ID:                  req.ID,
		Source:              req.Source,
		ApplicationDraft:    draft,
		Services:            services,
		BuildSources:        buildSources,
		EnvironmentBindings: environmentBindings,
		Files:               files,
		ExecutionHints:      req.ExecutionHints,
		PostCreateActions:   req.PostCreateActions,
	}, nil
}

func deliveryPlanInputFromRequest(req dto.DeliveryPlanRequest) domaindelivery.DeliveryPlanInput {
	return domaindelivery.DeliveryPlanInput{
		ID:                       req.ID,
		Source:                   req.Source,
		ApplicationID:            req.ApplicationID,
		ApplicationEnvironmentID: req.ApplicationEnvironmentID,
		Action:                   domaindelivery.ApplicationDeliveryActionKind(req.Action),
		TargetID:                 req.TargetID,
		TargetIDs:                req.TargetIDs,
		BuildSourceID:            req.BuildSourceID,
		ReleaseBundleID:          req.ReleaseBundleID,
		RefType:                  req.RefType,
		RefName:                  req.RefName,
		ImageTag:                 req.ImageTag,
		ReleaseName:              req.ReleaseName,
		ContainerName:            req.ContainerName,
		Reason:                   req.Reason,
		Variables:                req.Variables,
		BuildArgs:                req.BuildArgs,
	}
}

func remarshal(source any, target any) error {
	payload, err := json.Marshal(source)
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, target)
}
