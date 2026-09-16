package providerportal

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (h *providerHandler) GetIdentityProviderSetup(c *gin.Context) {
	if h.service == nil {
		writeError(c, fmt.Errorf("%w: identity provider service is not configured", apperrors.ErrUnsupportedOperation))
		return
	}
	publicURL := ""
	if h.accessURL != nil {
		publicURL = h.accessURL.AccessURL()
	}
	setup, err := h.service.GetProviderSetup(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("providerID"), publicURL)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, setup)
}

func (h *providerHandler) GetIdentityProviderUserMetadata(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h.service == nil {
		writeError(c, fmt.Errorf("%w: identity provider service is not configured", apperrors.ErrUnsupportedOperation))
		return
	}
	metadata, err := h.service.GetProviderUserMetadata(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("providerID"), c.Param("userID"), c.Query("clientId"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, metadata)
}
