package handlers

import (
	"context"
	"net/http"
	"reflect"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type DockerIdentityService interface {
	GetHostIdentity(context.Context, domainidentity.Principal, string) (sohaapi.DockerResourceIdentity, error)
	GetProjectIdentity(context.Context, domainidentity.Principal, string) (sohaapi.DockerResourceIdentity, error)
}

func dockerIdentityServiceMissing(service DockerIdentityService) bool {
	if service == nil {
		return true
	}
	value := reflect.ValueOf(service)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func (h *DockerHandler) GetHostIdentity(c *gin.Context) {
	if h.identities == nil {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	item, err := h.identities.GetHostIdentity(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DockerHandler) GetProjectIdentity(c *gin.Context) {
	if h.identities == nil {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	item, err := h.identities.GetProjectIdentity(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
