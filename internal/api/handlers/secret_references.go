package handlers

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	middleware "github.com/opensoha/soha/internal/api/middleware"
	response "github.com/opensoha/soha/internal/api/response"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"net/http"
)

func (h *SecretHandler) ListReferences(c *gin.Context) {
	service, ok := h.management.(interface {
		ListReferences(context.Context, identity.Principal, string) ([]sohaapi.SecretReferenceUsage, error)
	})
	if !ok {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	items, err := service.ListReferences(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("secretID"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Items(c, http.StatusOK, items)
}
