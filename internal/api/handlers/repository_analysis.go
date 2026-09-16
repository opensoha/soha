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

type repositoryAnalysisService interface {
	AnalyzeRepository(context.Context, domainidentity.Principal, string, sohaapi.RepositoryAnalysisInput) (sohaapi.RepositoryAnalysis, error)
}

func (h *ApplicationHandler) AnalyzeRepository(c *gin.Context) {
	var input sohaapi.RepositoryAnalysisInput
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid repository analysis input")
		return
	}
	service, ok := h.applications.(repositoryAnalysisService)
	if !ok {
		apiresponse.Error(c, http.StatusServiceUnavailable, "unavailable", "repository analysis is unavailable")
		return
	}
	if err := clearResponseWriteDeadline(c); err != nil {
		writeError(c, err)
		return
	}
	result, err := service.AnalyzeRepository(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("applicationID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, result)
}
