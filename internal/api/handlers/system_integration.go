package handlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/systemintegration"
)

type SystemIntegrationService interface {
	List(context.Context, domainidentity.Principal, domain.Filter) ([]sohaapi.SystemIntegration, error)
	Get(context.Context, domainidentity.Principal, string) (sohaapi.SystemIntegration, error)
	Create(context.Context, domainidentity.Principal, sohaapi.SystemIntegrationCreateRequest) (sohaapi.SystemIntegration, error)
	Update(context.Context, domainidentity.Principal, string, domain.UpdateInput) (sohaapi.SystemIntegration, error)
	Delete(context.Context, domainidentity.Principal, string) error
	Test(context.Context, domainidentity.Principal, string) (sohaapi.SystemIntegrationTestResult, error)
}

type SourceConnectionService interface {
	ListSourceConnections(context.Context, domainidentity.Principal) ([]sohaapi.SourceConnection, error)
	GetSourceConnection(context.Context, domainidentity.Principal, string) (sohaapi.SourceConnection, error)
	ListSourceRepositories(context.Context, domainidentity.Principal, string, string, string, int) ([]sohaapi.SourceRepository, string, error)
	ListSourceBranches(context.Context, domainidentity.Principal, string, string) ([]sohaapi.SourceBranch, error)
	ListSourceTags(context.Context, domainidentity.Principal, string, string) ([]sohaapi.SourceTag, error)
	GetSourceFile(context.Context, domainidentity.Principal, string, string, string, string) (sohaapi.SourceFile, error)
}

type SystemIntegrationHandler struct {
	integrations SystemIntegrationService
	sources      SourceConnectionService
}

func NewSystemIntegrationHandler(service interface {
	SystemIntegrationService
	SourceConnectionService
}) *SystemIntegrationHandler {
	return &SystemIntegrationHandler{integrations: service, sources: service}
}

func (h *SystemIntegrationHandler) List(c *gin.Context) {
	filter := domain.Filter{Category: c.Query("category"), ProviderType: c.Query("providerType")}
	if value, ok := c.GetQuery("enabled"); ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "enabled must be a boolean")
			return
		}
		filter.Enabled = &parsed
	}
	items, err := h.integrations.List(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), filter)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *SystemIntegrationHandler) Get(c *gin.Context) {
	item, err := h.integrations.Get(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("integrationID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *SystemIntegrationHandler) Create(c *gin.Context) {
	var request sohaapi.SystemIntegrationCreateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid system integration payload")
		return
	}
	item, err := h.integrations.Create(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), request)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

type systemIntegrationUpdateRequest struct {
	ExpectedVersion     int64                                          `json:"expectedVersion" binding:"required"`
	Name                *string                                        `json:"name"`
	Description         *string                                        `json:"description"`
	Enabled             *bool                                          `json:"enabled"`
	Configuration       *[]sohaapi.SystemIntegrationConfigurationField `json:"configuration"`
	Credentials         []sohaapi.SystemIntegrationCredentialInput     `json:"credentials"`
	ClearCredentialKeys []string                                       `json:"clearCredentialKeys"`
}

func (h *SystemIntegrationHandler) Update(c *gin.Context) {
	var request systemIntegrationUpdateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid system integration payload")
		return
	}
	item, err := h.integrations.Update(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("integrationID"), domain.UpdateInput{
		ExpectedVersion: request.ExpectedVersion, Name: request.Name, Description: request.Description, Enabled: request.Enabled,
		Configuration: request.Configuration, Credentials: request.Credentials, ClearCredentialKeys: request.ClearCredentialKeys,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *SystemIntegrationHandler) Delete(c *gin.Context) {
	if err := h.integrations.Delete(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("integrationID")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *SystemIntegrationHandler) Test(c *gin.Context) {
	item, err := h.integrations.Test(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("integrationID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *SystemIntegrationHandler) ListSources(c *gin.Context) {
	items, err := h.sources.ListSourceConnections(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *SystemIntegrationHandler) GetSource(c *gin.Context) {
	item, err := h.sources.GetSourceConnection(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("sourceConnectionID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *SystemIntegrationHandler) ListRepositories(c *gin.Context) {
	items, cursor, err := h.sources.ListSourceRepositories(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("sourceConnectionID"), c.Query("search"), c.Query("cursor"), parseLimit(c.Query("limit"), 50))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, sohaapi.SourceRepositoryListEnvelope{Items: items, NextCursor: cursor})
}

func (h *SystemIntegrationHandler) ListBranches(c *gin.Context) {
	items, err := h.sources.ListSourceBranches(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("sourceConnectionID"), c.Param("repositoryID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *SystemIntegrationHandler) ListTags(c *gin.Context) {
	items, err := h.sources.ListSourceTags(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("sourceConnectionID"), c.Param("repositoryID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *SystemIntegrationHandler) GetFile(c *gin.Context) {
	item, err := h.sources.GetSourceFile(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("sourceConnectionID"), c.Param("repositoryID"), c.Query("ref"), c.Query("path"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
