package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

type resourceCreationHandler struct {
	service ResourceCreationService
}

func (h *resourceCreationHandler) DecideResourceCreationScope(c *gin.Context) {
	var payload sohaapi.KubernetesResourceCreateScopeDecisionRequest
	if err := c.ShouldBindJSON(&payload); err != nil || payload.Action != sohaapi.Create {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid resource creation scope decision payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	result, err := h.service.DecideCreateScope(c.Request.Context(), principal, c.Param("clusterID"), domainresource.ResourceCreateScopeDecisionRequest{
		Namespace: payload.Namespace, ResourceGroup: payload.ResourceGroup, APIVersion: payload.APIVersion, Kind: payload.Kind,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, mapResourceCreateScopeDecision(result))
}

func mapResourceCreateScopeDecision(result domainresource.ResourceCreateScopeDecision) sohaapi.KubernetesResourceCreateScopeDecision {
	actions := make([]sohaapi.KubernetesResourceAction, 0, len(result.AllowedActions))
	for _, action := range result.AllowedActions {
		actions = append(actions, sohaapi.KubernetesResourceAction(action))
	}
	return sohaapi.KubernetesResourceCreateScopeDecision{
		Allowed: result.Allowed, Reason: result.Reason, AllowedActions: actions,
		ResourceScope: sohaapi.KubernetesResourceScope{
			ClusterIDs: nonNilStrings(result.ClusterIDs), Namespaces: nonNilStrings(result.Namespaces),
			ResourceGroups: nonNilStrings(result.ResourceGroups), ResourceKinds: nonNilStrings(result.ResourceKinds),
		},
		Capability: sohaapi.KubernetesResourceCapability{
			Key: result.Capability.Key, Status: sohaapi.ClusterCapabilityStatus(result.Capability.Status),
			Mode: sohaapi.KubernetesResourceCapabilityMode(result.Capability.Mode), Reason: result.Capability.Reason,
		},
	}
}

func (h *resourceCreationHandler) PreflightResourceCreation(c *gin.Context) {
	request, ok := bindResourceCreateRequest(c)
	if !ok {
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	result, err := h.service.PreflightCreate(c.Request.Context(), principal, c.Param("clusterID"), request)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, mapResourceCreatePreflight(result))
}

func (h *resourceCreationHandler) ExecuteResourceCreation(c *gin.Context) {
	requestID := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if requestID == "" {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "Idempotency-Key header is required")
		return
	}
	request, ok := bindResourceCreateRequest(c)
	if !ok {
		return
	}
	request.RequestID = requestID
	principal := apiMiddleware.PrincipalFromContext(c)
	result, err := h.service.ExecuteCreate(c.Request.Context(), principal, c.Param("clusterID"), request)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, mapResourceCreateExecution(c.Param("clusterID"), result))
}

func bindResourceCreateRequest(c *gin.Context) (domainresource.ResourceCreateRequest, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, domainresource.ResourceCreateMaxBodyBytes+(64<<10))
	var payload sohaapi.KubernetesResourceCreateRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid resource creation payload")
		return domainresource.ResourceCreateRequest{}, false
	}
	return domainresource.ResourceCreateRequest{
		Source: domainresource.ResourceCreateSource(payload.Source), Content: payload.Content,
		DefaultNamespace: payload.DefaultNamespace, ResourceGroup: payload.ResourceGroup,
		ExpectedAPIVersion: payload.ExpectedAPIVersion, ExpectedKind: payload.ExpectedKind,
	}, true
}

