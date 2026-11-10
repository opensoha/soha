package handlers

import (
	"context"
	"net/http"
	"reflect"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type DockerDeploymentRequests interface {
	GetProjectDeploymentRequest(context.Context, domainidentity.Principal, string, string, string) (domaindocker.Operation, error)
}

func dockerDeploymentRequestsMissing(value DockerDeploymentRequests) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func (h *DockerHandler) GetProjectDeploymentRequest(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h.deploymentRequests == nil {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	item, err := h.deploymentRequests.GetProjectDeploymentRequest(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), c.Param("requestKey"), c.Query("action"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
