package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type ObservabilityDataSources interface {
	ListDataSources(context.Context, domainidentity.Principal) ([]sohaapi.ObservabilityDataSource, error)
	CreateDataSource(context.Context, domainidentity.Principal, sohaapi.ObservabilityDataSourceInput) (sohaapi.ObservabilityDataSource, error)
	UpdateDataSource(context.Context, domainidentity.Principal, string, sohaapi.ObservabilityDataSourceInput) (sohaapi.ObservabilityDataSource, error)
	ValidateDataSource(context.Context, domainidentity.Principal, string) (sohaapi.ObservabilityDataSource, error)
}

type ObservabilityLogCollection interface {
	GetLogCollection(context.Context, domainidentity.Principal, string) (sohaapi.LogCollectionState, error)
	PreflightLogCollection(context.Context, domainidentity.Principal, string, sohaapi.LogCollectionPreflightInput) (sohaapi.LogCollectionPlan, error)
	EnableLogCollection(context.Context, domainidentity.Principal, string, sohaapi.LogCollectionEnableInput) (sohaapi.LogCollectionState, error)
	DisableLogCollection(context.Context, domainidentity.Principal, string, sohaapi.LogCollectionDisableInput) (sohaapi.LogCollectionState, error)
}

type ObservabilityHandler struct {
	dataSources ObservabilityDataSources
	collection  ObservabilityLogCollection
}

func NewObservabilityHandler(dataSources ObservabilityDataSources) *ObservabilityHandler {
	collection, _ := dataSources.(ObservabilityLogCollection)
	return &ObservabilityHandler{dataSources: dataSources, collection: collection}
}

func (h *ObservabilityHandler) GetLogCollection(c *gin.Context) {
	item, err := h.collection.GetLogCollection(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *ObservabilityHandler) PreflightLogCollection(c *gin.Context) {
	var input sohaapi.LogCollectionPreflightInput
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid log collection preflight payload")
		return
	}
	item, err := h.collection.PreflightLogCollection(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("clusterID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *ObservabilityHandler) EnableLogCollection(c *gin.Context) {
	var input sohaapi.LogCollectionEnableInput
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid log collection confirmation payload")
		return
	}
	item, err := h.collection.EnableLogCollection(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("clusterID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *ObservabilityHandler) DisableLogCollection(c *gin.Context) {
	var input sohaapi.LogCollectionDisableInput
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid log collection disable payload")
		return
	}
	item, err := h.collection.DisableLogCollection(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("clusterID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *ObservabilityHandler) ListDataSources(c *gin.Context) {
	items, err := h.dataSources.ListDataSources(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *ObservabilityHandler) CreateDataSource(c *gin.Context) {
	input, ok := bindObservabilityDataSourceInput(c)
	if !ok {
		return
	}
	item, err := h.dataSources.CreateDataSource(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *ObservabilityHandler) UpdateDataSource(c *gin.Context) {
	input, ok := bindObservabilityDataSourceInput(c)
	if !ok {
		return
	}
	item, err := h.dataSources.UpdateDataSource(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("dataSourceID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *ObservabilityHandler) ValidateDataSource(c *gin.Context) {
	item, err := h.dataSources.ValidateDataSource(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("dataSourceID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func bindObservabilityDataSourceInput(c *gin.Context) (sohaapi.ObservabilityDataSourceInput, bool) {
	var input sohaapi.ObservabilityDataSourceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid observability data source payload")
		return sohaapi.ObservabilityDataSourceInput{}, false
	}
	return input, true
}
