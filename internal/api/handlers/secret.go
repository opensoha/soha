package handlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsecret "github.com/opensoha/soha/internal/domain/secret"
	"github.com/opensoha/soha/internal/platform/keyring"
)

type SecretManagementService interface {
	List(context.Context, domainidentity.Principal, domainsecret.Filter) ([]sohaapi.SecretMetadata, error)
	Get(context.Context, domainidentity.Principal, string) (sohaapi.SecretMetadata, error)
	Create(context.Context, domainidentity.Principal, sohaapi.SecretCreateRequest) (sohaapi.SecretMetadata, error)
	Update(context.Context, domainidentity.Principal, string, domainsecret.UpdateInput) (sohaapi.SecretMetadata, error)
	Disable(context.Context, domainidentity.Principal, string) (sohaapi.SecretMetadata, error)
}

type SecretVersionService interface {
	ListVersions(context.Context, domainidentity.Principal, string) ([]sohaapi.SecretVersionMetadata, error)
	Rotate(context.Context, domainidentity.Principal, string, sohaapi.SecretRotateRequest) (sohaapi.SecretVersionMetadata, error)
	RevokeVersion(context.Context, domainidentity.Principal, string, int) (sohaapi.SecretVersionMetadata, error)
}

type SecretLeaseRedemptionService interface {
	RedeemLease(context.Context, string, string, string) (sohaapi.SecretLeaseRedemption, error)
}

type SecretHandler struct {
	management SecretManagementService
	versions   SecretVersionService
	leases     SecretLeaseRedemptionService
	runnerKeys keyring.Ring
}

func NewSecretHandler(service interface {
	SecretManagementService
	SecretVersionService
	SecretLeaseRedemptionService
}) *SecretHandler {
	return NewSecretHandlerWithRunnerKeys(service, keyring.Ring{})
}

func NewSecretHandlerWithRunnerKeys(service interface {
	SecretManagementService
	SecretVersionService
	SecretLeaseRedemptionService
}, keys keyring.Ring) *SecretHandler {
	return &SecretHandler{management: service, versions: service, leases: service, runnerKeys: keys}
}

func (h *SecretHandler) List(c *gin.Context) {
	items, err := h.management.List(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), domainsecret.Filter{
		ScopeType: domainsecret.ScopeType(c.Query("scopeType")), ScopeID: c.Query("scopeId"),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *SecretHandler) Get(c *gin.Context) {
	item, err := h.management.Get(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("secretID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *SecretHandler) Create(c *gin.Context) {
	var request sohaapi.SecretCreateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid secret payload")
		return
	}
	item, err := h.management.Create(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), request)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

type secretUpdateRequest struct {
	Name        *string                  `json:"name"`
	Description *string                  `json:"description"`
	Status      *sohaapi.SecretStatus    `json:"status"`
	Bindings    *[]sohaapi.SecretBinding `json:"bindings"`
}

func (h *SecretHandler) Update(c *gin.Context) {
	var request secretUpdateRequest
	if err := c.ShouldBindJSON(&request); err != nil || (request.Name == nil && request.Description == nil && request.Status == nil && request.Bindings == nil) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid secret update payload")
		return
	}
	input := domainsecret.UpdateInput{Name: request.Name, Description: request.Description}
	if request.Status != nil {
		status := domainsecret.Status(*request.Status)
		input.Status = &status
	}
	if request.Bindings != nil {
		bindings := make([]domainsecret.Binding, 0, len(*request.Bindings))
		for _, binding := range *request.Bindings {
			bindings = append(bindings, domainsecret.Binding{TargetType: string(binding.TargetType), TargetRef: binding.TargetRef})
		}
		input.Bindings = &bindings
	}
	item, err := h.management.Update(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("secretID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *SecretHandler) Disable(c *gin.Context) {
	item, err := h.management.Disable(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("secretID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *SecretHandler) ListVersions(c *gin.Context) {
	items, err := h.versions.ListVersions(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("secretID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *SecretHandler) Rotate(c *gin.Context) {
	var request sohaapi.SecretRotateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid secret rotation payload")
		return
	}
	item, err := h.versions.Rotate(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("secretID"), request)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *SecretHandler) RevokeVersion(c *gin.Context) {
	version, err := strconv.Atoi(c.Param("version"))
	if err != nil || version < 1 {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "version must be a positive integer")
		return
	}
	item, err := h.versions.RevokeVersion(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("secretID"), version)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *SecretHandler) RedeemLease(c *gin.Context) {
	if !authorizeExternalRunnerKeys(c, h.runnerKeys, appaccess.PermDeliveryExecutionTasksManage, appaccess.PermAIGatewayInvoke, appaccess.PermObserveAIChatUse) {
		apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "invalid runner token")
		return
	}
	item, err := h.leases.RedeemLease(c.Request.Context(), c.Param("secretLeaseID"), c.GetHeader("X-Soha-Secret-Lease-Token"), c.GetHeader("X-Soha-Agent-ID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
