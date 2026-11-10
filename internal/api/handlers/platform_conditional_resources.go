package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	contractresource "github.com/opensoha/soha-contracts/resource"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type conditionalResourceService interface {
	GetResourceIdentity(context.Context, domainidentity.Principal, string, string, string, string) (contractresource.ResourceIdentity, error)
	PlanResourceYAMLConditional(context.Context, domainidentity.Principal, string, string, string, string, contractresource.ConditionalResourceYAMLInput) (domainoperation.Plan, error)
	ApplyResourceYAMLConditional(context.Context, domainidentity.Principal, string, string, string, string, contractresource.ConditionalResourceYAMLInput) (domainresource.ResourceYAMLView, error)
	DeleteResourceConditional(context.Context, domainidentity.Principal, string, string, string, string, contractresource.ResourceMutationPreconditions) (contractresource.ResourceDeletionReceipt, error)
}

func decodeConditionalYAML(c *gin.Context, input *contractresource.ConditionalResourceYAMLInput) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 5<<20)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid conditional YAML payload")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "one conditional YAML payload is required")
		return false
	}
	return true
}

func (h *genericResourceHandler) conditionalResources(c *gin.Context) (conditionalResourceService, bool) {
	service, ok := h.service.(conditionalResourceService)
	if !ok {
		writeError(c, apperrors.ErrUnsupportedOperation)
	}
	return service, ok
}

func (h *genericResourceHandler) registerConditionalResourceRoutes(group gin.IRoutes) {
	h.registerConditionalWorkloadActionRoute(group)
	const path = "/clusters/:clusterID/resources/:kind/:name"
	group.GET(path+"/identity", func(c *gin.Context) {
		service, ok := h.conditionalResources(c)
		if !ok {
			return
		}
		item, err := service.GetResourceIdentity(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("clusterID"), c.Query("namespace"), c.Param("kind"), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		apiresponse.Item(c, http.StatusOK, item)
	})
	group.POST(path+"/conditional/yaml/preflight", func(c *gin.Context) {
		service, ok := h.conditionalResources(c)
		if !ok {
			return
		}
		var input contractresource.ConditionalResourceYAMLInput
		if !decodeConditionalYAML(c, &input) {
			return
		}
		item, err := service.PlanResourceYAMLConditional(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("clusterID"), c.Query("namespace"), c.Param("kind"), c.Param("name"), input)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	group.PUT(path+"/conditional/yaml", func(c *gin.Context) {
		service, ok := h.conditionalResources(c)
		if !ok {
			return
		}
		var input contractresource.ConditionalResourceYAMLInput
		if !decodeConditionalYAML(c, &input) {
			return
		}
		item, err := service.ApplyResourceYAMLConditional(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("clusterID"), c.Query("namespace"), c.Param("kind"), c.Param("name"), input)
		if err != nil {
			writeError(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		apiresponse.Item(c, http.StatusOK, item)
	})
	group.DELETE(path+"/conditional", func(c *gin.Context) {
		service, ok := h.conditionalResources(c)
		if !ok {
			return
		}
		expected := contractresource.ResourceMutationPreconditions{ExpectedUID: c.Query("expectedUid"), ExpectedResourceVersion: c.Query("expectedResourceVersion")}
		item, err := service.DeleteResourceConditional(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("clusterID"), c.Query("namespace"), c.Param("kind"), c.Param("name"), expected)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusAccepted, item)
	})
}
