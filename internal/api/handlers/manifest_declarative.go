package handlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	appmanifest "github.com/opensoha/soha/internal/application/manifest"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
)

type ManifestSourceService interface {
	GetSource(context.Context, domainidentity.Principal, string) (domainmanifest.Source, error)
	UpdateSource(context.Context, domainidentity.Principal, string, domainmanifest.SourceInput) (domainmanifest.Source, error)
	Render(context.Context, domainidentity.Principal, string, domainmanifest.RenderInput) (domainmanifest.RenderResult, error)
	Preflight(context.Context, domainidentity.Principal, string, domainmanifest.PreflightInput) (domaindelivery.ExecutionTask, error)
	Sync(context.Context, domainidentity.Principal, string, domainmanifest.SyncInput, string) (domainmanifest.SyncRun, domaindelivery.ExecutionTask, error)
	SyncWebhook(context.Context, domainidentity.Principal, string, domainmanifest.SyncWebhookInput) (domainmanifest.SyncRun, domaindelivery.ExecutionTask, error)
	ListSyncRuns(context.Context, domainidentity.Principal, string) ([]domainmanifest.SyncRun, error)
}

type ManifestBindingService interface {
	ListBindings(context.Context, domainidentity.Principal, string) ([]domainmanifest.EnvironmentBinding, error)
	CreateBinding(context.Context, domainidentity.Principal, string, domainmanifest.BindingInput) (domainmanifest.EnvironmentBinding, error)
	UpdateBinding(context.Context, domainidentity.Principal, string, domainmanifest.BindingUpdateInput) (domainmanifest.EnvironmentBinding, error)
	DeleteBinding(context.Context, domainidentity.Principal, string) error
	SetDesiredRevision(context.Context, domainidentity.Principal, string, domainmanifest.DesiredRevisionInput) (appmanifest.DeploymentActionResult, error)
}

type ManifestDeploymentService interface {
	ListDeployments(context.Context, domainidentity.Principal, domainmanifest.DeploymentFilter) (domainmanifest.DeploymentPage, error)
	GetDeployment(context.Context, domainidentity.Principal, string) (domainmanifest.Deployment, error)
	Reconcile(context.Context, domainidentity.Principal, string, string, domainmanifest.ActionInput) (appmanifest.DeploymentActionResult, error)
	Rollback(context.Context, domainidentity.Principal, string, domainmanifest.RollbackInput) (appmanifest.DeploymentActionResult, error)
}

type ManifestIntentService interface {
	CreateDeliveryIntent(context.Context, domainidentity.Principal, string, domainmanifest.DeliveryIntentInput) (domainmanifest.DeliveryIntent, error)
	ListDeliveryIntents(context.Context, domainidentity.Principal, string) ([]domainmanifest.DeliveryIntent, error)
	DecideDeliveryIntent(context.Context, domainidentity.Principal, string, string, domainmanifest.DeliveryIntentDecisionInput) (domainmanifest.DeliveryIntent, error)
}

type ManifestSourceHandler struct{ service ManifestSourceService }
type ManifestBindingHandler struct{ service ManifestBindingService }
type ManifestDeploymentHandler struct{ service ManifestDeploymentService }
type ManifestIntentHandler struct{ service ManifestIntentService }

func NewManifestSourceHandler(service ManifestSourceService) *ManifestSourceHandler {
	return &ManifestSourceHandler{service: service}
}

func NewManifestBindingHandler(service ManifestBindingService) *ManifestBindingHandler {
	return &ManifestBindingHandler{service: service}
}

func NewManifestDeploymentHandler(service ManifestDeploymentService) *ManifestDeploymentHandler {
	return &ManifestDeploymentHandler{service: service}
}

func NewManifestIntentHandler(service ManifestIntentService) *ManifestIntentHandler {
	return &ManifestIntentHandler{service: service}
}

