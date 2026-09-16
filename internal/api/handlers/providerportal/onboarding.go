package providerportal

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	appproviderportal "github.com/opensoha/soha/internal/application/providerportal"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type ApplicationOnboardingService interface {
	OnboardApplication(context.Context, domainidentity.Principal, appproviderportal.OnboardingInput) (appproviderportal.OnboardingResult, error)
}

type applicationOnboardingHandler struct{ service ApplicationOnboardingService }

func (h *applicationOnboardingHandler) OnboardIdentityApplication(c *gin.Context) {
	if h.service == nil {
		writeError(c, fmt.Errorf("%w: application onboarding is not configured", apperrors.ErrUnsupportedOperation))
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	var input appproviderportal.OnboardingInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, fmt.Errorf("%w: invalid application onboarding input", apperrors.ErrInvalidArgument))
		return
	}
	created, err := h.service.OnboardApplication(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input)
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	apiresponse.Item(c, http.StatusCreated, created)
}