func mapResourceCreatePreflight(result domainresource.ResourceCreatePreflight) sohaapi.KubernetesResourcePreflight {
	items := make([]sohaapi.KubernetesResourcePreflightItem, 0, len(result.Documents))
	for _, document := range result.Documents {
		actions := make([]sohaapi.KubernetesResourceAction, 0, len(document.Authorization.AllowedActions))
		for _, action := range document.Authorization.AllowedActions {
			actions = append(actions, sohaapi.KubernetesResourceAction(action))
		}
		item := sohaapi.KubernetesResourcePreflightItem{
			Authorization: sohaapi.KubernetesResourceAuthorizationDecision{
				Allowed: document.Authorization.Allowed, AllowedActions: actions,
				Reason:        document.Authorization.Reason,
				ResourceScope: sohaapi.KubernetesResourceScope{ClusterIDs: nonNilStrings(document.Authorization.ClusterIDs), Namespaces: nonNilStrings(document.Authorization.Namespaces), ResourceGroups: nonNilStrings(document.Authorization.ResourceGroups), ResourceKinds: nonNilStrings(document.Authorization.ResourceKinds)},
			},
			Capability: sohaapi.KubernetesResourceCapability{Key: document.Capability.Key, Mode: sohaapi.KubernetesResourceCapabilityMode(document.Capability.Mode), Status: sohaapi.ClusterCapabilityStatus(document.Capability.Status), Reason: document.Capability.Reason},
			Document: sohaapi.KubernetesResourceDocument{
				Index: document.Index, APIVersion: document.Resource.APIVersion, Kind: document.Resource.Kind,
				Name: document.Resource.Name, Namespace: document.OriginalNamespace,
				ContentHash: document.DocumentHash, ScopeMode: resourceScopeMode(document.Resource.Namespaced),
			},
			ResolvedNamespace: document.Resource.Namespace,
			Warnings:          mapResourceWarnings(document.Warnings),
			Errors:            []sohaapi.KubernetesResourceCreateError{},
			DryRun:            sohaapi.KubernetesResourceDryRunDecision{Status: sohaapi.KubernetesResourceDryRunStatusSkipped},
		}
		if document.DryRun.Valid {
			item.DryRun.Status = sohaapi.KubernetesResourceDryRunStatusPassed
		} else if document.ErrorCode == "resource_dry_run_failed" {
			item.DryRun.Status = sohaapi.KubernetesResourceDryRunStatusFailed
		}
		if document.ErrorCode != "" {
			diagnostic := contractResourceCreateError(document.ErrorCode, document.Error)
			item.Errors = append(item.Errors, diagnostic)
			if document.ErrorCode == "resource_create_denied" || document.ErrorCode == "high_risk_permission_required" || document.ErrorCode == "namespace_mismatch" {
				item.Authorization.Error = &diagnostic
			}
			if document.ErrorCode == "resource_dry_run_failed" {
				item.DryRun.Error = &diagnostic
			}
		}
		items = append(items, item)
	}
	return sohaapi.KubernetesResourcePreflight{Ready: result.Ready, ContentHash: result.ContentHash, Items: items}
}

func mapResourceCreateExecution(clusterID string, result domainresource.ResourceCreateExecution) sohaapi.KubernetesResourceCreateResult {
	items := make([]sohaapi.KubernetesResourceCreateResultItem, 0, len(result.Documents))
	for _, document := range result.Documents {
		documentHash := document.DocumentHash
		if documentHash == "" {
			documentHash = result.ContentHash
		}
		item := sohaapi.KubernetesResourceCreateResultItem{
			Document: sohaapi.KubernetesResourceDocument{
				Index: document.Index, APIVersion: document.Resource.APIVersion, Kind: document.Resource.Kind,
				Name: document.Resource.Name, Namespace: document.Resource.Namespace,
				ScopeMode: resourceScopeMode(document.Resource.Namespaced), ContentHash: documentHash,
			},
			Status: sohaapi.KubernetesResourceCreateResultStatus(document.Status), Warnings: []sohaapi.KubernetesResourceWarning{},
		}
		if document.Status == "succeeded" {
			item.ResourceRef = &sohaapi.KubernetesResourceRef{
				APIVersion: document.Resource.APIVersion, ClusterID: clusterID, Kind: document.Resource.Kind,
				Name: document.Resource.Name, Namespace: document.Resource.Namespace, ScopeMode: resourceScopeMode(document.Resource.Namespaced),
			}
		}
		if document.ErrorCode != "" {
			diagnostic := contractResourceCreateError(document.ErrorCode, document.Error)
			item.Error = &diagnostic
		}
		items = append(items, item)
	}
	return sohaapi.KubernetesResourceCreateResult{
		OperationID: result.OperationID, ContentHash: result.ContentHash,
		Status: sohaapi.KubernetesResourceCreateBatchStatus(result.Status), Items: items,
	}
}

func mapResourceWarnings(warnings []domainresource.ResourceCreateWarning) []sohaapi.KubernetesResourceWarning {
	result := make([]sohaapi.KubernetesResourceWarning, 0, len(warnings))
	for _, warning := range warnings {
		result = append(result, sohaapi.KubernetesResourceWarning{Code: contractResourceCreateErrorCode(warning.Code), Message: warning.Message})
	}
	return result
}

func contractResourceCreateError(code, message string) sohaapi.KubernetesResourceCreateError {
	return sohaapi.KubernetesResourceCreateError{Code: contractResourceCreateErrorCode(code), Message: message}
}

func contractResourceCreateErrorCode(code string) sohaapi.KubernetesResourceCreateErrorCode {
	value := sohaapi.KubernetesResourceCreateErrorCode(code)
	if value.Valid() {
		return value
	}
	return sohaapi.KubernetesResourceCreateErrorCodeResourceDryRunFailed
}

func resourceScopeMode(namespaced bool) sohaapi.KubernetesResourceScopeMode {
	if namespaced {
		return sohaapi.KubernetesResourceScopeModeNamespace
	}
	return sohaapi.KubernetesResourceScopeModeCluster
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