func (h *ManifestSourceHandler) Get(c *gin.Context) {
	item, err := h.service.GetSource(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("manifestPackageID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, manifestSourceDTO(item))
}

func (h *ManifestSourceHandler) Update(c *gin.Context) {
	var request sohaapi.ManifestSourceUpdateInput
	if err := c.ShouldBindJSON(&request); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid manifest source payload")
		return
	}
	input := domainmanifest.SourceInput{
		Mode: string(request.Mode), RepositoryID: request.RepositoryID, RefType: string(request.RefType),
		RefValue: request.RefValue, Path: request.Path, IncludePatterns: request.IncludePatterns,
		ExcludePatterns: request.ExcludePatterns, SyncPolicy: string(request.SyncPolicy),
		PollIntervalSeconds: request.PollIntervalSeconds, AutoPublish: request.AutoPublish, AutoDeploy: request.AutoDeploy,
		ExpectedGeneration: request.ExpectedGeneration,
	}
	item, err := h.service.UpdateSource(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("manifestPackageID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, manifestSourceDTO(item))
}

func (h *ManifestSourceHandler) Render(c *gin.Context) {
	var request sohaapi.ManifestRenderInput
	if err := c.ShouldBindJSON(&request); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid manifest render payload")
		return
	}
	item, err := h.service.Render(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("manifestPackageID"), domainmanifest.RenderInput{
		BindingID: request.BindingID,
		Revision:  request.Revision,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *ManifestSourceHandler) Preflight(c *gin.Context) {
	var request sohaapi.ManifestPreflightInput
	if err := c.ShouldBindJSON(&request); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid manifest preflight payload")
		return
	}
	item, err := h.service.Preflight(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("manifestPackageID"), domainmanifest.PreflightInput{
		BindingID: request.BindingID, Revision: request.Revision, ForceConflicts: request.ForceConflicts,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}

func (h *ManifestSourceHandler) Sync(c *gin.Context) {
	var request sohaapi.ManifestSyncInput
	if err := c.ShouldBindJSON(&request); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid manifest sync payload")
		return
	}
	run, task, err := h.service.Sync(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("manifestPackageID"), domainmanifest.SyncInput{
		ExpectedGeneration: request.ExpectedGeneration, RequestedCommit: request.RequestedCommit,
	}, c.GetHeader("Idempotency-Key"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, gin.H{"run": run, "task": task})
}

func (h *ManifestSourceHandler) SyncWebhook(c *gin.Context) {
	var request sohaapi.ManifestSyncWebhookInput
	if err := c.ShouldBindJSON(&request); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid manifest sync webhook payload")
		return
	}
	run, task, err := h.service.SyncWebhook(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("manifestSourceID"), domainmanifest.SyncWebhookInput{
		RepositoryID: request.RepositoryID, Ref: request.Ref, Commit: request.Commit, EventID: request.EventID,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, gin.H{"run": run, "task": task})
}

func (h *ManifestSourceHandler) ListSyncRuns(c *gin.Context) {
	items, err := h.service.ListSyncRuns(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("manifestPackageID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *ManifestBindingHandler) List(c *gin.Context) {
	items, err := h.service.ListBindings(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("manifestPackageID"))
	if err != nil {
		writeError(c, err)
		return
	}
	result := make([]sohaapi.ManifestBinding, 0, len(items))
	for _, item := range items {
		result = append(result, manifestBindingDTO(item))
	}
	apiresponse.Items(c, http.StatusOK, result)
}

func (h *ManifestBindingHandler) Create(c *gin.Context) {
	var request sohaapi.ManifestBindingInput
	if err := c.ShouldBindJSON(&request); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid manifest binding payload")
		return
	}
	item, err := h.service.CreateBinding(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("manifestPackageID"), bindingInput(request))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, manifestBindingDTO(item))
}

func (h *ManifestBindingHandler) Update(c *gin.Context) {
	var request sohaapi.ManifestBindingUpdateInput
	if err := c.ShouldBindJSON(&request); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid manifest binding payload")
		return
	}
	item, err := h.service.UpdateBinding(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("manifestBindingID"), domainmanifest.BindingUpdateInput{
		BindingInput: domainmanifest.BindingInput{
			ApplicationEnvironmentID: request.ApplicationEnvironmentID, ClusterID: request.ClusterID,
			Namespace: request.Namespace, Overlay: request.Overlay, RolloutStrategyID: request.RolloutStrategyID,
			VerificationPolicyID: request.VerificationPolicyID, DriftPolicy: string(request.DriftPolicy),
			DeletionPolicy: string(request.DeletionPolicy), Enabled: request.Enabled,
		},
		ExpectedVersion: request.ExpectedVersion,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, manifestBindingDTO(item))
}

func (h *ManifestBindingHandler) Delete(c *gin.Context) {
	if err := h.service.DeleteBinding(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("manifestBindingID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *ManifestBindingHandler) SetDesiredRevision(c *gin.Context) {
	var request sohaapi.ManifestDesiredRevisionInput
	if err := c.ShouldBindJSON(&request); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid desired revision payload")
		return
	}
	item, err := h.service.SetDesiredRevision(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("manifestBindingID"), domainmanifest.DesiredRevisionInput{
		DesiredRevision: request.DesiredRevision, ReconcilePolicy: string(request.ReconcilePolicy),
		ExpectedGeneration: request.ExpectedGeneration, Reason: request.Reason,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, manifestDeploymentActionDTO(item))
}

func (h *ManifestDeploymentHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("pageSize"))
	items, err := h.service.ListDeployments(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), domainmanifest.DeploymentFilter{
		PackageID: c.Query("packageId"), ApplicationID: c.Query("applicationId"), ApplicationEnvironmentID: c.Query("applicationEnvironmentId"),
		ClusterID: c.Query("clusterId"), Namespace: c.Query("namespace"), SourceMode: c.Query("sourceMode"),
		Phase: c.Query("phase"), Page: page, PageSize: pageSize,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	result := make([]sohaapi.ManifestDeployment, 0, len(items.Items))
	for _, item := range items.Items {
		result = append(result, manifestDeploymentDTO(item))
	}
	apiresponse.Item(c, http.StatusOK, sohaapi.ManifestDeploymentPage{Items: result, Total: items.Total, Page: items.Page, PageSize: items.PageSize})
}

func (h *ManifestDeploymentHandler) Get(c *gin.Context) {
	item, err := h.service.GetDeployment(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("manifestDeploymentID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, manifestDeploymentDTO(item))
}

func (h *ManifestDeploymentHandler) Reconcile(c *gin.Context) {
	h.action(c, domainmanifest.TaskActionApply)
}

func (h *ManifestDeploymentHandler) Repair(c *gin.Context) {
	h.action(c, domainmanifest.TaskActionRepair)
}

func (h *ManifestDeploymentHandler) Adopt(c *gin.Context) {
	h.action(c, domainmanifest.TaskActionAdopt)
}

func (h *ManifestDeploymentHandler) action(c *gin.Context, action string) {
	var request sohaapi.ManifestDeploymentActionInput
	if err := c.ShouldBindJSON(&request); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid manifest action payload")
		return
	}
	item, err := h.service.Reconcile(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("manifestDeploymentID"), action, domainmanifest.ActionInput{
		ExpectedGeneration: request.ExpectedGeneration, Reason: request.Reason, ForceConflicts: request.ForceConflicts,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, manifestDeploymentActionDTO(item))
}

func (h *ManifestDeploymentHandler) Rollback(c *gin.Context) {
	var request sohaapi.ManifestRollbackInput
	if err := c.ShouldBindJSON(&request); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid manifest rollback payload")
		return
	}
	item, err := h.service.Rollback(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("manifestDeploymentID"), domainmanifest.RollbackInput{
		ExpectedGeneration: request.ExpectedGeneration, TargetRevision: request.TargetRevision,
		UseLastKnownGood: request.UseLastKnownGood, Reason: request.Reason,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, manifestDeploymentActionDTO(item))
}

func (h *ManifestIntentHandler) List(c *gin.Context) {
	items, err := h.service.ListDeliveryIntents(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("manifestPackageID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *ManifestIntentHandler) Create(c *gin.Context) {
	var request sohaapi.ManifestDeliveryIntentInput
	if err := c.ShouldBindJSON(&request); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid manifest delivery intent payload")
		return
	}
	files := make([]domainmanifest.File, 0, len(request.Files))
	for _, file := range request.Files {
		files = append(files, domainmanifest.File{Path: file.Path, Content: file.Content})
	}
	item, err := h.service.CreateDeliveryIntent(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("manifestPackageID"), domainmanifest.DeliveryIntentInput{
		BindingID: request.BindingID, Files: files, Provider: request.Provider, Model: request.Model,
		PromptTemplateVersion: request.PromptTemplateVersion, RequestID: request.RequestID,
		EvidenceDigest: request.EvidenceDigest, EvidenceRefs: request.EvidenceRefs,
		Rationale: request.Rationale, Risk: request.Risk,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *ManifestIntentHandler) Accept(c *gin.Context) {
	h.decide(c, domainmanifest.IntentStatusAccepted)
}

func (h *ManifestIntentHandler) Reject(c *gin.Context) {
	h.decide(c, domainmanifest.IntentStatusRejected)
}

func (h *ManifestIntentHandler) decide(c *gin.Context, decision string) {
	var request sohaapi.ManifestDeliveryIntentDecisionInput
	if err := c.ShouldBindJSON(&request); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid manifest delivery intent decision payload")
		return
	}
	item, err := h.service.DecideDeliveryIntent(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("manifestDeliveryIntentID"), decision, domainmanifest.DeliveryIntentDecisionInput{
		ExpectedCurrentRevision:  request.ExpectedCurrentRevision,
		ExpectedPackageUpdatedAt: request.ExpectedPackageUpdatedAt,
		Comment:                  request.Comment,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func bindingInput(request sohaapi.ManifestBindingInput) domainmanifest.BindingInput {
	return domainmanifest.BindingInput{
		ApplicationEnvironmentID: request.ApplicationEnvironmentID, ClusterID: request.ClusterID,
		Namespace: request.Namespace, Overlay: request.Overlay, RolloutStrategyID: request.RolloutStrategyID,
		VerificationPolicyID: request.VerificationPolicyID, DriftPolicy: string(request.DriftPolicy),
		DeletionPolicy: string(request.DeletionPolicy), Enabled: request.Enabled,
	}
}

func manifestSourceDTO(item domainmanifest.Source) sohaapi.ManifestSource {
	return sohaapi.ManifestSource{
		ID: item.ID, PackageID: item.PackageID, Mode: sohaapi.ManifestSourceMode(item.Mode),
		RepositoryID: item.RepositoryID, RefType: sohaapi.ManifestSourceRefType(item.RefType),
		RefValue: item.RefValue, Path: item.Path, IncludePatterns: item.IncludePatterns,
		ExcludePatterns: item.ExcludePatterns, SyncPolicy: sohaapi.ManifestSourceSyncPolicy(item.SyncPolicy),
		PollIntervalSeconds: item.PollIntervalSeconds, AutoPublish: item.AutoPublish, AutoDeploy: item.AutoDeploy,
		LastResolvedCommit: item.LastResolvedCommit, LastTreeDigest: item.LastTreeDigest,
		LastCanonicalDigest: item.LastCanonicalDigest, LastSuccessfulSyncAt: item.LastSuccessfulSyncAt,
		LastErrorCode: item.LastErrorCode, LastErrorMessage: item.LastErrorMessage,
		Generation: item.Generation, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func manifestBindingDTO(item domainmanifest.EnvironmentBinding) sohaapi.ManifestBinding {
	return sohaapi.ManifestBinding{
		ID: item.ID, PackageID: item.PackageID, ApplicationEnvironmentID: item.ApplicationEnvironmentID,
		EnvironmentKey: item.EnvironmentKey, ClusterID: item.ClusterID, Namespace: item.Namespace,
		Overlay: item.Overlay, RolloutStrategyID: item.RolloutStrategyID,
		VerificationPolicyID: item.VerificationPolicyID, DriftPolicy: sohaapi.ManifestDriftPolicy(item.DriftPolicy),
		DeletionPolicy: sohaapi.ManifestDeletionPolicy(item.DeletionPolicy), Enabled: item.Enabled,
		Version: item.Version, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func manifestDeploymentDTO(item domainmanifest.Deployment) sohaapi.ManifestDeployment {
	conditions := make([]sohaapi.ManifestCondition, 0, len(item.Status.Conditions))
	for _, condition := range item.Status.Conditions {
		conditions = append(conditions, sohaapi.ManifestCondition{
			Type: sohaapi.ManifestConditionType(condition.Type), Status: sohaapi.ManifestConditionStatus(condition.Status),
			Reason: condition.Reason, Message: condition.Message, ObservedGeneration: condition.ObservedGeneration,
			LastTransitionAt: condition.LastTransitionAt, EvidenceRefs: condition.EvidenceRefs,
		})
	}
	inventory := make([]sohaapi.ManifestResourceInventory, 0, len(item.Status.Inventory))
	for _, resource := range item.Status.Inventory {
		inventory = append(inventory, sohaapi.ManifestResourceInventory{
			DeploymentID: resource.DeploymentID, Generation: resource.Generation, APIVersion: resource.APIVersion,
			Kind: resource.Kind, Namespace: resource.Namespace, Name: resource.Name, UID: resource.UID,
			ResourceVersion: resource.ResourceVersion, DesiredObjectDigest: resource.DesiredObjectDigest,
			ObservedObjectDigest: resource.ObservedObjectDigest, Health: resource.Health,
			LastObservedAt: resource.LastObservedAt,
		})
	}
	return sohaapi.ManifestDeployment{
		ID: item.ID, PackageID: item.PackageID, BindingID: item.BindingID, Generation: item.Generation,
		Spec: sohaapi.ManifestDeploymentSpec{
			DesiredRevision: item.Spec.DesiredRevision, DesiredDigest: item.Spec.DesiredDigest,
			ReconcilePolicy: sohaapi.ManifestReconcilePolicy(item.Spec.ReconcilePolicy),
			DriftPolicy:     sohaapi.ManifestDriftPolicy(item.Spec.DriftPolicy),
			DeletionPolicy:  sohaapi.ManifestDeletionPolicy(item.Spec.DeletionPolicy),
		},
		Status: sohaapi.ManifestDeploymentStatus{
			ObservedGeneration: item.Status.ObservedGeneration, AppliedRevision: item.Status.AppliedRevision,
			AppliedDigest: item.Status.AppliedDigest, LastKnownGoodRevision: item.Status.LastKnownGoodRevision,
			Phase: sohaapi.ManifestDeploymentPhase(item.Status.Phase), Conditions: conditions, Inventory: inventory,
			LastReconciledAt: item.Status.LastReconciledAt, LastExecutionTaskID: item.Status.LastExecutionTaskID,
			Drift: manifestDriftDTO(item.Status.Drift), LastErrorCode: item.Status.LastErrorCode,
			LastErrorMessage: item.Status.LastErrorMessage,
		},
		CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func manifestDeploymentActionDTO(item appmanifest.DeploymentActionResult) gin.H {
	return gin.H{"deployment": manifestDeploymentDTO(item.Deployment), "task": item.Task}
}

func manifestDriftDTO(item *domainmanifest.DriftReport) *sohaapi.ManifestDriftReport {
	if item == nil {
		return nil
	}
	resources := make([]struct {
		APIVersion string                       `json:"apiVersion"`
		Fields     []sohaapi.ManifestDriftField `json:"fields"`
		Kind       string                       `json:"kind"`
		Name       string                       `json:"name"`
		Namespace  string                       `json:"namespace"`
	}, 0, len(item.Resources))
	for _, resource := range item.Resources {
		fields := make([]sohaapi.ManifestDriftField, 0, len(resource.Fields))
		for _, field := range resource.Fields {
			fields = append(fields, sohaapi.ManifestDriftField{Path: field.Path, DesiredValue: field.DesiredValue, ObservedValue: field.ObservedValue, FieldManager: field.FieldManager})
		}
		resources = append(resources, struct {
			APIVersion string                       `json:"apiVersion"`
			Fields     []sohaapi.ManifestDriftField `json:"fields"`
			Kind       string                       `json:"kind"`
			Name       string                       `json:"name"`
			Namespace  string                       `json:"namespace"`
		}{APIVersion: resource.APIVersion, Fields: fields, Kind: resource.Kind, Name: resource.Name, Namespace: resource.Namespace})
	}
	return &sohaapi.ManifestDriftReport{Drifted: item.Drifted, ObservedAt: item.ObservedAt, AgeSeconds: item.AgeSeconds, Resources: resources, EvidenceRefs: item.EvidenceRefs}
}
